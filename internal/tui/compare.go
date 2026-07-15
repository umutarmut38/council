package tui

// The interactive /compare screen: pick a build, drill into its changed
// files, read each file's diff git-style, jump into the live worktree with
// $EDITOR, and mark one build with `x` to diff two implementations against
// each other.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/umutarmut38/council/internal/orchestrate"
)

// compareFileSet is the drilled-into state: one selection's file list.
type compareFileSet struct {
	Title  string // "agent vs base" or "agentA ↔ agentB"
	AgentA string
	AgentB string // "" when comparing against the run base
	Files  []orchestrate.DiffFile
}

// cmdCompare loads the candidate builds and opens the compare screen.
func (m *Model) cmdCompare() {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return
	}
	if m.orch.Run() == nil {
		if err := m.orch.UseRun(""); err != nil {
			m.Status = "compare: " + err.Error()
			return
		}
	}
	rows, err := m.orch.CompareBuilds()
	if err != nil {
		m.Status = "compare: " + err.Error()
		return
	}
	m.CompareRows = rows
	m.CompareIndex = 0
	m.compareMarked = ""
	m.compareFiles = nil
	m.ScreenMode = ScreenCompare
	m.InputMode = InputComposer
	m.PromptInput = ""
	m.Status = fmt.Sprintf("%d candidate build(s) — Enter inspect · x mark for pairwise diff · e worktree", len(rows))
}

// openCompareFiles loads the file list for one selection (vs base, or
// between two builds when agentB != "").
func (m *Model) openCompareFiles(agentA, agentB string) {
	var diff string
	var err error
	var title string
	if agentB == "" {
		diff, err = m.orch.DiffVsBase(agentA)
		title = agentA + " vs base"
	} else {
		diff, err = m.orch.DiffBuilds(agentA, agentB)
		title = agentA + " ↔ " + agentB
	}
	if err != nil {
		m.Status = "compare: " + err.Error()
		return
	}
	files := orchestrate.SplitUnifiedDiff(diff)
	if len(files) == 0 {
		m.Status = title + ": no differences"
		return
	}
	m.compareFiles = &compareFileSet{Title: title, AgentA: agentA, AgentB: agentB, Files: files}
	m.CompareFileIndex = 0
	m.Status = fmt.Sprintf("%s — %d file(s) · Enter diff · e open file · Esc back", title, len(files))
}

// compareWorktreeFile resolves the on-disk path for a changed file, trying
// the post-image side first (B for pairwise, the agent for vs-base).
func (m *Model) compareWorktreeFile(set *compareFileSet, file orchestrate.DiffFile) (string, bool) {
	agents := []string{set.AgentA}
	if set.AgentB != "" {
		agents = []string{set.AgentB, set.AgentA}
	}
	if m.orch == nil {
		return "", false
	}
	for _, agent := range agents {
		if wt, ok := m.orch.WorktreePath(agent); ok {
			full := filepath.Join(wt, file.Path)
			if _, err := os.Stat(full); err == nil {
				return full, true
			}
		}
	}
	return "", false
}

func (m *Model) handleCompareKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Level 2 lives in the artifact pager; this handler covers the row and
	// file levels.
	if m.compareFiles != nil {
		return m.handleCompareFilesKey(msg)
	}
	switch msg.String() {
	case "esc", "q":
		m.ScreenMode = ScreenPanes
		m.resizeAgents()
		m.Status = "panes"
	case "up", "k":
		if m.CompareIndex > 0 {
			m.CompareIndex--
		}
	case "down", "j":
		if m.CompareIndex < len(m.CompareRows)-1 {
			m.CompareIndex++
		}
	case "x":
		if row := m.selectedCompareRow(); row != nil {
			switch {
			case m.compareMarked == row.Agent:
				m.compareMarked = ""
				m.Status = "unmarked " + row.Agent
			case m.compareMarked == "":
				m.compareMarked = row.Agent
				m.Status = row.Agent + " marked — select another build and press Enter/x to diff them"
			default:
				m.openCompareFiles(m.compareMarked, row.Agent)
			}
		}
	case "enter":
		if row := m.selectedCompareRow(); row != nil {
			if m.compareMarked != "" && m.compareMarked != row.Agent {
				m.openCompareFiles(m.compareMarked, row.Agent)
			} else {
				m.openCompareFiles(row.Agent, "")
			}
		}
	case "d":
		// The whole diff vs base in one pager.
		if row := m.selectedCompareRow(); row != nil {
			diff, err := m.orch.DiffVsBase(row.Agent)
			if err != nil {
				m.Status = "compare: " + err.Error()
				return m, nil
			}
			m.openDiffText(row.Agent+" vs base", diff, ScreenCompare)
		}
	case "e", "o":
		// Browse the live worktree in $EDITOR.
		if row := m.selectedCompareRow(); row != nil && m.orch != nil {
			if wt, ok := m.orch.WorktreePath(row.Agent); ok {
				return m, m.openInEditor(wt)
			}
			m.Status = row.Agent + "'s worktree is gone (cleaned?) — only the captured diff is available (d)"
		}
	case "i":
		// Browse the live worktree in the integrated editor (tree + pane).
		if row := m.selectedCompareRow(); row != nil && m.orch != nil {
			if wt, ok := m.orch.WorktreePath(row.Agent); ok {
				m.openInIntegratedEditor(wt, ScreenCompare)
				return m, nil
			}
			m.Status = row.Agent + "'s worktree is gone (cleaned?) — only the captured diff is available (d)"
		}
	case "ctrl+c", "ctrl+x":
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) handleCompareFilesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	set := m.compareFiles
	switch msg.String() {
	case "esc", "q":
		m.compareFiles = nil
		m.Status = "compare"
	case "up", "k":
		if m.CompareFileIndex > 0 {
			m.CompareFileIndex--
		}
	case "down", "j":
		if m.CompareFileIndex < len(set.Files)-1 {
			m.CompareFileIndex++
		}
	case "enter", "d":
		if m.CompareFileIndex < len(set.Files) {
			file := set.Files[m.CompareFileIndex]
			m.openDiffText(set.Title+" — "+file.Path, file.Patch, ScreenCompare)
			if path, ok := m.compareWorktreeFile(set, file); ok {
				m.artifactFile = path
			}
		}
	case "e", "o":
		if m.CompareFileIndex < len(set.Files) {
			file := set.Files[m.CompareFileIndex]
			if path, ok := m.compareWorktreeFile(set, file); ok {
				return m, m.openInEditor(path)
			}
			m.Status = "no live worktree file for " + file.Path
		}
	case "i":
		if m.CompareFileIndex < len(set.Files) {
			file := set.Files[m.CompareFileIndex]
			if path, ok := m.compareWorktreeFile(set, file); ok {
				m.openInIntegratedEditor(path, ScreenCompare)
				return m, nil
			}
			m.Status = "no live worktree file for " + file.Path
		}
	case "ctrl+c", "ctrl+x":
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) selectedCompareRow() *orchestrate.BuildComparison {
	if m.CompareIndex < 0 || m.CompareIndex >= len(m.CompareRows) {
		return nil
	}
	return &m.CompareRows[m.CompareIndex]
}

func (m Model) renderCompare(bodyHeight int) []string {
	if m.compareFiles != nil {
		return m.renderCompareFiles(bodyHeight)
	}
	c := m.chrome()
	lines := make([]string, 0, bodyHeight)
	lines = append(lines, c.heading.Render(fitText("Compare builds", m.Width)))
	lines = append(lines, c.faint.Render(fitText(fmt.Sprintf("  %-2s %-18s %-5s %-6s %-6s %-7s %s", "", "AGENT", "PLAN", "FILES", "CHECK", "POINTS", "WORKTREE"), m.Width)))
	for i, row := range m.CompareRows {
		marker := "  "
		if row.Winner {
			marker = "★ "
		}
		letter := row.Letter
		if letter == "" {
			letter = "—"
		}
		worktree := "cleaned"
		if m.orch != nil {
			if _, ok := m.orch.WorktreePath(row.Agent); ok {
				worktree = "live"
			}
		}
		name := row.Agent
		if m.compareMarked == row.Agent {
			name = "[x] " + name
		}
		text := fmt.Sprintf("  %s%-18s %-5s %-6d %-6s %-7d %s", marker, name, letter, row.Files, row.CheckStatus, row.Points, worktree)
		switch {
		case i == m.CompareIndex:
			lines = append(lines, c.focus.Render(fitText(">"+text[1:], m.Width)))
		case m.compareMarked == row.Agent:
			lines = append(lines, c.suggest.Render(fitText(text, m.Width)))
		default:
			lines = append(lines, fitText(text, m.Width))
		}
	}
	lines = append(lines, "")
	lines = append(lines, c.faint.Render(fitText("Enter: changed files · d: full diff vs base · x: mark, then Enter on another build to diff the two · e: open worktree in $EDITOR · ★ review winner", m.Width)))
	return fitBlock(lines, m.Width, bodyHeight)
}

func (m Model) renderCompareFiles(bodyHeight int) []string {
	c := m.chrome()
	set := m.compareFiles
	lines := make([]string, 0, bodyHeight)
	lines = append(lines, c.heading.Render(fitText(set.Title, m.Width)))
	start := 0
	visible := bodyHeight - 3
	if visible > 0 && m.CompareFileIndex >= visible {
		start = m.CompareFileIndex - visible + 1
	}
	for i := start; i < len(set.Files) && len(lines) < bodyHeight-2; i++ {
		file := set.Files[i]
		stat := fmt.Sprintf("  %-2s %-50s +%d -%d", file.Status, file.Path, file.Added, file.Deleted)
		if i == m.CompareFileIndex {
			lines = append(lines, c.focus.Render(fitText("> "+strings.TrimPrefix(stat, "  "), m.Width)))
		} else {
			style := c.faint
			switch file.Status {
			case "A":
				style = c.status
			case "D":
				style = c.warn
			default:
				style = c.suggest
			}
			lines = append(lines, style.Render(fitText(stat, m.Width)))
		}
	}
	lines = append(lines, "")
	lines = append(lines, c.faint.Render(fitText("Enter: file diff · e: open the file in $EDITOR · Esc: back to builds", m.Width)))
	return fitBlock(lines, m.Width, bodyHeight)
}
