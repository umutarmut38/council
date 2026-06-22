package tui

import (
	"fmt"
	"regexp"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/umutarmut38/council/internal/agent"
)

// mouseScrollStep is how many transcript lines one wheel notch scrolls a pane.
const mouseScrollStep = 3

// mouseReportRE matches the body of an SGR/urxvt mouse report (with an optional
// leading ESC, "[", and "<"), e.g. "\x1b[<64;30;10M", "[<0;5;5m", or
// "64;30;10M". Used to drop such fragments if they leak into text input. Real
// keyboard input never matches this exact shape.
var mouseReportRE = regexp.MustCompile(`^\x1b?\[?<?[0-9]{1,4};[0-9]{1,4};[0-9]{1,4}[Mm]$`)

// isMouseReportFragment reports whether s is a leaked mouse-report escape body.
func isMouseReportFragment(s string) bool {
	return mouseReportRE.MatchString(s)
}

// handleMouseMsg routes wheel and click events. It is screen-mode aware: panes
// scroll their history / focus on click; the list and pager screens move their
// selection / scroll offset. No-op while mouse capture is toggled off.
//
// In direct mode and the integrated editor the mouse is instead forwarded to the
// agent's PTY (re-encoded as an SGR mouse sequence with pane-local coordinates)
// so mouse-aware programs (nvim, less, the agent's own TUI) receive it.
func (m Model) handleMouseMsg(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !m.mouseOn {
		return m, nil
	}

	switch m.ScreenMode {
	case ScreenPanes:
		if m.InputMode == InputDirect {
			return m.handleDirectMouse(msg)
		}
		return m.handlePanesMouse(msg)
	case ScreenEditor:
		return m.handleEditorMouse(msg)
	case ScreenArtifacts:
		return m.handleArtifactsMouse(msg)
	case ScreenOverview:
		m.scrollOverview(wheelDelta(msg))
	case ScreenSettings:
		m.scrollSettings(wheelDelta(msg))
	case ScreenCompare:
		m.scrollCompare(wheelDelta(msg))
	case ScreenRuns:
		m.scrollRuns(wheelDelta(msg))
	}
	return m, nil
}

// handleDirectMouse forwards mouse events to the focused agent's PTY while in
// direct mode. A click on a different pane refocuses instead, so the keyboard-
// free workflow still works.
func (m Model) handleDirectMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	idx := m.FocusedIndex
	if idx < 0 || idx >= len(m.Agents) {
		return m, nil
	}
	x0, y0, w, h, ok := m.paneContentRect(idx)
	inFocused := ok && msg.X >= x0 && msg.X < x0+w && msg.Y >= y0 && msg.Y < y0+h
	if !inFocused {
		// A left-click elsewhere refocuses that pane (and that becomes the new
		// direct-input target); other events outside the pane are ignored.
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			if hit, hok := m.hitTestPane(msg.X, msg.Y); hok {
				m.FocusedIndex = hit
				m.ensurePageForFocus()
				m.Status = "direct input to " + m.Agents[hit].Session.Name
			}
		}
		return m, nil
	}
	m.forwardMouseToPTY(m.Agents[idx].Session, msg, x0, y0)
	return m, nil
}

// handleEditorMouse forwards mouse to the integrated editor PTY when the event
// is over the editor pane; over the file tree the wheel moves the tree
// selection and a click selects/opens.
func (m Model) handleEditorMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	treeW := m.editorTreeWidth()
	top := m.headerLines()
	overTree := msg.X < treeW

	if overTree {
		switch {
		case msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown:
			m.editorTreeIndex = clampIndex(m.editorTreeIndex+wheelDelta(msg), len(m.editorTree))
		case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
			m.editorPaneFocused = false
			row := msg.Y - top - 1 // first tree row is the "FILES" heading
			if target := m.editorTreeTop + row; row >= 0 && target >= 0 && target < len(m.editorTree) {
				m.editorTreeIndex = target
			}
		}
		return m, nil
	}

	// Over the editor pane: focus it and forward to its PTY.
	if m.editorView == nil {
		return m, nil
	}
	m.editorPaneFocused = true
	x0 := treeW + 1 // tree + 1-column separator
	m.forwardMouseToPTY(m.editorView.Session, msg, x0, top)
	return m, nil
}

// forwardMouseToPTY re-encodes a mouse event in the target program's local
// (1-based) coordinates and writes it as an SGR (1006) sequence to the session.
func (m *Model) forwardMouseToPTY(session *agent.Session, msg tea.MouseMsg, x0, y0 int) {
	if session == nil {
		return
	}
	col := msg.X - x0 + 1
	row := msg.Y - y0 + 1
	if col < 1 {
		col = 1
	}
	if row < 1 {
		row = 1
	}
	if err := session.WriteString(encodeSGRMouse(msg, col, row)); err != nil {
		m.Status = err.Error()
	}
}

// encodeSGRMouse renders a mouse event as an SGR-extended (1006) report,
// e.g. "\x1b[<0;12;5M" for a left press at col 12 row 5.
func encodeSGRMouse(msg tea.MouseMsg, col, row int) string {
	code := mouseButtonCode(msg.Button)
	if msg.Action == tea.MouseActionMotion {
		code += 32
	}
	if msg.Shift {
		code += 4
	}
	if msg.Alt {
		code += 8
	}
	if msg.Ctrl {
		code += 16
	}
	final := byte('M') // press / wheel
	if msg.Action == tea.MouseActionRelease {
		final = 'm'
	}
	return fmt.Sprintf("\x1b[<%d;%d;%d%c", code, col, row, final)
}

// mouseButtonCode is the low-bit SGR button number for a button.
func mouseButtonCode(b tea.MouseButton) int {
	switch b {
	case tea.MouseButtonLeft:
		return 0
	case tea.MouseButtonMiddle:
		return 1
	case tea.MouseButtonRight:
		return 2
	case tea.MouseButtonWheelUp:
		return 64
	case tea.MouseButtonWheelDown:
		return 65
	case tea.MouseButtonWheelLeft:
		return 66
	case tea.MouseButtonWheelRight:
		return 67
	case tea.MouseButtonBackward:
		return 128
	case tea.MouseButtonForward:
		return 129
	default:
		return 0
	}
}

// wheelDelta is -1 for a wheel-up notch, +1 for wheel-down, 0 otherwise. It maps
// a wheel event to a selection-move direction (up = toward earlier items).
func wheelDelta(msg tea.MouseMsg) int {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return -1
	case tea.MouseButtonWheelDown:
		return 1
	}
	return 0
}

func (m Model) handlePanesMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		// Scroll the pane under the cursor, or the focused pane if the cursor
		// isn't over a pane (e.g. in the header).
		index := m.FocusedIndex
		if hit, ok := m.hitTestPane(msg.X, msg.Y); ok {
			index = hit
		}
		if index < 0 || index >= len(m.Agents) {
			return m, nil
		}
		view := m.Agents[index]
		width, height, ok := m.paneInnerSize(index)
		if !ok {
			return m, nil
		}
		if msg.Button == tea.MouseButtonWheelUp {
			view.ScrollOffset += mouseScrollStep
		} else {
			view.ScrollOffset -= mouseScrollStep
		}
		if max := view.maxScrollOffset(height, width); view.ScrollOffset > max {
			view.ScrollOffset = max
		}
		if view.ScrollOffset < 0 {
			view.ScrollOffset = 0
		}
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		if index, ok := m.hitTestPane(msg.X, msg.Y); ok {
			m.FocusedIndex = index
			m.ensurePageForFocus()
			m.Status = "focused " + m.Agents[index].Session.Name
		}
	}
	return m, nil
}

func (m Model) handleArtifactsMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	delta := wheelDelta(msg)
	if delta == 0 {
		return m, nil
	}
	if m.artifactView != "" {
		// Read-only pager: scroll the offset (clamped at the top; the renderer
		// bounds the bottom).
		m.artifactTop = max0(m.artifactTop + delta)
		return m, nil
	}
	// Artifact list: move the selection.
	if delta < 0 {
		if m.ArtifactIndex > 0 {
			m.ArtifactIndex--
		}
	} else if m.ArtifactIndex < len(m.Artifacts)-1 {
		m.ArtifactIndex++
	}
	return m, nil
}

func (m *Model) scrollOverview(delta int) {
	indexes := m.overviewIndexes()
	if len(indexes) == 0 || delta == 0 {
		return
	}
	m.OverviewIndex = clampIndex(m.OverviewIndex+delta, len(indexes))
}

func (m *Model) scrollSettings(delta int) {
	if delta == 0 {
		return
	}
	m.SettingsIndex = clampIndex(m.SettingsIndex+delta, len(m.settingsItems()))
}

func (m *Model) scrollCompare(delta int) {
	if delta == 0 {
		return
	}
	if m.compareFiles != nil {
		m.CompareFileIndex = clampIndex(m.CompareFileIndex+delta, len(m.compareFiles.Files))
		return
	}
	m.CompareIndex = clampIndex(m.CompareIndex+delta, len(m.CompareRows))
}

func (m *Model) scrollRuns(delta int) {
	if len(m.Runs) == 0 || delta == 0 {
		return
	}
	m.RunIndex = clampIndex(m.RunIndex+delta, len(m.Runs))
}

// clampIndex keeps i within [0, n-1]; returns 0 for an empty list.
func clampIndex(i int, n int) int {
	if n <= 0 {
		return 0
	}
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}
