package tui

// Opt-in freestyle worktrees in the TUI: wiring the FreeWorktrees manager, the
// off-thread staleness probe + cached border marker, and the /refresh command.

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/umutarmut38/council/internal/orchestrate"
)

// worktreeProbeBudget bounds one whole status probe so a slow git can't make it
// run long; agents left unprobed keep their last marker. Staleness is probed
// only on demand (/refresh), never on a timer — a periodic git status on a
// worktree froze the TUI.
const worktreeProbeBudget = 3 * time.Second

// SetFreeWorktrees wires the opt-in freestyle-worktree manager onto the model.
// Nil (the default) leaves every pane in the launch dir.
func (m *Model) SetFreeWorktrees(f *orchestrate.FreeWorktrees) {
	m.freeWorktrees = f
}

// probeWorktreeStatusCmd probes each freestyle pane's worktree status OFF the
// Update loop and reports back via worktreeStatusMsg. It captures the manager
// (whose Status is read-only) and the agent names, so the goroutine never
// touches the model — avoiding the bubbletea closure race.
func (m *Model) probeWorktreeStatusCmd() tea.Cmd {
	if m.freeWorktrees == nil {
		return nil
	}
	f := m.freeWorktrees
	names := make([]string, 0, len(m.Agents))
	for _, v := range m.Agents {
		names = append(names, v.Session.Name)
	}
	return func() tea.Msg {
		status := make(map[string]worktreeState, len(names))
		deadline := time.Now().Add(worktreeProbeBudget)
		for _, name := range names {
			if time.Now().After(deadline) {
				break // budget spent; probe the remaining panes next cycle
			}
			if behind, dirty, ok := f.Status(name); ok {
				status[name] = worktreeState{behind: behind, dirty: dirty}
			}
		}
		return worktreeStatusMsg{status: status}
	}
}

// worktreeMarker is the compact pane-border marker for a freestyle pane's
// freshness: `⟳` when behind repo HEAD, `*` when dirty (both may show). Empty
// when the feature is off or the worktree is fresh/unknown.
func (m Model) worktreeMarker(name string) string {
	if m.freeWorktrees == nil {
		return ""
	}
	st, ok := m.worktreeStatus[name]
	if !ok {
		return ""
	}
	marker := ""
	if st.behind {
		marker += "⟳"
	}
	if st.dirty {
		marker += "*"
	}
	if marker == "" {
		return ""
	}
	return " " + marker
}

// cmdRefresh resets a freestyle pane's worktree to repo HEAD — the ONLY reset
// path for a persistent worktree — and re-seeds it. It refuses a dirty worktree
// unless `force`, so agent work is never silently discarded. "/refresh" targets
// the focused pane; "/refresh all" every freestyle pane; append "force" to
// discard uncommitted changes.
func (m *Model) cmdRefresh(rest string) tea.Cmd {
	if m.freeWorktrees == nil {
		m.Status = "freestyle worktrees are off (set worktrees.freestyle: true in a git repo)"
		return nil
	}
	force, all := false, false
	target := ""
	for _, fld := range strings.Fields(rest) {
		switch strings.ToLower(fld) {
		case "force":
			force = true
		case "all":
			all = true
		default:
			target = fld
		}
	}
	var agents []string
	switch {
	case all:
		for _, v := range m.Agents {
			agents = append(agents, v.Session.Name)
		}
	case target != "":
		agents = []string{target}
	default:
		agents = []string{m.focusedName()}
	}

	var refreshed []string
	var lastErr string
	for _, name := range agents {
		if err := m.freeWorktrees.Refresh(name, force); err != nil {
			lastErr = err.Error()
		} else {
			refreshed = append(refreshed, name)
		}
	}
	switch {
	case lastErr != "" && len(refreshed) == 0:
		m.Status = "refresh: " + lastErr
	case lastErr != "":
		m.Status = "refreshed " + strings.Join(refreshed, ", ") + " (some refused: " + lastErr + ")"
	default:
		m.Status = "refreshed " + strings.Join(refreshed, ", ") + " to HEAD"
	}
	// Re-probe so the border marker reflects the reset immediately.
	return m.probeWorktreeStatusCmd()
}
