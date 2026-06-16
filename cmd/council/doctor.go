package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/orchestrate"
)

// doctor checks the whole setup: config validity (global and local), enabled
// agent commands, role coverage, git availability, run/worktree directories,
// stale worktrees, the review check command, risky flags, and terminal
// settings. Problems make it exit non-zero; warnings don't.
//
// The default run is read-only. `council doctor --fix` additionally performs
// safe, reversible, local repairs: write a default global config when missing,
// tighten loosened artifact permissions back to owner-only, and set
// review.check_command from a detected stack when it is empty.
func doctor(args []string) error {
	fs := flag.NewFlagSet("council doctor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	noLocalFlag := fs.Bool("no-local-config", false, "ignore repo-local .council.yaml")
	fix := fs.Bool("fix", false, "apply safe, local, reversible fixes (default is read-only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	noLocal := *noLocalFlag

	problems := 0
	warn := func(format string, v ...any) { fmt.Printf("warn %s\n", fmt.Sprintf(format, v...)) }
	fail := func(format string, v ...any) { fmt.Printf("FAIL %s\n", fmt.Sprintf(format, v...)); problems++ }
	ok := func(format string, v ...any) { fmt.Printf("ok   %s\n", fmt.Sprintf(format, v...)) }
	fixed := func(format string, v ...any) { fmt.Printf("fix  %s\n", fmt.Sprintf(format, v...)) }

	// Global config.
	cfgPath, err := config.DefaultPath()
	if err != nil {
		return err
	}
	cfg, _, err := config.Load(cfgPath)
	if errors.Is(err, os.ErrNotExist) && *fix {
		if werr := config.WriteDefault(cfgPath, false); werr != nil {
			fail("could not write default config %s: %v", cfgPath, werr)
			return errors.New("doctor found problems")
		}
		fixed("wrote default config %s (all agents disabled; enable some or run `council config wizard`)", cfgPath)
		cfg, _, err = config.Load(cfgPath)
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fail("global config %s missing — run `council config init` (or `council doctor --fix`)", cfgPath)
		} else {
			fail("global config %s: %v", cfgPath, err)
		}
		return errors.New("doctor found problems")
	}
	ok("global config %s parses", cfgPath)

	// Local config and trust.
	if noLocal {
		ok("repo-local config ignored (--no-local-config)")
	} else if localPath := config.FindLocalConfig(); localPath != "" {
		raw, rerr := os.ReadFile(localPath)
		if rerr != nil {
			fail("repo config %s: %v", localPath, rerr)
		} else {
			merged, merr := config.ApplyLocalOverride(cfg, raw)
			switch {
			case merr != nil:
				fail("repo config %s: %v", localPath, merr)
			case config.LocalConfigTrust(localPath, raw) == config.Trusted:
				merged.Normalize()
				cfg = merged
				ok("repo config %s applied (trusted)", localPath)
			case config.LocalConfigTrust(localPath, raw) == config.TrustChanged:
				warn("repo config %s CHANGED since trusted — not applied; run `council trust`", localPath)
			default:
				warn("repo config %s found but not trusted — not applied; run `council trust`", localPath)
			}
		}
	} else {
		ok("no repo-local config")
	}

	if err := cfg.Validate(); err != nil {
		fail("config: %v", err)
	}

	// Layout sanity.
	switch strings.ToLower(strings.TrimSpace(cfg.UI.Layout)) {
	case "", "grid", "paged-grid":
	default:
		warn("ui.layout %q is unknown (council uses a paged grid; use \"grid\")", cfg.UI.Layout)
	}

	// Enabled agents: command presence, terminal sanity, risky flags, roles.
	selected, warnings, err := config.SelectAgents(cfg, nil)
	if err != nil {
		fail("%v", err)
	}
	for _, w := range warnings {
		warn("%s", strings.TrimPrefix(w, "warning: "))
	}
	if len(selected) == 0 {
		warn("no enabled agents — enable some in %s or run `council config wizard`", cfgPath)
	}

	haveWorker, haveReviewer, anyRisky := false, false, false
	for _, spec := range selected {
		if spec.Config.ParticipatesIn(config.PhasePlan) {
			haveWorker = true
		}
		if spec.Config.ParticipatesIn(config.PhaseVote) {
			haveReviewer = true
		}
		binary := spec.Config.Command[0]
		if path, lookErr := exec.LookPath(binary); lookErr != nil {
			fail("%s: %s not found in PATH", spec.Name, binary)
		} else {
			ok("%s: %s", spec.Name, path)
		}
		// Env this agent gets beyond the global set (new keys or overrides) —
		// the per-agent routing that doesn't show up in the global env line.
		if extra := agentExtraEnvKeys(cfg.Env, spec.Config.Env); len(extra) > 0 {
			ok("%s env: %s", spec.Name, strings.Join(extra, ", "))
		}
		for _, phase := range []config.Phase{config.PhasePlan, config.PhaseVote, config.PhaseBuild} {
			phaseCmd := spec.Config.CommandForPhase(phase)
			if len(phaseCmd) > 0 && phaseCmd[0] != binary {
				if _, lookErr := exec.LookPath(phaseCmd[0]); lookErr != nil {
					fail("%s: %s command %q not found in PATH", spec.Name, phase, phaseCmd[0])
				}
			}
		}
		for where, flags := range config.AgentRiskyFlags(spec.Config) {
			anyRisky = true
			warn("%s %s carries auto-approval flag(s): %s", spec.Name, where, strings.Join(flags, " "))
		}
		term := spec.Config.Terminal
		switch term.SendMode {
		case "", "type", "paste":
		default:
			warn("%s: unknown send_mode %q (use type|paste)", spec.Name, term.SendMode)
		}
		switch term.SubmitSequence {
		case "", "cr", "lf", "crlf", "csi-enter", "none":
		default:
			warn("%s: unusual submit_sequence %q", spec.Name, term.SubmitSequence)
		}
		if term.SubmitDelayMs < 0 || term.SubmitDelayMs > 10000 {
			warn("%s: submit_delay_ms %d looks wrong", spec.Name, term.SubmitDelayMs)
		}
	}
	if len(selected) > 0 {
		if !haveWorker {
			warn("no agent covers the worker role — /plan and /build will have no participants")
		}
		if !haveReviewer {
			warn("no agent covers the reviewer role — /vote and /review will have no participants")
		}
	}

	// Policy.
	if mode := cfg.Policy.Normalized(); mode != config.PolicyNormal {
		ok("policy.mode: %s", mode)
	}

	// Git repository for orchestration.
	cwd, _ := os.Getwd()
	repoRoot, gerr := orchestrate.DetectRepoRoot(cwd)
	inRepo := gerr == nil
	if gerr != nil {
		warn("not inside a git repository — orchestration (plan/vote/build) is unavailable here")
	} else {
		ok("git repository: %s", repoRoot)

		// Writable run/worktree directories.
		for _, dir := range []string{cfg.Sessions.RootDir, filepath.Join(repoRoot, ".council", "worktrees")} {
			if dirWritable(dir) {
				ok("writable: %s", dir)
			} else {
				fail("not writable: %s", dir)
			}
		}

		// Stale worktrees.
		mgr := orchestrate.NewManager(repoRoot, "")
		if worktrees, lerr := mgr.List(); lerr == nil && len(worktrees) > 0 {
			names := make([]string, 0, len(worktrees))
			for _, wt := range worktrees {
				names = append(names, wt.Agent)
			}
			warn("%d council worktree(s) on disk (%s) — `council clean` removes them", len(worktrees), strings.Join(names, ", "))
		}
	}

	// Review check command.
	if len(cfg.Review.CheckCommand) == 0 {
		warn("review.check_command is empty — builds are not gated; `council stack detect` can set it")
	} else if _, lookErr := exec.LookPath(cfg.Review.CheckCommand[0]); lookErr != nil {
		fail("review.check_command %q not found in PATH", cfg.Review.CheckCommand[0])
	} else {
		ok("review.check_command: %s", strings.Join(cfg.Review.CheckCommand, " "))
	}

	// Pre-launch env and setup commands.
	if len(cfg.Env) > 0 {
		keys := make([]string, 0, len(cfg.Env))
		for k := range cfg.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		ok("env exported to all agents: %s", strings.Join(keys, ", "))
	}
	for _, sc := range cfg.Setup {
		if len(sc.Command) == 0 {
			warn("setup entry has no command")
			continue
		}
		if _, lookErr := exec.LookPath(sc.Command[0]); lookErr != nil {
			fail("setup %q: %s not found in PATH", sc.Label(), sc.Command[0])
		} else {
			ok("setup: %s", setupSummary(sc))
		}
	}

	printColorDiagnostics()

	// Safe, local, reversible repairs (only with --fix). The default run above
	// is read-only.
	if *fix {
		if cfg.Sessions.IsPrivate() {
			if n, ferr := tightenArtifactPerms(cfg.Sessions.RootDir); ferr != nil {
				warn("could not tighten %s: %v", cfg.Sessions.RootDir, ferr)
			} else if n > 0 {
				fixed("tightened %d artifact path(s) under %s to owner-only (0700/0600)", n, cfg.Sessions.RootDir)
			}
		}
		if len(cfg.Review.CheckCommand) == 0 && inRepo {
			if stack, cmd := detectStack(cwd); stack != "" {
				if werr := writeStackToLocalConfig(cmd); werr != nil {
					warn("could not set review.check_command: %v", werr)
				} else {
					// Reflect the fix so the guidance below doesn't re-suggest it.
					cfg.Review.CheckCommand = cmd
				}
			}
		}
	}

	printDoctorGuidance(cfg, cwd, inRepo, len(selected), anyRisky)

	if problems > 0 {
		return fmt.Errorf("doctor found %d problem(s)", problems)
	}
	return nil
}

// printDoctorGuidance prints the prescriptive half of doctor: a roster of known
// agent CLIs with install/enable hints, an explanation of any risky flags,
// recommended config snippets, and a single best next action. It is read-only.
func printDoctorGuidance(cfg config.Config, cwd string, inRepo bool, enabledCount int, anyRisky bool) {
	fmt.Println("agents — known CLIs:")
	for _, name := range config.PresetNames() {
		preset, _ := config.AgentPreset(name)
		bin := preset.Command[0]
		enabled := cfg.Agents[name].Enabled
		if path, lookErr := exec.LookPath(bin); lookErr == nil {
			if enabled {
				fmt.Printf("  %-9s installed, enabled (%s)\n", name, path)
			} else {
				fmt.Printf("  %-9s installed — enable with `council config add-agent %s`\n", name, name)
			}
			continue
		}
		if hint := config.PresetInstallHint(name); hint != "" {
			fmt.Printf("  %-9s not found — install: %s\n", name, hint)
		} else {
			fmt.Printf("  %-9s not found on PATH\n", name)
		}
	}

	if anyRisky {
		fmt.Println("note: auto-approval flags run phases unattended but bypass each tool's own")
		fmt.Println("      permission prompts — keep them only in repos you trust. `policy.mode: safe`")
		fmt.Println("      refuses them, `normal` warns, `aggressive` allows them.")
	}

	if enabledCount == 0 {
		fmt.Println("suggested: enable an agent (then `council` can launch it):")
		fmt.Println("  agents:")
		fmt.Println("    claude: { enabled: true }")
		fmt.Println("  # or run `council config wizard` for guided setup")
	}
	if len(cfg.Review.CheckCommand) == 0 {
		if stack, cmd := detectStack(cwd); stack != "" {
			fmt.Printf("suggested: gate builds with the detected %s stack (`council stack detect`, or `council doctor --fix`):\n", stack)
			fmt.Printf("  review:\n    check_command: [%s]\n", strings.Join(cmd, ", "))
		}
	}

	fmt.Printf("next: %s\n", nextAction(cfg, inRepo, enabledCount))
}

// nextAction picks the single most useful next step from the diagnosed state.
func nextAction(cfg config.Config, inRepo bool, enabledCount int) string {
	switch {
	case enabledCount == 0:
		return "run `council config wizard` to enable the agent CLIs you use"
	case !inRepo:
		return "cd into a git repository, then `council plan \"<issue>\"` to run the council"
	case len(cfg.Review.CheckCommand) == 0:
		return "set a review gate with `council stack detect`, then `council` to start"
	default:
		return "run `council` to start, or `council plan \"<issue>\"` to run the council"
	}
}

// tightenArtifactPerms walks rootDir and resets any directory to 0700 and any
// file to 0600 when it carries broader-than-owner permission bits. It only ever
// removes access (safe and reversible) and skips unreadable entries. It returns
// the number of paths changed. A missing rootDir is not an error (nothing to do).
func tightenArtifactPerms(rootDir string) (int, error) {
	if rootDir == "" {
		rootDir = ".council/runs"
	}
	if _, err := os.Stat(rootDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	fixed := 0
	err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable entries; never abort the walk
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		want := os.FileMode(0o600)
		if d.IsDir() {
			want = 0o700
		}
		// Any bit set outside the owner-only mask means the path is too open.
		if info.Mode().Perm()&^want != 0 {
			if os.Chmod(path, want) == nil {
				fixed++
			}
		}
		return nil
	})
	return fixed, err
}

// agentExtraEnvKeys returns the sorted env keys an agent receives that aren't
// already covered by the global env with the same value — i.e. the per-agent
// additions and overrides. global may be nil.
func agentExtraEnvKeys(global, agent map[string]string) []string {
	var keys []string
	for k, v := range agent {
		if gv, ok := global[k]; !ok || gv != v {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// printColorDiagnostics shows what this terminal advertises and how it
// renders the sequences council uses — the fastest way to explain "the
// colors work in terminal A but not terminal B". Text and glyph rows are
// separate on purpose: VS Code draws box/block glyphs itself ("custom
// glyphs" on the GPU), and that path can drop colors that plain text
// renders fine.
func printColorDiagnostics() {
	fmt.Printf("term TERM=%q COLORTERM=%q TERM_PROGRAM=%q\n",
		os.Getenv("TERM"), os.Getenv("COLORTERM"), os.Getenv("TERM_PROGRAM"))
	text, glyphs, borders := "", "", ""
	for _, c := range []struct {
		idx  int
		name string
	}{
		{81, "blue"}, {114, "green"}, {203, "red"}, {212, "pink"},
	} {
		text += fmt.Sprintf("\x1b[38;5;%dm%s\x1b[0m ", c.idx, c.name)
		glyphs += fmt.Sprintf("\x1b[38;5;%dm███\x1b[0m ", c.idx)
		borders += fmt.Sprintf("\x1b[38;5;%dm╭─╮│╰╯\x1b[0m ", c.idx)
	}
	fmt.Printf("term colored text:    %s\n", text)
	fmt.Printf("term colored blocks:  %s\n", glyphs)
	fmt.Printf("term colored borders: %s\n", borders)
	fmt.Println("term all three rows should be blue/green/red/pink. If TEXT is colored but")
	fmt.Println("term BLOCKS/BORDERS are not, the terminal's custom-glyph renderer drops the")
	fmt.Println(`term color — in VS Code set "terminal.integrated.customGlyphs": false and`)
	fmt.Println(`term reload the window ("terminal.integrated.gpuAcceleration": "off" is the`)
	fmt.Println("term fallback if that alone doesn't fix it).")
}

// dirWritable reports whether dir (created if missing) accepts new files.
func dirWritable(dir string) bool {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false
	}
	probe := filepath.Join(dir, ".council-doctor-probe")
	if err := os.WriteFile(probe, []byte("probe"), 0o600); err != nil {
		return false
	}
	_ = os.Remove(probe)
	return true
}
