package tui

// Key handling: composer keys, direct (pass-through) mode, and the overview,
// settings, and runs screens.

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Any key other than Ctrl+C cancels a pending interrupt arm (see the ctrl+c
	// branch below). handleKey is a value receiver, so this persists across keys.
	if msg.String() != "ctrl+c" {
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
			_ = session.WriteString("\x03")
			m.interruptArmed = ""
			m.Status = "interrupted " + session.Name
			return m, nil
		}
		m.interruptArmed = session.Name
		m.interruptArmedAt = time.Now()
		m.Status = "press Ctrl+C again to interrupt " + session.Name
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
