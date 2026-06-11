package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/fsperm"
	runstore "github.com/umutarmut38/council/internal/session"
)

// configSources records where the effective config came from, so each run can
// save an accurate provenance trail next to its artifacts.
type configSources struct {
	GlobalPath string
	GlobalRaw  []byte
	LocalPath  string // applied local config ("" if none)
	LocalRaw   []byte
	LocalSkip  string // local config that was found but not applied, with reason
}

// loadEffectiveConfig loads the global config and overlays a repo-local
// .council.yaml only when it is trusted (or the user trusts it interactively).
// A repo-local config can change which commands council executes, so an
// untrusted or changed file is never applied silently.
func loadEffectiveConfig(noLocal bool) (config.Config, configSources, error) {
	var sources configSources

	cfgPath, err := config.DefaultPath()
	if err != nil {
		return config.Config{}, sources, err
	}
	cfg, rawGlobal, err := config.Load(cfgPath)
	if err != nil {
		return config.Config{}, sources, err
	}
	sources.GlobalPath = cfgPath
	sources.GlobalRaw = rawGlobal

	if noLocal {
		return cfg, sources, nil
	}
	localPath := config.FindLocalConfig()
	if localPath == "" {
		return cfg, sources, nil
	}
	rawLocal, err := os.ReadFile(localPath)
	if err != nil {
		return cfg, sources, err
	}

	switch config.LocalConfigTrust(localPath, rawLocal) {
	case config.Trusted:
	case config.TrustChanged:
		if !confirmTrust(localPath, rawLocal, "has CHANGED since you last trusted it") {
			sources.LocalSkip = localPath
			return cfg, sources, nil
		}
	default: // TrustUnknown
		if !confirmTrust(localPath, rawLocal, "is not trusted yet") {
			sources.LocalSkip = localPath
			return cfg, sources, nil
		}
	}

	merged, err := config.ApplyLocalOverride(cfg, rawLocal)
	if err != nil {
		return cfg, sources, fmt.Errorf("%s: %w", localPath, err)
	}
	merged.Normalize()
	sources.LocalPath = localPath
	sources.LocalRaw = rawLocal
	fmt.Fprintf(os.Stderr, "Using repo config %s\n", localPath)
	return merged, sources, nil
}

// applyRuntimeConfig wires config-driven runtime behavior (artifact privacy,
// transcript redaction) and enforces policy.mode: safe — enabled agents must
// not carry auto-approval flags in safe mode.
func applyRuntimeConfig(cfg config.Config) error {
	fsperm.SetPrivate(cfg.Sessions.IsPrivate())
	runstore.SetRedact(cfg.Sessions.Redact)

	if !cfg.Policy.IsSafe() {
		return nil
	}
	var violations []string
	for name, agentCfg := range cfg.Agents {
		if !agentCfg.Enabled {
			continue
		}
		for where, flags := range config.AgentRiskyFlags(agentCfg) {
			violations = append(violations, fmt.Sprintf("%s %s: %s", name, where, strings.Join(flags, " ")))
		}
	}
	if len(violations) > 0 {
		return fmt.Errorf("policy.mode is \"safe\" but the config carries auto-approval flags:\n  %s\nRemove the flags or relax policy.mode", strings.Join(violations, "\n  "))
	}
	return nil
}

// effectiveYAML marshals the normalized, merged config a run actually used.
func effectiveYAML(cfg config.Config) []byte {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil
	}
	return data
}

// JSON renders the provenance trail (paths + content hashes) for a run.
func (s configSources) JSON() []byte {
	hash := func(raw []byte) string {
		if len(raw) == 0 {
			return ""
		}
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	payload := struct {
		GlobalPath   string `json:"global_path,omitempty"`
		GlobalSHA256 string `json:"global_sha256,omitempty"`
		LocalPath    string `json:"local_path,omitempty"`
		LocalSHA256  string `json:"local_sha256,omitempty"`
		LocalSkipped string `json:"local_skipped,omitempty"`
	}{s.GlobalPath, hash(s.GlobalRaw), s.LocalPath, hash(s.LocalRaw), s.LocalSkip}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil
	}
	return data
}

// confirmTrust asks the user whether to trust a repo-local config. In
// non-interactive contexts it declines, pointing at `council trust`.
func confirmTrust(path string, raw []byte, reason string) bool {
	if !stdinIsTerminal() {
		fmt.Fprintf(os.Stderr, "Skipping repo config %s: it %s.\nRun `council trust` in this repo to trust it, or pass --no-local-config to silence this.\n", path, reason)
		return false
	}
	fmt.Fprintf(os.Stderr, "Repo config %s %s.\nIt can change which commands council runs (including auto-approval flags).\nTrust and apply it? [y/N] ", path, reason)
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		fmt.Fprintln(os.Stderr, "Skipped repo config. Run `council trust` later to trust it.")
		return false
	}
	if err := config.TrustLocalConfig(path, raw); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not record trust: %v\n", err)
	}
	return true
}

func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// councilTrust implements `council trust [--revoke] [--show]`.
func councilTrust(args []string) error {
	revoke := false
	show := false
	for _, a := range args {
		switch a {
		case "--revoke":
			revoke = true
		case "--show":
			show = true
		default:
			return fmt.Errorf("unknown flag %q (usage: council trust [--revoke] [--show])", a)
		}
	}

	path := config.FindLocalConfig()
	if path == "" {
		return errors.New("no repo-local .council.yaml found")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if revoke {
		if err := config.RevokeLocalConfigTrust(path); err != nil {
			return err
		}
		fmt.Printf("Revoked trust for %s\n", path)
		return nil
	}
	if show {
		switch config.LocalConfigTrust(path, raw) {
		case config.Trusted:
			fmt.Printf("%s: trusted\n", path)
		case config.TrustChanged:
			fmt.Printf("%s: trusted before, but content changed (run `council trust` to re-trust)\n", path)
		default:
			fmt.Printf("%s: not trusted\n", path)
		}
		return nil
	}
	if err := config.TrustLocalConfig(path, raw); err != nil {
		return err
	}
	fmt.Printf("Trusted %s\n", path)
	return nil
}
