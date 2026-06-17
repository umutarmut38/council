package tui

// The command palette: typing "/" opens a vertical, arrow-navigable list of
// matching commands above the composer, with the commands that make sense for
// the current pipeline stage sorted first.

import (
	"fmt"
	"strings"

	"github.com/umutarmut38/council/internal/command"
)

// paletteMaxRows caps the palette so it never swallows the panes.
const paletteMaxRows = 8

// paletteActive reports whether the command palette should be open: the
// composer holds a "/" command word that is still being typed.
func (m Model) paletteActive() bool {
	return m.ScreenMode == ScreenPanes &&
		m.InputMode == InputComposer &&
		strings.HasPrefix(m.PromptInput, "/") &&
		!strings.Contains(m.PromptInput, " ")
}

// paletteMatches returns the commands matching the typed prefix, ordered:
// recommended stage commands first (in stage order), then recently-used
// commands, then the rest in declaration order.
func (m Model) paletteMatches() []command.Composer {
	prefix := strings.ToLower(strings.TrimPrefix(m.PromptInput, "/"))
	all := command.Composers()
	byName := map[string]command.Composer{}
	for _, c := range all {
		byName[c.Name] = c
	}

	matches := make([]command.Composer, 0, len(all))
	seen := map[string]bool{}
	add := func(name string) {
		c, ok := byName[name]
		if !ok || seen[name] || !strings.HasPrefix(c.Name, prefix) {
			return
		}
		matches = append(matches, c)
		seen[name] = true
	}
	for _, name := range m.stageCommandNames() {
		add(name)
	}
	for _, name := range m.recentCommands {
		add(name)
	}
	for _, c := range all {
		if seen[c.Name] || !strings.HasPrefix(c.Name, prefix) {
			continue
		}
		matches = append(matches, c)
	}
	return matches
}

// recordRecentCommand pushes a dispatched command name to the front of the
// recently-used list, de-duplicated and capped, so the palette can surface
// what the user reaches for most.
func (m *Model) recordRecentCommand(name string) {
	const maxRecent = 6
	next := make([]string, 0, maxRecent)
	next = append(next, name)
	for _, existing := range m.recentCommands {
		if existing == name {
			continue
		}
		next = append(next, existing)
		if len(next) == maxRecent {
			break
		}
	}
	m.recentCommands = next
}

// commandDisabledReason returns a short, human reason when a composer command
// cannot do anything useful in the current context, or "" when it is enabled.
// It mirrors the guards in the command handlers so the palette never promises
// an action the handler would just reject.
func (m Model) commandDisabledReason(name string) string {
	switch name {
	case "plan", "vote", "build", "review", "refine", "adopt", "preview", "compare", "judge", "report", "status", "artifacts":
		if m.orch == nil {
			return "needs a git repo"
		}
	case "start-build":
		if len(m.pendingBuild) == 0 {
			return "run /build first"
		}
	case "finish", "nudge", "resend":
		if m.phase == "" {
			return "no phase in progress"
		}
	}
	return ""
}

// paletteNextHint names the single most useful command right now, for the
// header line of the palette. It prefers the run's computed next step, falling
// back to the top stage command.
func (m Model) paletteNextHint() string {
	next := ""
	if m.progress != nil && strings.TrimSpace(m.progress.Next) != "" {
		next = strings.Fields(m.progress.Next)[0]
	} else if names := m.stageCommandNames(); len(names) > 0 {
		next = "/" + names[0]
	}
	if next == "" {
		return ""
	}
	word := strings.TrimPrefix(next, "/")
	if c, ok := command.LookupComposer(word); ok && c.Desc != "" {
		return "recommended next: " + next + " — " + c.Desc
	}
	return "recommended next: " + next
}

// stageCommandNames lists the commands most useful right now, best first.
// This drives both the palette ordering and the "what can I do here" feel of
// each pipeline stage.
func (m Model) stageCommandNames() []string {
	if len(m.attentionAgents()) > 0 {
		return []string{"attention", "nudge", "restart", "resend", "direct", "finish", "status"}
	}
	switch m.phase {
	case "plan", "refine":
		return []string{"finish", "resend", "restart", "nudge", "attention", "status", "artifacts"}
	case "vote", "review":
		return []string{"finish", "resend", "restart", "nudge", "attention", "judge", "status", "artifacts"}
	case "build":
		if len(m.pendingBuild) > 0 {
			return []string{"start-build", "restart", "status", "artifacts"}
		}
		return []string{"review", "attention", "restart", "direct", "status", "artifacts"}
	}
	p := m.progress
	if p == nil {
		return []string{"plan", "runs", "resume", "settings", "help"}
	}
	switch {
	case p.Adopted != "":
		return []string{"report", "status", "clean", "runs", "artifacts"}
	case p.BuildWinner != "":
		return []string{"compare", "preview", "adopt", "judge", "report", "artifacts"}
	case p.Diffs > 0:
		return []string{"review", "compare", "artifacts", "status"}
	case p.PlanWinner != "":
		return []string{"build", "refine", "judge", "artifacts", "status"}
	case p.Plans > 0:
		return []string{"vote", "judge", "artifacts", "status"}
	default:
		return []string{"plan", "resume", "runs", "status"}
	}
}

// renderPalette renders the vertical command list. A header names the
// recommended next action; recommended-for-this-stage rows are marked with ●,
// recently-used rows with ↺, each row shows its keybinding (when any), and
// commands that can't run yet are dimmed with the reason.
func (m Model) renderPalette() []string {
	matches := m.paletteMatches()
	if len(matches) == 0 {
		return []string{suggestStyle.Render(fitText("no matching command — Esc clears", m.Width))}
	}

	recommended := map[string]bool{}
	for _, name := range m.stageCommandNames() {
		recommended[name] = true
	}
	recent := map[string]bool{}
	for _, name := range m.recentCommands {
		recent[name] = true
	}

	selected := m.CmdSuggestIndex
	if selected >= len(matches) {
		selected = len(matches) - 1
	}
	// Keep the selection visible inside the row cap; on short terminals the
	// palette shrinks so the panes always keep at least a few lines.
	start := 0
	visible := paletteMaxRows
	if room := m.Height - chromeHeight - 10; room < visible {
		visible = room
	}
	if visible < 3 {
		visible = 3
	}
	if len(matches) > visible && selected >= visible-1 {
		start = selected - (visible - 2)
		if start+visible > len(matches) {
			start = len(matches) - visible
		}
	}

	lines := make([]string, 0, visible+3)
	if hint := m.paletteNextHint(); hint != "" {
		lines = append(lines, headingStyle.Render(fitText("▸ "+hint, m.Width)))
	}
	rows := 0
	for i := start; i < len(matches) && rows < visible; i++ {
		rows++
		c := matches[i]
		usage := "/" + c.Name
		if c.Args != "" {
			usage += " " + c.Args
		}
		mark := " "
		switch {
		case recommended[c.Name]:
			mark = "●"
		case recent[c.Name]:
			mark = "↺"
		}
		desc := c.Desc
		disabled := m.commandDisabledReason(c.Name)
		if disabled != "" {
			desc = "disabled — " + disabled
		}
		row := fmt.Sprintf(" %s %-22s %-9s %s", mark, usage, c.Key, desc)
		switch {
		case i == selected:
			lines = append(lines, focusStyle.Render(fitText(">"+row[1:], m.Width)))
		case disabled != "":
			lines = append(lines, faintStyle.Render(fitText(row, m.Width)))
		case recommended[c.Name]:
			lines = append(lines, suggestStyle.Render(fitText(row, m.Width)))
		default:
			lines = append(lines, faintStyle.Render(fitText(row, m.Width)))
		}
	}
	if rest := len(matches) - (start + visible); rest > 0 {
		lines = append(lines, faintStyle.Render(fitText(fmt.Sprintf("   … %d more — keep typing to filter", rest), m.Width)))
	}
	lines = append(lines, faintStyle.Render(fitText("↑/↓ select · Tab/Enter complete · ● = suggested for this stage · ↺ = recent", m.Width)))
	return lines
}

// movePaletteSelection moves the palette cursor; returns false when the
// palette is closed so the key can do its normal job.
func (m *Model) movePaletteSelection(delta int) bool {
	if !m.paletteActive() {
		return false
	}
	matches := m.paletteMatches()
	if len(matches) == 0 {
		return true
	}
	m.CmdSuggestIndex += delta
	if m.CmdSuggestIndex < 0 {
		m.CmdSuggestIndex = len(matches) - 1
	}
	if m.CmdSuggestIndex >= len(matches) {
		m.CmdSuggestIndex = 0
	}
	return true
}

// acceptPaletteSelection completes the input to the selected command.
// Returns true when it consumed the key.
func (m *Model) acceptPaletteSelection() bool {
	if !m.paletteActive() {
		return false
	}
	matches := m.paletteMatches()
	if len(matches) == 0 {
		return false
	}
	selected := m.CmdSuggestIndex
	if selected >= len(matches) {
		selected = len(matches) - 1
	}
	c := matches[selected]
	// Already typed exactly: let Enter submit it instead.
	if strings.EqualFold(m.PromptInput, "/"+c.Name) && selected == 0 {
		return false
	}
	m.PromptInput = "/" + c.Name + " "
	m.CmdSuggestIndex = 0
	m.Status = "/" + c.Name + " — " + c.Desc
	return true
}
