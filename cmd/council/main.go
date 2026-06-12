package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/orchestrate"
	runstore "github.com/umutarmut38/council/internal/session"
	"github.com/umutarmut38/council/internal/tui"
	"github.com/umutarmut38/council/internal/version"
)

func main() {
	err := run(os.Args[1:])
	stopSetup() // tear down any supervised background setup processes
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) >= 1 {
		switch args[0] {
		case "version", "--version", "-v":
			fmt.Println(version.String())
			return nil
		}
	}

	if len(args) >= 2 && args[0] == "config" {
		return runConfigCommand(args[1], args[2:])
	}

	if len(args) >= 1 && args[0] == "doctor" {
		return doctor(args[1:])
	}

	if len(args) >= 1 && args[0] == "trust" {
		return councilTrust(args[1:])
	}

	if len(args) >= 1 {
		switch args[0] {
		case "plan", "vote", "build", "run", "review", "adopt", "clean", "clean-runs",
			"status", "resume", "report", "stack", "scorecard", "queue", "pr":
			return runOrchestration(args[0], args[1:])
		}
	}

	flags := flag.NewFlagSet("council", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	agentList := flags.String("agents", "", "comma-separated agent names to launch")
	noLocal := flags.Bool("no-local-config", false, "ignore repo-local .council.yaml")
	if err := flags.Parse(args); err != nil {
		return err
	}

	remaining := flags.Args()
	initialPrompt := ""
	if len(remaining) > 0 {
		switch remaining[0] {
		case "ask":
			if len(remaining) < 2 {
				return errors.New(`usage: council ask "<prompt>"`)
			}
			initialPrompt = strings.Join(remaining[1:], " ")
		case "help", "-h", "--help":
			printUsage()
			return nil
		default:
			return fmt.Errorf("unknown command %q", remaining[0])
		}
	}

	cfg, sources, err := loadEffectiveConfig(*noLocal)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfgPath, perr := config.DefaultPath()
			if perr != nil {
				return perr
			}
			if err := config.WriteDefault(cfgPath, false); err != nil {
				return err
			}
			fmt.Printf("Created default config at %s.\nEnable the agents you use (or run `council config wizard`), then run council again.\n", cfgPath)
			return nil
		}
		return err
	}

	if err := applyRuntimeConfig(cfg); err != nil {
		return err
	}

	names := parseAgentList(*agentList)
	selected, warnings, err := config.SelectAgents(cfg, names)
	for _, warning := range warnings {
		fmt.Fprintln(os.Stderr, warning)
	}
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		return errors.New("no agents selected; enable agents in ~/.council.yaml (or run `council config wizard`), or pass --agents name1,name2")
	}

	if err := ensureSetup(cfg); err != nil {
		return err
	}

	store, err := runstore.New(cfg.Sessions.RootDir, effectiveYAML(cfg), sources.JSON())
	if err != nil {
		return err
	}
	if initialPrompt != "" {
		if err := store.SavePrompt(initialPrompt); err != nil {
			return err
		}
	}

	sessions := make([]*agent.Session, 0, len(selected))
	for _, spec := range selected {
		session := agent.NewSession(spec.Name, spec.Config, store.RawLogPath(spec.Name))
		sessions = append(sessions, session)
	}

	// Best-effort controller so in-chat /plan, /vote, /build work from the
	// interactive session. Nil (disabled) outside a git repo or with no agents.
	orch, _ := orchestrate.NewController(cfg, names, "")

	return launchTUI(sessions, store, cfg, initialPrompt, nil, orch)
}

// launchTUI starts the agent sessions inside the Bubble Tea program and blocks
// until the user quits. Shared by the interactive `council` command and each
// orchestration phase. orch enables the in-chat orchestration commands.
func launchTUI(sessions []*agent.Session, store *runstore.Store, cfg config.Config, initialPrompt string, initialPrompts map[string]string, orch *orchestrate.Controller) error {
	return launchTUIWithTranscripts(sessions, store, cfg, initialPrompt, initialPrompts, nil, orch)
}

func launchTUIWithTranscripts(sessions []*agent.Session, store *runstore.Store, cfg config.Config, initialPrompt string, initialPrompts map[string]string, transcripts map[string]string, orch *orchestrate.Controller) error {
	defer func() {
		for _, session := range sessions {
			_ = session.Terminate()
		}
	}()

	var program *tea.Program
	launch := func(s *agent.Session) {
		if err := s.Start(
			func(name string, data []byte) {
				program.Send(tui.AgentOutputMsg{Name: name, Session: s, Data: data})
			},
			func(name string, exitCode *int, err error) {
				program.Send(tui.AgentExitMsg{Name: name, Session: s, ExitCode: exitCode, Err: err})
			},
		); err != nil {
			program.Send(tui.AgentStartErrorMsg{Name: s.Name, Session: s, Err: err})
		}
	}

	model := tui.NewModelWithConfig(sessions, store, cfg, initialPrompt, initialPrompts, time.Duration(cfg.UI.InitialPromptDelayMs)*time.Millisecond, launch, orch)
	model.LoadTranscripts(transcripts)
	// 120 FPS (bubbletea's max): agent PTYs stream constantly, and the default
	// frame budget makes scrolling output feel choppy.
	program = tea.NewProgram(model, tea.WithAltScreen(), tea.WithFPS(120))

	_, err := program.Run()
	return err
}

func parseAgentList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func printUsage() {
	fmt.Println(`Usage:
  council [--agents claude,codex] [--no-local-config]
  council [--agents claude,codex] ask "<prompt>"
  council config init [--force]       write the default (safe) config
  council config wizard               interactive setup
  council config add-agent <preset>   add a known agent CLI to the config
  council doctor                      check config, commands, repo, run dirs
  council trust [--revoke|--show]     trust this repo's .council.yaml
  council version

Orchestration (each phase runs in live panes, one git worktree per agent):
  council plan  "<issue>" | --file issue.md | --issue 123
  council vote  [run]            tally ranked votes into a winner
  council build [run]            all agents implement the winning plan
  council review [run]           gate builds + reviewers pick the best
  council adopt [run] [agent] [--dry-run] [--yes]
  council run   "<issue>"        plan -> vote -> build
  council resume [run]           reopen an older run with fresh agent processes
  council status [run]           show a run's phase, artifacts, and winners
  council report [run] [--post]  write report.md (--post comments on the issue)
  council pr [run] [agent]       open a PR from a build branch (via gh)
  council scorecard              agent performance across runs
  council queue add|list|run|clear   batch issues through council
  council stack detect|set <go|node|rust|python>   set review.check_command
  council clean [--dry-run] [--yes]  remove council worktrees + branches
  council clean-runs [--keep N] [--dry-run]  prune old run artifacts`)
}
