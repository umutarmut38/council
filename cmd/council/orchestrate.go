package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/orchestrate"
)

func runOrchestration(command string, args []string) error {
	switch command {
	case "plan":
		return councilPlan(args)
	case "vote":
		return councilVote(args)
	case "build":
		return councilBuild(args)
	case "review":
		return councilReview(args)
	case "adopt":
		return councilAdopt(args)
	case "run":
		return councilRunAll(args)
	case "clean":
		return councilClean(args)
	case "clean-runs":
		return councilCleanRuns(args)
	case "status":
		return councilStatus(args)
	case "resume":
		return councilResume(args)
	case "report":
		return councilReport(args)
	case "stack":
		return councilStack(args)
	case "scorecard":
		return councilScorecard(args)
	case "queue":
		return councilQueue(args)
	case "pr":
		return councilPR(args)
	case "artifacts":
		return councilArtifacts(args)
	}
	return fmt.Errorf("unknown orchestration command %q", command)
}

// parseWithTrailingFlags parses args while allowing flags to appear after
// positional arguments (`council adopt <run> --yes`); the Go flag package
// otherwise stops at the first positional and treats the flags as arguments.
func parseWithTrailingFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// newOrchFlagSet creates a flag set with the flags every orchestration
// subcommand shares.
func newOrchFlagSet(name string) (*flag.FlagSet, *bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	noLocal := fs.Bool("no-local-config", false, "ignore repo-local .council.yaml")
	return fs, noLocal
}

func loadConfig(noLocal bool) (config.Config, error) {
	cfg, _, err := loadEffectiveConfig(noLocal)
	if err != nil {
		return cfg, err
	}
	if err := applyRuntimeConfig(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// runPhase launches a phase's sessions in the TUI and blocks until the user quits.
func runPhase(ctrl *orchestrate.Controller, phase config.Phase, cfg config.Config, prompts map[string]string) error {
	// Bring up pre-launch setup (idempotent: once per invocation, even when
	// `council run` chains plan→vote→build).
	if err := ensureSetup(cfg); err != nil {
		return err
	}
	store, err := ctrl.Store(phase)
	if err != nil {
		return err
	}
	ctrl.Run().RecordPhaseStart(string(phase), ctrl.AgentsForPhase(phase))
	defer ctrl.Run().RecordPhaseEnd(string(phase))
	// nil controller: a CLI phase drives one phase only, no nested in-chat orchestration.
	return launchTUI(ctrl.PhaseSessions(phase, store, prompts), store, cfg, "", ctrl.InteractivePrompts(phase, prompts), nil)
}

func councilPlan(args []string) error {
	fs, noLocal := newOrchFlagSet("council plan")
	file := fs.String("file", "", "read the issue from a markdown file")
	issueNum := fs.Int("issue", 0, "fetch the issue from GitHub by number (via gh)")
	agentsOverride := fs.String("agents", "", "comma-separated agent names")
	base := fs.String("base", "", "base ref for worktrees (default HEAD)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadConfig(*noLocal)
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	issue, err := orchestrate.ResolveIssue(
		orchestrate.IssueSpec{Inline: strings.Join(fs.Args(), " "), File: *file, Number: *issueNum},
		cwd, fileRefOpts(cfg))
	if err != nil {
		return err
	}
	ctrl, err := orchestrate.NewController(cfg, parseAgentList(*agentsOverride), *base)
	if err != nil {
		return err
	}
	if err := ctrl.StartRun(issue); err != nil {
		return err
	}
	fmt.Printf("Run %s — planning with %s\n", ctrl.Run().Stamp, strings.Join(ctrl.AgentsForPhase(config.PhasePlan), ", "))
	return doPlan(ctrl, cfg)
}

func fileRefOpts(cfg config.Config) orchestrate.FileRefOptions {
	opts := orchestrate.FileRefOptionsFromConfig(cfg)
	opts.Warn = func(msg string) { fmt.Fprintln(os.Stderr, "warning:", msg) }
	return opts
}

func doPlan(ctrl *orchestrate.Controller, cfg config.Config) error {
	prompts, err := ctrl.PlanPrompts()
	if err != nil {
		return err
	}
	if err := runPhase(ctrl, config.PhasePlan, cfg, prompts); err != nil {
		return err
	}
	found, missing, err := ctrl.CollectPlans()
	if err != nil {
		return err
	}
	fmt.Printf("Collected %d plan(s) into %s\n", len(found), ctrl.Run().PlansDir())
	if len(missing) > 0 {
		fmt.Printf("No plan written by: %s\n", strings.Join(missing, ", "))
	}
	if len(found) == 0 {
		return errors.New("no plans were produced")
	}
	return nil
}

func councilVote(args []string) error {
	fs, noLocal := newOrchFlagSet("council vote")
	agentsOverride := fs.String("agents", "", "comma-separated agent names")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*noLocal)
	if err != nil {
		return err
	}
	ctrl, err := orchestrate.NewController(cfg, parseAgentList(*agentsOverride), "")
	if err != nil {
		return err
	}
	if err := ctrl.UseRun(fs.Arg(0)); err != nil {
		return err
	}
	return doVote(ctrl, cfg)
}

func doVote(ctrl *orchestrate.Controller, cfg config.Config) error {
	prompts, err := ctrl.VotePrompts()
	if err != nil {
		return err
	}
	if err := runPhase(ctrl, config.PhaseVote, cfg, prompts); err != nil {
		return err
	}
	res, err := ctrl.CollectVotesAndTally()
	if err != nil {
		return err
	}
	fmt.Printf("\nWinner: %s (Plan %s)\n", res.WinnerAgent, res.WinnerLetter)
	return nil
}

func councilBuild(args []string) error {
	fs, noLocal := newOrchFlagSet("council build")
	agentsOverride := fs.String("agents", "", "comma-separated agent names")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*noLocal)
	if err != nil {
		return err
	}
	ctrl, err := orchestrate.NewController(cfg, parseAgentList(*agentsOverride), "")
	if err != nil {
		return err
	}
	if err := ctrl.UseRun(fs.Arg(0)); err != nil {
		return err
	}
	return doBuild(ctrl, cfg)
}

func doBuild(ctrl *orchestrate.Controller, cfg config.Config) error {
	prompt, err := ctrl.BuildPrompt()
	if err != nil {
		return err
	}
	if err := runPhase(ctrl, config.PhaseBuild, cfg, promptsForAgents(ctrl.AgentsForPhase(config.PhaseBuild), prompt)); err != nil {
		return err
	}
	fmt.Println("\nImplementations ready on branches:")
	for _, name := range ctrl.AgentsForPhase(config.PhaseBuild) {
		fmt.Printf("  %-10s council/%s/%s\n", name, name, ctrl.Run().Stamp)
	}
	return nil
}

// councilReview gates the builds (diff + check command per worktree), runs the
// reviewer vote in live panes, and writes the build result — the CLI twin of
// the in-chat /review.
func councilReview(args []string) error {
	fs, noLocal := newOrchFlagSet("council review")
	agentsOverride := fs.String("agents", "", "comma-separated agent names")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*noLocal)
	if err != nil {
		return err
	}
	ctrl, err := orchestrate.NewController(cfg, parseAgentList(*agentsOverride), "")
	if err != nil {
		return err
	}
	if err := ctrl.UseRun(fs.Arg(0)); err != nil {
		return err
	}

	prompts, survivors, err := ctrl.ReviewPrompts()
	if err != nil {
		return err
	}
	switch len(survivors) {
	case 0:
		return errors.New("no builds passed the checks")
	case 1:
		if err := ctrl.SetSingleWinner(survivors[0]); err != nil {
			return err
		}
		fmt.Printf("Only %s passed the checks — `council adopt` applies it.\n", survivors[0])
		return writeReportQuiet(ctrl)
	}

	fmt.Printf("Reviewing %d builds with %s\n", len(survivors), strings.Join(ctrl.AgentsForPhase(config.PhaseReview), ", "))
	if err := runPhase(ctrl, config.PhaseReview, cfg, prompts); err != nil {
		return err
	}
	res, err := ctrl.CollectReviewsAndTally()
	if err != nil {
		return err
	}
	fmt.Printf("\nBest build: %s — `council adopt` applies it.\n", res.WinnerAgent)
	return writeReportQuiet(ctrl)
}

func writeReportQuiet(ctrl *orchestrate.Controller) error {
	if path, err := orchestrate.WriteReport(ctrl.Run()); err == nil {
		fmt.Printf("Report: %s\n", path)
	}
	return nil
}

// councilAdopt applies a build's diff to the working tree:
// council adopt [run] [agent] [--dry-run] [--yes]
func councilAdopt(args []string) error {
	fs, noLocal := newOrchFlagSet("council adopt")
	dryRun := fs.Bool("dry-run", false, "show what would be applied without touching the tree")
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	positional, err := parseWithTrailingFlags(fs, args)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(*noLocal)
	if err != nil {
		return err
	}
	ctrl, err := orchestrate.NewController(cfg, nil, "")
	if err != nil {
		return err
	}

	// Positional args: [run] [agent]. A known run stamp comes first; a single
	// argument that isn't an existing run is treated as the agent.
	runArg, agentArg := "", ""
	switch len(positional) {
	case 0:
	case 1:
		if err := ctrl.UseRun(positional[0]); err == nil {
			runArg = positional[0]
		} else {
			agentArg = positional[0]
		}
	default:
		runArg, agentArg = positional[0], positional[1]
	}
	if ctrl.Run() == nil {
		if err := ctrl.UseRun(runArg); err != nil {
			return err
		}
	}

	plan, err := ctrl.PlanAdopt(agentArg)
	if err != nil {
		return err
	}
	fmt.Printf("Adopt %s (run %s): %d file(s)\n", plan.Agent, ctrl.Run().Stamp, len(plan.Files))
	for _, f := range plan.Files {
		fmt.Printf("  %s\n", f)
	}
	if len(plan.DirtyFiles) > 0 {
		fmt.Printf("WARNING: the working tree has %d uncommitted file(s):\n", len(plan.DirtyFiles))
		for _, f := range plan.DirtyFiles {
			fmt.Printf("  %s\n", f)
		}
	}
	if plan.CheckError != "" {
		return fmt.Errorf("the diff does not apply cleanly:\n%s", plan.CheckError)
	}
	if *dryRun {
		fmt.Println("Dry run: nothing applied.")
		return nil
	}
	if !*yes && cfg.Policy.ConfirmDestructive() {
		if !confirmPrompt(fmt.Sprintf("Apply %s's changes to the working tree?", plan.Agent)) {
			return errors.New("aborted")
		}
	}
	adopted, files, err := ctrl.Adopt(plan.Agent)
	if err != nil {
		return err
	}
	fmt.Printf("Applied %s's changes (%d files, uncommitted). Review with `git diff`, then commit.\n", adopted, len(files))
	return nil
}

func confirmPrompt(question string) bool {
	if !stdinIsTerminal() {
		fmt.Fprintln(os.Stderr, "non-interactive session; pass --yes to proceed")
		return false
	}
	fmt.Printf("%s [y/N] ", question)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

func promptsForAgents(agents []string, prompt string) map[string]string {
	prompts := map[string]string{}
	for _, name := range agents {
		prompts[name] = prompt
	}
	return prompts
}

func councilRunAll(args []string) error {
	fs, noLocal := newOrchFlagSet("council run")
	file := fs.String("file", "", "read the issue from a markdown file")
	issueNum := fs.Int("issue", 0, "fetch the issue from GitHub by number (via gh)")
	agentsOverride := fs.String("agents", "", "comma-separated agent names")
	base := fs.String("base", "", "base ref for worktrees (default HEAD)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadConfig(*noLocal)
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	issue, err := orchestrate.ResolveIssue(
		orchestrate.IssueSpec{Inline: strings.Join(fs.Args(), " "), File: *file, Number: *issueNum},
		cwd, fileRefOpts(cfg))
	if err != nil {
		return err
	}
	ctrl, err := orchestrate.NewController(cfg, parseAgentList(*agentsOverride), *base)
	if err != nil {
		return err
	}
	if err := ctrl.StartRun(issue); err != nil {
		return err
	}
	fmt.Printf("Run %s — plan → vote → build with %s\n", ctrl.Run().Stamp, strings.Join(ctrl.AgentsForPhase(config.PhasePlan), ", "))
	if err := doPlan(ctrl, cfg); err != nil {
		return err
	}
	if err := doVote(ctrl, cfg); err != nil {
		return err
	}
	return doBuild(ctrl, cfg)
}

func councilClean(args []string) error {
	fs, noLocal := newOrchFlagSet("council clean")
	dryRun := fs.Bool("dry-run", false, "list what would be removed")
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*noLocal)
	if err != nil {
		return err
	}
	ctrl, err := orchestrate.NewController(cfg, nil, "")
	if err != nil {
		return err
	}

	worktrees, err := ctrl.ListWorktrees()
	if err != nil {
		return err
	}
	if len(worktrees) == 0 {
		fmt.Println("No council worktrees to remove.")
		return nil
	}
	fmt.Printf("Would remove %d worktree(s):\n", len(worktrees))
	for _, wt := range worktrees {
		fmt.Printf("  %s (branch %s)\n", wt.Path, wt.Branch)
	}
	if *dryRun {
		return nil
	}
	if !*yes && cfg.Policy.ConfirmDestructive() {
		if !confirmPrompt("Remove them (worktrees AND branches)?") {
			return errors.New("aborted")
		}
	}
	removed, err := ctrl.Clean()
	if err != nil {
		return err
	}
	fmt.Printf("Removed worktrees: %s\n", strings.Join(removed, ", "))
	return nil
}

func councilStatus(args []string) error {
	fs, noLocal := newOrchFlagSet("council status")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*noLocal)
	if err != nil {
		return err
	}
	run, err := orchestrate.OpenRun(cfg.Sessions.RootDir, fs.Arg(0))
	if err != nil {
		return err
	}
	printRunStatus(run)
	return nil
}

// printRunStatus shows the phase, artifacts, winners, checks, and paths.
func printRunStatus(run *orchestrate.Run) {
	fmt.Printf("Run %s (%s)\n", run.Stamp, run.Dir)

	if state, err := run.LoadState(); err == nil && state.Phase != "" {
		fmt.Printf("  active phase: %s", state.Phase)
		if len(state.Participants) > 0 {
			fmt.Printf(" (participants: %s)", strings.Join(state.Participants, ", "))
		}
		if !state.PromptSent {
			fmt.Print(" — prompt not sent yet")
		}
		fmt.Println()
	} else {
		fmt.Println("  active phase: none")
	}

	summary, err := orchestrate.SummarizeRun(run.RootDir, run.Stamp)
	if err != nil {
		fmt.Printf("  (could not summarize: %v)\n", err)
		return
	}
	listOrNone := func(label string, names []string, dir string) {
		if len(names) == 0 {
			fmt.Printf("  %s: (none)\n", label)
			return
		}
		fmt.Printf("  %s: %s  (%s)\n", label, strings.Join(names, ", "), dir)
	}
	listOrNone("plans", summary.Plans, run.PlansDir())
	listOrNone("votes", summary.Votes, run.VotesDir())
	if summary.Winner != "" {
		fmt.Printf("  plan winner: %s  (%s)\n", summary.Winner, run.ResultPath())
	}
	listOrNone("build diffs", summary.Diffs, run.BuildsDir())

	// Check results per diff.
	for _, agent := range summary.Diffs {
		if log, err := os.ReadFile(run.CheckLogPath(agent)); err == nil {
			status := "FAIL"
			if strings.HasSuffix(strings.TrimSpace(string(log)), "PASS") {
				status = "PASS"
			}
			fmt.Printf("  check %s: %s  (%s)\n", agent, status, run.CheckLogPath(agent))
		}
	}
	listOrNone("reviews", summary.Reviews, run.BuildsDir())
	if data, err := os.ReadFile(run.BuildResultPath()); err == nil {
		var payload struct {
			Winner string `json:"winner_agent"`
		}
		if json.Unmarshal(data, &payload) == nil && payload.Winner != "" {
			fmt.Printf("  build winner: %s  (%s)\n", payload.Winner, run.BuildResultPath())
		}
	}
	if adopted, ok := run.Adoption(); ok {
		fmt.Printf("  adopted: %s\n", adopted)
	}
	if _, err := os.Stat(run.Dir + "/report.md"); err == nil {
		fmt.Printf("  report: %s/report.md\n", run.Dir)
	}
}

func councilResume(args []string) error {
	fs, noLocal := newOrchFlagSet("council resume")
	agentsOverride := fs.String("agents", "", "comma-separated agent names")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*noLocal)
	if err != nil {
		return err
	}
	ctrl, err := orchestrate.NewController(cfg, parseAgentList(*agentsOverride), "")
	if err != nil {
		return err
	}
	if err := ctrl.UseRun(fs.Arg(0)); err != nil {
		return err
	}
	store, err := ctrl.Store(config.Phase("resume"))
	if err != nil {
		return err
	}
	if err := ensureSetup(cfg); err != nil {
		return err
	}
	sessions := ctrl.ResumeSessions(store)
	transcripts := orchestrate.LoadTranscripts(ctrl.Run().Dir, ctrl.Agents())
	fmt.Printf("Resuming run %s\n", ctrl.Run().Stamp)
	return launchTUIWithTranscripts(sessions, store, cfg, "", nil, transcripts, ctrl)
}
