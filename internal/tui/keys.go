package tui

// Key handling: composer keys, direct (pass-through) mode, and the overview,
// settings, and runs screens.

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// interruptArmStatus is the confirmation prompt shown while a Ctrl+C interrupt
// is armed for the named agent.
func interruptArmStatus(name string) string {
	return "press Ctrl+C again to interrupt " + name
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While the retro activation intro is playing, any key skips straight into
	// themed mode. It does NOT exit retro mode (exit is /eva again); the key is
	// consumed so it doesn't also act on the panes underneath. Ctrl+X still
	// quits, so the user is never trapped waiting out the intro.
	if m.retroActive && !m.retroIntroDone {
		if msg.String() == "ctrl+x" {
			m.terminateAgents()
			return m, tea.Quit
		}
		m.retroIntroDone = true
		// Skipping into themed mode reveals the header band; resize the panes to
		// the reduced body so they don't render stale until a manual resize.
		m.resizeAgents()
		return m, nil
	}

	// Any key other than Ctrl+C cancels a pending interrupt arm (see the ctrl+c
	// branch below). handleKey is a value receiver, so this persists across keys.
	if msg.String() != "ctrl+c" && m.interruptArmed != "" {
		// Drop the now-stale "press Ctrl+C again…" prompt if it's still showing;
		// leave any other status (a handler below may set its own) intact.
		if m.Status == interruptArmStatus(m.interruptArmed) {
			m.Status = "interrupt cancelled"
		}
		m.interruptArmed = ""
	}

	switch m.ScreenMode {
	case ScreenOverview:
		return m.handleOverviewKey(msg)
	case ScreenSettings:
		return m.handleSettingsKey(msg)
	case ScreenRuns:
		return m.handleRunsKey(msg)
	case ScreenArtifacts:
		return m.handleArtifactsKey(msg)
	case ScreenCompare:
		return m.handleCompareKey(msg)
	case ScreenEditor:
		return m.handleEditorKey(msg)
	}

	if m.InputMode == InputDirect {
		return m.handleDirectKey(msg)
	}

	switch msg.String() {
	case "tab":
		if m.acceptPaletteSelection() {
			return m, nil
		}
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
	case "ctrl+w":
		// Toggle mouse capture at runtime. Capturing the mouse disables native
		// terminal text selection, so this is the quick escape hatch to copy/paste.
		m.mouseOn = !m.mouseOn
		if m.mouseOn {
			m.Status = "mouse on"
			return m, tea.EnableMouseCellMotion
		}
		m.Status = "mouse off (text selection enabled)"
		return m, tea.DisableMouse
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
		session := m.focusedSession()
		if session == nil {
			return m, nil
		}
		// Confirm-before-interrupt: the first Ctrl+C arms; a second within the
		// window actually sends \x03. Guards against accidentally interrupting
		// the focused pane from the composer.
		if m.interruptArmed == session.Name && time.Since(m.interruptArmedAt) <= interruptArmWindow {
			m.interruptArmed = ""
			if err := session.WriteString("\x03"); err != nil {
				m.Status = "interrupt failed: " + err.Error()
			} else {
				m.Status = "interrupted " + session.Name
			}
			return m, nil
		}
		m.interruptArmed = session.Name
		m.interruptArmedAt = time.Now()
		m.Status = interruptArmStatus(session.Name)
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
		if m.acceptPaletteSelection() {
			return m, nil
		}
		return m, m.submitInput()
	case "up":
		if m.movePaletteSelection(-1) {
			return m, nil
		}
		if m.moveFileSuggestion(-1) {
			return m, nil
		}
		return m, nil
	case "down":
		if m.movePaletteSelection(1) {
			return m, nil
		}
		if m.moveFileSuggestion(1) {
			return m, nil
		}
		return m, nil
	case "backspace":
		m.PromptInput = dropLastRune(m.PromptInput)
		m.FileSuggestIndex = 0
		m.CmdSuggestIndex = 0
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
			// Guard against a mouse-report escape sequence leaking into the
			// composer as text. This happens on terminals/multiplexers whose wheel
			// reports bubbletea doesn't parse as a MouseMsg (or when the ESC is
			// split across PTY reads): the body ("[<64;30;10M") would otherwise be
			// typed into the input box.
			if isMouseReportFragment(string(msg.Runes)) {
				return m, nil
			}
			m.PromptInput += string(msg.Runes)
			m.FileSuggestIndex = 0
			m.CmdSuggestIndex = 0
			m.fileSuggestHidden = ""
		}
		return m, nil
	}
}

func (m Model) handleDirectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "f2", "ctrl+o":
		// Exit direct mode via F2/Ctrl+O only. Esc deliberately falls through to
		// the pane (see keyToPTY) so vim-like programs can leave insert mode.
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

	m.sendKeyToSession(session, msg)
	// Direct keystrokes mean the user is handling whatever the pane asked for.
	if view := m.findAgentForMessage(session.Name, session); view != nil {
		view.clearAttention()
	}
	return m, nil
}

func (m *Model) handleOverviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	indexes := m.overviewIndexes()
	switch msg.String() {
	case "esc":
		m.ScreenMode = ScreenPanes
		m.resizeAgents()
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

func (m *Model) handleRunsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.ScreenMode = ScreenPanes
		m.resizeAgents()
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

func keyToPTY(msg tea.KeyMsg, enterSequence string) string {
	if len(msg.Runes) > 0 {
		return string(msg.Runes)
	}

	value := msg.String()
	switch value {
	case "enter":
		return submitSequence(enterSequence)
	case "esc":
		// Pass Escape through to the program (e.g. vim/nvim leaving insert mode).
		// Direct mode no longer intercepts Esc — only F2/Ctrl+O exit — so Esc
		// reaches the pane there too; the integrated editor pane also relies on
		// this passthrough.
		return "\x1b"
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
