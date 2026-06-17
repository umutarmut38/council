package tui

import "fmt"

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
	// Single pass: count displayed agents up to agentIndex instead of
	// allocating displayAgentIndexes(), since this is on a UI hot path.
	pos := 0
	for i, view := range m.Agents {
		if !m.agentIsDisplayed(view.Session.Name) {
			continue
		}
		if i == agentIndex {
			return pos
		}
		pos++
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
