package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
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
	case "run":
		return councilRunAll(args)
	case "clean":
		return councilClean(args)
	case "status":
		return councilStatus(args)
	case "resume":
		return councilResume(args)
	}
	return fmt.Errorf("unknown orchestration command %q", command)
}

func loadConfig() (config.Config, error) {
	cfgPath, err := config.DefaultPath()
	if err != nil {
		return config.Config{}, err
	}
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		return cfg, err
	}
	merged, localPath, err := config.ApplyLocal(cfg)
	if err != nil {
		return cfg, err
	}
	if localPath != "" {
		fmt.Fprintf(os.Stderr, "Using repo config %s\n", localPath)
	}
	return merged, nil
}

// runPhase launches a phase's sessions in the TUI and blocks until the user quits.
func runPhase(ctrl *orchestrate.Controller, phase config.Phase, cfg config.Config, prompts map[string]string) error {
	store, err := ctrl.Store(phase)
	if err != nil {
		return err
	}
	// nil controller: a CLI phase drives one phase only, no nested in-chat orchestration.
	return launchTUI(ctrl.PhaseSessions(phase, store, prompts), store, cfg, "", ctrl.InteractivePrompts(phase, prompts), nil)
}

func councilPlan(args []string) error {
	fs := flag.NewFlagSet("council plan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	file := fs.String("file", "", "read the issue from a markdown file")
	issueNum := fs.Int("issue", 0, "fetch the issue from GitHub by number (via gh)")
	agentsOverride := fs.String("agents", "", "comma-separated agent names")
	base := fs.String("base", "", "base ref for worktrees (default HEAD)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	issue, err := orchestrate.ResolveIssue(orchestrate.IssueSpec{Inline: strings.Join(fs.Args(), " "), File: *file, Number: *issueNum}, cwd)
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
	fs := flag.NewFlagSet("council vote", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	agentsOverride := fs.String("agents", "", "comma-separated agent names")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig()
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
	fs := flag.NewFlagSet("council build", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	agentsOverride := fs.String("agents", "", "comma-separated agent names")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig()
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

func promptsForAgents(agents []string, prompt string) map[string]string {
	prompts := map[string]string{}
	for _, name := range agents {
		prompts[name] = prompt
	}
	return prompts
}

func councilRunAll(args []string) error {
	fs := flag.NewFlagSet("council run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	file := fs.String("file", "", "read the issue from a markdown file")
	issueNum := fs.Int("issue", 0, "fetch the issue from GitHub by number (via gh)")
	agentsOverride := fs.String("agents", "", "comma-separated agent names")
	base := fs.String("base", "", "base ref for worktrees (default HEAD)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	issue, err := orchestrate.ResolveIssue(orchestrate.IssueSpec{Inline: strings.Join(fs.Args(), " "), File: *file, Number: *issueNum}, cwd)
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
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	ctrl, err := orchestrate.NewController(cfg, nil, "")
	if err != nil {
		return err
	}
	removed, err := ctrl.Clean()
	if err != nil {
		return err
	}
	if len(removed) == 0 {
		fmt.Println("No council worktrees to remove.")
		return nil
	}
	fmt.Printf("Removed worktrees: %s\n", strings.Join(removed, ", "))
	return nil
}

func councilStatus(args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	run, err := orchestrate.OpenRun(cfg.Sessions.RootDir, firstArg(args))
	if err != nil {
		return err
	}
	fmt.Printf("Run %s (%s)\n", run.Stamp, run.Dir)
	fmt.Printf("  plans: %s\n", strings.Join(listMarkdown(run.PlansDir()), ", "))
	fmt.Printf("  votes: %s\n", strings.Join(listMarkdown(run.VotesDir()), ", "))
	return nil
}

func councilResume(args []string) error {
	fs := flag.NewFlagSet("council resume", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	agentsOverride := fs.String("agents", "", "comma-separated agent names")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig()
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
	sessions := ctrl.ResumeSessions(store)
	transcripts := orchestrate.LoadTranscripts(ctrl.Run().Dir, ctrl.Agents())
	fmt.Printf("Resuming run %s\n", ctrl.Run().Stamp)
	return launchTUIWithTranscripts(sessions, store, cfg, "", nil, transcripts, ctrl)
}

func firstArg(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

func listMarkdown(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{"(none)"}
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			names = append(names, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	if len(names) == 0 {
		return []string{"(none)"}
	}
	sort.Strings(names)
	return names
}
