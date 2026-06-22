package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
)

// transcriptModel builds a single-pane model whose pane renders the plain-text
// transcript, seeded with `count` numbered lines.
func transcriptModel(t *testing.T, count int) (Model, *agentView) {
	t.Helper()
	cfg := config.AgentConfig{}
	cfg.Terminal.Renderer = "transcript"
	session := agent.NewSession("alpha", cfg, "")
	model := NewModel([]*agent.Session{session}, nil, 1000, "", 0, nil, nil)
	model.Width = 40
	model.Height = 20
	model.resizeAgents()
	view := model.Agents[0]
	for i := 1; i <= count; i++ {
		model.appendOutput(view, fmt.Sprintf("line %d\n", i))
	}
	return model, view
}

func TestTranscriptScrollOffsetWindow(t *testing.T) {
	_, view := transcriptModel(t, 50)
	const height, width = 10, 30

	live := view.transcriptLines(height, width)
	if last := strings.TrimSpace(live[len(live)-1]); last != "line 50" {
		t.Fatalf("offset 0 should be bottom-anchored, got last %q", last)
	}

	// Scroll up five lines: the window shifts back by five.
	view.ScrollOffset = 5
	scrolled := view.transcriptLines(height, width)
	if last := strings.TrimSpace(scrolled[len(scrolled)-1]); last != "line 45" {
		t.Fatalf("offset 5 should end at line 45, got %q", last)
	}

	// Over-scrolling clamps to the top of the history rather than running off it.
	view.ScrollOffset = 9999
	top := view.transcriptLines(height, width)
	if first := strings.TrimSpace(top[0]); first != "line 1" {
		t.Fatalf("over-scroll should clamp to line 1, got %q", first)
	}
}

func TestMaxScrollOffset(t *testing.T) {
	_, view := transcriptModel(t, 50)
	if got := view.maxScrollOffset(10, 30); got != 40 {
		t.Fatalf("maxScrollOffset = %d, want 40", got)
	}
	// Fewer lines than the window height → nothing to scroll.
	_, small := transcriptModel(t, 3)
	if got := small.maxScrollOffset(10, 30); got != 0 {
		t.Fatalf("maxScrollOffset for short history = %d, want 0", got)
	}
}

func TestHitTestPaneSingle(t *testing.T) {
	model, _ := transcriptModel(t, 1)
	top := model.headerLines()

	// A click inside the body maps to the only pane.
	if idx, ok := model.hitTestPane(5, top+1); !ok || idx != 0 {
		t.Fatalf("body click = (%d,%v), want (0,true)", idx, ok)
	}
	// A click in the header is not a pane.
	if _, ok := model.hitTestPane(5, 0); ok {
		t.Fatalf("header click should not hit a pane")
	}
	// Out of bounds is not a pane.
	if _, ok := model.hitTestPane(9999, top+1); ok {
		t.Fatalf("out-of-bounds click should not hit a pane")
	}
}

func TestMouseWheelScrollsFocusedPane(t *testing.T) {
	model, view := transcriptModel(t, 50)
	model.ScreenMode = ScreenPanes

	up := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp, X: 5, Y: model.headerLines() + 1}
	updated, _ := model.Update(up)
	model = updated.(Model)
	if view.ScrollOffset != mouseScrollStep {
		t.Fatalf("wheel up: ScrollOffset = %d, want %d", view.ScrollOffset, mouseScrollStep)
	}

	down := up
	down.Button = tea.MouseButtonWheelDown
	updated, _ = model.Update(down)
	if view.ScrollOffset != 0 {
		t.Fatalf("wheel down should return to live tail, got %d", view.ScrollOffset)
	}
}

func TestMouseDisabledIsNoop(t *testing.T) {
	model, view := transcriptModel(t, 50)
	model.ScreenMode = ScreenPanes
	model.mouseOn = false

	up := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp, X: 5, Y: model.headerLines() + 1}
	model.Update(up)
	if view.ScrollOffset != 0 {
		t.Fatalf("mouse off: ScrollOffset = %d, want 0", view.ScrollOffset)
	}
}

// TestScrolledPaneRendersMarkerAndHistory exercises the same render path the
// wheel triggers (VHS's xterm.js can't deliver mouse events, so this stands in
// for it): a scrolled pane shows the ↑N marker and earlier history.
func TestScrolledPaneRendersMarkerAndHistory(t *testing.T) {
	model, view := transcriptModel(t, 100)
	width, height := model.Width, model.bodyHeight()

	live := ansiRE.ReplaceAllString(strings.Join(model.renderPane(0, width, height), "\n"), "")
	if strings.Contains(live, "↑") {
		t.Fatalf("live pane should not show a scroll marker:\n%s", live)
	}
	if !strings.Contains(live, "line 100") {
		t.Fatalf("live pane should show the latest line:\n%s", live)
	}

	view.ScrollOffset = 40
	scrolled := ansiRE.ReplaceAllString(strings.Join(model.renderPane(0, width, height), "\n"), "")
	if !strings.Contains(scrolled, "↑40") {
		t.Fatalf("scrolled pane should show the ↑40 marker:\n%s", scrolled)
	}
	if strings.Contains(scrolled, "line 100") {
		t.Fatalf("scrolled pane should have scrolled past the latest line:\n%s", scrolled)
	}
	if !strings.Contains(scrolled, "line 60") {
		t.Fatalf("scrolled pane should show earlier history (line 60):\n%s", scrolled)
	}
}

func TestEncodeSGRMouse(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.MouseMsg
		col  int
		row  int
		want string
	}{
		{"left press", tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}, 12, 5, "\x1b[<0;12;5M"},
		{"left release", tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}, 12, 5, "\x1b[<0;12;5m"},
		{"wheel up", tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp}, 3, 4, "\x1b[<64;3;4M"},
		{"ctrl+right", tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, Ctrl: true}, 1, 1, "\x1b[<18;1;1M"},
	}
	for _, c := range cases {
		if got := encodeSGRMouse(c.msg, c.col, c.row); got != c.want {
			t.Errorf("%s: encodeSGRMouse = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestDirectModeMousePTYCoords verifies the translation a direct-mode click
// feeds to the PTY: the focused pane's content origin maps the click to local
// 1-based coordinates and encodes a matching SGR report. (The actual PTY write
// needs a live process, so it isn't asserted here.)
func TestDirectModeMousePTYCoords(t *testing.T) {
	session := agent.NewSession("alpha", config.AgentConfig{}, "")
	model := NewModel([]*agent.Session{session}, nil, 1000, "", 0, nil, nil)
	model.Width = 40
	model.Height = 20
	model.resizeAgents()

	x0, y0, w, h, ok := model.paneContentRect(0)
	if !ok || w <= 0 || h <= 0 {
		t.Fatalf("paneContentRect = (%d,%d,%d,%d,%v)", x0, y0, w, h, ok)
	}
	click := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x0 + 4, Y: y0 + 2}
	got := encodeSGRMouse(click, click.X-x0+1, click.Y-y0+1)
	if want := "\x1b[<0;5;3M"; got != want {
		t.Fatalf("encoded click = %q, want %q", got, want)
	}
}

func TestDirectModeClickRefocuses(t *testing.T) {
	a := agent.NewSession("alpha", config.AgentConfig{}, "")
	b := agent.NewSession("bravo", config.AgentConfig{}, "")
	model := NewModel([]*agent.Session{a, b}, nil, 1000, "", 0, nil, nil)
	model.Width = 80
	model.Height = 20
	model.resizeAgents()
	model.ScreenMode = ScreenPanes
	model.InputMode = InputDirect
	model.FocusedIndex = 0

	// Click inside the second pane: in direct mode that refocuses it.
	x0, y0, _, _, ok := model.paneContentRect(1)
	if !ok {
		t.Fatal("paneContentRect(1) not ok")
	}
	click := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x0 + 1, Y: y0 + 1}
	updated, _ := model.Update(click)
	if got := updated.(Model).FocusedIndex; got != 1 {
		t.Fatalf("direct-mode click on pane 2 should refocus it, FocusedIndex = %d", got)
	}
}

func TestComposerDropsLeakedMouseFragment(t *testing.T) {
	if !isMouseReportFragment("[<64;30;10M") {
		t.Fatal("expected SGR body to be detected as a mouse fragment")
	}
	if isMouseReportFragment("hello") {
		t.Fatal("plain text must not be treated as a mouse fragment")
	}
	// Without the "<" introducer it's plausible user input, not a leaked report.
	if isMouseReportFragment("64;30;10M") {
		t.Fatal("plain numeric input must not be treated as a mouse fragment")
	}

	model, _ := transcriptModel(t, 1)
	model.ScreenMode = ScreenPanes
	model.InputMode = InputComposer
	model.PromptInput = "hi"

	leak := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<64;30;10M")}
	updated, _ := model.Update(leak)
	if got := updated.(Model).PromptInput; got != "hi" {
		t.Fatalf("composer should ignore the leaked fragment, got %q", got)
	}

	typed := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}
	updated, _ = updated.(Model).Update(typed)
	if got := updated.(Model).PromptInput; got != "hix" {
		t.Fatalf("normal typing should still append, got %q", got)
	}
}

// TestDirectModeFallsBackWhenChildMouseOff verifies that when the focused
// program has not enabled mouse tracking, direct-mode wheel scrolls council's
// own pane history instead of forwarding garbage to the PTY.
func TestDirectModeFallsBackWhenChildMouseOff(t *testing.T) {
	model, view := transcriptModel(t, 50)
	model.ScreenMode = ScreenPanes
	model.InputMode = InputDirect
	view.MouseModeOn = false // child did not opt into mouse

	up := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp, X: 5, Y: model.headerLines() + 1}
	updated, _ := model.Update(up)
	if got := updated.(Model).Agents[0].ScrollOffset; got != mouseScrollStep {
		t.Fatalf("child mouse off: wheel should scroll council history, ScrollOffset = %d, want %d", got, mouseScrollStep)
	}
}

// TestDirectModeForwardsWhenChildMouseOn verifies that when the program enabled
// mouse tracking, direct-mode wheel is forwarded to the PTY (so council does
// not also scroll its own history).
func TestDirectModeForwardsWhenChildMouseOn(t *testing.T) {
	model, view := transcriptModel(t, 50)
	model.ScreenMode = ScreenPanes
	model.InputMode = InputDirect
	view.MouseModeOn = true

	x0, y0, _, _, _ := model.paneContentRect(0)
	up := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp, X: x0 + 1, Y: y0 + 1}
	updated, _ := model.Update(up)
	if got := updated.(Model).Agents[0].ScrollOffset; got != 0 {
		t.Fatalf("child mouse on: wheel should forward to PTY, not scroll; ScrollOffset = %d", got)
	}
}

func TestEditorMouseIgnoresOutsideBody(t *testing.T) {
	model, _ := transcriptModel(t, 1)
	model.ScreenMode = ScreenEditor
	model.editorTree = []editorNode{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	model.editorTreeIndex = 1

	// A click in the header (above the editor body) must not move the tree.
	click := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 1, Y: 0}
	updated, _ := model.Update(click)
	if got := updated.(Model).editorTreeIndex; got != 1 {
		t.Fatalf("header click should be ignored, editorTreeIndex = %d, want 1", got)
	}
}

func TestEditorTreeWheelMovesSelection(t *testing.T) {
	model, _ := transcriptModel(t, 1)
	model.ScreenMode = ScreenEditor
	model.editorTree = []editorNode{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	model.editorTreeIndex = 0

	// Wheel down over the tree (x in the tree column) advances the selection.
	down := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown, X: 1, Y: model.headerLines() + 3}
	updated, _ := model.Update(down)
	model = updated.(Model)
	if model.editorTreeIndex != 1 {
		t.Fatalf("wheel down over tree: editorTreeIndex = %d, want 1", model.editorTreeIndex)
	}
	up := down
	up.Button = tea.MouseButtonWheelUp
	updated, _ = model.Update(up)
	if got := updated.(Model).editorTreeIndex; got != 0 {
		t.Fatalf("wheel up over tree: editorTreeIndex = %d, want 0", got)
	}
}

// TestScrollAnchorsOnNewOutput checks that output arriving while scrolled up
// keeps the viewed content anchored (ScrollOffset grows by the new line count)
// rather than letting the window drift toward the live bottom.
func TestScrollAnchorsOnNewOutput(t *testing.T) {
	model, view := transcriptModel(t, 50)
	view.ScrollOffset = 10
	model.appendOutput(view, "x1\nx2\nx3\n") // 3 new finalized lines
	if view.ScrollOffset != 13 {
		t.Fatalf("ScrollOffset after 3 new lines = %d, want 13", view.ScrollOffset)
	}
	// A partial line (no newline) adds nothing yet.
	model.appendOutput(view, "partial")
	if view.ScrollOffset != 13 {
		t.Fatalf("partial output must not move the anchor, ScrollOffset = %d", view.ScrollOffset)
	}
	// At the live tail (offset 0) new output is not anchored.
	view.ScrollOffset = 0
	model.appendOutput(view, "y1\n")
	if view.ScrollOffset != 0 {
		t.Fatalf("live tail must stay at 0, got %d", view.ScrollOffset)
	}
}

// TestRenderClampsStaleOffset checks that rendering clamps and persists an
// over-large ScrollOffset to the current content window.
func TestRenderClampsStaleOffset(t *testing.T) {
	model, view := transcriptModel(t, 50)
	view.ScrollOffset = 9999
	_ = model.renderPane(0, model.Width, model.bodyHeight())
	if view.ScrollOffset >= 9999 || view.ScrollOffset < 0 {
		t.Fatalf("stale offset should be clamped, got %d", view.ScrollOffset)
	}
}

func TestEditorSeparatorIgnored(t *testing.T) {
	model, _ := transcriptModel(t, 1)
	model.ScreenMode = ScreenEditor
	model.editorTree = []editorNode{{Name: "a"}, {Name: "b"}}
	model.editorTreeIndex = 1
	treeW := model.editorTreeWidth()

	// A click on the 1-column separator is ignored (no tree move, no panic).
	click := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: treeW, Y: model.headerLines() + 1}
	updated, _ := model.Update(click)
	if got := updated.(Model).editorTreeIndex; got != 1 {
		t.Fatalf("separator click should be ignored, editorTreeIndex = %d, want 1", got)
	}
}

func TestClampIndex(t *testing.T) {
	cases := []struct{ i, n, want int }{
		{-1, 5, 0},
		{0, 5, 0},
		{4, 5, 4},
		{7, 5, 4},
		{0, 0, 0},
		{3, 0, 0},
	}
	for _, c := range cases {
		if got := clampIndex(c.i, c.n); got != c.want {
			t.Errorf("clampIndex(%d,%d) = %d, want %d", c.i, c.n, got, c.want)
		}
	}
}
