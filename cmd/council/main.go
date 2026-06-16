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
	"github.com/umutarmut38/council/internal/command"
	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/orchestrate"
	runstore "github.com/umutarmut38/council/internal/session"
	"github.com/umutarmut38/council/internal/tui"
	"github.com/umutarmut38/council/internal/version"
)

func main() {
	os.Exit(mainExitCode(os.Args[1:]))
}

func mainExitCode(args []string) int {
	defer stopSetup() // tear down any supervised background setup processes
	if err := run(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func run(args []string) error {
	if len(args) >= 1 {
		if c, ok := command.LookupCLI(args[0]); ok && c.Name == "version" {
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

	if len(args) >= 1 && command.IsOrchestration(args[0]) {
		return runOrchestration(args[0], args[1:])
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
	fmt.Println(command.UsageString())
}
