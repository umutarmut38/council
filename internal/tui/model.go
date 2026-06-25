package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/orchestrate"
	runstore "github.com/umutarmut38/council/internal/session"
	"github.com/umutarmut38/council/internal/setup"
	"github.com/umutarmut38/council/internal/theme"
	"github.com/umutarmut38/council/internal/tui/anim"
)

type TargetMode int

const (
	TargetFocused TargetMode = iota
	TargetAll
	TargetPersonality
	TargetCategory
)

type InputMode int

const (
	InputComposer InputMode = iota
	InputDirect
)

type ScreenMode int

const (
	ScreenPanes ScreenMode = iota
	ScreenOverview
	ScreenSettings
	ScreenRuns
	ScreenArtifacts
	ScreenCompare
	ScreenEditor
)

type AgentOutputMsg struct {
	Name    string
	Session *agent.Session
	Data    []byte
}

type AgentExitMsg struct {
	Name     string
	Session  *agent.Session
	ExitCode *int
	Err      error
}

type AgentStartErrorMsg struct {
	Name    string
	Session *agent.Session
	Err     error
}

type initialPromptMsg string
type initialAgentPromptsMsg map[string]string
type phasePromptsMsg map[string]string
type pollArtifactsMsg struct{}
type buildProgressMsg struct{}

// buildProgressResultMsg carries the result of an off-thread build-activity
// probe back to the Update loop, so the git shell-outs never block rendering.
type buildProgressResultMsg struct{ active, total int }

// animTickMsg drives the activity-animation frame loop (the rotating 3D head
// and the /eva intro). The loop reschedules itself while it is live.
type animTickMsg time.Time

// reviewReadyMsg carries the result of the (async) build-check gate.
type reviewReadyMsg struct {
	prompts   map[string]string
	survivors []string
	err       error
}

type screenCell struct {
	Ch  rune
	SGR string
}

type agentView struct {
	Session *agent.Session

	Lines   []string
	Partial string

	Screen    [][]screenCell
	Width     int
	Height    int
	CursorRow int
	CursorCol int
	SavedRow  int
	SavedCol  int
	// CursorHidden tracks DECTCEM (ESC[?25l / ESC[?25h). Zero value false =
	// visible. The integrated editor pane renders a block cursor when it is
	// focused and the program has not hidden the cursor.
	CursorHidden bool
	// MouseModeOn tracks whether the child program enabled X11 mouse tracking
	// (DECSET ?1000/?1002/?1003). Mouse events are only forwarded to the PTY
	// while this is set.
	MouseModeOn bool
	ScrollTop   int
	ScrollBot   int
	CurrentSGR  string

	// pending holds an escape/OSC sequence that was split across read buffers,
	// to be completed by the next chunk instead of leaking as literal text.
	pending string

	// PhaseDone marks that this agent wrote its artifact for the current phase.
	PhaseDone bool

	// Attention marks a pane that likely needs direct user input (an approval
	// prompt was detected, or /attention was used). Cleared when the user
	// interacts with the pane or its artifact lands; auto-set flags also
	// clear themselves when the prompt leaves the screen.
	Attention bool
	// AttentionManual records that the flag came from /attention, so the
	// auto-clear never dismisses it.
	AttentionManual bool
	// lastOutputAt drives the idle check of the approval-prompt detection.
	lastOutputAt time.Time

	// ScrollOffset is how many wrapped lines the pane is scrolled up from the
	// live bottom (0 = live tail). While > 0 the pane renders from the plain-text
	// transcript (v.Lines) so it can show history the VT100 screen no longer
	// holds; at 0 it renders live (screen emulation or transcript) as normal.
	ScrollOffset int
}

type Model struct {
	Agents               []*agentView
	FocusedIndex         int
	PageIndex            int
	Width                int
	Height               int
	PromptInput          string
	InputMode            InputMode
	ScreenMode           ScreenMode
	Target               TargetMode
	TargetName           string
	Zoomed               bool
	Status               string
	Store                *runstore.Store
	Config               config.Config
	MaxScrollback        int
	initialPrompt        string
	initialPrompts       map[string]string
	initialPromptDelay   time.Duration
	initialPromptSent    bool
	launch               func(*agent.Session)
	agentsStarted        bool
	FileChoices          []string
	FileSuggestIndex     int
	CmdSuggestIndex      int
	fileSuggestHidden    string
	OverviewIndex        int
	SettingsIndex        int
	Runs                 []orchestrate.RunSummary
	RunIndex             int
	DisplayPersonalities map[string]bool

	// recentCommands holds the most-recently dispatched composer command
	// names, newest first, so the palette can surface them near the top.
	recentCommands []string

	// inputHistory holds composer lines the user submitted this session,
	// oldest first, for arrow up/down recall. historyPos walks it
	// (len == "at the live draft"); historyDraft preserves the in-progress
	// line while browsing history.
	inputHistory []string
	historyPos   int
	historyDraft string

	// setupStatus is the observability snapshot of pre-launch setup/env,
	// shown by /setup. Nil when no setup/env was configured.
	setupStatus *setup.Status

	// orchestration
	orch         *orchestrate.Controller
	phase        string            // "", "plan", "vote", "build"
	watching     map[string]string // agent -> artifact path watched this phase
	pendingBuild map[string]string // build prompts staged by /build, sent by /start-build
	phasePrompts map[string]string // prompts sent this phase, for /resend
	pendingAdopt *orchestrate.AdoptPlan
	pendingClean bool
	progress     *runProgress // cached HUD state; refreshProgress() updates it
	buildActive  int          // cached live build activity; updated off-thread by buildProgressResultMsg
	buildTotal   int          // cached worktree count from the same probe
	layoutLocked bool         // user adjusted rows/cols in settings: adaptive off
	mouseOn      bool         // mouse capture is active (Ctrl+W toggles it)
	// attentionCheckPending debounces the delayed approval-prompt re-check.
	attentionCheckPending bool

	// interruptArmed holds the agent name for which a Ctrl+C interrupt is armed;
	// a second Ctrl+C within interruptArmWindow actually sends \x03. Cleared by
	// any other key (see handleKey). There is no timer-driven disarm: the window
	// is re-checked on the next Ctrl+C, which re-arms (rather than interrupting)
	// once interruptArmWindow has elapsed. Empty = disarmed.
	interruptArmed   string
	interruptArmedAt time.Time

	// activity animation + retro mode. animFrame is the monotonically increasing
	// frame counter that drives the rotating 3D head and the /eva intro;
	// animLoopRunning guards against stacking multiple self-rescheduling tick
	// chains. retroActive toggles the persistent retro theme; while it is on but
	// retroIntroDone is false the activation intro plays full-screen, advanced by
	// retroIntroFrame.
	animFrame       int
	animLoopRunning bool
	retroActive     bool
	retroIntroFrame int
	retroIntroDone  bool
	// retroIntroLoop keeps the activation intro playing indefinitely (it never
	// auto-advances into themed mode; a key still skips it). crtOff disables the
	// CRT scanline overlay while themed.
	retroIntroLoop bool
	crtOff         bool
	// activeChrome caches the chrome style set for the current frame so the
	// render tree doesn't rebuild it for every pane. View sets it; chrome()
	// returns it when present (nil falls back to computing on demand).
	activeChrome *chromeStyles
	// baseChrome is the configured theme's chrome (ui.theme), built once at
	// construction. chrome() uses it as the non-retro base; nil (e.g. a Model
	// built without NewModelWithConfig in a test) falls back to defaultChrome.
	baseChrome *chromeStyles

	// artifact browser (/artifacts)
	Artifacts          []artifactEntry
	ArtifactIndex      int
	artifactView       string // rendered file content when viewing one artifact
	artifactPath       string // viewer title (a path, or a synthetic label)
	artifactFile       string // real file behind the view, for `e` (editor); "" = none
	artifactIsDiff     bool   // colorize +/-/@@ lines git-style
	viewerFromList     bool   // opened from the /artifacts list (Esc returns there)
	viewerReturnScreen ScreenMode
	artifactWrap       []string
	artifactTop        int

	// compare screen (/compare)
	CompareRows      []orchestrate.BuildComparison
	CompareIndex     int
	CompareFileIndex int
	compareMarked    string // build marked with `x` for a pairwise diff
	compareFiles     *compareFileSet

	// integrated editor screen (/edit). editorView hosts the editor PTY (nil
	// until a file is opened) and is deliberately kept out of m.Agents.
	editorView         *agentView
	editorRoot         string          // repo root the tree is anchored at
	editorSessionRoot  string          // editorRoot the live editorView was launched in (CWD); relaunch when it changes
	editorTree         []editorNode    // flattened visible tree rows
	editorExpanded     map[string]bool // expanded directories (absolute paths)
	editorTreeIndex    int             // selected tree row
	editorTreeTop      int             // first visible tree row (scroll)
	editorPaneFocused  bool            // false = tree focus, true = PTY focus
	editorReturnScreen ScreenMode      // where Esc returns (panes, or the originating screen)
}

// editorDoneMsg returns control after an external $EDITOR session.
type editorDoneMsg struct{ err error }

type artifactEntry struct {
	Label string
	Path  string
}

var (
	csiPattern     = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	oscPattern     = regexp.MustCompile(`\x1b\][^\a]*(\a|\x1b\\)`)
	controlPattern = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`)

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	faintStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	// Focused borders stay pink but a notch below neon; unfocused borders are
	// dimmed harder so the active pane reads instantly.
	focusStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("175"))
	borderStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	inputStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("229"))
	suggestStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("147"))
	headingStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	railStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)

const (
	// headerHeight is the compact header: title + status (a phase rail adds a
	// third row during a run).
	headerHeight = 2
	// headerBandHeight is the taller header used when the activity animation is
	// on; it must be tall enough to dock the 3D head grid. Capped so the body
	// still clears its minimum at the headShown floor (Height>=20): band(10) +
	// footer(4) + body(6) == 20.
	headerBandHeight = 10
	// footer = suggestion line + input box (top border, content, bottom border)
	footerHeight = 4
)

// interruptArmWindow is how long the first Ctrl+C stays armed before a second
// Ctrl+C is required again to interrupt the focused agent.
const interruptArmWindow = 2 * time.Second

func NewModel(sessions []*agent.Session, store *runstore.Store, maxScrollback int, initialPrompt string, initialPromptDelay time.Duration, launch func(*agent.Session), orch *orchestrate.Controller) Model {
	return NewModelWithPrompts(sessions, store, maxScrollback, initialPrompt, nil, initialPromptDelay, launch, orch)
}

func NewModelWithPrompts(sessions []*agent.Session, store *runstore.Store, maxScrollback int, initialPrompt string, initialPrompts map[string]string, initialPromptDelay time.Duration, launch func(*agent.Session), orch *orchestrate.Controller) Model {
	cfg := config.Config{UI: config.UIConfig{MaxScrollbackLines: maxScrollback}}
	cfg.Normalize()
	return NewModelWithConfig(sessions, store, cfg, initialPrompt, initialPrompts, initialPromptDelay, launch, orch)
}

func NewModelWithConfig(sessions []*agent.Session, store *runstore.Store, cfg config.Config, initialPrompt string, initialPrompts map[string]string, initialPromptDelay time.Duration, launch func(*agent.Session), orch *orchestrate.Controller) Model {
	cfg.Normalize()
	maxScrollback := cfg.UI.MaxScrollbackLines
	if initialPromptDelay <= 0 {
		initialPromptDelay = 3 * time.Second
	}

	views := make([]*agentView, 0, len(sessions))
	for _, session := range sessions {
		view := &agentView{Session: session, Width: 120, Height: 40}
		view.setScreenSize(120, 40)
		if session.StartError != nil {
			view.addDisplayLine("start error: " + session.StartError.Error())
			view.Lines = append(view.Lines, "start error: "+session.StartError.Error())
		}
		views = append(views, view)
	}

	status := "ready"
	if store != nil && store.Started() {
		status = "session " + store.RunDir
	}

	model := Model{
		Agents:             views,
		Store:              store,
		Config:             cfg,
		MaxScrollback:      maxScrollback,
		initialPrompt:      initialPrompt,
		initialPrompts:     initialPrompts,
		initialPromptDelay: initialPromptDelay,
		launch:             launch,
		orch:               orch,
		Target:             TargetAll,
		Status:             status,
		FileChoices:        discoverFileChoices(),
		mouseOn:            cfg.UI.MouseEnabled(),
	}
	base := themeToChrome(theme.Resolve(cfg.UI.Theme, cfg.UI.Themes))
	model.baseChrome = &base
	model.sortAgents()
	return model
}

func (m *Model) startAll() {
	if m.launch == nil {
		return
	}
	for _, v := range m.Agents {
		m.launch(v.Session)
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{}
	if len(m.initialPrompts) > 0 {
		prompts := copyPrompts(m.initialPrompts)
		cmds = append(cmds, tea.Tick(m.initialPromptDelay, func(time.Time) tea.Msg {
			return initialAgentPromptsMsg(prompts)
		}))
	} else if m.initialPrompt != "" {
		cmds = append(cmds, tea.Tick(m.initialPromptDelay, func(time.Time) tea.Msg {
			return initialPromptMsg(m.initialPrompt)
		}))
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.resizeAgents()
		m.resizeEditor()
		if !m.agentsStarted && m.launch != nil {
			m.agentsStarted = true
			return m, func() tea.Msg {
				m.startAll()
				return nil
			}
		}
		return m, nil
	case AgentOutputMsg:
		// The integrated editor's PTY is not in m.Agents, so route it explicitly.
		if m.editorView != nil && msg.Session == m.editorView.Session {
			m.appendOutput(m.editorView, string(msg.Data))
			return m, nil
		}
		if view := m.findAgentForMessage(msg.Name, msg.Session); view != nil {
			m.appendOutput(view, string(msg.Data))
			if m.noteAttentionOutput(view) {
				return m, m.scheduleAttentionCheck()
			}
		}
		return m, nil
	case attentionCheckMsg:
		return m, m.runAttentionCheck()
	case AgentExitMsg:
		// The integrated editor closed (e.g. :q): drop back to the file tree.
		if m.editorView != nil && msg.Session == m.editorView.Session {
			m.editorView = nil
			m.editorPaneFocused = false
			if m.ScreenMode == ScreenEditor {
				m.Status = "editor closed — pick a file to reopen"
			}
			return m, nil
		}
		if view := m.findAgentForMessage(msg.Name, msg.Session); view != nil {
			if msg.ExitCode != nil {
				view.Session.ExitCode = msg.ExitCode
				view.Session.Done = true
				m.appendOutput(view, fmt.Sprintf("\n[%s exited with code %d]\n", msg.Name, *msg.ExitCode))
			} else {
				view.Session.Done = true
				m.appendOutput(view, fmt.Sprintf("\n[%s exited]\n", msg.Name))
			}
			if msg.Err != nil {
				m.appendOutput(view, "exit error: "+msg.Err.Error()+"\n")
			}
		}
		return m, nil
	case AgentStartErrorMsg:
		if view := m.findAgentForMessage(msg.Name, msg.Session); view != nil {
			view.Session.MarkStartError(msg.Err)
			m.appendOutput(view, "start error: "+msg.Err.Error()+"\n")
			m.Status = msg.Name + " failed to start"
		}
		return m, nil
	case initialPromptMsg:
		if !m.initialPromptSent {
			if err := m.ensureRun(); err != nil {
				m.Status = "cannot start run: " + err.Error()
				return m, nil
			}
			m.sendAll(string(msg))
			m.initialPromptSent = true
			m.Status = "broadcast initial prompt"
		}
		return m, nil
	case initialAgentPromptsMsg:
		if !m.initialPromptSent {
			if err := m.ensureRun(); err != nil {
				m.Status = "cannot start run: " + err.Error()
				return m, nil
			}
			m.sendPrompts(map[string]string(msg))
			m.initialPromptSent = true
			m.Status = "sent initial prompts"
		}
		return m, nil
	case phasePromptsMsg:
		// Send off the UI thread: PTY writes can block if an agent isn't draining,
		// and a synchronous send here would freeze the whole TUI.
		prompts := map[string]string(msg)
		if m.orch != nil && m.phase != "" {
			_ = m.orch.MarkPhasePromptSent(config.Phase(m.phase))
		}
		// Remember what was sent so /resend can repeat it per agent.
		m.phasePrompts = copyPrompts(prompts)
		m.Status = m.phase + " prompt sent — agents working"
		return m, func() tea.Msg {
			m.sendPrompts(prompts)
			return nil
		}
	case editorDoneMsg:
		if msg.err != nil {
			m.Status = "editor: " + msg.err.Error()
		} else {
			m.Status = "back from editor"
		}
		m.resizeAgents()
		return m, nil
	case pollArtifactsMsg:
		return m, m.pollArtifacts()
	case buildProgressMsg:
		return m, m.buildProgress()
	case buildProgressResultMsg:
		return m, m.handleBuildProgressResult(msg)
	case reviewReadyMsg:
		return m.handleReviewReady(msg)
	case animTickMsg:
		m.animFrame++
		if m.retroActive && !m.retroIntroDone {
			m.retroIntroFrame++
			// In loop mode the intro never auto-advances (a key still skips it).
			if !m.retroIntroLoop && m.retroIntroFrame >= retroIntroFrames {
				m.retroIntroDone = true
				// The themed header band now appears, shrinking the body; resize
				// the panes' PTYs to match so they don't render stale until the
				// next terminal resize.
				m.resizeAgents()
			}
		}
		if m.animationLive() {
			return m, m.animTick()
		}
		// Nothing wants frames anymore: stop the loop so a later /eva can
		// cleanly restart it.
		m.animLoopRunning = false
		return m, nil
	case tea.MouseMsg:
		return m.handleMouseMsg(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// retroIntroFrames is how many frames the activation intro plays before it
// auto-advances into themed mode (any key skips it sooner). It spans the full
// stepped retro sequence in anim.Splash (the last beat, ACTIVATION, lands a few
// frames before this).
const retroIntroFrames = 64

// animationLive reports whether the frame loop is needed: it runs only while
// retro mode is active (the intro, then the themed UI with its rotating head and
// CRT sweep). Outside retro mode there is no repaint.
func (m Model) animationLive() bool {
	return m.retroActive
}

// animInterval is the per-frame cadence (~14 fps) for the retro-mode animation
// loop — the rotating head and the CRT sweep. The loop only runs while retro mode
// is active (see animationLive), so there is no animation, and no repaint,
// outside it.
const animInterval = 70 * time.Millisecond

// animTick schedules the next frame.
func (m Model) animTick() tea.Cmd {
	return tea.Tick(animInterval, func(t time.Time) tea.Msg {
		return animTickMsg(t)
	})
}

// kickAnimLoop starts the self-rescheduling frame loop if it should run and
// isn't already, returning the tick command (or nil). The animLoopRunning guard
// prevents stacking multiple tick chains (which would multiply the frame rate).
func (m *Model) kickAnimLoop() tea.Cmd {
	if m.animLoopRunning || !m.animationLive() {
		return nil
	}
	m.animLoopRunning = true
	return m.animTick()
}

// toggleRetro flips the persistent retro theme. Turning it on (re)plays the
// full-screen activation intro and then leaves the UI recolored until toggled
// off; turning it off reverts cleanly to the configured colors and header
// height. It kicks the frame loop when needed; when turning off, the loop
// stops itself on the next tick once nothing on screen still needs frames
// (see animationLive).
func (m *Model) toggleRetro(loop bool) tea.Cmd {
	m.retroActive = !m.retroActive
	if m.retroActive {
		m.retroIntroFrame = 0
		m.retroIntroDone = false
		m.retroIntroLoop = loop
		if loop {
			m.Status = "EVA mode — activation intro looping (any key to enter)"
		} else {
			m.Status = "EVA mode engaged"
		}
		return m.kickAnimLoop()
	}
	m.retroIntroDone = false
	m.retroIntroLoop = false
	m.Status = "EVA mode disengaged"
	// The header band is gone; restore the panes to the full body height.
	m.resizeAgents()
	return nil
}

func (m Model) View() string {
	if m.Width == 0 || m.Height == 0 {
		return "starting council..."
	}
	if m.Width < 48 || m.Height < 14 {
		return "Window too small for council. Resize to at least 48x14."
	}

	// While retro mode is engaging, the activation intro takes over the whole
	// screen until it finishes (or a key skips it).
	if m.retroActive && !m.retroIntroDone {
		return anim.Splash(m.Width, m.Height, m.retroIntroFrame)
	}

	// Build the frame's chrome set once; chrome() hands this cached copy to every
	// sub-renderer (header, footer, body, and each pane) so it isn't rebuilt per
	// pane.
	cs := m.chrome()
	m.activeChrome = &cs

	header := m.renderHeader()
	footer := m.renderFooter()
	bodyHeight := m.Height - m.chromeLines()
	if bodyHeight < 6 {
		bodyHeight = 6
	}

	body := m.renderBody(bodyHeight)
	// An open palette grows the footer; cover the bottom body rows instead of
	// shrinking the body — reflowing the panes on every "/" keystroke made
	// their contents jump around.
	if extra := strings.Count(footer, "\n") + 1 - footerHeight; extra > 0 {
		lines := strings.Split(body, "\n")
		if extra < len(lines) {
			body = strings.Join(lines[:len(lines)-extra], "\n")
		}
	}
	screen := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	if m.retroThemed() && !m.crtOff {
		// CRT scanline overlay across the whole screen — panes included — for the
		// full in-console look. Zero-width SGR keeps the exact dimensions, and
		// crtRowBG leaves any agent-set background intact. Toggle off with /crt.
		screen = applyCRT(screen, m.animFrame)
	}
	return screen
}

func (m *Model) appendOutput(view *agentView, chunk string) {
	view.appendTranscript(chunk, m.MaxScrollback)
	view.applyTerminal(chunk)
}

func (v *agentView) appendTranscript(chunk string, maxScrollback int) {
	text := cleanTranscriptOutput(chunk)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	parts := strings.Split(text, "\n")
	if len(parts) == 1 {
		v.Partial += parts[0]
		return
	}

	v.Lines = append(v.Lines, v.Partial+parts[0])
	for _, part := range parts[1 : len(parts)-1] {
		v.Lines = append(v.Lines, part)
	}
	v.Partial = parts[len(parts)-1]

	// While scrolled up, keep the viewed content anchored as new lines land
	// below the fold: ScrollOffset counts lines up from the live bottom, so it
	// must grow by the number of newly finalized lines. (Measured in raw lines;
	// rendering re-clamps to the wrapped window, so the offset stays bounded and
	// pins at the top once there's nothing older to show.)
	if v.ScrollOffset > 0 {
		v.ScrollOffset += len(parts) - 1
	}

	if len(v.Lines) > maxScrollback {
		v.Lines = v.Lines[len(v.Lines)-maxScrollback:]
	}
}

// targetStep is one stop in the Ctrl+B broadcast-target cycle. For the group
// modes, name is the personality or category name.
type targetStep struct {
	mode TargetMode
	name string
}

// targetCycle is the ordered list Ctrl+B walks: broadcast-to-all, then each
// group of the active ui.group_by (personality or category groups that have
// agents, in configured order), then the focused agent. With group_by: none
// there are no groups, so it stays all <-> focused.
func (m Model) targetCycle() []targetStep {
	steps := []targetStep{{mode: TargetAll}}
	switch m.groupByLabel() {
	case "personality":
		for _, name := range m.orderedUsedPersonalities() {
			steps = append(steps, targetStep{mode: TargetPersonality, name: name})
		}
	case "category":
		for _, name := range m.orderedUsedCategories() {
			steps = append(steps, targetStep{mode: TargetCategory, name: name})
		}
	}
	steps = append(steps, targetStep{mode: TargetFocused})
	return steps
}

// currentTargetIndex finds the active target in the cycle, or -1 when the target
// was set off-cycle (e.g. a /target personality while grouping by category).
func (m Model) currentTargetIndex(cycle []targetStep) int {
	// Compare the name for every step (it is "" for all/focused): a stale
	// TargetName left on an all/focused target then reads as off-cycle, so the
	// next toggle re-applies a clean step instead of carrying the stale name.
	for i, s := range cycle {
		if s.mode == m.Target && s.name == m.TargetName {
			return i
		}
	}
	return -1
}

// applyTarget sets the active input target and the matching status line.
func (m *Model) applyTarget(s targetStep) {
	m.Target = s.mode
	m.TargetName = s.name
	switch s.mode {
	case TargetPersonality:
		m.Status = "input targets personality " + m.personalityLabel(s.name)
	case TargetCategory:
		m.Status = "input targets category " + m.categoryLabel(s.name)
	case TargetFocused:
		m.Status = "input targets " + m.focusedName()
	default:
		m.Status = "input targets all agents"
	}
}

// toggleTarget advances the input target one step through targetCycle (wrapping).
// Advancing from an off-cycle target lands on broadcast-to-all.
func (m *Model) toggleTarget() {
	cycle := m.targetCycle()
	next := (m.currentTargetIndex(cycle) + 1) % len(cycle)
	m.applyTarget(cycle[next])
}

func (m *Model) toggleZoom() {
	m.Zoomed = !m.Zoomed
	m.resizeAgents()
	if m.Zoomed {
		m.Status = "zoomed " + m.focusedName() + " (Ctrl+F to restore)"
		return
	}
	m.Status = "restored grid"
}

func (m *Model) openOverview() {
	m.ScreenMode = ScreenOverview
	m.InputMode = InputComposer
	m.PromptInput = ""
	m.OverviewIndex = m.overviewPositionForAgent(m.FocusedIndex)
	m.Status = "overview"
}

func (m Model) overviewIndexes() []int {
	indexes := make([]int, len(m.Agents))
	for i := range m.Agents {
		indexes[i] = i
	}
	return indexes
}

func (m Model) overviewPositionForAgent(agentIndex int) int {
	indexes := m.overviewIndexes()
	for pos, idx := range indexes {
		if idx == agentIndex {
			return pos
		}
	}
	return 0
}

func (m Model) focusedName() string {
	if len(m.Agents) == 0 || m.FocusedIndex < 0 || m.FocusedIndex >= len(m.Agents) {
		return "agent"
	}
	return m.Agents[m.FocusedIndex].Session.Name
}

func (m Model) focusedSession() *agent.Session {
	if len(m.Agents) == 0 || m.FocusedIndex < 0 || m.FocusedIndex >= len(m.Agents) {
		return nil
	}
	return m.Agents[m.FocusedIndex].Session
}

func (m Model) agentExists(name string) bool {
	for _, v := range m.Agents {
		if strings.EqualFold(v.Session.Name, name) {
			return true
		}
	}
	return false
}

// ---- in-chat orchestration ----

func (m Model) saveTranscripts() error {
	if m.Store == nil {
		return nil
	}
	for _, view := range m.Agents {
		content := view.transcript()
		if err := m.Store.SaveTranscript(view.Session.Name, content); err != nil {
			return err
		}
	}
	return nil
}

func (v *agentView) transcript() string {
	lines := make([]string, 0, len(v.Lines)+1)
	lines = append(lines, v.Lines...)
	if v.Partial != "" {
		lines = append(lines, v.Partial)
	}
	return strings.Join(lines, "\n")
}

func (m Model) terminateAgents() {
	for _, view := range m.Agents {
		_ = view.Session.Terminate()
	}
	// The integrated editor session is not in m.Agents; tear it down too so no
	// editor process is leaked on quit.
	if m.editorView != nil {
		_ = m.editorView.Session.Terminate()
	}
}

func (m *Model) focusByName(name string) {
	for i, view := range m.Agents {
		if strings.EqualFold(view.Session.Name, name) {
			m.FocusedIndex = i
			m.ensurePageForFocus()
			m.Status = "focused " + view.Session.Name
			return
		}
	}
	m.Status = "unknown agent: " + name
}

func (m Model) findAgent(name string) *agentView {
	for _, view := range m.Agents {
		if view.Session.Name == name {
			return view
		}
	}
	return nil
}

func (m Model) findAgentForMessage(name string, session *agent.Session) *agentView {
	if session == nil {
		return m.findAgent(name)
	}
	for _, view := range m.Agents {
		if view.Session == session {
			return view
		}
	}
	return nil
}
