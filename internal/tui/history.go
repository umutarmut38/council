package tui

// Composer input history: shell-style recall of submitted lines via arrow
// up/down. State lives on Model (inputHistory/historyPos/historyDraft); these
// methods are pure model mutators with no I/O.

// maxInputHistory caps the in-memory history; older entries are dropped.
const maxInputHistory = 200

// recordInputHistory appends a submitted line, skipping a consecutive
// duplicate (like a shell), then resets navigation to the live draft.
func (m *Model) recordInputHistory(text string) {
	if n := len(m.inputHistory); n == 0 || m.inputHistory[n-1] != text {
		m.inputHistory = append(m.inputHistory, text)
		if len(m.inputHistory) > maxInputHistory {
			m.inputHistory = m.inputHistory[len(m.inputHistory)-maxInputHistory:]
		}
	}
	m.resetHistoryNav()
}

// historyPrev recalls an older entry into the composer (arrow up).
func (m *Model) historyPrev() {
	if len(m.inputHistory) == 0 || m.historyPos == 0 {
		return
	}
	if m.historyPos == len(m.inputHistory) {
		m.historyDraft = m.PromptInput
	}
	m.historyPos--
	m.PromptInput = m.inputHistory[m.historyPos]
}

// historyNext moves back toward newer entries and the live draft (arrow down).
func (m *Model) historyNext() {
	if m.historyPos >= len(m.inputHistory) {
		return
	}
	m.historyPos++
	if m.historyPos == len(m.inputHistory) {
		m.PromptInput = m.historyDraft
		return
	}
	m.PromptInput = m.inputHistory[m.historyPos]
}

// resetHistoryNav points navigation back at the live draft, so the next arrow
// up starts from the newest entry. Called after submit and after edits.
func (m *Model) resetHistoryNav() {
	m.historyPos = len(m.inputHistory)
	m.historyDraft = ""
}
