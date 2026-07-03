package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/orchestrate"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func TestToastText(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name         string
		status       string
		age          time.Duration
		statusAtZero bool
		want         string
	}{
		{"fresh non-sticky shows", "saved transcripts", time.Second, false, "saved transcripts"},
		{"expired non-sticky hides", "saved transcripts", 5 * time.Second, false, ""},
		{"sticky persists past TTL", "cost reconcile failed", 30 * time.Second, false, "cost reconcile failed"},
		{"empty status hides", "", time.Second, false, ""},
		{"never-stamped hides", "saved transcripts", 0, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := Model{Status: tc.status, now: now}
			if !tc.statusAtZero {
				m.statusAt = now.Add(-tc.age)
			}
			if got := m.toastText(); got != tc.want {
				t.Fatalf("toastText() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStatusSticky(t *testing.T) {
	for s, want := range map[string]bool{
		"start error: boom":       true,
		"cannot start run":        true,
		"save failed: disk full":  true,
		"price unknown for model": true,
		"saved transcripts":       false,
		"sent to all agents":      false,
	} {
		if got := statusSticky(s); got != want {
			t.Errorf("statusSticky(%q) = %v, want %v", s, got, want)
		}
	}
}

// The Update wrapper stamps statusAt when Status changes (detected via
// statusSeen) and leaves it alone when an identical tick repeats.
func TestUpdateStampsToastOnStatusChange(t *testing.T) {
	m := Model{Status: "sent to all agents"} // set before the wrapper observes it
	updated, _ := m.Update(usageTickMsg{})
	nm := updated.(Model)
	if nm.statusAt.IsZero() || nm.statusSeen != "sent to all agents" {
		t.Fatalf("first tick should stamp toast: statusAt=%v seen=%q", nm.statusAt, nm.statusSeen)
	}
	if nm.toastText() != "sent to all agents" {
		t.Fatalf("fresh toast should render, got %q", nm.toastText())
	}
	firstAt := nm.statusAt
	updated2, _ := nm.Update(usageTickMsg{})
	if nm2 := updated2.(Model); nm2.statusAt != firstAt {
		t.Fatalf("identical status should not re-stamp: %v vs %v", nm2.statusAt, firstAt)
	}
}

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+n":
		return tea.KeyMsg{Type: tea.KeyCtrlN}
	case "ctrl+p":
		return tea.KeyMsg{Type: tea.KeyCtrlP}
	case "ctrl+g":
		return tea.KeyMsg{Type: tea.KeyCtrlG}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestViewClipsPaneAndInputWidth(t *testing.T) {
	sessions := []*agent.Session{
		agent.NewSession("claude", config.AgentConfig{}, ""),
		agent.NewSession("codex", config.AgentConfig{}, ""),
	}
	model := NewModel(sessions, nil, 1000, "", 0, nil, nil)
	model.Width = 80
	model.Height = 24
	model.resizeAgents()
	model.PromptInput = strings.Repeat("input-", 40)

	model.appendOutput(model.Agents[0], strings.Repeat("left-", 80))
	model.appendOutput(model.Agents[1], strings.Repeat("right-", 80))

	lines := strings.Split(model.View(), "\n")
	if len(lines) != model.Height {
		t.Fatalf("view height = %d, want %d", len(lines), model.Height)
	}
	for i, line := range lines {
		plain := ansiRE.ReplaceAllString(line, "")
		if got := len([]rune(plain)); got != model.Width {
			t.Fatalf("line %d width = %d, want %d: %q", i, got, model.Width, plain)
		}
	}
}

func TestViewPreservesSGRColor(t *testing.T) {
	sessions := []*agent.Session{
		agent.NewSession("codex", config.AgentConfig{}, ""),
	}
	model := NewModel(sessions, nil, 1000, "", 0, nil, nil)
	model.Width = 80
	model.Height = 18
	model.resizeAgents()

	model.appendOutput(model.Agents[0], "\x1b[31mred\x1b[0m\n")

	view := model.View()
	if !strings.Contains(view, "\x1b[31m") {
		t.Fatalf("view does not preserve red SGR escape: %q", view)
	}
}

func TestEraseLinePreservesSGRBackground(t *testing.T) {
	session := agent.NewSession("codex", config.AgentConfig{}, "")
	model := NewModel([]*agent.Session{session}, nil, 1000, "", 0, nil, nil)
	model.Width = 40
	model.Height = 8
	model.resizeAgents()

	view := model.Agents[0]
	model.appendOutput(view, "\x1b[48;5;235m\x1b[2K")
	line := view.screenLines(view.Height, view.Width)[0]

	if !strings.Contains(line, "\x1b[48;5;235m") {
		t.Fatalf("background SGR was lost while erasing line: %q", line)
	}
}

func TestCodexPromptRowGetsBackgroundBand(t *testing.T) {
	session := agent.NewSession("codex", config.AgentConfig{}, "")
	model := NewModel([]*agent.Session{session}, nil, 1000, "", 0, nil, nil)
	model.Width = 40
	model.Height = 8
	model.resizeAgents()

	view := model.Agents[0]
	model.appendOutput(view, "\x1b[1m›\x1b[22m test")
	line := view.screenLines(view.Height, view.Width)[0]

	if !strings.Contains(line, "\x1b[48;5;235m") {
		t.Fatalf("codex prompt row is missing synthetic background: %q", line)
	}
	if plain := ansiRE.ReplaceAllString(line, ""); len([]rune(plain)) != view.Width {
		t.Fatalf("prompt row width = %d, want %d: %q", len([]rune(plain)), view.Width, plain)
	}
}

func TestTerminalScrollRegionSpinnerDoesNotStack(t *testing.T) {
	session := agent.NewSession("copilot", config.AgentConfig{}, "")
	model := NewModel([]*agent.Session{session}, nil, 1000, "", 0, nil, nil)
	model.Width = 80
	model.Height = 24
	model.resizeAgents()

	view := model.Agents[0]
	model.appendOutput(view, "\x1b[1;18r\x1b[18;3H\x1b[35m● Loading\x1b[0m")
	model.appendOutput(view, "\r\x1b[35m◉ Load\x1b[0m")
	model.appendOutput(view, "\r\x1b[35m◎ Loading: 7 skills\x1b[0m")

	plain := ansiRE.ReplaceAllString(strings.Join(view.screenLines(view.Height, view.Width), "\n"), "")
	if strings.Count(plain, "Load") != 1 {
		t.Fatalf("spinner frames stacked instead of overwriting: %q", plain)
	}
}

func TestZoomShowsOnlyFocusedPaneFullWidth(t *testing.T) {
	sessions := []*agent.Session{
		agent.NewSession("claude", config.AgentConfig{}, ""),
		agent.NewSession("codex", config.AgentConfig{}, ""),
	}
	model := NewModel(sessions, nil, 1000, "", 0, nil, nil)
	model.Width = 80
	model.Height = 24
	model.resizeAgents()

	model.appendOutput(model.Agents[0], "CLAUDEPANE\n")
	model.appendOutput(model.Agents[1], "CODEXPANE\n")

	model.FocusedIndex = 1
	model.toggleZoom()
	if !model.Zoomed {
		t.Fatal("expected zoomed")
	}

	lines := strings.Split(model.View(), "\n")
	if len(lines) != model.Height {
		t.Fatalf("view height = %d, want %d", len(lines), model.Height)
	}
	for i, line := range lines {
		plain := ansiRE.ReplaceAllString(line, "")
		if got := len([]rune(plain)); got != model.Width {
			t.Fatalf("line %d width = %d, want full %d: %q", i, got, model.Width, plain)
		}
	}

	body := ansiRE.ReplaceAllString(model.View(), "")
	if !strings.Contains(body, "CODEXPANE") {
		t.Fatalf("zoomed focused pane should show its content: %q", body)
	}
	if strings.Contains(body, "CLAUDEPANE") {
		t.Fatalf("zoom should hide non-focused panes: %q", body)
	}
}

func TestTabCompletesCommandThenSwitchesFocus(t *testing.T) {
	sessions := []*agent.Session{
		agent.NewSession("claude", config.AgentConfig{}, ""),
		agent.NewSession("codex", config.AgentConfig{}, ""),
	}
	model := NewModel(sessions, nil, 1000, "", 0, nil, nil)

	// Tab completes a partial command instead of switching focus.
	model.PromptInput = "/zo"
	updated, _ := model.handleKey(keyMsg("tab"))
	m := updated.(Model)
	if m.PromptInput != "/zoom " {
		t.Fatalf("tab completion = %q, want %q", m.PromptInput, "/zoom ")
	}
	if m.FocusedIndex != 0 {
		t.Fatalf("focus should not move while completing, got %d", m.FocusedIndex)
	}

	// With no command in progress, Tab cycles focus as before.
	m.PromptInput = ""
	updated2, _ := m.handleKey(keyMsg("tab"))
	if updated2.(Model).FocusedIndex != 1 {
		t.Fatalf("tab should switch focus, got %d", updated2.(Model).FocusedIndex)
	}
}

func TestPagedLayoutNavigation(t *testing.T) {
	adaptiveOff := false
	cfg := config.Config{
		UI: config.UIConfig{PageRows: 1, PageCols: 2, MaxScrollbackLines: 1000, AdaptiveGrid: &adaptiveOff},
	}
	cfg.Normalize()
	sessions := []*agent.Session{
		agent.NewSession("a", config.AgentConfig{}, ""),
		agent.NewSession("b", config.AgentConfig{}, ""),
		agent.NewSession("c", config.AgentConfig{}, ""),
	}
	model := NewModelWithConfig(sessions, nil, cfg, "", nil, 0, nil, nil)
	model.Width = 80
	model.Height = 18
	model.resizeAgents()
	model.appendOutput(model.Agents[0], "AAA\n")
	model.appendOutput(model.Agents[1], "BBB\n")
	model.appendOutput(model.Agents[2], "CCC\n")

	body := ansiRE.ReplaceAllString(model.View(), "")
	if !strings.Contains(body, "AAA") || !strings.Contains(body, "BBB") {
		t.Fatalf("first page missing first two panes: %q", body)
	}
	if strings.Contains(body, "CCC") {
		t.Fatalf("first page should hide third pane: %q", body)
	}

	updated, _ := model.handleKey(keyMsg("ctrl+n"))
	model = updated.(Model)
	if model.PageIndex != 1 || model.FocusedIndex != 2 {
		t.Fatalf("next page = page %d focus %d, want page 1 focus 2", model.PageIndex, model.FocusedIndex)
	}
	body = ansiRE.ReplaceAllString(model.View(), "")
	if !strings.Contains(body, "CCC") || strings.Contains(body, "AAA") {
		t.Fatalf("second page should show only third pane: %q", body)
	}
}

func TestTabCrossesPageBoundary(t *testing.T) {
	adaptiveOff := false
	cfg := config.Config{UI: config.UIConfig{PageRows: 1, PageCols: 2, MaxScrollbackLines: 1000, AdaptiveGrid: &adaptiveOff}}
	cfg.Normalize()
	model := NewModelWithConfig([]*agent.Session{
		agent.NewSession("a", config.AgentConfig{}, ""),
		agent.NewSession("b", config.AgentConfig{}, ""),
		agent.NewSession("c", config.AgentConfig{}, ""),
	}, nil, cfg, "", nil, 0, nil, nil)
	model.FocusedIndex = 1
	model.ensurePageForFocus()

	updated, _ := model.handleKey(keyMsg("tab"))
	model = updated.(Model)
	if model.FocusedIndex != 2 || model.PageIndex != 1 {
		t.Fatalf("tab across page = focus %d page %d, want focus 2 page 1", model.FocusedIndex, model.PageIndex)
	}
}

func TestSettingsAdjustsGridAndGrouping(t *testing.T) {
	cfg := config.Config{UI: config.UIConfig{PageRows: 2, PageCols: 2, GroupBy: "none", MaxScrollbackLines: 1000}}
	cfg.Normalize()
	model := NewModelWithConfig([]*agent.Session{agent.NewSession("a", config.AgentConfig{}, "")}, nil, cfg, "", nil, 0, nil, nil)
	model.ScreenMode = ScreenSettings

	// Item 0 is the adaptive toggle; rows are item 1.
	updated, _ := model.handleKey(keyMsg("down"))
	model = updated.(Model)
	updated, _ = model.handleKey(keyMsg("right"))
	model = updated.(Model)
	if model.Config.UI.PageRows != 3 {
		t.Fatalf("page rows = %d, want 3", model.Config.UI.PageRows)
	}
	if model.adaptiveLayout() {
		t.Fatal("manually adjusting rows should lock the adaptive layout")
	}

	updated, _ = model.handleKey(keyMsg("down"))
	model = updated.(Model)
	updated, _ = model.handleKey(keyMsg("down"))
	model = updated.(Model)
	updated, _ = model.handleKey(keyMsg("right"))
	model = updated.(Model)
	if model.Config.UI.GroupBy != "personality" {
		t.Fatalf("group by = %q, want personality", model.Config.UI.GroupBy)
	}
}

func TestOverviewEnterFocusesSelectedAgent(t *testing.T) {
	model := NewModel([]*agent.Session{
		agent.NewSession("a", config.AgentConfig{}, ""),
		agent.NewSession("b", config.AgentConfig{}, ""),
	}, nil, 1000, "", 0, nil, nil)
	model.ScreenMode = ScreenOverview

	updated, _ := model.handleKey(keyMsg("down"))
	model = updated.(Model)
	updated, _ = model.handleKey(keyMsg("enter"))
	model = updated.(Model)
	if model.ScreenMode != ScreenPanes || model.FocusedIndex != 1 {
		t.Fatalf("overview enter = mode %v focus %d, want panes focus 1", model.ScreenMode, model.FocusedIndex)
	}
}

func TestCtrlGOpensOverview(t *testing.T) {
	model := NewModel([]*agent.Session{agent.NewSession("a", config.AgentConfig{}, "")}, nil, 1000, "", 0, nil, nil)

	updated, _ := model.handleKey(keyMsg("ctrl+g"))
	model = updated.(Model)
	if model.ScreenMode != ScreenOverview {
		t.Fatalf("ctrl+g mode = %v, want overview", model.ScreenMode)
	}
}

func TestCtrlCConfirmsBeforeInterrupt(t *testing.T) {
	model := NewModel([]*agent.Session{agent.NewSession("a", config.AgentConfig{}, "")}, nil, 1000, "", 0, nil, nil)

	// A non-empty prompt: Ctrl+C clears input and does not arm an interrupt.
	model.PromptInput = "hello"
	updated, _ := model.handleKey(keyMsg("ctrl+c"))
	model = updated.(Model)
	if model.PromptInput != "" {
		t.Fatalf("ctrl+c with input should clear it, got %q", model.PromptInput)
	}
	if model.interruptArmed != "" {
		t.Fatalf("ctrl+c with input should not arm, got %q", model.interruptArmed)
	}

	// First Ctrl+C on an empty prompt arms rather than interrupting.
	updated, _ = model.handleKey(keyMsg("ctrl+c"))
	model = updated.(Model)
	if model.interruptArmed != "a" {
		t.Fatalf("first ctrl+c should arm agent a, got %q", model.interruptArmed)
	}
	if !strings.Contains(model.Status, "press Ctrl+C again") {
		t.Fatalf("status should prompt for confirmation, got %q", model.Status)
	}

	// Second Ctrl+C within the window attempts the interrupt and disarms. The
	// test session has no live PTY, so the write reports failure — either way the
	// status reflects an interrupt attempt rather than the arm prompt.
	updated, _ = model.handleKey(keyMsg("ctrl+c"))
	model = updated.(Model)
	if model.interruptArmed != "" {
		t.Fatalf("second ctrl+c should disarm, got %q", model.interruptArmed)
	}
	if !strings.Contains(model.Status, "interrupt") || strings.Contains(model.Status, "again") {
		t.Fatalf("status should report an interrupt attempt, got %q", model.Status)
	}
}

func TestCtrlCDisarmsOnOtherKey(t *testing.T) {
	model := NewModel([]*agent.Session{agent.NewSession("a", config.AgentConfig{}, "")}, nil, 1000, "", 0, nil, nil)

	updated, _ := model.handleKey(keyMsg("ctrl+c"))
	model = updated.(Model)
	if model.interruptArmed != "a" {
		t.Fatalf("first ctrl+c should arm, got %q", model.interruptArmed)
	}

	// An unrelated key cancels the arm (use a navigation key so the empty prompt
	// stays empty — a text key would clear input on the next Ctrl+C instead).
	updated, _ = model.handleKey(keyMsg("down"))
	model = updated.(Model)
	if model.interruptArmed != "" {
		t.Fatalf("other key should disarm, got %q", model.interruptArmed)
	}
	// The stale confirmation prompt must not linger after disarming.
	if strings.Contains(model.Status, "press Ctrl+C again") {
		t.Fatalf("disarm should clear the stale prompt, got %q", model.Status)
	}

	// A subsequent Ctrl+C re-arms instead of interrupting.
	updated, _ = model.handleKey(keyMsg("ctrl+c"))
	model = updated.(Model)
	if model.interruptArmed != "a" {
		t.Fatalf("ctrl+c after disarm should re-arm, got %q", model.interruptArmed)
	}
	if !strings.Contains(model.Status, "press Ctrl+C again") {
		t.Fatalf("status should re-prompt, got %q", model.Status)
	}
}

func TestCtrlCReArmsAfterWindowExpires(t *testing.T) {
	model := NewModel([]*agent.Session{agent.NewSession("a", config.AgentConfig{}, "")}, nil, 1000, "", 0, nil, nil)

	updated, _ := model.handleKey(keyMsg("ctrl+c"))
	model = updated.(Model)
	// Backdate the arm beyond the window: the next Ctrl+C must re-arm, not interrupt.
	model.interruptArmedAt = model.interruptArmedAt.Add(-2 * interruptArmWindow)

	updated, _ = model.handleKey(keyMsg("ctrl+c"))
	model = updated.(Model)
	if !strings.Contains(model.Status, "press Ctrl+C again") {
		t.Fatalf("expired arm should re-prompt, got %q", model.Status)
	}
	if model.interruptArmed != "a" {
		t.Fatalf("expired arm should re-arm, got %q", model.interruptArmed)
	}
}

func TestTargetPersonalityAndCategoryRecipients(t *testing.T) {
	model := personalityTestModel()

	model.handleTargetCommand([]string{"target", "personality", "builder"})
	if model.Target != TargetPersonality || model.TargetName != "builder" {
		t.Fatalf("target = %v %q, want builder personality", model.Target, model.TargetName)
	}
	recipients := model.recipientViewsForPersonality(model.TargetName)
	if got := agentNames(recipients); strings.Join(got, ",") != "codex,cursor" {
		t.Fatalf("personality recipients = %v", got)
	}

	model.handleTargetCommand([]string{"target", "category", "strategy"})
	if model.Target != TargetCategory || model.TargetName != "strategy" {
		t.Fatalf("target = %v %q, want strategy category", model.Target, model.TargetName)
	}
	recipients = model.recipientViewsForCategory(model.TargetName)
	if got := agentNames(recipients); strings.Join(got, ",") != "claude" {
		t.Fatalf("category recipients = %v", got)
	}
}

// assertTargetCycle drives toggleTarget through the expected sequence of steps.
func assertTargetCycle(t *testing.T, model *Model, want []targetStep) {
	t.Helper()
	for i, w := range want {
		model.toggleTarget()
		if model.Target != w.mode || model.TargetName != w.name {
			t.Fatalf("step %d: target = %v %q, want %v %q", i, model.Target, model.TargetName, w.mode, w.name)
		}
	}
}

func TestToggleTargetCyclesCategories(t *testing.T) {
	model := personalityTestModel() // group_by: category
	model.applyTarget(targetStep{mode: TargetAll})
	assertTargetCycle(t, &model, []targetStep{
		{mode: TargetCategory, name: "strategy"},       // Order 10
		{mode: TargetCategory, name: "implementation"}, // Order 20
		{mode: TargetFocused},
		{mode: TargetAll}, // wraps
		{mode: TargetCategory, name: "strategy"},
	})
}

func TestToggleTargetCyclesPersonalities(t *testing.T) {
	model := personalityTestModel()
	model.Config.UI.GroupBy = "personality"
	model.applyTarget(targetStep{mode: TargetAll})
	assertTargetCycle(t, &model, []targetStep{
		{mode: TargetPersonality, name: "architect"}, // Order 10
		{mode: TargetPersonality, name: "builder"},   // Order 20
		{mode: TargetFocused},
		{mode: TargetAll}, // wraps
	})
}

func TestToggleTargetGroupByNone(t *testing.T) {
	model := personalityTestModel()
	model.Config.UI.GroupBy = "none"
	model.applyTarget(targetStep{mode: TargetAll})
	// No groups: stays all <-> focused, the unchanged baseline.
	assertTargetCycle(t, &model, []targetStep{
		{mode: TargetFocused},
		{mode: TargetAll},
		{mode: TargetFocused},
	})
}

func TestToggleTargetSelfHealsStaleName(t *testing.T) {
	model := personalityTestModel() // group_by: category
	// An inconsistent state: an all-target carrying a leftover group name.
	model.Target = TargetAll
	model.TargetName = "stale"
	model.toggleTarget()
	if model.Target != TargetAll || model.TargetName != "" {
		t.Fatalf("toggle from stale all: target = %v %q, want all with empty name", model.Target, model.TargetName)
	}
}

func TestToggleTargetFromOffCycle(t *testing.T) {
	model := personalityTestModel() // group_by: category
	// A personality target set via /target is off-cycle when grouping by
	// category; advancing lands on broadcast-to-all rather than getting stuck.
	model.applyTarget(targetStep{mode: TargetPersonality, name: "architect"})
	model.toggleTarget()
	if model.Target != TargetAll || model.TargetName != "" {
		t.Fatalf("off-cycle toggle: target = %v %q, want all", model.Target, model.TargetName)
	}
}

func TestShowPersonalityFiltersDisplayedPanes(t *testing.T) {
	model := personalityTestModel()
	model.Width = 80
	model.Height = 18
	model.resizeAgents()
	model.appendOutput(model.Agents[0], "CLAUDE\n")
	model.appendOutput(model.Agents[1], "CODEX\n")
	model.appendOutput(model.Agents[2], "CURSOR\n")

	model.handleShowCommand([]string{"show", "personality", "builder"})
	if len(model.DisplayPersonalities) != 1 || !model.DisplayPersonalities["builder"] {
		t.Fatalf("display personalities = %v, want builder only", model.DisplayPersonalities)
	}
	if got := model.visibleAgentIndexes(); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("visible indexes = %v, want codex/cursor", got)
	}
	body := ansiRE.ReplaceAllString(model.View(), "")
	if strings.Contains(body, "CLAUDE") {
		t.Fatalf("filtered view should hide architect pane: %q", body)
	}
	if !strings.Contains(body, "CODEX") || !strings.Contains(body, "CURSOR") {
		t.Fatalf("filtered view should show builders: %q", body)
	}

	model.handleShowCommand([]string{"show", "all"})
	if len(model.DisplayPersonalities) != 0 {
		t.Fatalf("show all should clear filter: %v", model.DisplayPersonalities)
	}
}

func TestOverviewSpaceTogglesSelectedPersonality(t *testing.T) {
	model := personalityTestModel()
	model.ScreenMode = ScreenOverview
	model.OverviewIndex = 1 // codex -> builder

	updated, _ := model.handleOverviewKey(keyMsg("space"))
	model = updated.(Model)
	if model.DisplayPersonalities["builder"] {
		t.Fatalf("builder should be hidden after toggle: %v", model.DisplayPersonalities)
	}
	if !model.DisplayPersonalities["architect"] {
		t.Fatalf("other used personalities should remain visible: %v", model.DisplayPersonalities)
	}
}

func TestFileReferenceSuggestionsUseArrowsAndEnter(t *testing.T) {
	model := NewModel([]*agent.Session{agent.NewSession("a", config.AgentConfig{}, "")}, nil, 1000, "", 0, nil, nil)
	model.FileChoices = []string{"README.md", "internal/tui/model.go"}
	model.PromptInput = "inspect @internal/t"

	updated, cmd := model.handleKey(keyMsg("enter"))
	if cmd != nil {
		t.Fatal("accepting a file suggestion should not submit the prompt")
	}
	model = updated.(Model)
	if got, want := model.PromptInput, "inspect @internal/tui/model.go "; got != want {
		t.Fatalf("accepted file ref = %q, want %q", got, want)
	}
}

func personalityTestModel() Model {
	cfg := config.Config{
		UI: config.UIConfig{PageRows: 2, PageCols: 2, GroupBy: "category", MaxScrollbackLines: 1000},
		Agents: map[string]config.AgentConfig{
			"claude": {Personality: "architect"},
			"codex":  {Personality: "builder"},
			"cursor": {Personality: "builder"},
		},
		PersonalityCategories: map[string]config.PersonalityCategoryConfig{
			"strategy":       {Label: "Strategy", Order: 10},
			"implementation": {Label: "Implementation", Order: 20},
		},
		Personalities: map[string]config.PersonalityConfig{
			"architect": {Label: "Architect", Category: "strategy", Order: 10},
			"builder":   {Label: "Builder", Category: "implementation", Order: 20},
		},
	}
	cfg.Normalize()
	return NewModelWithConfig([]*agent.Session{
		agent.NewSession("claude", cfg.Agents["claude"], ""),
		agent.NewSession("codex", cfg.Agents["codex"], ""),
		agent.NewSession("cursor", cfg.Agents["cursor"], ""),
	}, nil, cfg, "", nil, 0, nil, nil)
}

func agentNames(views []*agentView) []string {
	names := make([]string, 0, len(views))
	for _, view := range views {
		names = append(names, view.Session.Name)
	}
	return names
}

func TestStartBuildRequiresStagedBuild(t *testing.T) {
	sessions := []*agent.Session{agent.NewSession("claude", config.AgentConfig{}, "")}
	model := NewModel(sessions, nil, 1000, "", 0, nil, nil)

	// /start-build with nothing staged is rejected.
	if cmd := model.cmdStartBuild(); cmd != nil {
		t.Fatal("expected nil cmd when no build is staged")
	}
	if !strings.Contains(model.Status, "/build first") {
		t.Fatalf("status = %q, want a 'run /build first' message", model.Status)
	}

	// Once /build has staged prompts, /start-build sends them and clears state.
	model.pendingBuild = map[string]string{"claude": "do the build"}
	if cmd := model.cmdStartBuild(); cmd == nil {
		t.Fatal("expected a send command when a build is staged")
	}
	if model.pendingBuild != nil {
		t.Fatal("staged build should be consumed after start")
	}
	if !strings.Contains(model.Status, "started") {
		t.Fatalf("status = %q, want a 'started' message", model.Status)
	}
}

func TestInChatOrchestrationGuardsWithoutController(t *testing.T) {
	sessions := []*agent.Session{agent.NewSession("claude", config.AgentConfig{}, "")}
	model := NewModel(sessions, nil, 1000, "", 0, nil, nil) // orch == nil

	model.PromptInput = "/plan do a thing"
	if cmd := model.submitInput(); cmd != nil {
		t.Fatal("expected nil cmd when orchestration is unavailable")
	}
	if !strings.Contains(model.Status, "unavailable") {
		t.Fatalf("status = %q, want an 'unavailable' message", model.Status)
	}

	// Plain message still returns no command and reports a send.
	model.PromptInput = "hello"
	_ = model.submitInput()
	if !strings.Contains(model.Status, "sent") {
		t.Fatalf("status = %q, want a send confirmation", model.Status)
	}
}

func TestBeginPhaseResetsDirectInputState(t *testing.T) {
	root := initTUITestRepo(t)
	chdirTUI(t, root)

	cfg := config.Config{
		Agents: map[string]config.AgentConfig{
			"builder": {Enabled: true, Command: []string{"true"}},
			"planner": {
				Enabled: true,
				Command: []string{"true"},
				Orchestration: config.OrchestrationConfig{
					ExcludeBuild: true,
				},
			},
		},
		Sessions: config.SessionConfig{RootDir: filepath.Join(root, ".council", "runs")},
	}
	cfg.Normalize()
	ctrl, err := orchestrate.NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.StartRun("do it"); err != nil {
		t.Fatal(err)
	}

	model := NewModel([]*agent.Session{agent.NewSession("old", config.AgentConfig{}, "")}, nil, 1000, "", 0, nil, ctrl)
	model.Width = 80
	model.Height = 24
	model.InputMode = InputDirect
	model.PromptInput = "stale"
	model.Target = TargetFocused

	model.beginPhase("plan", config.PhasePlan, map[string]string{
		"builder": "builder prompt",
		"planner": "planner prompt",
	})

	if model.InputMode != InputComposer {
		t.Fatalf("input mode = %v, want composer", model.InputMode)
	}
	if model.PromptInput != "" {
		t.Fatalf("prompt input = %q, want empty", model.PromptInput)
	}
	if model.Target != TargetAll {
		t.Fatalf("target = %v, want all", model.Target)
	}
	if len(model.Agents) != 2 {
		t.Fatalf("phase agents = %d, want 2", len(model.Agents))
	}
}

func TestCmdStatusRefreshesProgressFromDisk(t *testing.T) {
	root := initTUITestRepo(t)
	chdirTUI(t, root)

	cfg := config.Config{
		Agents: map[string]config.AgentConfig{
			"planner": {Enabled: true, Command: []string{"true"}},
		},
		Sessions: config.SessionConfig{RootDir: filepath.Join(root, ".council", "runs")},
	}
	cfg.Normalize()
	ctrl, err := orchestrate.NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.StartRun("do it"); err != nil {
		t.Fatal(err)
	}
	run := ctrl.Run()
	if err := os.WriteFile(run.PlanPath("planner"), []byte("plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run.WriteResult(orchestrate.Result{WinnerAgent: "planner", WinnerLetter: "A", Points: map[string]int{}, Firsts: map[string]int{}}, nil); err != nil {
		t.Fatal(err)
	}

	model := NewModel([]*agent.Session{agent.NewSession("planner", config.AgentConfig{}, "")}, nil, 1000, "", 0, nil, ctrl)
	model.Width = 80
	model.Height = 24
	model.phase = "vote"
	// Stale snapshot from before the artifacts landed.
	model.progress = &runProgress{Stamp: run.Stamp}

	model.cmdStatus()

	if model.progress == nil || model.progress.Plans != 1 {
		t.Fatalf("cmdStatus did not refresh progress: %+v", model.progress)
	}
	if model.progress.PlanWinner != "planner" {
		t.Fatalf("refreshed winner = %q, want planner", model.progress.PlanWinner)
	}
	if !strings.Contains(model.artifactView, "Plans 1") {
		t.Fatalf("status body missing fresh counts: %q", model.artifactView)
	}
}

// TestResumedRefineFinishClearsStaleVote guards the resume path of the
// refine→revote round. A refine round runs as a plan phase and is reopened
// under the "plan" phase label (config.PhasePlan) on resume, so finishPhase must
// still run the revote reset — clearing the prior vote's anonymized plan copies,
// ballots, tally, and the .orig.md backups — keyed off the refine backups rather
// than the label. Without the fix the resumed round finishes as a plain plan
// phase and the next /vote tallies stale ballots against the pre-refine plans.
func TestResumedRefineFinishClearsStaleVote(t *testing.T) {
	root := initTUITestRepo(t)
	chdirTUI(t, root)

	cfg := config.Config{
		Agents: map[string]config.AgentConfig{
			"a": {Enabled: true, Command: []string{"true"}},
			"b": {Enabled: true, Command: []string{"true"}},
		},
		Sessions: config.SessionConfig{RootDir: filepath.Join(root, ".council", "runs")},
	}
	cfg.Normalize()

	ctrl, err := orchestrate.NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.StartRun("do it"); err != nil {
		t.Fatal(err)
	}
	run := ctrl.Run()

	// Initial plans + a full first vote, so the run carries the stale vote
	// artifacts a revote must replace: anonymized plan copies, per-voter
	// ballots, the letter assignments, and the tally.
	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(run.PlanPath(name), []byte("plan "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ctrl.VotePrompts(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(run.VotePath(name), []byte("RANKING: A > B\nWINNER: A"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ctrl.CollectVotesAndTally(); err != nil {
		t.Fatal(err)
	}

	// Start the refine round (backs each plan up to .orig.md and removes the
	// live files), record the active phase as the TUI does, then write only a's
	// refined plan back — the crash happens before b finishes.
	if _, err := ctrl.RefinePrompts(""); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.SaveActivePhase(config.PhasePlan, []string{"a", "b"}, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(run.PlanPath("a"), []byte("refined plan a"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reopen the run in a fresh controller + model and resume exactly as a
	// relaunched TUI would: the refine round comes back under the "plan" label.
	resumed, err := orchestrate.NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := resumed.UseRun(run.Stamp); err != nil {
		t.Fatal(err)
	}
	model := NewModel([]*agent.Session{agent.NewSession("a", config.AgentConfig{}, "")}, nil, 1000, "", 0, nil, resumed)
	model.Width = 80
	model.Height = 24
	model.resumeRun("")
	if model.phase != string(config.PhasePlan) {
		t.Fatalf("resumed refine phase label = %q, want %q (this label is why the reset was skipped pre-fix)", model.phase, config.PhasePlan)
	}

	// b finishes its refined plan; the watched phase now completes.
	if err := os.WriteFile(run.PlanPath("b"), []byte("refined plan b"), 0o644); err != nil {
		t.Fatal(err)
	}
	model.finishPhase()

	// The refine reset must have run despite the "plan" label: no stale vote
	// artifacts and no leftover refine backups survive.
	stale := map[string]string{
		"vote result.json": run.ResultPath(),
		"vote result.md":   run.SummaryPath(),
		"plan-assignments": run.VoteRefsPath(),
		"ballot a":         run.VotePath("a"),
		"ballot b":         run.VotePath("b"),
	}
	for label, path := range stale {
		if fileExists(path) {
			t.Fatalf("resumed refine finish left stale %s at %s", label, path)
		}
	}
	for _, name := range []string{"a", "b"} {
		orig := strings.TrimSuffix(run.PlanPath(name), ".md") + ".orig.md"
		if fileExists(orig) {
			t.Fatalf("resumed refine finish left the refine backup %s", orig)
		}
	}
	if anon, _ := filepath.Glob(filepath.Join(run.VotesDir(), "plan-*.md")); len(anon) != 0 {
		t.Fatalf("resumed refine finish left stale anonymized plans: %v", anon)
	}

	// And the next /vote re-anonymizes from the REFINED plans, not the originals.
	if _, err := resumed.VotePrompts(); err != nil {
		t.Fatalf("revote after resumed refine: %v", err)
	}
	refs, err := run.LoadVoteRefs()
	if err != nil {
		t.Fatalf("revote refs missing: %v", err)
	}
	var letterForA string
	for _, r := range refs {
		if r.Agent == "a" {
			letterForA = r.Letter
		}
	}
	if letterForA == "" {
		t.Fatal("revote assigned no letter to a")
	}
	anon, err := os.ReadFile(run.AnonPlanPath(letterForA))
	if err != nil {
		t.Fatalf("revote anonymized copy for a missing: %v", err)
	}
	if string(anon) != "refined plan a" {
		t.Fatalf("revote anonymized a = %q, want the refined plan content", string(anon))
	}
}

func TestStaleExitFromReplacedSessionIsIgnored(t *testing.T) {
	oldSession := agent.NewSession("codex", config.AgentConfig{}, "")
	newSession := agent.NewSession("codex", config.AgentConfig{}, "")
	model := NewModel([]*agent.Session{newSession}, nil, 1000, "", 0, nil, nil)
	code := -1

	updated, _ := model.Update(AgentExitMsg{Name: "codex", Session: oldSession, ExitCode: &code})
	afterStale := updated.(Model)
	if afterStale.Agents[0].Session.Done {
		t.Fatal("stale exit marked the replacement session done")
	}
	if body := strings.Join(afterStale.Agents[0].Lines, "\n"); strings.Contains(body, "exited") {
		t.Fatalf("stale exit was rendered into replacement pane: %q", body)
	}

	updated, _ = afterStale.Update(AgentExitMsg{Name: "codex", Session: newSession, ExitCode: &code})
	afterCurrent := updated.(Model)
	if !afterCurrent.Agents[0].Session.Done {
		t.Fatal("current session exit was ignored")
	}
}

func initTUITestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runGit("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", ".")
	runGit("commit", "-m", "init")
	return dir
}

func chdirTUI(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(old)
	})
}

func TestSplitEscapeSequenceDoesNotLeak(t *testing.T) {
	session := agent.NewSession("x", config.AgentConfig{}, "")
	model := NewModel([]*agent.Session{session}, nil, 1000, "", 0, nil, nil)
	model.Width = 40
	model.Height = 6
	model.resizeAgents()

	view := model.Agents[0]
	// A bold SGR is split across two reads: "\x1b[" then "1m". The tail must be
	// buffered, not rendered as the literal text "1m".
	model.appendOutput(view, "AB\x1b[")
	model.appendOutput(view, "1mC\x1b[0m")

	plain := ansiRE.ReplaceAllString(strings.Join(view.screenLines(view.Height, view.Width), "\n"), "")
	if strings.Contains(plain, "1m") {
		t.Fatalf("split SGR leaked as literal text: %q", plain)
	}
	if !strings.Contains(plain, "ABC") {
		t.Fatalf("expected ABC after rejoining split escape, got %q", plain)
	}
}

func TestTranscriptRendererUsesCleanScrollback(t *testing.T) {
	session := agent.NewSession("codex", config.AgentConfig{
		Terminal: config.TerminalConfig{Renderer: "transcript"},
	}, "")
	model := NewModel([]*agent.Session{session}, nil, 1000, "", 0, nil, nil)
	model.Width = 60
	model.Height = 16
	model.resizeAgents()

	view := model.Agents[0]
	model.appendOutput(view, "\x1b[31mfirst\x1b[0m\nsecond\n")
	body := strings.Join(view.bodyLines(4, 20), "\n")

	if strings.Contains(body, "\x1b[31m") {
		t.Fatalf("transcript renderer leaked ansi: %q", body)
	}
	if !strings.Contains(body, "first") || !strings.Contains(body, "second") {
		t.Fatalf("transcript renderer missing output: %q", body)
	}
}

func TestSubmitSequenceNames(t *testing.T) {
	tests := map[string]string{
		"":                  "\r",
		"cr":                "\r",
		"lf":                "\n",
		"crlf":              "\r\n",
		"ctrl+u":            "\x15",
		"csi-enter":         "\x1b[13;1u",
		"csi-enter-legacy":  "\x1b[13u",
		"csi-ctrl-enter":    "\x1b[13;5u",
		"csi-shift-enter":   "\x1b[13;2u",
		"raw:\x1b[13;5u":    "\x1b[13;5u",
		"unknown-fallback":  "\r",
		"kitty-ctrl-enter":  "\x1b[13;5u",
		"kitty-shift-enter": "\x1b[13;2u",
	}

	for name, want := range tests {
		if got := submitSequence(name); got != want {
			t.Fatalf("submitSequence(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestOptionalSequenceNames(t *testing.T) {
	tests := map[string]string{
		"":         "",
		"none":     "",
		"ctrl+u":   "\x15",
		"raw:\x1b": "\x1b",
		"unknown":  "",
	}

	for name, want := range tests {
		if got := optionalSequence(name); got != want {
			t.Fatalf("optionalSequence(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestLinePayloadSendModes(t *testing.T) {
	codex := config.TerminalConfig{
		SendMode:           "paste",
		BeforeSendSequence: "ctrl+u",
		SubmitSequence:     "cr",
	}
	if got, want := linePayload(codex, "test"), "\x15\x1b[200~test\x1b[201~\r"; got != want {
		t.Fatalf("codex payload = %q, want %q", got, want)
	}

	cursor := config.TerminalConfig{
		SendMode:            "paste",
		BeforeSendSequence:  "ctrl+u",
		SubmitSequence:      "csi-enter",
		AfterSubmitSequence: "ctrl+u",
	}
	if got, want := linePayload(cursor, "test"), "\x15\x1b[200~test\x1b[201~\x1b[13;1u\x15"; got != want {
		t.Fatalf("cursor payload = %q, want %q", got, want)
	}

	plain := config.TerminalConfig{SendMode: "type", SubmitSequence: "cr"}
	if got, want := linePayload(plain, "test"), "test\r"; got != want {
		t.Fatalf("plain payload = %q, want %q", got, want)
	}
}

func TestBuildStagesPromptsForWorkersNotCurrentPanes(t *testing.T) {
	root := initTUITestRepo(t)
	chdirTUI(t, root)

	cfg := config.Config{
		Agents: map[string]config.AgentConfig{
			"worker":   {Enabled: true, Command: []string{"true"}, Role: []string{config.RoleWorker}},
			"reviewer": {Enabled: true, Command: []string{"true"}, Role: []string{config.RoleReviewer}},
		},
		Sessions: config.SessionConfig{RootDir: filepath.Join(root, ".council", "runs")},
	}
	cfg.Normalize()
	ctrl, err := orchestrate.NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.StartRun("do it"); err != nil {
		t.Fatal(err)
	}
	// The worker produced the winning plan.
	if err := os.WriteFile(ctrl.Run().PlanPath("worker"), []byte("plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ctrl.Run().ResultPath(), []byte(`{"winner_agent":"worker"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Simulate the post-vote state: the current pane is the reviewer.
	model := NewModel([]*agent.Session{agent.NewSession("reviewer", config.AgentConfig{}, "")}, nil, 1000, "", 0, nil, ctrl)
	model.Width = 80
	model.Height = 24
	model.resizeAgents()

	model.cmdBuild()

	if model.pendingBuild["worker"] == "" {
		t.Fatalf("/build should stage a prompt for the worker, got %v", model.pendingBuild)
	}
	if _, ok := model.pendingBuild["reviewer"]; ok {
		t.Fatalf("/build should not stage for the reviewer, got %v", model.pendingBuild)
	}
}
