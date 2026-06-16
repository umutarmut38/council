package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
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

// chromeLines is the dynamic chrome height: the phase rail adds a third
// header line while an orchestration run is active. The command/file palettes
// are deliberately NOT counted — they overlay the bottom of the body (View
// trims the covered rows) so open/close never reflows the agent panes.
func (m Model) chromeLines() int {
	c := chromeHeight
	if m.progress != nil {
		c++
	}
	return c
}

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

func (m *Model) focusNext() {
	indexes := m.displayAgentIndexes()
	if len(indexes) == 0 {
		return
	}
	pos := m.displayPositionForAgent(m.FocusedIndex)
	if pos < 0 {
		pos = -1
	}
	m.FocusedIndex = indexes[(pos+1)%len(indexes)]
	m.ensurePageForFocus()
	m.Status = "focused " + m.Agents[m.FocusedIndex].Session.Name
}

func (m *Model) focusPrevious() {
	indexes := m.displayAgentIndexes()
	if len(indexes) == 0 {
		return
	}
	pos := m.displayPositionForAgent(m.FocusedIndex)
	if pos < 0 {
		pos = 0
	}
	pos--
	if pos < 0 {
		pos = len(indexes) - 1
	}
	m.FocusedIndex = indexes[pos]
	m.ensurePageForFocus()
	m.Status = "focused " + m.Agents[m.FocusedIndex].Session.Name
}

// gridDims sizes the pane grid. With the adaptive layout (the default), the
// grid follows the number of visible panes — one pane gets the whole screen,
// two get full-height columns, three or four a 2x2 — so worker-only or
// reviewer-only phases don't waste half the terminal. Larger rosters and
// locked layouts use the configured page_rows x page_cols.
func (m Model) gridDims() (rows int, cols int) {
	rows = m.Config.UI.PageRows
	cols = m.Config.UI.PageCols
	if rows <= 0 {
		rows = 2
	}
	if cols <= 0 {
		cols = 2
	}
	if m.adaptiveLayout() {
		switch n := len(m.displayAgentIndexes()); {
		case n <= 1:
			return 1, 1
		case n == 2:
			return 1, 2
		case n <= 4:
			return 2, 2
		}
	}
	return rows, cols
}

// adaptiveLayout reports whether the grid adapts to the visible pane count.
func (m Model) adaptiveLayout() bool {
	return m.Config.UI.AdaptiveEnabled() && !m.layoutLocked
}

func (m Model) pageSize() int {
	rows, cols := m.gridDims()
	size := rows * cols
	if size <= 0 {
		return 4
	}
	return size
}

func (m Model) pageCount() int {
	count := len(m.displayAgentIndexes())
	if count == 0 {
		return 1
	}
	size := m.pageSize()
	return (count + size - 1) / size
}

func (m Model) pageForIndex(index int) int {
	pos := m.displayPositionForAgent(index)
	if pos < 0 {
		return 0
	}
	page := pos / m.pageSize()
	if page >= m.pageCount() {
		return m.pageCount() - 1
	}
	return page
}

func (m Model) pageBounds() (start int, end int) {
	indexes := m.displayAgentIndexes()
	size := m.pageSize()
	start = m.PageIndex * size
	if start >= len(indexes) {
		start = 0
	}
	end = start + size
	if end > len(indexes) {
		end = len(indexes)
	}
	return start, end
}

func (m Model) visibleAgentIndexes() []int {
	start, end := m.pageBounds()
	displayed := m.displayAgentIndexes()
	indexes := make([]int, 0, end-start)
	for i := start; i < end; i++ {
		indexes = append(indexes, displayed[i])
	}
	return indexes
}

func (m Model) displayAgentIndexes() []int {
	indexes := make([]int, 0, len(m.Agents))
	for i, view := range m.Agents {
		if m.agentIsDisplayed(view.Session.Name) {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func (m Model) displayPositionForAgent(agentIndex int) int {
	for pos, idx := range m.displayAgentIndexes() {
		if idx == agentIndex {
			return pos
		}
	}
	return -1
}

func (m Model) agentIsDisplayed(agentName string) bool {
	if len(m.DisplayPersonalities) == 0 {
		return true
	}
	personality, _, ok := m.Config.PersonalityForAgent(agentName)
	return ok && m.DisplayPersonalities[personality]
}

func (m *Model) ensurePageForFocus() {
	indexes := m.displayAgentIndexes()
	if len(indexes) == 0 {
		m.PageIndex = 0
		return
	}
	if m.displayPositionForAgent(m.FocusedIndex) < 0 {
		m.FocusedIndex = indexes[0]
	}
	m.PageIndex = m.pageForIndex(m.FocusedIndex)
}

func (m *Model) nextPage() {
	indexes := m.displayAgentIndexes()
	if len(indexes) == 0 {
		return
	}
	m.PageIndex = (m.PageIndex + 1) % m.pageCount()
	pos := m.PageIndex * m.pageSize()
	if pos >= len(indexes) {
		pos = len(indexes) - 1
	}
	m.FocusedIndex = indexes[pos]
	m.resizeAgents()
	m.Status = fmt.Sprintf("page %d/%d", m.PageIndex+1, m.pageCount())
}

func (m *Model) previousPage() {
	indexes := m.displayAgentIndexes()
	if len(indexes) == 0 {
		return
	}
	m.PageIndex--
	if m.PageIndex < 0 {
		m.PageIndex = m.pageCount() - 1
	}
	pos := m.PageIndex * m.pageSize()
	if pos >= len(indexes) {
		pos = len(indexes) - 1
	}
	m.FocusedIndex = indexes[pos]
	m.resizeAgents()
	m.Status = fmt.Sprintf("page %d/%d", m.PageIndex+1, m.pageCount())
}

func (m *Model) gotoPage(page int) {
	if page < 0 {
		page = 0
	}
	if page >= m.pageCount() {
		page = m.pageCount() - 1
	}
	indexes := m.displayAgentIndexes()
	if len(indexes) == 0 {
		return
	}
	m.PageIndex = page
	pos := m.PageIndex * m.pageSize()
	if pos >= len(indexes) {
		pos = len(indexes) - 1
	}
	m.FocusedIndex = indexes[pos]
	m.resizeAgents()
	m.Status = fmt.Sprintf("page %d/%d", m.PageIndex+1, m.pageCount())
}

func (m *Model) setDisplayedPersonalities(names []string) {
	next := map[string]bool{}
	for _, name := range names {
		next[name] = true
	}
	if !m.anyAgentsForPersonalities(next) {
		m.Status = "no agents match those personalities"
		return
	}
	m.DisplayPersonalities = next
	m.ensurePageForFocus()
	m.resizeAgents()
	m.Status = "showing " + strings.Join(m.personalityLabels(names), ", ")
}

func (m Model) anyAgentsForPersonalities(personalities map[string]bool) bool {
	for _, view := range m.Agents {
		personality, _, ok := m.Config.PersonalityForAgent(view.Session.Name)
		if ok && personalities[personality] {
			return true
		}
	}
	return false
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

func (m Model) resolvePersonality(value string) (string, bool) {
	value = normalizeLookup(value)
	for name, personality := range m.Config.Personalities {
		if normalizeLookup(name) == value || normalizeLookup(personality.Label) == value {
			return name, true
		}
	}
	return "", false
}

func (m Model) resolvePersonalityList(value string) ([]string, bool) {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == ';'
	})
	names := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, field := range fields {
		name, ok := m.resolvePersonality(field)
		if !ok {
			return nil, false
		}
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	return names, len(names) > 0
}

func (m Model) resolveCategory(value string) (string, bool) {
	value = normalizeLookup(value)
	for name, category := range m.Config.PersonalityCategories {
		if normalizeLookup(name) == value || normalizeLookup(category.Label) == value {
			return name, true
		}
	}
	return "", false
}

func normalizeLookup(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, " ", "-")
	return value
}

func (m Model) personalityLabel(name string) string {
	if personality, ok := m.Config.Personalities[name]; ok {
		return personalityLabel(name, personality)
	}
	return name
}

func personalityLabel(name string, personality config.PersonalityConfig) string {
	if personality.Label != "" {
		return personality.Label
	}
	return name
}

func (m Model) personalityLabels(names []string) []string {
	labels := make([]string, 0, len(names))
	for _, name := range names {
		labels = append(labels, m.personalityLabel(name))
	}
	return labels
}

func (m Model) categoryLabel(name string) string {
	if category, ok := m.Config.PersonalityCategories[name]; ok && category.Label != "" {
		return category.Label
	}
	return name
}

func (m Model) personalitiesForCategory(categoryName string) []string {
	names := make([]string, 0)
	for name, personality := range m.Config.Personalities {
		if personality.Category == categoryName {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		left := m.Config.Personalities[names[i]]
		right := m.Config.Personalities[names[j]]
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		return names[i] < names[j]
	})
	return names
}

func (m *Model) showPersonalityForAgent(agentName string) {
	personality, _, ok := m.Config.PersonalityForAgent(agentName)
	if !ok {
		return
	}
	if len(m.DisplayPersonalities) == 0 {
		return
	}
	m.DisplayPersonalities[personality] = true
}

func (m *Model) toggleDisplayPersonalityForAgent(agentName string) {
	personality, cfg, ok := m.Config.PersonalityForAgent(agentName)
	if !ok {
		m.Status = agentName + " has no personality"
		return
	}
	if len(m.DisplayPersonalities) == 0 {
		m.DisplayPersonalities = m.allUsedPersonalities()
	}
	if m.DisplayPersonalities[personality] && len(m.DisplayPersonalities) == 1 {
		m.Status = "at least one personality must stay visible"
		return
	}
	if m.DisplayPersonalities[personality] {
		delete(m.DisplayPersonalities, personality)
		m.Status = "hid " + personalityLabel(personality, cfg)
	} else {
		m.DisplayPersonalities[personality] = true
		m.Status = "showing " + personalityLabel(personality, cfg)
	}
	m.ensurePageForFocus()
	m.resizeAgents()
}

func (m Model) allUsedPersonalities() map[string]bool {
	out := map[string]bool{}
	for _, view := range m.Agents {
		if personality, _, ok := m.Config.PersonalityForAgent(view.Session.Name); ok {
			out[personality] = true
		}
	}
	return out
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

type settingItem struct {
	name  string
	value string
}

func (m Model) settingsItems() []settingItem {
	adaptive := "on"
	if !m.adaptiveLayout() {
		adaptive = "off"
		if m.layoutLocked {
			adaptive = "off (locked by manual rows/cols)"
		}
	}
	return []settingItem{
		{name: "adaptive grid", value: adaptive},
		{name: "page rows", value: strconv.Itoa(m.Config.UI.PageRows)},
		{name: "page cols", value: strconv.Itoa(m.Config.UI.PageCols)},
		{name: "group by", value: m.groupByLabel()},
	}
}

// layoutPreview describes the grid the current settings produce, e.g.
// "2 agents -> 1 row x 2 cols, full height".
func (m Model) layoutPreview() string {
	n := len(m.displayAgentIndexes())
	rows, cols := m.gridDims()
	desc := fmt.Sprintf("%d agent(s) -> %d row(s) x %d col(s)", n, rows, cols)
	if rows == 1 {
		desc += ", full height"
	}
	if n > rows*cols {
		desc += fmt.Sprintf(", %d page(s)", m.pageCount())
	}
	if m.phase != "" {
		desc += fmt.Sprintf(" · %s participants: %d", m.phase, len(m.watching))
	}
	return desc
}

func (m *Model) adjustSetting(delta int) {
	focused := m.focusedName()
	switch m.SettingsIndex {
	case 0:
		// Toggling adaptive also clears a manual lock.
		enabled := !m.adaptiveLayout()
		m.Config.UI.AdaptiveGrid = &enabled
		m.layoutLocked = false
	case 1:
		m.Config.UI.PageRows += delta
		if m.Config.UI.PageRows < 1 {
			m.Config.UI.PageRows = 1
		}
		if m.Config.UI.PageRows > 6 {
			m.Config.UI.PageRows = 6
		}
		// Manually sizing the grid locks the adaptive layout for this session.
		m.layoutLocked = true
	case 2:
		m.Config.UI.PageCols += delta
		if m.Config.UI.PageCols < 1 {
			m.Config.UI.PageCols = 1
		}
		if m.Config.UI.PageCols > 6 {
			m.Config.UI.PageCols = 6
		}
		m.layoutLocked = true
	case 3:
		options := []string{"none", "personality", "category"}
		current := 0
		for i, opt := range options {
			if opt == m.groupByLabel() {
				current = i
				break
			}
		}
		current = (current + delta) % len(options)
		if current < 0 {
			current = len(options) - 1
		}
		m.Config.UI.GroupBy = options[current]
		m.sortAgents()
		m.focusByName(focused)
	}
	m.ensurePageForFocus()
	m.resizeAgents()
	m.Status = "settings updated"
}

func (m *Model) sortAgents() {
	if len(m.Agents) < 2 || m.groupByLabel() == "none" {
		return
	}
	sort.SliceStable(m.Agents, func(i, j int) bool {
		left := m.agentSortKey(m.Agents[i].Session.Name)
		right := m.agentSortKey(m.Agents[j].Session.Name)
		for n := range left {
			if left[n] == right[n] {
				continue
			}
			return left[n] < right[n]
		}
		return m.Agents[i].Session.Name < m.Agents[j].Session.Name
	})
}

func (m Model) agentSortKey(name string) [4]string {
	group := m.groupByLabel()
	personalityName, personality, hasPersonality := m.Config.PersonalityForAgent(name)
	categoryName, category, hasCategory := m.Config.CategoryForPersonality(personalityName)
	groupOrder := "999999"
	groupName := "ungrouped"
	switch group {
	case "category":
		if hasCategory {
			groupOrder = fmt.Sprintf("%06d", category.Order)
			groupName = categoryName
			if category.Label != "" {
				groupName = category.Label
			}
		}
	case "personality":
		if hasPersonality {
			groupOrder = fmt.Sprintf("%06d", personality.Order)
			groupName = personalityName
			if personality.Label != "" {
				groupName = personality.Label
			}
		}
	}
	personalityOrder := "999999"
	if hasPersonality {
		personalityOrder = fmt.Sprintf("%06d", personality.Order)
	}
	return [4]string{groupOrder, strings.ToLower(groupName), personalityOrder, strings.ToLower(name)}
}

func (m Model) agentPersonalityLabel(name string) string {
	personalityName, personality, ok := m.Config.PersonalityForAgent(name)
	if !ok {
		return "no personality"
	}
	if personality.Label != "" {
		return personality.Label
	}
	return personalityName
}

func (m Model) agentGroupLabel(name string) string {
	switch m.groupByLabel() {
	case "personality":
		return m.agentPersonalityLabel(name)
	case "category":
		personalityName, _, ok := m.Config.PersonalityForAgent(name)
		if !ok {
			return "Ungrouped"
		}
		categoryName, category, ok := m.Config.CategoryForPersonality(personalityName)
		if !ok {
			return "Ungrouped"
		}
		if category.Label != "" {
			return category.Label
		}
		return categoryName
	default:
		return "Agents"
	}
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

func (m *Model) resizeAgents() {
	if len(m.Agents) == 0 || m.Width == 0 || m.Height == 0 {
		return
	}

	if m.Zoomed {
		// A zoomed pane fills the whole body. Size every agent to it so the
		// focused one renders at full size and switching focus is instant.
		innerWidth := m.Width - 2
		innerHeight := (m.Height - m.chromeLines()) - 2
		if innerWidth < 1 {
			innerWidth = 1
		}
		if innerHeight < 1 {
			innerHeight = 1
		}
		for _, view := range m.Agents {
			view.setScreenSize(innerWidth, innerHeight)
			_ = view.Session.Resize(innerWidth, innerHeight)
		}
		return
	}

	rows, cols := m.gridDims()
	widths := distribute(m.Width, cols)
	heights := distribute(m.Height-m.chromeLines(), rows)
	indexes := m.visibleAgentIndexes()
	defaultWidth := widths[0] - 2
	defaultHeight := heights[0] - 2
	if defaultWidth < 1 {
		defaultWidth = 1
	}
	if defaultHeight < 1 {
		defaultHeight = 1
	}
	for _, view := range m.Agents {
		view.setScreenSize(defaultWidth, defaultHeight)
		_ = view.Session.Resize(defaultWidth, defaultHeight)
	}
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			pos := row*cols + col
			if pos >= len(indexes) {
				continue
			}
			index := indexes[pos]
			innerWidth := widths[col] - 2
			innerHeight := heights[row] - 2
			if innerWidth < 1 {
				innerWidth = 1
			}
			if innerHeight < 1 {
				innerHeight = 1
			}
			m.Agents[index].setScreenSize(innerWidth, innerHeight)
			_ = m.Agents[index].Session.Resize(innerWidth, innerHeight)
		}
	}
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
