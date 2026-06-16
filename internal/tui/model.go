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

	Screen     [][]screenCell
	Width      int
	Height     int
	CursorRow  int
	CursorCol  int
	SavedRow   int
	SavedCol   int
	ScrollTop  int
	ScrollBot  int
	CurrentSGR string

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
	layoutLocked bool         // user adjusted rows/cols in settings: adaptive off
	// attentionCheckPending debounces the delayed approval-prompt re-check.
	attentionCheckPending bool

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
	headerHeight = 2
	// footer = suggestion line + input box (top border, content, bottom border)
	footerHeight = 4
	chromeHeight = headerHeight + footerHeight
)

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
	if store != nil {
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
	}
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
		if !m.agentsStarted && m.launch != nil {
			m.agentsStarted = true
			return m, func() tea.Msg {
				m.startAll()
				return nil
			}
		}
		return m, nil
	case AgentOutputMsg:
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
			m.sendAll(string(msg))
			m.initialPromptSent = true
			m.Status = "broadcast initial prompt"
		}
		return m, nil
	case initialAgentPromptsMsg:
		if !m.initialPromptSent {
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
	case reviewReadyMsg:
		return m.handleReviewReady(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) View() string {
	if m.Width == 0 || m.Height == 0 {
		return "starting council..."
	}
	if m.Width < 48 || m.Height < 14 {
		return "Window too small for council. Resize to at least 48x14."
	}

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
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
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

	if len(v.Lines) > maxScrollback {
		v.Lines = v.Lines[len(v.Lines)-maxScrollback:]
	}
}

func (m *Model) toggleTarget() {
	if m.Target == TargetAll {
		m.Target = TargetFocused
		m.TargetName = ""
		m.Status = "input targets " + m.focusedName()
		return
	}
	m.Target = TargetAll
	m.TargetName = ""
	m.Status = "input targets all agents"
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
