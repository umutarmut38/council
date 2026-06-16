package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/umutarmut38/council/internal/config"
)

func runConfigCommand(sub string, args []string) error {
	switch sub {
	case "init":
		interactive := false
		rest := args[:0]
		for _, a := range args {
			if a == "--interactive" || a == "-i" {
				interactive = true
				continue
			}
			rest = append(rest, a)
		}
		if interactive {
			return configWizard()
		}
		return initConfig(rest)
	case "wizard":
		return configWizard()
	case "add-agent":
		return configAddAgent(args)
	case "schema":
		return runConfigSchema(args)
	default:
		return fmt.Errorf("unknown config command %q (init | wizard | add-agent | schema)", sub)
	}
}

// runConfigSchema prints the configuration reference. By default it emits the
// Markdown tables generated into docs/configuration.md (so it can be piped or
// diffed); --json emits a machine-readable JSON Schema (draft 2020-12) for
// editor integrations.
func runConfigSchema(args []string) error {
	flags := flag.NewFlagSet("config schema", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	asJSON := flags.Bool("json", false, "emit a JSON Schema (draft 2020-12) instead of Markdown")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *asJSON {
		fmt.Println(config.SchemaJSONString())
		return nil
	}
	fmt.Println("# Configuration schema")
	fmt.Println()
	fmt.Println(config.SchemaMarkdown())
	return nil
}

func initConfig(args []string) error {
	flags := flag.NewFlagSet("config init", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	force := flags.Bool("force", false, "overwrite an existing config")
	if err := flags.Parse(args); err != nil {
		return err
	}

	cfgPath, err := config.DefaultPath()
	if err != nil {
		return err
	}
	if err := config.WriteDefault(cfgPath, *force); err != nil {
		return err
	}
	fmt.Printf("Wrote %s (all agents disabled; enable the ones you use, or run `council config wizard`)\n", cfgPath)
	return nil
}

// configWizard walks first-time setup: detect installed agent CLIs, choose
// which to enable and their roles, optionally opt into auto-approval phase
// commands, detect the project stack for review.check_command, and write the
// global config.
func configWizard() error {
	if !stdinIsTerminal() {
		return errors.New("config wizard needs an interactive terminal")
	}
	in := bufio.NewReader(os.Stdin)
	askYesNo := func(prompt string, def bool) bool {
		suffix := "[Y/n]"
		if !def {
			suffix = "[y/N]"
		}
		fmt.Printf("%s %s ", prompt, suffix)
		line, _ := in.ReadString('\n')
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "" {
			return def
		}
		return line == "y" || line == "yes"
	}
	ask := func(prompt, def string) string {
		if def != "" {
			fmt.Printf("%s [%s] ", prompt, def)
		} else {
			fmt.Printf("%s ", prompt)
		}
		line, _ := in.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return def
		}
		return line
	}

	fmt.Println("council setup — detecting agent CLIs on your PATH…")
	cfg := config.Default()

	enabledCount := 0
	for _, name := range config.PresetNames() {
		preset, _ := config.AgentPreset(name)
		binary := preset.Command[0]
		path, lookErr := exec.LookPath(binary)
		if lookErr != nil {
			fmt.Printf("  - %s: %s not found, skipping\n", name, binary)
			continue
		}
		if !askYesNo(fmt.Sprintf("  - %s (%s): enable?", name, path), true) {
			continue
		}
		preset.Enabled = true
		role := ask(fmt.Sprintf("    role for %s (worker = plans+builds, reviewer = votes+reviews, both)", name), "both")
		switch strings.ToLower(role) {
		case "worker":
			preset.Role = []string{config.RoleWorker}
		case "reviewer":
			preset.Role = []string{config.RoleReviewer}
		}
		if auto := config.PresetAutoApproveCommand(name); len(auto) > 0 {
			fmt.Printf("    %s can run orchestration phases unattended with: %s\n", name, strings.Join(auto, " "))
			fmt.Println("    WARNING: that flag bypasses the tool's own permission prompts.")
			if askYesNo("    enable auto-approval for plan/vote/build phases?", false) {
				preset.Orchestration.PlanCommand = auto
				preset.Orchestration.VoteCommand = auto
				if !preset.Orchestration.ExcludeBuild {
					preset.Orchestration.BuildCommand = auto
				}
			}
		}
		cfg.Agents[name] = preset
		enabledCount++
	}
	if enabledCount == 0 {
		fmt.Println("No agents enabled; you can rerun `council config wizard` any time.")
	}

	// Stack detection for the review gate.
	if stack, cmd := detectStack("."); stack != "" {
		if askYesNo(fmt.Sprintf("Detected a %s project. Gate build review with `%s`?", stack, strings.Join(cmd, " ")), true) {
			cfg.Review.CheckCommand = cmd
		}
	}

	mode := ask("policy mode (safe = refuse auto-approval flags, normal, aggressive = skip confirmations)", "normal")
	cfg.Policy.Mode = strings.ToLower(strings.TrimSpace(mode))

	cfgPath, err := config.DefaultPath()
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(cfgPath); statErr == nil {
		if !askYesNo(fmt.Sprintf("%s exists. Overwrite?", cfgPath), false) {
			return errors.New("aborted; existing config kept")
		}
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		return err
	}
	fmt.Printf("Wrote %s — run `council doctor` to verify, then `council` to start.\n", cfgPath)
	return nil
}

// configAddAgent adds a known preset to the global config:
// council config add-agent codex [--name codex-worker] [--role worker|reviewer]
func configAddAgent(args []string) error {
	fs := flag.NewFlagSet("config add-agent", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	name := fs.String("name", "", "agent name in the config (defaults to the preset name)")
	role := fs.String("role", "", "worker | reviewer (empty = both)")
	// Accept the preset before the flags (`add-agent codex --role worker`);
	// the flag package stops at the first positional argument otherwise.
	presetName := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		presetName = args[0]
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if presetName == "" && fs.NArg() == 1 {
		presetName = fs.Arg(0)
	} else if fs.NArg() > 0 {
		presetName = ""
	}
	if presetName == "" {
		return fmt.Errorf("usage: council config add-agent <%s> [--name x] [--role worker|reviewer]", strings.Join(config.PresetNames(), "|"))
	}
	preset, okPreset := config.AgentPreset(presetName)
	if !okPreset {
		return fmt.Errorf("unknown preset %q; known: %s", presetName, strings.Join(config.PresetNames(), ", "))
	}
	preset.Enabled = true
	switch strings.ToLower(strings.TrimSpace(*role)) {
	case "":
	case "worker":
		preset.Role = []string{config.RoleWorker}
	case "reviewer":
		preset.Role = []string{config.RoleReviewer}
	default:
		return fmt.Errorf("unknown role %q (worker | reviewer)", *role)
	}

	agentName := strings.TrimSpace(*name)
	if agentName == "" {
		agentName = presetName
	}

	cfgPath, err := config.DefaultPath()
	if err != nil {
		return err
	}
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		cfg = config.Default()
	}
	if existing, exists := cfg.Agents[agentName]; exists && existing.Enabled {
		return fmt.Errorf("agent %q already exists and is enabled in %s", agentName, cfgPath)
	}
	cfg.Agents[agentName] = preset
	if err := config.ValidateAgentNames(cfg); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		return err
	}
	fmt.Printf("Added %s (preset %s) to %s.\nNote: rewriting the config drops YAML comments.\n", agentName, presetName, cfgPath)
	return nil
}
