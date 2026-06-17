package tui

import (
	"fmt"
	"strconv"
)

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
