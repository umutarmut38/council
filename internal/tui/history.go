package tui

// Composer input history: shell-style recall of submitted lines via arrow
// up/down. State lives on Model (inputHistory/historyPos/historyDraft/
// historyLast); these methods are pure model mutators with no I/O.

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
	if len(m.inputHistory) == 0 {
		return
	}
	m.syncHistoryDraft()
	if m.historyPos == 0 {
		return
	}
	if m.historyPos == len(m.inputHistory) {
		m.historyDraft = m.PromptInput
	}
	m.historyPos--
	m.setFromHistory(m.inputHistory[m.historyPos])
}

// historyNext moves back toward newer entries and the live draft (arrow down).
func (m *Model) historyNext() {
	m.syncHistoryDraft()
	if m.historyPos >= len(m.inputHistory) {
		return
	}
	m.historyPos++
	if m.historyPos == len(m.inputHistory) {
		m.setFromHistory(m.historyDraft)
		return
	}
	m.setFromHistory(m.inputHistory[m.historyPos])
}

// syncHistoryDraft restarts navigation from the live draft when the composer
// was edited by any path other than history navigation itself (typing,
// backspace, ctrl+u/ctrl+c clear, esc, palette/file-ref fill, …). Comparing
// against historyLast is one guard that covers every such site, instead of
// sprinkling resetHistoryNav() across each of them.
func (m *Model) syncHistoryDraft() {
	if m.PromptInput != m.historyLast {
		m.resetHistoryNav()
	}
}

// setFromHistory writes a recalled line and records it so the next edit is
// detected as an external change.
func (m *Model) setFromHistory(text string) {
	m.PromptInput = text
	m.historyLast = text
}

// resetHistoryNav points navigation back at the live draft, so the next arrow
// up starts from the newest entry.
func (m *Model) resetHistoryNav() {
	m.historyPos = len(m.inputHistory)
	m.historyDraft = ""
	m.historyLast = ""
}
