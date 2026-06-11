package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/orchestrate"
)

// doctor checks the whole setup: config validity (global and local), enabled
// agent commands, role coverage, git availability, run/worktree directories,
// stale worktrees, the review check command, risky flags, and terminal
// settings. Problems make it exit non-zero; warnings don't.
func doctor(args []string) error {
	noLocal := false
	for _, a := range args {
		if a == "--no-local-config" {
			noLocal = true
		}
	}

	problems := 0
	warn := func(format string, v ...any) { fmt.Printf("warn %s\n", fmt.Sprintf(format, v...)) }
	fail := func(format string, v ...any) { fmt.Printf("FAIL %s\n", fmt.Sprintf(format, v...)); problems++ }
	ok := func(format string, v ...any) { fmt.Printf("ok   %s\n", fmt.Sprintf(format, v...)) }

	// Global config.
	cfgPath, err := config.DefaultPath()
	if err != nil {
		return err
	}
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fail("global config %s missing — run `council config init`", cfgPath)
			return errors.New("doctor found problems")
		}
		fail("global config %s: %v", cfgPath, err)
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

	if err := config.ValidateAgentNames(cfg); err != nil {
		fail("agent names: %v", err)
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

	haveWorker, haveReviewer := false, false
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
		for _, phase := range []config.Phase{config.PhasePlan, config.PhaseVote, config.PhaseBuild} {
			phaseCmd := spec.Config.CommandForPhase(phase)
			if len(phaseCmd) > 0 && phaseCmd[0] != binary {
				if _, lookErr := exec.LookPath(phaseCmd[0]); lookErr != nil {
					fail("%s: %s command %q not found in PATH", spec.Name, phase, phaseCmd[0])
				}
			}
		}
		for where, flags := range config.AgentRiskyFlags(spec.Config) {
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

	printColorDiagnostics()

	if problems > 0 {
		return fmt.Errorf("doctor found %d problem(s)", problems)
	}
	return nil
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
