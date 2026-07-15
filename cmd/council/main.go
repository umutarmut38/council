package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
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
	// A --help/-h anywhere after a known command token prints that command's
	// structured help and exits 0, instead of the flag package's terse dump or a
	// manual parser's "unknown flag" error. Runs before any config load or repo
	// check so `council plan --help` works outside a git repo.
	if c, ok := helpTarget(args); ok {
		fmt.Println(command.HelpString(c))
		return nil
	}

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
	// Suppress the FlagSet's terse default usage: Parse calls it both for
	// -h/--help and on a bad flag, but we print the full usage ourselves on
	// ErrHelp below, and a flag error already prints its own message. Without
	// this, `--help` would emit the two-flag dump to stderr and the full usage
	// to stdout.
	flags.Usage = func() {}
	agentList := flags.String("agents", "", "comma-separated agent names to launch")
	noLocal := flags.Bool("no-local-config", false, "ignore repo-local .council.yaml")
	if err := flags.Parse(args); err != nil {
		// The flag package intercepts -h/--help (in any position) and returns
		// ErrHelp after printing its own terse flag dump; show the full usage
		// and exit 0 instead, matching `council help`.
		if errors.Is(err, flag.ErrHelp) {
			printUsage()
			return nil
		}
		return err
	}

	remaining := flags.Args()
	// `launch`/`ask` can follow the global flags (`council --agents x ask --help`);
	// the top-of-run helpTarget only sees args[0], so re-check the positionals the
	// flag parser left behind before treating a trailing --help as an ask prompt.
	if c, ok := helpTarget(remaining); ok {
		fmt.Println(command.HelpString(c))
		return nil
	}
	initialPrompt := ""
	if len(remaining) > 0 {
		switch remaining[0] {
		case "launch":
			// The explicit form of bare `council` (the registry names this
			// command "launch"); put any --agents/--no-local-config before it.
			// Reject trailing args so a typo like `council launch foo` errors
			// instead of being silently dropped.
			if len(remaining) > 1 {
				return fmt.Errorf("council launch takes no arguments (got %q); put flags before it", remaining[1])
			}
		case "ask":
			if len(remaining) < 2 {
				return errors.New(`usage: council ask "<prompt>"`)
			}
			initialPrompt = strings.Join(remaining[1:], " ")
		case "help":
			// Bare word only; -h/--help are caught at flags.Parse above.
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

	// Deferred: the run directory is created on the first prompt (see
	// Model.ensureRun), so merely launching the TUI no longer litters
	// .council/runs. Raw-log paths and the initial prompt are wired up then too.
	store := runstore.NewDeferred(cfg.Sessions.RootDir, effectiveYAML(cfg), sources.JSON())

	sessions := make([]*agent.Session, 0, len(selected))
	for _, spec := range selected {
		session := agent.NewSession(spec.Name, spec.Config, "")
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
	// On any exit (a quit key or a panic) terminate the panes FIRST so a
	// shutdown-only reporter (Copilot writes its token totals only in
	// session.shutdown, on process exit) flushes its session file, THEN run the
	// final reconcile that reads it. Ordering matters: reconciling before
	// termination would miss exactly the exit-only totals FinalizeUsage exists to
	// capture. Both live in one defer so the order holds on every return path.
	var final tea.Model
	var sessionsMu sync.Mutex
	allSessions := append([]*agent.Session(nil), sessions...)
	shuttingDown := false
	defer func() {
		if fm, ok := final.(tui.Model); ok {
			fm.FlushUsage()
		}
		sessionsMu.Lock()
		shuttingDown = true
		owned := append([]*agent.Session(nil), allSessions...)
		sessionsMu.Unlock()
		var terminateWG sync.WaitGroup
		for _, session := range owned {
			terminateWG.Add(1)
			go func() {
				defer terminateWG.Done()
				_ = session.Terminate()
			}()
		}
		terminateWG.Wait()
		deadline := time.Now().Add(2 * time.Second)
		for _, session := range owned {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				break
			}
			session.WaitDone(remaining)
		}
		if fm, ok := final.(tui.Model); ok {
			fm.FinalizeUsage()
		}
	}()

	var program *tea.Program
	launch := func(s *agent.Session) {
		sessionsMu.Lock()
		if shuttingDown {
			sessionsMu.Unlock()
			_ = s.Terminate()
			return
		}
		seen := false
		for _, existing := range allSessions {
			if existing == s {
				seen = true
				break
			}
		}
		if !seen {
			allSessions = append(allSessions, s)
		}
		sessionsMu.Unlock()
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
	model.SetSetupStatus(setupStatus)
	// Opt-in freestyle worktrees: only for the interactive freestyle session (a
	// controller is present in a git repo). CLI single-phase launches pass a nil
	// controller and never relocate their panes.
	if cfg.Worktrees.Freestyle && orch != nil {
		model.SetFreeWorktrees(orchestrate.NewFreeWorktrees(orch.RepoRoot(), cfg.Worktrees))
	}
	model.LoadTranscripts(transcripts)
	// 120 FPS (bubbletea's max): agent PTYs stream constantly, and the default
	// frame budget makes scrolling output feel choppy.
	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithFPS(120)}
	if cfg.UI.MouseEnabled() {
		// Cell motion (not all motion): we only need wheel + click, not a flood
		// of drag-motion events. Capture disables native text selection, so it is
		// toggleable at runtime (Ctrl+W).
		opts = append(opts, tea.WithMouseCellMotion())
	}
	program = tea.NewProgram(model, opts...)

	var err error
	final, err = program.Run()
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

// helpTarget resolves `council <cmd> ... --help` to the CLI entry whose help
// should print. It matches only when a bare --help/-h token follows a known
// command, so a --help buried inside quoted issue text is not treated as a help
// request, and bare `council --help` (no command) falls through to full usage.
func helpTarget(args []string) (command.CLI, bool) {
	if len(args) == 0 {
		return command.CLI{}, false
	}
	hasHelp := false
	for _, a := range args[1:] {
		if a == "--help" || a == "-h" {
			hasHelp = true
			break
		}
	}
	if !hasHelp {
		return command.CLI{}, false
	}
	// Two-token commands (e.g. "config init") resolve before single tokens.
	if len(args) >= 2 {
		if c, ok := command.LookupCLI(args[0] + " " + args[1]); ok {
			return c, true
		}
	}
	return command.LookupCLI(args[0])
}
