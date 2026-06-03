package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/orchestrate"
	runstore "github.com/umutarmut38/council/internal/session"
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
	fileSuggestHidden    string
	OverviewIndex        int
	SettingsIndex        int
	Runs                 []orchestrate.RunSummary
	RunIndex             int
	DisplayPersonalities map[string]bool

	// orchestration
	orch         *orchestrate.Controller
	phase        string            // "", "plan", "vote", "build"
	watching     map[string]string // agent -> artifact path watched this phase
	pendingBuild map[string]string // build prompts staged by /build, sent by /start-build
}

var (
	csiPattern     = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	oscPattern     = regexp.MustCompile(`\x1b\][^\a]*(\a|\x1b\\)`)
	controlPattern = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`)

	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	faintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	statusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	focusStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	borderStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	inputStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("229"))
	suggestStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("147"))
	headingStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
)

const (
	headerHeight = 2
	// footer = suggestion line + input box (top border, content, bottom border)
	footerHeight = 4
	chromeHeight = headerHeight + footerHeight
)

type commandInfo struct {
	Name string
	Args string
	Desc string
}

// commands drives both /help text and the live suggestion line. Keep it in
// sync with the cases handled in handleCommand.
var commands = []commandInfo{
	{"all", "msg", "send to every agent"},
	{"send", "agent msg", "send to one agent"},
	{"direct", "[agent]", "type straight into a pane"},
	{"zoom", "[agent]", "fullscreen the focused pane"},
	{"page", "next|prev|n", "switch pane pages"},
	{"overview", "", "show all agents"},
	{"settings", "", "adjust layout for this session"},
	{"runs", "", "browse previous runs"},
	{"resume", "[run]", "resume an older run"},
	{"focus", "agent", "focus a pane"},
	{"target", "all|focus|personality|category", "scope messages AND phases (plan/vote/build)"},
	{"show", "all|personality|category", "choose displayed personalities"},
	{"hide", "personality", "hide a personality"},
	{"clear", "[agent]", "clear pane output"},
	{"save", "", "save transcripts"},
	{"plan", "<issue>", "council: each agent drafts a plan"},
	{"vote", "", "council: agents rank the plans"},
	{"build", "", "council: stage winning plan in worktrees (no start)"},
	{"start-build", "", "council: send the build prompt staged by /build"},
	{"review", "", "council: check + vote the best build"},
	{"adopt", "[agent]", "council: apply a build as a patch (winner, or named)"},
	{"finish", "", "force-collect the current phase now"},
	{"status", "", "show the active run/phase"},
	{"clean", "", "remove council worktrees"},
	{"help", "", "list commands"},
	{"quit", "", "quit council"},
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
		}
		return m, nil
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
		m.Status = m.phase + " prompt sent — agents working"
		return m, func() tea.Msg {
			m.sendPrompts(prompts)
			return nil
		}
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
	bodyHeight := m.Height - chromeHeight
	if bodyHeight < 6 {
		bodyHeight = 6
	}

	body := m.renderBody(bodyHeight)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.ScreenMode {
	case ScreenOverview:
		return m.handleOverviewKey(msg)
	case ScreenSettings:
		return m.handleSettingsKey(msg)
	case ScreenRuns:
		return m.handleRunsKey(msg)
	}

	if m.InputMode == InputDirect {
		return m.handleDirectKey(msg)
	}

	switch msg.String() {
	case "tab":
		if m.completeCommand() {
			return m, nil
		}
		m.focusNext()
		return m, nil
	case "shift+tab":
		m.focusPrevious()
		return m, nil
	case "ctrl+b":
		m.toggleTarget()
		return m, nil
	case "ctrl+f":
		m.toggleZoom()
		return m, nil
	case "ctrl+g":
		m.openOverview()
		return m, nil
	case "ctrl+n":
		m.nextPage()
		return m, nil
	case "ctrl+p":
		m.previousPage()
		return m, nil
	case "f2", "ctrl+o":
		m.InputMode = InputDirect
		m.PromptInput = ""
		m.Status = "direct input to " + m.focusedName()
		return m, nil
	case "ctrl+s":
		if err := m.saveTranscripts(); err != nil {
			m.Status = "save failed: " + err.Error()
		} else if m.Store != nil {
			m.Status = "saved transcripts to " + m.Store.TranscriptDir
		} else {
			m.Status = "saved transcripts"
		}
		return m, nil
	case "ctrl+x":
		m.terminateAgents()
		return m, tea.Quit
	case "ctrl+q":
		m.Status = "quit is Ctrl+X"
		return m, nil
	case "ctrl+u":
		m.PromptInput = ""
		m.Status = "input cleared"
		return m, nil
	case "ctrl+c":
		if m.PromptInput != "" {
			m.PromptInput = ""
			m.Status = "input cleared"
			return m, nil
		}
		if session := m.focusedSession(); session != nil {
			_ = session.WriteString("\x03")
			m.Status = "sent ctrl+c to " + session.Name
		}
		return m, nil
	case "ctrl+d":
		if m.PromptInput == "" {
			if session := m.focusedSession(); session != nil {
				_ = session.WriteString("\x04")
				m.Status = "sent ctrl+d to " + session.Name
			}
		}
		return m, nil
	case "enter":
		if m.acceptFileSuggestion() {
			return m, nil
		}
		return m, m.submitInput()
	case "up":
		if m.moveFileSuggestion(-1) {
			return m, nil
		}
		return m, nil
	case "down":
		if m.moveFileSuggestion(1) {
			return m, nil
		}
		return m, nil
	case "backspace":
		m.PromptInput = dropLastRune(m.PromptInput)
		m.FileSuggestIndex = 0
		m.fileSuggestHidden = ""
		return m, nil
	case "esc":
		if token, ok := m.activeFileRefToken(); ok {
			m.fileSuggestHidden = token
			return m, nil
		}
		m.PromptInput = ""
		return m, nil
	default:
		if len(msg.Runes) > 0 {
			m.PromptInput += string(msg.Runes)
			m.FileSuggestIndex = 0
			m.fileSuggestHidden = ""
		}
		return m, nil
	}
}

func (m Model) handleDirectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "f2", "ctrl+o":
		m.InputMode = InputComposer
		m.Status = "composer mode"
		return m, nil
	case "ctrl+x":
		m.terminateAgents()
		return m, tea.Quit
	}

	session := m.focusedSession()
	if session == nil {
		return m, nil
	}

	value := keyToPTY(msg, session.Config.Terminal.SubmitSequence)
	if value == "" {
		return m, nil
	}
	if msg.String() == "enter" {
		value += optionalSequence(session.Config.Terminal.AfterSubmitSequence)
	}
	if err := session.WriteString(value); err != nil {
		m.Status = err.Error()
	}
	return m, nil
}

func (m Model) renderHeader() string {
	names := make([]string, 0, len(m.Agents))
	for _, view := range m.Agents {
		name := view.Session.Name
		if view.Session.Done {
			name += " done"
		}
		names = append(names, name)
	}

	line := "Council | Agents: " + strings.Join(names, ", ")
	if m.Store != nil {
		line += " | " + m.Store.RunDir
	}
	page := fmt.Sprintf("Page %d/%d · agents %s · grouped by %s", m.PageIndex+1, m.pageCount(), m.pageRangeLabel(), m.groupByLabel())
	if filter := m.displayFilterLabel(); filter != "" {
		page += " · showing " + filter
	}
	if m.ScreenMode != ScreenPanes {
		page = strings.ToUpper(m.screenModeName()) + " · " + page
	}
	return titleStyle.Render(fitText(line, m.Width)) + "\n" + statusStyle.Render(fitText(page+" · "+m.Status, m.Width))
}

func (m Model) renderFooter() string {
	if m.ScreenMode != ScreenPanes {
		hint := "Esc back"
		switch m.ScreenMode {
		case ScreenOverview:
			hint = "Overview: ↑/↓ select · Space show/hide personality · Enter focus · Esc back"
		case ScreenSettings:
			hint = "Settings: ↑/↓ select · ←/→ change · Esc back"
		case ScreenRuns:
			hint = "Runs: ↑/↓ select · Enter resume · Esc back"
		}
		return strings.Join([]string{
			suggestStyle.Render(fitText(hint, m.Width)),
			inputBoxTop(m.screenModeName(), m.Width),
			inputBoxContent(m.Status, m.Width),
			inputBoxBottom(m.Width),
		}, "\n")
	}

	if m.InputMode == InputDirect {
		hint := suggestStyle.Render(fitText("DIRECT MODE — keystrokes go straight to the pane. Esc/F2 returns to the composer.", m.Width))
		label := "direct: " + m.focusedName()
		content := "keys → " + m.focusedName()
		return strings.Join([]string{hint, inputBoxTop(label, m.Width), inputBoxContent(content, m.Width), inputBoxBottom(m.Width)}, "\n")
	}

	label := m.targetLabel()
	if m.Zoomed {
		label = "[zoom] " + label
	}

	prompt := m.targetPrompt()
	content := prompt + " > " + m.PromptInput + "_"

	return strings.Join([]string{
		m.suggestionLine(),
		inputBoxTop(label, m.Width),
		inputBoxContent(content, m.Width),
		inputBoxBottom(m.Width),
	}, "\n")
}

// suggestionLine shows matching /commands while one is being typed, and the key
// hints otherwise.
func (m Model) suggestionLine() string {
	if text, ok := m.fileSuggestionLine(); ok {
		return suggestStyle.Render(fitText(text, m.Width))
	}

	if strings.HasPrefix(m.PromptInput, "/") {
		prefix := strings.ToLower(strings.TrimPrefix(strings.Fields(m.PromptInput + " ")[0], "/"))
		parts := make([]string, 0, len(commands))
		for _, c := range commands {
			if strings.HasPrefix(c.Name, prefix) {
				entry := "/" + c.Name
				if c.Args != "" {
					entry += " " + c.Args
				}
				parts = append(parts, entry+" — "+c.Desc)
			}
		}
		text := strings.Join(parts, "   ")
		if text == "" {
			text = "no matching command"
		}
		return suggestStyle.Render(fitText(text, m.Width))
	}

	help := "Enter send | Ctrl+G overview | F2 direct | Ctrl+B target | Ctrl+F zoom | Ctrl+N/P page | Tab focus | @file"
	return faintStyle.Render(fitText(help, m.Width))
}

func (m Model) targetLabel() string {
	switch m.Target {
	case TargetFocused:
		return "→ " + m.focusedName()
	case TargetPersonality:
		return "personality: " + m.personalityLabel(m.TargetName)
	case TargetCategory:
		return "category: " + m.categoryLabel(m.TargetName)
	default:
		return "broadcast to all"
	}
}

func (m Model) targetPrompt() string {
	switch m.Target {
	case TargetFocused:
		return m.focusedName()
	case TargetPersonality:
		return "personality:" + m.personalityLabel(m.TargetName)
	case TargetCategory:
		return "category:" + m.categoryLabel(m.TargetName)
	default:
		return "all"
	}
}

func (m Model) activeFileRefToken() (string, bool) {
	start, query, ok := m.activeFileRef()
	if !ok {
		return "", false
	}
	return m.PromptInput[start : start+1+len(query)], true
}

func (m Model) activeFileRef() (start int, query string, ok bool) {
	idx := strings.LastIndex(m.PromptInput, "@")
	if idx < 0 {
		return 0, "", false
	}
	if idx > 0 {
		prev, _ := utf8.DecodeLastRuneInString(m.PromptInput[:idx])
		if prev != 0 && !isRefBoundary(prev) {
			return 0, "", false
		}
	}
	query = m.PromptInput[idx+1:]
	if strings.ContainsAny(query, " \t\r\n") {
		return 0, "", false
	}
	if idx == 0 && query != "" && !strings.ContainsAny(query, "./") && m.agentExists(query) {
		return 0, "", false
	}
	token := m.PromptInput[idx:]
	if token != "" && token == m.fileSuggestHidden {
		return 0, "", false
	}
	return idx, query, true
}

func isRefBoundary(r rune) bool {
	return r == ' ' || r == '\t' || r == '(' || r == '[' || r == '{' || r == ':' || r == ','
}

func (m Model) fileSuggestionMatches() []string {
	_, query, ok := m.activeFileRef()
	if !ok {
		return nil
	}
	query = strings.ToLower(strings.TrimSpace(query))
	prefix := make([]string, 0)
	contains := make([]string, 0)
	for _, choice := range m.FileChoices {
		lower := strings.ToLower(choice)
		switch {
		case query == "", strings.HasPrefix(lower, query):
			prefix = append(prefix, choice)
		case strings.Contains(lower, query):
			contains = append(contains, choice)
		}
	}
	matches := append(prefix, contains...)
	if len(matches) > 8 {
		return matches[:8]
	}
	return matches
}

func (m Model) fileSuggestionLine() (string, bool) {
	matches := m.fileSuggestionMatches()
	if len(matches) == 0 {
		if _, _, ok := m.activeFileRef(); ok {
			return "@file: no matches", true
		}
		return "", false
	}
	index := m.FileSuggestIndex
	if index < 0 {
		index = 0
	}
	if index >= len(matches) {
		index = len(matches) - 1
	}
	parts := make([]string, 0, len(matches))
	for i, match := range matches {
		if i == index {
			parts = append(parts, "> "+match)
		} else {
			parts = append(parts, match)
		}
	}
	return fmt.Sprintf("@file %d/%d · ↑/↓ choose · Enter insert · Esc close   %s", index+1, len(matches), strings.Join(parts, "   ")), true
}

func (m *Model) moveFileSuggestion(delta int) bool {
	matches := m.fileSuggestionMatches()
	if len(matches) == 0 {
		return false
	}
	m.FileSuggestIndex = (m.FileSuggestIndex + delta) % len(matches)
	if m.FileSuggestIndex < 0 {
		m.FileSuggestIndex = len(matches) - 1
	}
	return true
}

func (m *Model) acceptFileSuggestion() bool {
	start, query, ok := m.activeFileRef()
	if !ok {
		return false
	}
	matches := m.fileSuggestionMatches()
	if len(matches) == 0 {
		return false
	}
	if m.FileSuggestIndex < 0 {
		m.FileSuggestIndex = 0
	}
	if m.FileSuggestIndex >= len(matches) {
		m.FileSuggestIndex = len(matches) - 1
	}
	end := start + 1 + len(query)
	replacement := "@" + matches[m.FileSuggestIndex]
	if end == len(m.PromptInput) {
		replacement += " "
	}
	m.PromptInput = m.PromptInput[:start] + replacement + m.PromptInput[end:]
	m.FileSuggestIndex = 0
	m.fileSuggestHidden = ""
	return true
}

func discoverFileChoices() []string {
	out, err := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard").Output()
	if err == nil {
		return cleanFileChoices(strings.Split(string(out), "\n"))
	}

	paths := make([]string, 0)
	_ = filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == "." {
			return nil
		}
		clean := filepath.ToSlash(path)
		if d.IsDir() {
			if shouldSkipFileChoice(clean, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if !shouldSkipFileChoice(clean, false) {
			paths = append(paths, strings.TrimPrefix(clean, "./"))
		}
		return nil
	})
	return cleanFileChoices(paths)
}

func cleanFileChoices(paths []string) []string {
	seen := map[string]bool{}
	choices := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(filepath.ToSlash(path))
		path = strings.TrimPrefix(path, "./")
		if path == "" || seen[path] || shouldSkipFileChoice(path, false) {
			continue
		}
		seen[path] = true
		choices = append(choices, path)
	}
	sort.Strings(choices)
	return choices
}

func shouldSkipFileChoice(path string, dir bool) bool {
	if path == "" {
		return true
	}
	parts := strings.Split(path, "/")
	for _, part := range parts {
		switch part {
		case ".git", ".council", "node_modules", "vendor", "dist", "build", "target":
			return true
		}
		if strings.HasPrefix(part, ".") && part != "." {
			switch part {
			case ".github", ".agents", ".codex":
			default:
				return true
			}
		}
	}
	return dir && strings.HasPrefix(filepath.Base(path), ".")
}

func inputBoxTop(label string, width int) string {
	if width < 2 {
		return borderStyle.Render(strings.Repeat("─", max0(width)))
	}
	inner := width - 2
	lbl := ""
	if label != "" {
		lbl = "─ " + label + " "
	}
	lbl = truncateText(lbl, inner)
	pad := inner - lipgloss.Width(lbl)
	if pad < 0 {
		pad = 0
	}
	return borderStyle.Render("╭" + lbl + strings.Repeat("─", pad) + "╮")
}

func inputBoxBottom(width int) string {
	if width < 2 {
		return borderStyle.Render(strings.Repeat("─", max0(width)))
	}
	return borderStyle.Render("╰" + strings.Repeat("─", width-2) + "╯")
}

func inputBoxContent(text string, width int) string {
	if width < 2 {
		return inputStyle.Render(fitText(text, width))
	}
	inner := width - 2
	return borderStyle.Render("│") + inputStyle.Render(fitText(" "+text, inner)) + borderStyle.Render("│")
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func (m Model) renderBody(bodyHeight int) string {
	switch m.ScreenMode {
	case ScreenOverview:
		return strings.Join(m.renderOverview(bodyHeight), "\n")
	case ScreenSettings:
		return strings.Join(m.renderSettings(bodyHeight), "\n")
	case ScreenRuns:
		return strings.Join(m.renderRuns(bodyHeight), "\n")
	default:
		return m.renderGrid(bodyHeight)
	}
}

func (m Model) renderGrid(bodyHeight int) string {
	if len(m.Agents) == 0 {
		return strings.Join(blankBlock(m.Width, bodyHeight), "\n")
	}

	if m.Zoomed {
		return strings.Join(m.renderPane(m.FocusedIndex, m.Width, bodyHeight), "\n")
	}

	rows, cols := m.gridDims()
	widths := distribute(m.Width, cols)
	heights := distribute(bodyHeight, rows)
	indexes := m.visibleAgentIndexes()

	rowViews := make([]string, 0, rows)
	for row := 0; row < rows; row++ {
		paneLines := make([][]string, 0, cols)
		for col := 0; col < cols; col++ {
			pos := row*cols + col
			if pos >= len(indexes) {
				paneLines = append(paneLines, blankBlock(widths[col], heights[row]))
				continue
			}
			paneLines = append(paneLines, m.renderPane(indexes[pos], widths[col], heights[row]))
		}
		for line := 0; line < heights[row]; line++ {
			var b strings.Builder
			for col := 0; col < cols; col++ {
				b.WriteString(paneLines[col][line])
			}
			rowViews = append(rowViews, b.String())
		}
	}

	return strings.Join(rowViews, "\n")
}

func (m Model) renderOverview(bodyHeight int) []string {
	lines := make([]string, 0, bodyHeight)
	indexes := m.overviewIndexes()
	lastGroup := ""
	for pos, idx := range indexes {
		if idx < 0 || idx >= len(m.Agents) {
			continue
		}
		view := m.Agents[idx]
		group := m.agentGroupLabel(view.Session.Name)
		if group != lastGroup {
			lines = append(lines, headingStyle.Render(fitText(group, m.Width)))
			lastGroup = group
		}
		marker := " "
		if pos == m.OverviewIndex {
			marker = ">"
		}
		state := "running"
		if view.Session.Done {
			state = "done"
		}
		visibility := "visible"
		if !m.agentIsDisplayed(view.Session.Name) {
			visibility = "hidden"
		}
		page := m.pageForIndex(idx) + 1
		label := fmt.Sprintf("%s %s · %s · %s · page %d · %s", marker, view.Session.Name, m.agentPersonalityLabel(view.Session.Name), visibility, page, state)
		lines = append(lines, fitText(label, m.Width))
	}
	if len(lines) == 0 {
		lines = append(lines, "No agents")
	}
	return fitBlock(lines, m.Width, bodyHeight)
}

func (m Model) renderSettings(bodyHeight int) []string {
	items := m.settingsItems()
	lines := make([]string, 0, bodyHeight)
	lines = append(lines, headingStyle.Render(fitText("Settings", m.Width)))
	for i, item := range items {
		marker := " "
		if i == m.SettingsIndex {
			marker = ">"
		}
		lines = append(lines, fitText(fmt.Sprintf("%s %s: %s", marker, item.name, item.value), m.Width))
	}
	lines = append(lines, "")
	lines = append(lines, faintStyle.Render(fitText("Changes apply to this session. Edit YAML to make them permanent.", m.Width)))
	return fitBlock(lines, m.Width, bodyHeight)
}

func (m Model) renderRuns(bodyHeight int) []string {
	lines := []string{headingStyle.Render(fitText("Runs", m.Width))}
	if len(m.Runs) == 0 {
		lines = append(lines, faintStyle.Render(fitText("No runs found.", m.Width)))
		return fitBlock(lines, m.Width, bodyHeight)
	}
	for i, run := range m.Runs {
		marker := " "
		if i == m.RunIndex {
			marker = ">"
		}
		parts := []string{marker + " " + run.Stamp}
		if run.Winner != "" {
			parts = append(parts, "winner "+run.Winner)
		}
		artifact := fmt.Sprintf("plans:%d votes:%d reviews:%d", len(run.Plans), len(run.Votes), len(run.Reviews))
		parts = append(parts, artifact)
		if run.PromptPreview != "" {
			parts = append(parts, run.PromptPreview)
		}
		lines = append(lines, fitText(strings.Join(parts, " · "), m.Width))
	}
	return fitBlock(lines, m.Width, bodyHeight)
}

func (m Model) renderPane(index int, width int, height int) []string {
	if width < 4 {
		width = 4
	}
	if height < 3 {
		height = 3
	}

	view := m.Agents[index]
	state := "running"
	if view.Session.StartError != nil {
		state = "failed"
	} else if view.Session.Done {
		if view.Session.ExitCode != nil {
			state = fmt.Sprintf("exit %d", *view.Session.ExitCode)
		} else {
			state = "done"
		}
	}

	if m.phase != "" && view.PhaseDone {
		state += " ✓" + m.phase
	}

	marker := " "
	style := borderStyle
	if index == m.FocusedIndex {
		marker = ">"
		style = focusStyle
	}

	title := fmt.Sprintf(" %s %s [%s] ", marker, view.Session.Name, state)
	lines := make([]string, 0, height)
	lines = append(lines, style.Render(topBorder(title, width)))

	bodyHeight := height - 2
	body := view.bodyLines(bodyHeight, width-2)
	for _, line := range body {
		lines = append(lines, style.Render("|")+fitText(line, width-2)+style.Render("|"))
	}
	lines = append(lines, style.Render("+"+strings.Repeat("-", width-2)+"+"))
	return lines
}

func (v *agentView) bodyLines(height int, width int) []string {
	if v.Session.Config.Terminal.Renderer == "transcript" {
		return v.transcriptLines(height, width)
	}
	return v.screenLines(height, width)
}

func (v *agentView) transcriptLines(height int, width int) []string {
	lines := make([]string, 0, len(v.Lines)+1)
	lines = append(lines, v.Lines...)
	if v.Partial != "" {
		lines = append(lines, v.Partial)
	}

	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		wrapped = append(wrapped, hardWrap(line, width)...)
	}
	if len(wrapped) > height {
		wrapped = wrapped[len(wrapped)-height:]
	}
	for len(wrapped) < height {
		wrapped = append([]string{""}, wrapped...)
	}
	return wrapped
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

func (m Model) gridDims() (rows int, cols int) {
	rows = m.Config.UI.PageRows
	cols = m.Config.UI.PageCols
	if rows <= 0 {
		rows = 2
	}
	if cols <= 0 {
		cols = 2
	}
	return rows, cols
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

func (m Model) pageRangeLabel() string {
	displayed := m.displayAgentIndexes()
	if len(displayed) == 0 {
		return "0 of 0"
	}
	start, end := m.pageBounds()
	return fmt.Sprintf("%d-%d of %d", start+1, end, len(displayed))
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

func (m *Model) handlePageCommand(fields []string) {
	if len(fields) < 2 {
		m.Status = fmt.Sprintf("page %d/%d", m.PageIndex+1, m.pageCount())
		return
	}
	switch strings.ToLower(fields[1]) {
	case "next", "n":
		m.nextPage()
	case "prev", "previous", "p":
		m.previousPage()
	default:
		n, err := strconv.Atoi(fields[1])
		if err != nil {
			m.Status = "usage: /page next|prev|number"
			return
		}
		m.gotoPage(n - 1)
	}
}

func (m *Model) handleTargetCommand(fields []string) {
	if len(fields) < 2 {
		m.Status = "usage: /target all|focus|personality name|category name"
		return
	}
	switch strings.ToLower(fields[1]) {
	case "all":
		m.Target = TargetAll
		m.TargetName = ""
		m.Status = "input targets all agents"
	case "focus", "focused":
		m.Target = TargetFocused
		m.TargetName = ""
		m.Status = "input targets " + m.focusedName()
	case "personality", "p":
		if len(fields) < 3 {
			m.Status = "usage: /target personality name"
			return
		}
		name, ok := m.resolvePersonality(fields[2])
		if !ok {
			m.Status = "unknown personality: " + fields[2]
			return
		}
		m.Target = TargetPersonality
		m.TargetName = name
		m.Status = "input targets personality " + m.personalityLabel(name)
	case "category", "c":
		if len(fields) < 3 {
			m.Status = "usage: /target category name"
			return
		}
		name, ok := m.resolveCategory(fields[2])
		if !ok {
			m.Status = "unknown category: " + fields[2]
			return
		}
		m.Target = TargetCategory
		m.TargetName = name
		m.Status = "input targets category " + m.categoryLabel(name)
	default:
		m.Status = "usage: /target all|focus|personality name|category name"
	}
}

func (m *Model) handleShowCommand(fields []string) {
	if len(fields) < 2 {
		m.Status = "usage: /show all|personality names|category name"
		return
	}
	switch strings.ToLower(fields[1]) {
	case "all":
		m.DisplayPersonalities = nil
		m.ensurePageForFocus()
		m.resizeAgents()
		m.Status = "showing all personalities"
	case "personality", "personalities", "p":
		if len(fields) < 3 {
			m.Status = "usage: /show personality name[,name]"
			return
		}
		names, ok := m.resolvePersonalityList(strings.Join(fields[2:], " "))
		if !ok || len(names) == 0 {
			m.Status = "no matching personalities"
			return
		}
		m.setDisplayedPersonalities(names)
	case "category", "c":
		if len(fields) < 3 {
			m.Status = "usage: /show category name"
			return
		}
		category, ok := m.resolveCategory(fields[2])
		if !ok {
			m.Status = "unknown category: " + fields[2]
			return
		}
		names := m.personalitiesForCategory(category)
		if len(names) == 0 {
			m.Status = "category has no personalities: " + fields[2]
			return
		}
		m.setDisplayedPersonalities(names)
	default:
		m.Status = "usage: /show all|personality names|category name"
	}
}

func (m *Model) handleHideCommand(fields []string) {
	if len(fields) < 2 {
		m.Status = "usage: /hide personality name"
		return
	}
	nameArg := fields[1]
	if strings.EqualFold(nameArg, "personality") && len(fields) >= 3 {
		nameArg = fields[2]
	}
	name, ok := m.resolvePersonality(nameArg)
	if !ok {
		m.Status = "unknown personality: " + nameArg
		return
	}
	if len(m.DisplayPersonalities) == 0 {
		m.DisplayPersonalities = m.allUsedPersonalities()
	}
	if len(m.DisplayPersonalities) == 1 && m.DisplayPersonalities[name] {
		m.Status = "at least one personality must stay visible"
		return
	}
	delete(m.DisplayPersonalities, name)
	m.ensurePageForFocus()
	m.resizeAgents()
	m.Status = "hid " + m.personalityLabel(name)
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

// completeCommand fills in the first matching command name when the user is
// still typing the command word (after a leading "/"). Returns true if it
// changed the input, so Tab only completes instead of switching focus then.
func (m *Model) completeCommand() bool {
	if !strings.HasPrefix(m.PromptInput, "/") || strings.Contains(m.PromptInput, " ") {
		return false
	}
	prefix := strings.ToLower(strings.TrimPrefix(m.PromptInput, "/"))
	for _, c := range commands {
		if strings.HasPrefix(c.Name, prefix) {
			m.PromptInput = "/" + c.Name + " "
			m.Status = "/" + c.Name + " — " + c.Desc
			return true
		}
	}
	return false
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

func (m Model) screenModeName() string {
	switch m.ScreenMode {
	case ScreenOverview:
		return "overview"
	case ScreenSettings:
		return "settings"
	case ScreenRuns:
		return "runs"
	default:
		return "panes"
	}
}

func (m *Model) openOverview() {
	m.ScreenMode = ScreenOverview
	m.InputMode = InputComposer
	m.PromptInput = ""
	m.OverviewIndex = m.overviewPositionForAgent(m.FocusedIndex)
	m.Status = "overview"
}

func (m Model) groupByLabel() string {
	group := strings.TrimSpace(m.Config.UI.GroupBy)
	if group == "" {
		return "none"
	}
	return group
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

func (m Model) displayFilterLabel() string {
	if len(m.DisplayPersonalities) == 0 {
		return ""
	}
	names := make([]string, 0, len(m.DisplayPersonalities))
	for name := range m.DisplayPersonalities {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		left := m.Config.Personalities[names[i]]
		right := m.Config.Personalities[names[j]]
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		return names[i] < names[j]
	})
	return strings.Join(m.personalityLabels(names), ",")
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

func (m *Model) handleOverviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	indexes := m.overviewIndexes()
	switch msg.String() {
	case "esc":
		m.ScreenMode = ScreenPanes
		m.Status = "panes"
	case "up":
		if len(indexes) > 0 {
			m.OverviewIndex--
			if m.OverviewIndex < 0 {
				m.OverviewIndex = len(indexes) - 1
			}
		}
	case "down":
		if len(indexes) > 0 {
			m.OverviewIndex = (m.OverviewIndex + 1) % len(indexes)
		}
	case "enter":
		if len(indexes) > 0 && m.OverviewIndex >= 0 && m.OverviewIndex < len(indexes) {
			m.FocusedIndex = indexes[m.OverviewIndex]
			if !m.agentIsDisplayed(m.Agents[m.FocusedIndex].Session.Name) {
				m.showPersonalityForAgent(m.Agents[m.FocusedIndex].Session.Name)
			}
			m.ensurePageForFocus()
			m.ScreenMode = ScreenPanes
			m.resizeAgents()
			m.Status = "focused " + m.focusedName()
		}
	case " ", "space":
		if len(indexes) > 0 && m.OverviewIndex >= 0 && m.OverviewIndex < len(indexes) {
			m.toggleDisplayPersonalityForAgent(m.Agents[indexes[m.OverviewIndex]].Session.Name)
		}
	case "ctrl+n":
		m.nextPage()
	case "ctrl+p":
		m.previousPage()
	}
	return *m, nil
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
	return []settingItem{
		{name: "page rows", value: strconv.Itoa(m.Config.UI.PageRows)},
		{name: "page cols", value: strconv.Itoa(m.Config.UI.PageCols)},
		{name: "group by", value: m.groupByLabel()},
	}
}

func (m *Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.settingsItems()
	switch msg.String() {
	case "esc", "enter":
		m.ScreenMode = ScreenPanes
		m.resizeAgents()
		m.Status = "panes"
	case "up":
		m.SettingsIndex--
		if m.SettingsIndex < 0 {
			m.SettingsIndex = len(items) - 1
		}
	case "down":
		m.SettingsIndex = (m.SettingsIndex + 1) % len(items)
	case "left":
		m.adjustSetting(-1)
	case "right":
		m.adjustSetting(1)
	}
	return *m, nil
}

func (m *Model) adjustSetting(delta int) {
	focused := m.focusedName()
	switch m.SettingsIndex {
	case 0:
		m.Config.UI.PageRows += delta
		if m.Config.UI.PageRows < 1 {
			m.Config.UI.PageRows = 1
		}
		if m.Config.UI.PageRows > 6 {
			m.Config.UI.PageRows = 6
		}
	case 1:
		m.Config.UI.PageCols += delta
		if m.Config.UI.PageCols < 1 {
			m.Config.UI.PageCols = 1
		}
		if m.Config.UI.PageCols > 6 {
			m.Config.UI.PageCols = 6
		}
	case 2:
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

func (m *Model) handleRunsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.ScreenMode = ScreenPanes
		m.Status = "panes"
	case "up":
		if len(m.Runs) > 0 {
			m.RunIndex--
			if m.RunIndex < 0 {
				m.RunIndex = len(m.Runs) - 1
			}
		}
	case "down":
		if len(m.Runs) > 0 {
			m.RunIndex = (m.RunIndex + 1) % len(m.Runs)
		}
	case "enter":
		if len(m.Runs) > 0 && m.RunIndex >= 0 && m.RunIndex < len(m.Runs) {
			return *m, m.resumeRun(m.Runs[m.RunIndex].Stamp)
		}
	}
	return *m, nil
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

func (m *Model) submitInput() tea.Cmd {
	text := strings.TrimSpace(m.PromptInput)
	m.PromptInput = ""
	if text == "" {
		return nil
	}

	if handled, cmd := m.handleCommand(text); handled {
		return cmd
	}
	// "@agent msg" targets a pane; "@path" anywhere else inlines a file.
	if strings.HasPrefix(text, "@") {
		first := strings.TrimPrefix(strings.Fields(text)[0], "@")
		if strings.EqualFold(first, "all") || m.agentExists(first) {
			m.handleAddressedInput(text)
			return nil
		}
	}

	text = expandRefs(text)
	switch m.Target {
	case TargetAll:
		m.sendAll(text)
		m.Status = "sent to all agents"
		return nil
	case TargetPersonality:
		count := m.sendPersonality(m.TargetName, text)
		m.Status = fmt.Sprintf("sent to %d agent(s) with personality %s", count, m.personalityLabel(m.TargetName))
		return nil
	case TargetCategory:
		count := m.sendCategory(m.TargetName, text)
		m.Status = fmt.Sprintf("sent to %d agent(s) in category %s", count, m.categoryLabel(m.TargetName))
		return nil
	default:
		if session := m.focusedSession(); session != nil {
			if err := sendLine(session, m.Config.PromptForAgent(session.Name, text)); err != nil {
				m.Status = err.Error()
				return nil
			}
			m.Status = "sent to " + session.Name
			return nil
		}
	}
	if session := m.focusedSession(); session != nil {
		if err := sendLine(session, m.Config.PromptForAgent(session.Name, text)); err != nil {
			m.Status = err.Error()
			return nil
		}
		m.Status = "sent to " + session.Name
	}
	return nil
}

func (m Model) agentExists(name string) bool {
	for _, v := range m.Agents {
		if strings.EqualFold(v.Session.Name, name) {
			return true
		}
	}
	return false
}

// expandRefs inlines @path file references relative to council's working dir.
func expandRefs(text string) string {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		cwd = "."
	}
	return orchestrate.ExpandFileRefs(text, cwd)
}

func (m *Model) handleCommand(text string) (bool, tea.Cmd) {
	if !strings.HasPrefix(text, "/") {
		return false, nil
	}

	fields := strings.Fields(text)
	if len(fields) == 0 {
		return true, nil
	}

	command := strings.TrimPrefix(strings.ToLower(fields[0]), "/")
	rest := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	switch command {
	case "all", "broadcast":
		if rest == "" {
			m.Status = "usage: /all message"
			return true, nil
		}
		m.sendAll(expandRefs(rest))
		m.Status = "sent to all agents"
	case "send":
		if len(fields) < 3 {
			m.Status = "usage: /send agent message"
			return true, nil
		}
		name := fields[1]
		message := strings.TrimSpace(strings.TrimPrefix(rest, name))
		m.sendNamed(name, message)
	case "focus":
		if len(fields) < 2 {
			m.Status = "usage: /focus agent"
			return true, nil
		}
		m.focusByName(fields[1])
	case "direct", "window":
		if len(fields) >= 2 {
			m.focusByName(fields[1])
		}
		m.InputMode = InputDirect
		m.PromptInput = ""
		m.Status = "direct input to " + m.focusedName()
	case "zoom", "full":
		if len(fields) >= 2 {
			m.focusByName(fields[1])
			if m.Zoomed {
				m.resizeAgents()
				m.Status = "zoomed " + m.focusedName()
			} else {
				m.toggleZoom()
			}
		} else {
			m.toggleZoom()
		}
	case "page":
		m.handlePageCommand(fields)
	case "overview", "agents":
		m.openOverview()
	case "settings", "prefs":
		m.ScreenMode = ScreenSettings
		m.InputMode = InputComposer
		m.PromptInput = ""
		m.Status = "settings"
	case "runs":
		m.cmdRuns()
	case "resume":
		return true, m.cmdResume(rest)
	case "target":
		m.handleTargetCommand(fields)
	case "show":
		m.handleShowCommand(fields)
	case "hide":
		m.handleHideCommand(fields)
	case "plan":
		return true, m.cmdPlan(rest)
	case "vote":
		return true, m.cmdVote()
	case "build":
		return true, m.cmdBuild()
	case "start-build", "startbuild":
		return true, m.cmdStartBuild()
	case "review":
		return true, m.cmdReview()
	case "adopt":
		return true, m.cmdAdopt(rest)
	case "finish":
		m.finishPhase()
	case "status":
		m.cmdStatus()
	case "clean":
		m.cmdClean()
	case "save":
		if err := m.saveTranscripts(); err != nil {
			m.Status = "save failed: " + err.Error()
		} else {
			m.Status = "saved transcripts"
		}
	case "clear":
		m.clearScreens(rest)
	case "quit", "exit":
		m.terminateAgents()
		m.Status = "quit with Ctrl+X"
	case "help":
		names := make([]string, 0, len(commands))
		for _, c := range commands {
			names = append(names, "/"+c.Name)
		}
		m.Status = "commands: " + strings.Join(names, " ") + "  |  @agent msg, Tab completes"
	default:
		m.Status = "unknown command: " + fields[0]
	}
	return true, nil
}

// ---- in-chat orchestration ----

func (m *Model) cmdPlan(rest string) tea.Cmd {
	if m.orch == nil {
		m.Status = "orchestration unavailable (run council inside a git repo)"
		return nil
	}
	issue := strings.TrimSpace(expandRefs(rest))
	if issue == "" {
		m.Status = "usage: /plan <issue or @file>"
		return nil
	}
	if err := m.orch.StartRun(issue); err != nil {
		m.Status = "plan: " + err.Error()
		return nil
	}
	if !m.scopePhaseOrWarn("plan") {
		return nil
	}
	prompts, err := m.orch.PlanPrompts()
	if err != nil {
		m.Status = "plan: " + err.Error()
		return nil
	}
	m.beginPhase("plan", config.PhasePlan, prompts)
	m.Status = "planning — run " + m.orch.Run().Stamp
	return m.phaseCmds(m.orch.InteractivePrompts(config.PhasePlan, prompts))
}

func (m *Model) cmdVote() tea.Cmd {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return nil
	}
	if m.orch.Run() == nil {
		if err := m.orch.UseRun(""); err != nil {
			m.Status = "vote: " + err.Error()
			return nil
		}
	}
	if !m.scopePhaseOrWarn("vote") {
		return nil
	}
	prompts, err := m.orch.VotePrompts()
	if err != nil {
		m.Status = "vote: " + err.Error()
		return nil
	}
	m.beginPhase("vote", config.PhaseVote, prompts)
	m.Status = "voting on plans"
	return m.phaseCmds(m.orch.InteractivePrompts(config.PhaseVote, prompts))
}

func (m *Model) cmdBuild() tea.Cmd {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return nil
	}
	if m.orch.Run() == nil {
		if err := m.orch.UseRun(""); err != nil {
			m.Status = "build: " + err.Error()
			return nil
		}
	}
	if !m.scopePhaseOrWarn("build") {
		return nil
	}
	// BuildPrompt creates/resets the worktrees and returns the prompt. We launch
	// the agents in their worktrees now but DO NOT send the prompt yet, so the
	// user can adjust things in each tool first and then run /start-build.
	prompt, err := m.orch.BuildPrompt()
	if err != nil {
		m.Status = "build: " + err.Error()
		return nil
	}
	// Key the prompt by the build participants (workers), not the current panes:
	// after /vote the panes are the reviewers, so keying by panes would stage
	// nothing for /start-build.
	prompts := map[string]string{}
	for _, name := range m.orch.AgentsForPhase(config.PhaseBuild) {
		prompts[name] = prompt
	}
	if len(prompts) == 0 {
		m.Status = "build: no worker agents (role: [worker]) to build"
		return nil
	}
	m.beginPhase("build", config.PhaseBuild, prompts)
	m.pendingBuild = m.orch.InteractivePrompts(config.PhaseBuild, prompts)
	m.Status = "build ready in worktrees — adjust the tools, then /start-build"
	return nil
}

// cmdStartBuild sends the staged build prompt to the agents prepared by /build.
func (m *Model) cmdStartBuild() tea.Cmd {
	if len(m.pendingBuild) == 0 {
		m.Status = "nothing staged — run /build first"
		return nil
	}
	prompts := m.pendingBuild
	m.pendingBuild = nil
	m.Status = "build started"
	return func() tea.Msg { return phasePromptsMsg(prompts) }
}

// cmdReview gates the built implementations (run check command per worktree,
// drop failures) off the UI thread, then continues in handleReviewReady.
func (m *Model) cmdReview() tea.Cmd {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return nil
	}
	if m.orch.Run() == nil {
		if err := m.orch.UseRun(""); err != nil {
			m.Status = "review: " + err.Error()
			return nil
		}
	}
	if !m.scopePhaseOrWarn("review") {
		return nil
	}
	m.Status = "running build checks…"
	orch := m.orch
	return func() tea.Msg {
		prompts, survivors, err := orch.ReviewPrompts()
		return reviewReadyMsg{prompts: prompts, survivors: survivors, err: err}
	}
}

func (m Model) handleReviewReady(msg reviewReadyMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.Status = "review: " + msg.err.Error()
		return m, nil
	}
	switch len(msg.survivors) {
	case 0:
		m.Status = "no builds passed the checks"
		return m, nil
	case 1:
		_ = m.orch.SetSingleWinner(msg.survivors[0])
		m.Status = "only " + msg.survivors[0] + " passed — /adopt to apply it"
		return m, nil
	default:
		m.beginPhase("review", config.PhaseReview, msg.prompts)
		m.Status = fmt.Sprintf("reviewing %d builds", len(msg.survivors))
		return m, m.phaseCmds(m.orch.InteractivePrompts(config.PhaseReview, msg.prompts))
	}
}

// scopePhaseOrWarn restricts the next orchestration phase to the agents picked
// by the current /target (all/focus/personality/category). Returns false (and
// sets a status) when the target matches no agents.
func (m *Model) scopePhaseOrWarn(phase string) bool {
	names := m.scopedAgentNames()
	m.orch.SetScope(names)
	if names != nil && len(names) == 0 {
		label := m.TargetName
		if label == "" {
			label = "the current target"
		}
		m.Status = phase + ": no agents match " + label
		return false
	}
	if names != nil {
		m.Status = phase + ": " + strings.Join(names, ", ")
	}
	return true
}

// scopedAgentNames returns the agents selected by the current /target, or nil
// for "all".
func (m *Model) scopedAgentNames() []string {
	if m.orch == nil {
		return nil
	}
	switch m.Target {
	case TargetFocused:
		return []string{m.focusedName()}
	case TargetPersonality:
		return m.agentsMatching(func(name string) bool {
			pn, _, ok := m.Config.PersonalityForAgent(name)
			return ok && pn == m.TargetName
		})
	case TargetCategory:
		return m.agentsMatching(func(name string) bool {
			pn, _, ok := m.Config.PersonalityForAgent(name)
			if !ok {
				return false
			}
			cn, _, ok2 := m.Config.CategoryForPersonality(pn)
			return ok2 && cn == m.TargetName
		})
	default:
		return nil // TargetAll
	}
}

func (m *Model) agentsMatching(pred func(string) bool) []string {
	out := []string{}
	for _, name := range m.orch.Agents() {
		if pred(name) {
			out = append(out, name)
		}
	}
	return out
}

// cmdAdopt applies the winning implementation's diff onto the working tree.
// cmdAdopt applies a build's diff to the working tree. With no argument it
// adopts the reviewed winner; "/adopt <agent>" overrides the recommendation.
func (m *Model) cmdAdopt(rest string) tea.Cmd {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return nil
	}
	if m.orch.Run() == nil {
		if err := m.orch.UseRun(""); err != nil {
			m.Status = "adopt: " + err.Error()
			return nil
		}
	}
	adopted, err := m.orch.Adopt(strings.TrimSpace(rest))
	if err != nil {
		m.Status = "adopt: " + err.Error()
		return nil
	}
	m.Status = "applied " + adopted + "'s changes (uncommitted) — review and commit"
	return nil
}

func (m *Model) cmdStatus() {
	if m.orch == nil || m.orch.Run() == nil {
		m.Status = "no active run"
		return
	}
	phase := m.phase
	if phase == "" {
		phase = "idle"
	}
	m.Status = "run " + m.orch.Run().Stamp + " · phase " + phase
}

func (m *Model) cmdClean() {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return
	}
	removed, err := m.orch.Clean()
	if err != nil {
		m.Status = "clean: " + err.Error()
		return
	}
	m.Status = fmt.Sprintf("removed %d worktree(s)", len(removed))
}

func (m *Model) cmdRuns() {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return
	}
	runs, err := orchestrate.ListRuns(m.Config.Sessions.RootDir, 20)
	if err != nil {
		m.Status = "runs: " + err.Error()
		return
	}
	m.Runs = runs
	m.RunIndex = 0
	m.ScreenMode = ScreenRuns
	m.InputMode = InputComposer
	m.PromptInput = ""
	m.Status = fmt.Sprintf("%d run(s)", len(runs))
}

func (m *Model) cmdResume(rest string) tea.Cmd {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return nil
	}
	return m.resumeRun(strings.TrimSpace(rest))
}

func (m *Model) resumeRun(stamp string) tea.Cmd {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return nil
	}
	if err := m.orch.UseRun(stamp); err != nil {
		m.Status = "resume: " + err.Error()
		return nil
	}
	transcripts := orchestrate.LoadTranscripts(m.orch.Run().Dir, m.orch.Agents())
	target, err := m.orch.ResumeTarget()
	if err != nil {
		m.Status = "resume: " + err.Error()
		return nil
	}
	if target.Phase != "" {
		return m.resumePhase(target, transcripts)
	}

	store, err := runstore.OpenAt(m.orch.Run().Dir, "resume")
	if err != nil {
		m.Status = "resume: " + err.Error()
		return nil
	}
	sessions := m.orch.ResumeSessions(store)
	m.InputMode = InputComposer
	m.PromptInput = ""
	m.Target = TargetAll
	m.Store = store
	m.pendingBuild = nil
	m.phase = ""
	m.watching = nil
	m.ScreenMode = ScreenPanes
	m.replaceAgentsWithTranscripts(sessions, transcripts)
	if target.Status != "" {
		m.Status = target.Status
	} else {
		m.Status = "resumed run " + m.orch.Run().Stamp
	}
	return nil
}

func (m *Model) resumePhase(target orchestrate.ResumeTarget, transcripts map[string]string) tea.Cmd {
	store, err := m.orch.Store(target.Phase)
	if err != nil {
		m.Status = "resume: " + err.Error()
		return nil
	}
	sessions := m.orch.PhaseSessions(target.Phase, store, target.Prompts)
	m.InputMode = InputComposer
	m.PromptInput = ""
	m.Target = TargetAll
	m.Store = store
	m.pendingBuild = nil
	m.ScreenMode = ScreenPanes
	m.replaceAgentsWithTranscripts(sessions, transcripts)
	m.phase = string(target.Phase)
	m.watching = m.orch.ArtifactPaths(target.Phase)
	if target.PendingBuild {
		m.pendingBuild = m.orch.InteractivePrompts(target.Phase, target.Prompts)
	}
	_ = m.orch.SaveActivePhase(target.Phase, target.Participants, target.SendPrompts)
	m.Status = target.Status
	if target.PendingBuild {
		return m.phaseCmds(nil)
	}
	if target.SendPrompts {
		return m.phaseCmds(m.orch.InteractivePrompts(target.Phase, target.Prompts))
	}
	return m.phaseCmds(nil)
}

// beginPhase relaunches every pane in its worktree with the phase command and
// arms the artifact watcher for plan/vote.
func (m *Model) beginPhase(label string, phase config.Phase, prompts map[string]string) {
	store, err := m.orch.Store(phase)
	if err != nil {
		m.Status = err.Error()
		return
	}
	sessions := m.orch.PhaseSessions(phase, store, prompts)
	m.InputMode = InputComposer
	m.PromptInput = ""
	m.Target = TargetAll
	m.Store = store
	m.pendingBuild = nil // any new phase invalidates a staged build
	m.replaceAgents(sessions)
	m.phase = label
	m.watching = m.orch.ArtifactPaths(phase)
	_ = m.orch.SaveActivePhase(phase, m.orch.AgentsForPhase(phase), false)
}

func (m *Model) replaceAgents(sessions []*agent.Session) {
	m.replaceAgentsWithTranscripts(sessions, nil)
}

func (m *Model) replaceAgentsWithTranscripts(sessions []*agent.Session, transcripts map[string]string) {
	for _, v := range m.Agents {
		_ = v.Session.Terminate()
	}
	views := make([]*agentView, 0, len(sessions))
	for _, s := range sessions {
		v := &agentView{Session: s, Width: 120, Height: 40}
		v.setScreenSize(120, 40)
		if text := transcripts[s.Name]; text != "" {
			v.appendTranscript(text+"\n", m.MaxScrollback)
			v.applyTerminal(text + "\n")
		}
		views = append(views, v)
	}
	m.Agents = views
	m.sortAgents()
	m.FocusedIndex = 0
	m.PageIndex = 0
	m.Zoomed = false
	m.resizeAgents()
	m.startAll()
}

func (m *Model) LoadTranscripts(transcripts map[string]string) {
	if len(transcripts) == 0 {
		return
	}
	for _, view := range m.Agents {
		if text := transcripts[view.Session.Name]; text != "" {
			view.appendTranscript(text+"\n", m.MaxScrollback)
			view.applyTerminal(text + "\n")
		}
	}
}

func (m *Model) phaseCmds(prompts map[string]string) tea.Cmd {
	prompts = copyPrompts(prompts)
	cmds := []tea.Cmd{}
	if len(prompts) > 0 {
		cmds = append(cmds, tea.Tick(m.initialPromptDelay, func(time.Time) tea.Msg {
			return phasePromptsMsg(prompts)
		}))
	}
	if len(m.watching) > 0 {
		cmds = append(cmds, pollAfter())
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func pollAfter() tea.Cmd {
	return tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg { return pollArtifactsMsg{} })
}

// pollArtifacts marks panes done as their artifact files appear and finishes the
// phase once all are present. Returns the next poll tick, or nil when done.
func (m *Model) pollArtifacts() tea.Cmd {
	if m.phase == "" || len(m.watching) == 0 {
		return nil
	}
	allDone := true
	for _, v := range m.Agents {
		path, ok := m.watching[v.Session.Name]
		if !ok {
			continue
		}
		if v.PhaseDone {
			continue
		}
		if fileExists(path) {
			v.PhaseDone = true
		} else {
			allDone = false
		}
	}
	if allDone {
		m.finishPhase()
		return nil
	}
	return pollAfter()
}

func (m *Model) finishPhase() {
	if m.orch == nil {
		return
	}
	switch m.phase {
	case "plan":
		found, missing, err := m.orch.CollectPlans()
		if err != nil {
			m.Status = "collect plans: " + err.Error()
			return
		}
		status := fmt.Sprintf("collected %d plan(s) — type /vote", len(found))
		if len(missing) > 0 {
			status += " · no plan: " + strings.Join(missing, ",")
		}
		m.Status = status
	case "vote":
		res, err := m.orch.CollectVotesAndTally()
		if err != nil {
			m.Status = "tally: " + err.Error()
			return
		}
		m.Status = "winner: " + res.WinnerAgent + " (Plan " + res.WinnerLetter + ") — type /build"
	case "build":
		m.Status = "build done — see worktree branches"
	case "review":
		res, err := m.orch.CollectReviewsAndTally()
		if err != nil {
			m.Status = "review tally: " + err.Error()
			return
		}
		status := "best build: " + res.WinnerAgent + " — /adopt (or /adopt <agent>)"
		if all := m.orch.AdoptableBuilds(); len(all) > 1 {
			status += " · builds: " + strings.Join(all, ", ")
		}
		m.Status = status
	}
	m.phase = ""
	m.watching = nil
	_ = m.orch.ClearActivePhase()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (m *Model) handleAddressedInput(text string) {
	fields := strings.Fields(text)
	if len(fields) < 2 {
		m.Status = "usage: @agent message"
		return
	}

	target := strings.TrimPrefix(fields[0], "@")
	message := expandRefs(strings.TrimSpace(strings.TrimPrefix(text, fields[0])))
	if strings.EqualFold(target, "all") {
		m.sendAll(message)
		m.Status = "sent to all agents"
		return
	}
	m.sendNamed(target, message)
}

func (m *Model) sendNamed(name string, message string) {
	if message == "" {
		m.Status = "empty message for " + name
		return
	}
	for _, view := range m.Agents {
		if strings.EqualFold(view.Session.Name, name) {
			if err := sendLine(view.Session, m.Config.PromptForAgent(view.Session.Name, message)); err != nil {
				m.Status = err.Error()
				return
			}
			m.Status = "sent to " + view.Session.Name
			return
		}
	}
	m.Status = "unknown agent: " + name
}

func (m *Model) sendAll(message string) {
	for _, view := range m.Agents {
		if view.Session.Done {
			continue
		}
		_ = sendLine(view.Session, m.Config.PromptForAgent(view.Session.Name, message))
	}
}

func (m *Model) sendPersonality(personality string, message string) int {
	count := 0
	for _, view := range m.recipientViewsForPersonality(personality) {
		_ = sendLine(view.Session, m.Config.PromptForAgent(view.Session.Name, message))
		count++
	}
	return count
}

func (m *Model) sendCategory(category string, message string) int {
	count := 0
	for _, view := range m.recipientViewsForCategory(category) {
		_ = sendLine(view.Session, m.Config.PromptForAgent(view.Session.Name, message))
		count++
	}
	return count
}

func (m Model) recipientViewsForPersonality(personality string) []*agentView {
	views := make([]*agentView, 0)
	for _, view := range m.Agents {
		if view.Session.Done {
			continue
		}
		name, _, ok := m.Config.PersonalityForAgent(view.Session.Name)
		if ok && name == personality {
			views = append(views, view)
		}
	}
	return views
}

func (m Model) recipientViewsForCategory(category string) []*agentView {
	views := make([]*agentView, 0)
	for _, view := range m.Agents {
		if view.Session.Done {
			continue
		}
		_, personality, ok := m.Config.PersonalityForAgent(view.Session.Name)
		if !ok {
			continue
		}
		if personality.Category == category {
			views = append(views, view)
		}
	}
	return views
}

func (m *Model) sendPrompts(prompts map[string]string) {
	for _, view := range m.Agents {
		if view.Session.Done {
			continue
		}
		prompt := prompts[view.Session.Name]
		if prompt == "" {
			continue
		}
		_ = sendLine(view.Session, m.Config.PromptForAgent(view.Session.Name, prompt))
	}
}

func copyPrompts(prompts map[string]string) map[string]string {
	if len(prompts) == 0 {
		return nil
	}
	out := make(map[string]string, len(prompts))
	for name, prompt := range prompts {
		out[name] = prompt
	}
	return out
}

func sendLine(session *agent.Session, message string) error {
	if session == nil {
		return nil
	}

	text, submit := splitLinePayload(session.Config.Terminal, message)

	// With no delay, send the whole payload in one write (the common case).
	// Some TUI agents run an async input loop and treat an Enter that arrives
	// in the same burst as the text as a literal newline rather than a submit;
	// for those, set terminal.submit_delay_ms so the submit lands on its own.
	delay := submitDelay(session.Config.Terminal)
	if submit == "" || delay <= 0 {
		return session.WriteString(text + submit)
	}

	if err := session.WriteString(text); err != nil {
		return err
	}
	go func() {
		time.Sleep(delay)
		_ = session.WriteString(submit)
	}()
	return nil
}

func splitLinePayload(terminal config.TerminalConfig, message string) (text string, submit string) {
	text = optionalSequence(terminal.BeforeSendSequence)
	switch strings.ToLower(strings.TrimSpace(terminal.SendMode)) {
	case "paste", "bracketed-paste":
		text += bracketedPaste(message)
	default:
		text += message
	}
	submit = submitSequence(terminal.SubmitSequence)
	submit += optionalSequence(terminal.AfterSubmitSequence)
	return text, submit
}

func linePayload(terminal config.TerminalConfig, message string) string {
	text, submit := splitLinePayload(terminal, message)
	return text + submit
}

func submitDelay(terminal config.TerminalConfig) time.Duration {
	if terminal.SubmitDelayMs <= 0 {
		return 0
	}
	return time.Duration(terminal.SubmitDelayMs) * time.Millisecond
}

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

func (m *Model) clearScreens(target string) {
	target = strings.TrimSpace(target)
	if target == "" || strings.EqualFold(target, "all") {
		for _, view := range m.Agents {
			view.clearScreen()
		}
		m.Status = "cleared screens"
		return
	}
	for _, view := range m.Agents {
		if strings.EqualFold(view.Session.Name, target) {
			view.clearScreen()
			m.Status = "cleared " + view.Session.Name
			return
		}
	}
	m.Status = "unknown agent: " + target
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
		innerHeight := (m.Height - chromeHeight) - 2
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
	heights := distribute(m.Height-chromeHeight, rows)
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

func keyToPTY(msg tea.KeyMsg, enterSequence string) string {
	if len(msg.Runes) > 0 {
		return string(msg.Runes)
	}

	value := msg.String()
	switch value {
	case "enter":
		return submitSequence(enterSequence)
	case "backspace":
		return "\x7f"
	case "tab":
		return "\t"
	case "shift+tab":
		return "\x1b[Z"
	case "ctrl+space":
		return "\x00"
	case "ctrl+c":
		return "\x03"
	case "ctrl+d":
		return "\x04"
	case "ctrl+z":
		return "\x1a"
	case "up":
		return "\x1b[A"
	case "down":
		return "\x1b[B"
	case "right":
		return "\x1b[C"
	case "left":
		return "\x1b[D"
	case "delete":
		return "\x1b[3~"
	case "insert":
		return "\x1b[2~"
	case "home":
		return "\x1b[H"
	case "end":
		return "\x1b[F"
	case "pgup":
		return "\x1b[5~"
	case "pgdown":
		return "\x1b[6~"
	}

	if strings.HasPrefix(value, "ctrl+") && len(value) == len("ctrl+a") {
		ch := value[len(value)-1]
		if ch >= 'a' && ch <= 'z' {
			return string([]byte{ch - 'a' + 1})
		}
	}

	return ""
}

func submitSequence(name string) string {
	return terminalSequence(name, "\r")
}

func optionalSequence(name string) string {
	return terminalSequence(name, "")
}

func terminalSequence(name string, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "":
		return fallback
	case "none", "off", "false":
		return ""
	case "cr", "enter":
		return "\r"
	case "lf", "ctrl+j":
		return "\n"
	case "crlf":
		return "\r\n"
	case "esc", "escape":
		return "\x1b"
	case "ctrl+c":
		return "\x03"
	case "ctrl+d":
		return "\x04"
	case "ctrl+u", "clear-line", "kill-line":
		return "\x15"
	case "csi-enter", "kitty-enter":
		return "\x1b[13;1u"
	case "csi-enter-legacy", "kitty-enter-legacy":
		return "\x1b[13u"
	case "csi-ctrl-enter", "ctrl+enter", "kitty-ctrl-enter":
		return "\x1b[13;5u"
	case "csi-shift-enter", "shift+enter", "kitty-shift-enter":
		return "\x1b[13;2u"
	}
	if strings.HasPrefix(name, "raw:") {
		return strings.TrimPrefix(name, "raw:")
	}
	return fallback
}

func bracketedPaste(message string) string {
	clean := strings.ReplaceAll(message, "\x1b[200~", "")
	clean = strings.ReplaceAll(clean, "\x1b[201~", "")
	return "\x1b[200~" + clean + "\x1b[201~"
}

func (v *agentView) setScreenSize(width int, height int) {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	old := v.Screen
	v.Width = width
	v.Height = height
	v.Screen = make([][]screenCell, height)
	v.ScrollTop = 0
	v.ScrollBot = height - 1

	start := 0
	if len(old) > height {
		start = len(old) - height
	}
	for i := 0; i < height && start+i < len(old); i++ {
		v.Screen[i] = trimCells(old[start+i], width)
	}
	for i := range v.Screen {
		v.Screen[i] = trimCells(v.Screen[i], width)
	}
	v.clampCursor()
}

func (v *agentView) screenLines(height int, width int) []string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		return nil
	}
	if v.Width != width || v.Height != height {
		v.setScreenSize(width, height)
	}
	lines := make([]string, height)
	for i := 0; i < height; i++ {
		var cells []screenCell
		if i < len(v.Screen) {
			cells = trimCells(v.Screen[i], width)
		}
		cells = v.decorateDisplayCells(cells, width)
		lines[i] = renderCells(cells, width)
	}
	return lines
}

func (v *agentView) decorateDisplayCells(cells []screenCell, width int) []screenCell {
	if width <= 0 || v.Session == nil || v.Session.Name != "codex" || !isCodexPromptRow(cells) {
		return cells
	}
	out := ensureCells(trimCells(cells, width), width)
	for i := range out {
		if out[i].Ch == 0 {
			out[i].Ch = ' '
		}
		if !hasBackgroundSGR(out[i].SGR) {
			out[i].SGR += "\x1b[48;5;235m"
		}
	}
	return out
}

func isCodexPromptRow(cells []screenCell) bool {
	text := strings.TrimLeft(cellsText(cells), " ")
	return strings.HasPrefix(text, "› ")
}

func cellsText(cells []screenCell) string {
	var b strings.Builder
	for _, cell := range cells {
		if cell.Ch == 0 {
			b.WriteRune(' ')
		} else {
			b.WriteRune(cell.Ch)
		}
	}
	return b.String()
}

func hasBackgroundSGR(sgr string) bool {
	if sgr == "" {
		return false
	}
	matches := csiPattern.FindAllString(sgr, -1)
	has := false
	for _, seq := range matches {
		if !strings.HasSuffix(seq, "m") {
			continue
		}
		params := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b["), "m")
		for _, code := range strings.Split(params, ";") {
			switch code {
			case "0", "49":
				has = false
			case "40", "41", "42", "43", "44", "45", "46", "47", "48", "100", "101", "102", "103", "104", "105", "106", "107":
				has = true
			}
		}
	}
	return has
}

func (v *agentView) applyTerminal(raw string) {
	if v.pending != "" {
		raw = v.pending + raw
		v.pending = ""
	}
	raw = oscPattern.ReplaceAllString(raw, "")
	for i := 0; i < len(raw); {
		b := raw[i]
		switch b {
		case '\x1b':
			next, complete := v.consumeEscape(raw, i)
			if !complete {
				// Sequence is cut off at the chunk boundary; stash the tail
				// and finish it when the next chunk arrives. Cap the buffer so
				// a malformed stream can't grow it without bound.
				if len(raw)-i <= 8192 {
					v.pending = raw[i:]
					return
				}
				i++
				continue
			}
			if next <= i {
				i++
			} else {
				i = next
			}
		case '\r':
			v.CursorCol = 0
			i++
		case '\n':
			v.newLine()
			i++
		case '\b':
			if v.CursorCol > 0 {
				v.CursorCol--
			}
			i++
		case '\t':
			spaces := 4 - (v.CursorCol % 4)
			for j := 0; j < spaces; j++ {
				v.putRune(' ')
			}
			i++
		default:
			r, size := utf8.DecodeRuneInString(raw[i:])
			if r == utf8.RuneError && size == 1 {
				i++
				continue
			}
			if r >= ' ' && r != '\x7f' {
				v.putRune(r)
			}
			i += size
		}
	}
}

// consumeEscape applies the escape sequence starting at index. It returns the
// index just past the sequence and whether the sequence was complete. When the
// sequence is cut off at the end of the buffer it returns complete=false so the
// caller can buffer the remainder for the next chunk.
func (v *agentView) consumeEscape(raw string, index int) (int, bool) {
	if index+1 >= len(raw) {
		return index, false
	}

	switch raw[index+1] {
	case '[':
		j := index + 2
		for j < len(raw) && !isCSIFinal(raw[j]) {
			j++
		}
		if j >= len(raw) {
			return index, false
		}
		v.handleCSI(raw[index+2:j], raw[j])
		return j + 1, true
	case ']':
		// OSC: a complete one was already stripped before the loop, so reaching
		// here means it is cut off at the buffer boundary.
		return index, false
	case 'P', 'X', '^', '_':
		// DCS/SOS/PM/APC string, terminated by ST (ESC \). Strip if complete,
		// otherwise buffer the remainder.
		end := strings.Index(raw[index:], "\x1b\\")
		if end < 0 {
			return index, false
		}
		return index + end + 2, true
	case 'c':
		v.clearScreen()
		return index + 2, true
	case '7':
		v.SavedRow = v.CursorRow
		v.SavedCol = v.CursorCol
		return index + 2, true
	case '8':
		v.CursorRow = v.SavedRow
		v.CursorCol = v.SavedCol
		v.clampCursor()
		return index + 2, true
	default:
		return index + 2, true
	}
}

func (v *agentView) handleCSI(rawParams string, command byte) {
	params := parseCSIParams(rawParams)
	first := paramDefault(params, 0, 1)

	switch command {
	case 'A':
		v.CursorRow -= first
	case 'B':
		v.CursorRow += first
	case 'C':
		v.CursorCol += first
	case 'D':
		v.CursorCol -= first
	case 'E':
		v.CursorRow += first
		v.CursorCol = 0
	case 'F':
		v.CursorRow -= first
		v.CursorCol = 0
	case 'G':
		v.CursorCol = first - 1
	case 'H', 'f':
		v.CursorRow = paramDefault(params, 0, 1) - 1
		v.CursorCol = paramDefault(params, 1, 1) - 1
	case 'J':
		v.eraseScreen(paramDefault(params, 0, 0))
	case 'K':
		v.eraseLine(paramDefault(params, 0, 0))
	case 'm':
		v.handleSGR(rawParams)
	case 'L':
		v.insertLines(first)
	case 'M':
		v.deleteLines(first)
	case 'P':
		v.deleteChars(first)
	case '@':
		v.insertBlanks(first)
	case 'S':
		v.scrollUp(first)
	case 'T':
		v.scrollDown(first)
	case 'd':
		v.CursorRow = first - 1
	case 'r':
		top := paramDefault(params, 0, 1) - 1
		bottom := paramDefault(params, 1, v.Height) - 1
		if top < 0 {
			top = 0
		}
		if bottom >= v.Height {
			bottom = v.Height - 1
		}
		if top < bottom {
			v.ScrollTop = top
			v.ScrollBot = bottom
			v.CursorRow = 0
			v.CursorCol = 0
		}
	case 'h', 'l':
		if strings.Contains(rawParams, "?1049") || strings.Contains(rawParams, "?1047") || strings.Contains(rawParams, "?47") {
			v.clearScreen()
		}
	case 's':
		v.SavedRow = v.CursorRow
		v.SavedCol = v.CursorCol
	case 'u':
		v.CursorRow = v.SavedRow
		v.CursorCol = v.SavedCol
	}
	v.clampCursor()
}

func (v *agentView) handleSGR(rawParams string) {
	rawParams = strings.TrimSpace(rawParams)
	if rawParams == "" {
		rawParams = "0"
	}

	parts := strings.Split(rawParams, ";")
	for _, part := range parts {
		if part == "0" {
			v.CurrentSGR = ""
			break
		}
	}
	if rawParams != "0" {
		v.CurrentSGR += "\x1b[" + rawParams + "m"
	}
	if len(v.CurrentSGR) > 512 {
		v.CurrentSGR = "\x1b[" + rawParams + "m"
	}
}

func (v *agentView) putRune(r rune) {
	if v.Height == 0 || v.Width == 0 {
		return
	}
	if v.CursorCol >= v.Width {
		v.newLine()
	}

	line := v.Screen[v.CursorRow]
	for len(line) < v.CursorCol {
		line = append(line, screenCell{Ch: ' '})
	}
	cell := screenCell{Ch: r, SGR: v.CurrentSGR}
	if v.CursorCol < len(line) {
		line[v.CursorCol] = cell
	} else {
		line = append(line, cell)
	}
	if len(line) > v.Width {
		line = line[:v.Width]
	}
	v.Screen[v.CursorRow] = line
	v.CursorCol++
}

func (v *agentView) addDisplayLine(line string) {
	v.CursorCol = 0
	for _, r := range line {
		v.putRune(r)
	}
	v.newLine()
}

func (v *agentView) newLine() {
	v.CursorCol = 0
	if v.CursorRow == v.ScrollBot {
		v.scrollRegionUp(v.ScrollTop, v.ScrollBot, 1)
		return
	}
	v.CursorRow++
	if v.CursorRow < v.Height {
		return
	}
	if v.Height <= 0 {
		v.CursorRow = 0
	} else {
		v.CursorRow = v.Height - 1
	}
}

func (v *agentView) clearScreen() {
	for i := range v.Screen {
		v.Screen[i] = nil
	}
	v.CursorRow = 0
	v.CursorCol = 0
	v.ScrollTop = 0
	v.ScrollBot = v.Height - 1
}

func (v *agentView) eraseScreen(mode int) {
	switch mode {
	case 0:
		v.eraseLine(0)
		for i := v.CursorRow + 1; i < len(v.Screen); i++ {
			v.Screen[i] = nil
		}
	case 1:
		for i := 0; i < v.CursorRow; i++ {
			v.Screen[i] = nil
		}
		v.eraseLine(1)
	case 2, 3:
		v.clearScreen()
	}
}

func (v *agentView) eraseLine(mode int) {
	if v.CursorRow < 0 || v.CursorRow >= len(v.Screen) {
		return
	}
	line := v.Screen[v.CursorRow]
	switch mode {
	case 0:
		if v.CursorCol < 0 {
			v.CursorCol = 0
		}
		if v.CursorCol > v.Width {
			v.CursorCol = v.Width
		}
		line = ensureCells(line, v.Width)
		for i := v.CursorCol; i < v.Width; i++ {
			line[i] = v.blankCell()
		}
	case 1:
		line = ensureCells(line, v.Width)
		end := v.CursorCol
		if end >= v.Width {
			end = v.Width - 1
		}
		for i := 0; i <= end && i < len(line); i++ {
			line[i] = v.blankCell()
		}
	case 2:
		line = make([]screenCell, v.Width)
		for i := range line {
			line[i] = v.blankCell()
		}
	}
	v.Screen[v.CursorRow] = line
}

func (v *agentView) insertLines(count int) {
	if count <= 0 || v.CursorRow < v.ScrollTop || v.CursorRow > v.ScrollBot {
		return
	}
	if count > v.ScrollBot-v.CursorRow+1 {
		count = v.ScrollBot - v.CursorRow + 1
	}
	for i := v.ScrollBot; i >= v.CursorRow+count; i-- {
		v.Screen[i] = v.Screen[i-count]
	}
	for i := 0; i < count; i++ {
		v.Screen[v.CursorRow+i] = nil
	}
}

func (v *agentView) deleteLines(count int) {
	if count <= 0 || v.CursorRow < v.ScrollTop || v.CursorRow > v.ScrollBot {
		return
	}
	if count > v.ScrollBot-v.CursorRow+1 {
		count = v.ScrollBot - v.CursorRow + 1
	}
	for i := v.CursorRow; i+count <= v.ScrollBot; i++ {
		v.Screen[i] = v.Screen[i+count]
	}
	for i := v.ScrollBot - count + 1; i <= v.ScrollBot; i++ {
		v.Screen[i] = nil
	}
}

func (v *agentView) insertBlanks(count int) {
	if count <= 0 || v.CursorRow < 0 || v.CursorRow >= v.Height {
		return
	}
	line := v.Screen[v.CursorRow]
	for len(line) < v.CursorCol {
		line = append(line, screenCell{Ch: ' '})
	}
	prefix := append([]screenCell(nil), line[:minInt(v.CursorCol, len(line))]...)
	blanks := make([]screenCell, count)
	for i := range blanks {
		blanks[i] = v.blankCell()
	}
	suffix := append(blanks, line[minInt(v.CursorCol, len(line)):]...)
	line = append(prefix, suffix...)
	if len(line) > v.Width {
		line = line[:v.Width]
	}
	v.Screen[v.CursorRow] = line
}

func (v *agentView) deleteChars(count int) {
	if count <= 0 || v.CursorRow < 0 || v.CursorRow >= v.Height {
		return
	}
	line := v.Screen[v.CursorRow]
	if v.CursorCol >= len(line) {
		return
	}
	end := v.CursorCol + count
	if end > len(line) {
		end = len(line)
	}
	line = append(line[:v.CursorCol], line[end:]...)
	v.Screen[v.CursorRow] = line
}

func (v *agentView) scrollUp(count int) {
	v.scrollRegionUp(v.ScrollTop, v.ScrollBot, count)
}

func (v *agentView) scrollRegionUp(top int, bottom int, count int) {
	if count <= 0 {
		return
	}
	if top < 0 {
		top = 0
	}
	if bottom >= v.Height {
		bottom = v.Height - 1
	}
	if top > bottom {
		return
	}
	size := bottom - top + 1
	if count > size {
		count = size
	}
	for i := top; i+count <= bottom; i++ {
		v.Screen[i] = v.Screen[i+count]
	}
	for i := bottom - count + 1; i <= bottom; i++ {
		v.Screen[i] = nil
	}
}

func (v *agentView) scrollDown(count int) {
	if count <= 0 {
		return
	}
	top := v.ScrollTop
	bottom := v.ScrollBot
	if top < 0 {
		top = 0
	}
	if bottom >= v.Height {
		bottom = v.Height - 1
	}
	if top > bottom {
		return
	}
	size := bottom - top + 1
	if count > size {
		count = size
	}
	for i := bottom; i >= top+count; i-- {
		v.Screen[i] = v.Screen[i-count]
	}
	for i := top; i < top+count; i++ {
		v.Screen[i] = nil
	}
}

func (v *agentView) clampCursor() {
	if v.Height <= 0 {
		v.CursorRow = 0
	} else {
		if v.CursorRow < 0 {
			v.CursorRow = 0
		}
		if v.CursorRow >= v.Height {
			v.CursorRow = v.Height - 1
		}
	}
	if v.ScrollTop < 0 {
		v.ScrollTop = 0
	}
	if v.ScrollBot <= 0 || v.ScrollBot >= v.Height {
		v.ScrollBot = v.Height - 1
	}
	if v.ScrollTop >= v.ScrollBot {
		v.ScrollTop = 0
		v.ScrollBot = v.Height - 1
	}
	if v.Width <= 0 {
		v.CursorCol = 0
	} else {
		if v.CursorCol < 0 {
			v.CursorCol = 0
		}
		if v.CursorCol >= v.Width {
			v.CursorCol = v.Width - 1
		}
	}
}

func parseCSIParams(raw string) []int {
	raw = strings.TrimLeft(raw, "?><")
	if raw == "" {
		return nil
	}
	fields := strings.Split(raw, ";")
	params := make([]int, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			params = append(params, 0)
			continue
		}
		value, err := strconv.Atoi(field)
		if err != nil {
			params = append(params, 0)
			continue
		}
		params = append(params, value)
	}
	return params
}

func paramDefault(params []int, index int, fallback int) int {
	if index >= len(params) || params[index] == 0 {
		return fallback
	}
	return params[index]
}

func isCSIFinal(b byte) bool {
	return b >= 0x40 && b <= 0x7e
}

func trimCells(cells []screenCell, width int) []screenCell {
	if width <= 0 || len(cells) == 0 {
		return nil
	}
	if len(cells) > width {
		cells = cells[:width]
	}
	out := make([]screenCell, len(cells))
	copy(out, cells)
	return out
}

func ensureCells(cells []screenCell, width int) []screenCell {
	if width <= 0 {
		return nil
	}
	if len(cells) >= width {
		return cells[:width]
	}
	out := make([]screenCell, width)
	copy(out, cells)
	for i := len(cells); i < width; i++ {
		out[i] = screenCell{Ch: ' '}
	}
	return out
}

func (v *agentView) blankCell() screenCell {
	return screenCell{Ch: ' ', SGR: v.CurrentSGR}
}

func renderCells(cells []screenCell, width int) string {
	if width <= 0 {
		return ""
	}

	var b strings.Builder
	active := ""
	for col := 0; col < width; col++ {
		cell := screenCell{Ch: ' '}
		if col < len(cells) {
			cell = cells[col]
			if cell.Ch == 0 {
				cell.Ch = ' '
			}
		}

		if cell.SGR != active {
			if active != "" {
				b.WriteString("\x1b[0m")
			}
			if cell.SGR != "" {
				b.WriteString(cell.SGR)
			}
			active = cell.SGR
		}
		b.WriteRune(cell.Ch)
	}
	if active != "" {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

func cleanTranscriptOutput(value string) string {
	value = oscPattern.ReplaceAllString(value, "")
	value = csiPattern.ReplaceAllString(value, "")
	value = controlPattern.ReplaceAllString(value, "")
	return value
}

func topBorder(title string, width int) string {
	if width < 2 {
		return strings.Repeat("-", width)
	}
	inner := width - 2
	title = truncateText(title, inner)
	if lipgloss.Width(title) >= inner {
		return "+" + title + "+"
	}
	return "+" + title + strings.Repeat("-", inner-lipgloss.Width(title)) + "+"
}

func blankBlock(width int, height int) []string {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	lines := make([]string, height)
	blank := strings.Repeat(" ", width)
	for i := range lines {
		lines[i] = blank
	}
	return lines
}

func fitBlock(lines []string, width int, height int) []string {
	if height < 0 {
		height = 0
	}
	out := make([]string, 0, height)
	for _, line := range lines {
		if len(out) >= height {
			break
		}
		out = append(out, fitText(line, width))
	}
	for len(out) < height {
		out = append(out, fitText("", width))
	}
	return out
}

func distribute(total int, count int) []int {
	if count <= 0 {
		return nil
	}
	values := make([]int, count)
	base := total / count
	remainder := total % count
	for i := range values {
		values[i] = base
		if i < remainder {
			values[i]++
		}
	}
	return values
}

func fitText(value string, width int) string {
	value = strings.ReplaceAll(value, "\t", "    ")
	value = truncateText(value, width)
	visible := lipgloss.Width(value)
	if visible < width {
		value += strings.Repeat(" ", width-visible)
	}
	return value
}

func truncateText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		for _, r := range value {
			return string(r)
		}
		return ""
	}

	var b strings.Builder
	used := 0
	for _, r := range value {
		rw := lipgloss.Width(string(r))
		if used+rw > width-1 {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return b.String() + "~"
}

func hardWrap(line string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	if line == "" {
		return []string{""}
	}

	lines := make([]string, 0)
	current := strings.Builder{}
	used := 0
	for _, r := range line {
		rw := lipgloss.Width(string(r))
		if rw <= 0 {
			rw = 1
		}
		if used+rw > width && current.Len() > 0 {
			lines = append(lines, current.String())
			current.Reset()
			used = 0
		}
		current.WriteRune(r)
		used += rw
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func dropLastRune(value string) string {
	if value == "" {
		return ""
	}
	_, size := utf8.DecodeLastRuneInString(value)
	if size <= 0 {
		return ""
	}
	return value[:len(value)-size]
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
