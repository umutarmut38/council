package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/umutarmut38/council/internal/config"
)

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

// orderedUsedPersonalities lists the personalities that have at least one agent,
// sorted by configured Order then name — the order Ctrl+B cycles through them.
func (m Model) orderedUsedPersonalities() []string {
	used := m.allUsedPersonalities()
	names := make([]string, 0, len(used))
	for name := range used {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		li := m.Config.Personalities[names[i]]
		lj := m.Config.Personalities[names[j]]
		if li.Order != lj.Order {
			return li.Order < lj.Order
		}
		return names[i] < names[j]
	})
	return names
}

// orderedUsedCategories lists the categories that have at least one agent (via
// their personalities), sorted by configured Order then name.
func (m Model) orderedUsedCategories() []string {
	seen := map[string]bool{}
	for personality := range m.allUsedPersonalities() {
		if name, _, ok := m.Config.CategoryForPersonality(personality); ok {
			seen[name] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		li := m.Config.PersonalityCategories[names[i]]
		lj := m.Config.PersonalityCategories[names[j]]
		if li.Order != lj.Order {
			return li.Order < lj.Order
		}
		return names[i] < names[j]
	})
	return names
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
