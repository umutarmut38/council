package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
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
	if err := run(os.Args[1:]); err != nil {
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

	if len(args) >= 2 && args[0] == "config" && args[1] == "init" {
		return initConfig(args[2:])
	}

	if len(args) >= 1 && args[0] == "doctor" {
		return doctor()
	}

	if len(args) >= 1 {
		switch args[0] {
		case "plan", "vote", "build", "run", "clean", "status", "resume":
			return runOrchestration(args[0], args[1:])
		}
	}

	flags := flag.NewFlagSet("council", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	agentList := flags.String("agents", "", "comma-separated agent names to launch")
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

	cfgPath, err := config.DefaultPath()
	if err != nil {
		return err
	}

	cfg, rawConfig, err := config.Load(cfgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := config.WriteDefault(cfgPath, false); err != nil {
				return err
			}
			fmt.Printf("Created default config at %s.\nEdit it, then run council again.\n", cfgPath)
			return nil
		}
		return err
	}
	if merged, localPath, lerr := config.ApplyLocal(cfg); lerr != nil {
		return lerr
	} else if localPath != "" {
		cfg = merged
		fmt.Fprintf(os.Stderr, "Using repo config %s\n", localPath)
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
		return errors.New("no agents selected; enable agents in ~/.council.yaml or pass --agents name1,name2")
	}

	store, err := runstore.New(cfg.Sessions.RootDir, rawConfig)
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
	program = tea.NewProgram(model, tea.WithAltScreen())

	_, err := program.Run()
	return err
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
	fmt.Printf("Wrote %s\n", cfgPath)
	return nil
}

func doctor() error {
	cfgPath, err := config.DefaultPath()
	if err != nil {
		return err
	}
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	selected, _, err := config.SelectAgents(cfg, nil)
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		fmt.Println("No enabled agents.")
		return nil
	}

	hadProblem := false
	for _, spec := range selected {
		if len(spec.Config.Command) == 0 {
			fmt.Printf("x %s: no command configured\n", spec.Name)
			hadProblem = true
			continue
		}
		binary := spec.Config.Command[0]
		path, err := exec.LookPath(binary)
		if err != nil {
			fmt.Printf("x %s: %s not found in PATH\n", spec.Name, binary)
			hadProblem = true
			continue
		}
		fmt.Printf("ok %s: %s\n", spec.Name, path)
	}

	if hadProblem {
		return errors.New("doctor found missing agent commands")
	}
	return nil
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
  council [--agents claude,codex]
 council [--agents claude,codex] ask "<prompt>"
  council config init [--force]
  council doctor
  council version

Orchestration (each phase runs in live panes, one git worktree per agent):
  council plan  "<issue>" | --file issue.md | --issue 123
  council vote  [run]            tally ranked votes into a winner
  council build [run]            all agents implement the winning plan
  council run   "<issue>"        plan -> vote -> build
  council resume [run]           reopen an older run with fresh agent processes
  council status [run]           show a run's artifacts
  council clean                  remove council worktrees + branches`)
}
