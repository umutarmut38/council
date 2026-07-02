package tui

// Artifact browser (/artifacts): inspect a run's plans, votes, diffs, check
// logs, reviews, report, and transcripts without leaving the TUI. Also the
// generic text viewer used by /preview, /compare, and /clean previews.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// cmdArtifacts collects the current run's artifacts and opens the browser.
func (m *Model) cmdArtifacts() {
	if m.orch == nil {
		m.Status = "orchestration unavailable"
		return
	}
	if m.orch.Run() == nil {
		if err := m.orch.UseRun(""); err != nil {
			m.Status = "artifacts: " + err.Error()
			return
		}
	}
	run := m.orch.Run()
	entries := []artifactEntry{}
	add := func(label, path string) {
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			entries = append(entries, artifactEntry{Label: label, Path: path})
		}
	}

	add("report", filepath.Join(run.Dir, "report.md"))
	add("issue", run.IssuePath())
	addDir := func(prefix, dir string, suffixes ...string) {
		items, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		names := make([]string, 0, len(items))
		for _, e := range items {
			if e.IsDir() {
				continue
			}
			for _, suffix := range suffixes {
				if strings.HasSuffix(e.Name(), suffix) {
					names = append(names, e.Name())
					break
				}
			}
		}
		sort.Strings(names)
		for _, name := range names {
			add(prefix+name, filepath.Join(dir, name))
		}
	}
	addDir("plan: ", run.PlansDir(), ".md")
	addDir("vote: ", run.VotesDir(), ".md", ".json")
	addDir("build: ", run.BuildsDir(), ".diff", ".log", ".md", ".json", ".txt")
	add("timings", run.TimingsPath())
	add("adopted", run.AdoptionPath())

	// Transcripts last; they are long and usually only needed for debugging.
	_ = filepath.WalkDir(filepath.Join(run.Dir, "transcripts"), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".txt") {
			rel, _ := filepath.Rel(run.Dir, path)
			add("transcript: "+filepath.ToSlash(rel), path)
		}
		return nil
	})

	if len(entries) == 0 {
		m.Status = "no artifacts yet for run " + run.Stamp
		return
	}
	m.Artifacts = entries
	m.ArtifactIndex = 0
	m.artifactView = ""
	m.artifactPath = ""
	m.ScreenMode = ScreenArtifacts
	m.InputMode = InputComposer
	m.PromptInput = ""
	// The split's right column edits files in the editor pane; root it at the
	// repo so launchEditor's CWD and the :e relative paths resolve. Start with
	// the list focused (the pane opens on Enter).
	m.editorRoot = detectEditorRoot()
	m.editorPaneFocused = false
	m.editorReturnScreen = ScreenPanes
	m.Status = fmt.Sprintf("%d artifact(s) — run %s", len(entries), run.Stamp)
}

// openArtifactText shows arbitrary text (a preview, comparison, etc.) in the
// artifact viewer without it being a file on disk. Esc from these synthetic
// views returns to the panes, not the artifacts list.
func (m *Model) openArtifactText(title, content string) {
	m.artifactView = content
	m.artifactPath = title
	m.artifactFile = ""
	m.artifactIsDiff = false
	m.artifactIsCost = false
	m.viewerFromList = false
	m.viewerReturnScreen = ScreenPanes
	m.artifactTop = 0
	m.artifactWrap = nil
	m.ScreenMode = ScreenArtifacts
	m.InputMode = InputComposer
	m.PromptInput = ""
}

// openDiffText opens unified-diff content in the pager with git-style line
// coloring; Esc returns to returnTo instead of the panes.
func (m *Model) openDiffText(title, content string, returnTo ScreenMode) {
	m.openArtifactText(title, content)
	m.artifactIsDiff = true
	m.viewerReturnScreen = returnTo
}

func (m *Model) openArtifactFile(entry artifactEntry) {
	data, err := os.ReadFile(entry.Path)
	if err != nil {
		m.Status = "artifacts: " + err.Error()
		return
	}
	m.openArtifactText(entry.Path, cleanTranscriptOutput(string(data)))
	m.artifactFile = entry.Path
	m.viewerFromList = true
	m.Status = entry.Label
}

func (m *Model) closeArtifactView() {
	fromList := m.viewerFromList
	m.artifactView = ""
	m.artifactPath = ""
	m.artifactFile = ""
	m.viewerFromList = false
	m.artifactWrap = nil
	m.artifactTop = 0
	// Synthetic views return to their origin screen (panes by default, the
	// compare screen for its diffs); only files opened from the /artifacts
	// list go back to that list.
	if !fromList || len(m.Artifacts) == 0 {
		m.ScreenMode = m.viewerReturnScreen
		m.viewerReturnScreen = ScreenPanes
		if m.ScreenMode == ScreenPanes {
			m.resizeAgents()
		}
	}
}

// editorArgv resolves the editor command as argv (space-split), with
// precedence ui.editor → $VISUAL → $EDITOR → vim. It always returns at least
// one element. Shared by the external editor (openInEditor) and the integrated
// editor pane (/edit).
func (m *Model) editorArgv() []string {
	editor := strings.TrimSpace(m.Config.UI.Editor)
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vim"
	}
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		parts = []string{"vim"}
	}
	return parts
}

// openInEditor suspends the TUI and opens path in the configured editor
// (ui.editor, else $VISUAL/$EDITOR, else vim), so diffs and artifacts can be
// inspected with full tooling.
func (m *Model) openInEditor(path string) tea.Cmd {
	parts := append(m.editorArgv(), path)
	cmd := exec.Command(parts[0], parts[1:]...)
	m.Status = "opened " + path + " in " + parts[0]
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return editorDoneMsg{err: err} })
}

func (m *Model) handleArtifactsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Synthetic views (preview, compare diffs, adopt preview, setup) stay a
	// full-width read-only pager with their own key handling.
	if m.artifactView != "" {
		return m.handleArtifactViewerKey(msg)
	}

	// Artifact list split: list (left) + editable editor pane (right). When the
	// pane is focused, keys go straight to the editor (Esc passes through).
	if model, cmd, handled := m.routeEditorPaneKey(msg, "artifacts — ↑/↓ select · Enter edit · Tab editor"); handled {
		return model, cmd
	}
	switch msg.String() {
	case "esc", "q":
		m.ScreenMode = ScreenPanes
		m.resizeAgents()
	case "up", "k":
		if m.ArtifactIndex > 0 {
			m.ArtifactIndex--
		}
	case "down", "j":
		if m.ArtifactIndex < len(m.Artifacts)-1 {
			m.ArtifactIndex++
		}
	case "g", "home":
		m.ArtifactIndex = 0
	case "G", "end":
		m.ArtifactIndex = max0(len(m.Artifacts) - 1)
	case "enter":
		// Open the selected artifact in the editor pane (editable) and focus it.
		if len(m.Artifacts) > 0 {
			m.openInEditorPane(m.Artifacts[m.ArtifactIndex].Path)
		}
	case "tab":
		if m.editorView != nil && !m.editorView.Session.Done {
			m.editorPaneFocused = true
			m.Status = "editor — Esc passes through · F2/Ctrl+O back to list"
		}
	case "e", "o":
		// Alternative: open the selected artifact in the external $EDITOR.
		if len(m.Artifacts) > 0 {
			return m, m.openInEditor(m.Artifacts[m.ArtifactIndex].Path)
		}
	case "ctrl+c", "ctrl+x":
		m.terminateAgents()
		return m, tea.Quit
	}
	return m, nil
}

// handleArtifactViewerKey drives the read-only pager used for synthetic views
// (preview, compare diffs, adopt preview, setup status) — content that has no
// editable file behind it.
func (m *Model) handleArtifactViewerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	pageStep := max0(m.Height-m.chromeLines()) - 1
	if pageStep < 1 {
		pageStep = 10
	}
	switch msg.String() {
	case "esc", "q":
		m.closeArtifactView()
	case "up", "k":
		m.artifactTop = max0(m.artifactTop - 1)
	case "down", "j":
		m.artifactTop++
	case "pgup":
		m.artifactTop = max0(m.artifactTop - pageStep)
	case "pgdown", " ":
		m.artifactTop += pageStep
	case "g", "home":
		m.artifactTop = 0
	case "e", "o":
		if m.artifactFile != "" {
			return m, m.openInEditor(m.artifactFile)
		}
		m.Status = "no file behind this view — /preview <agent> has the diff, or open from /artifacts"
	case "i":
		// A diff view backed by a real worktree file (from /compare) can still
		// open in the integrated editor, returning to its origin screen.
		if m.artifactFile != "" {
			m.openInIntegratedEditor(m.artifactFile, m.viewerReturnScreen)
		}
	case "y":
		if m.viewingAdoptPreview() {
			// applyAdopt owns the post-apply screen: a receipt on success, the
			// panes on failure. Don't force a transition here or it would clobber
			// the receipt the user needs to see.
			return m, m.applyAdopt(m.pendingAdopt.Agent)
		}
	case "n":
		if m.viewingAdoptPreview() {
			m.pendingAdopt = nil
			m.closeArtifactView()
			m.ScreenMode = ScreenPanes
			m.Status = "adopt cancelled"
		}
	case "ctrl+c", "ctrl+x":
		m.terminateAgents()
		return m, tea.Quit
	}
	return m, nil
}

// transcriptPrivacyNote explains, in the artifacts browser, how the listed
// transcripts relate to the raw PTY logs: transcripts honor sessions.redact and
// are what the browser shows, while raw PTY logs are kept separately (under
// raw/) and are never redacted. It makes the privacy boundary explicit so a
// user doesn't assume the listed artifacts are scrubbed.
func transcriptPrivacyNote(redact bool) string {
	if redact {
		return "transcripts here are redacted (sessions.redact on); raw PTY logs are stored separately and are NOT redacted"
	}
	return "transcripts here are NOT redacted (set sessions.redact); raw PTY logs are stored separately and are never redacted"
}

// colorDiffLine renders one unified-diff line with git-style colors, drawn from
// the active chrome (c) so the diff honors the configured theme. It is a
// package function (not a Model method), so the caller threads c in rather than
// reading the package styles directly.
func colorDiffLine(line string, width int, c chromeStyles) string {
	text := fitText(line, width)
	switch {
	case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		return c.faint.Render(text)
	case strings.HasPrefix(line, "+"):
		return c.status.Render(text)
	case strings.HasPrefix(line, "-"):
		return c.warn.Render(text)
	case strings.HasPrefix(line, "@@"):
		return c.rail.Render(text)
	case strings.HasPrefix(line, "diff --git"), strings.HasPrefix(line, "index "),
		strings.HasPrefix(line, "new file"), strings.HasPrefix(line, "deleted file"),
		strings.HasPrefix(line, "rename "):
		return c.heading.Render(text)
	default:
		return text
	}
}

// colorCostLine styles a line of the /cost view: the markdown heading, the
// column header, the Total row, and the Share:/Hints: labels stand out; share
// bars use the status color; hint and price-source lines dim. Whole-line
// styling only, so the pager's width math is untouched.
func colorCostLine(line string, width int, c chromeStyles) string {
	text := fitText(line, width)
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, "#"),
		strings.HasPrefix(line, "Agent"),
		strings.HasPrefix(line, "Total"),
		strings.HasPrefix(trimmed, "Share:"),
		strings.HasPrefix(trimmed, "Hints:"):
		return c.heading.Render(text)
	case strings.ContainsRune(line, '█'), strings.ContainsRune(line, '░'):
		return c.status.Render(text)
	case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "prices:"):
		return c.faint.Render(text)
	default:
		return text
	}
}

// viewingAdoptPreview reports whether the artifact viewer is showing a staged
// adopt preview, where y/n apply or cancel it.
func (m *Model) viewingAdoptPreview() bool {
	return m.pendingAdopt != nil && strings.HasPrefix(m.artifactPath, "adopt preview: ")
}

func (m Model) renderArtifacts(bodyHeight int) []string {
	c := m.chrome()
	lines := make([]string, 0, bodyHeight)
	if m.artifactView != "" {
		// Viewer mode: wrapped content with a scroll window.
		wrapped := m.artifactWrap
		if wrapped == nil {
			for _, line := range strings.Split(m.artifactView, "\n") {
				wrapped = append(wrapped, hardWrap(line, max0(m.Width-2))...)
			}
		}
		top := m.artifactTop
		if top > len(wrapped)-1 {
			top = max0(len(wrapped) - 1)
		}
		hint := "↑/↓ scroll · Esc back"
		if m.artifactFile != "" {
			hint = "e $EDITOR · i editor · " + hint
		}
		if m.viewingAdoptPreview() {
			hint = "y apply · n cancel · e edit diff · ↑/↓ scroll · Esc back"
		}
		header := fmt.Sprintf("%s (%d lines) — %s", m.artifactPath, len(wrapped), hint)
		lines = append(lines, c.heading.Render(fitText(header, m.Width)))
		for i := top; i < len(wrapped) && len(lines) < bodyHeight; i++ {
			line := fitText(wrapped[i], m.Width)
			switch {
			case m.artifactIsDiff:
				line = colorDiffLine(wrapped[i], m.Width, c)
			case m.artifactIsCost:
				line = colorCostLine(wrapped[i], m.Width, c)
			}
			lines = append(lines, line)
		}
		return fitBlock(lines, m.Width, bodyHeight)
	}

	// Artifact list split: list (left) + editable editor pane (right).
	listW := m.editorTreeWidth()
	paneW := max0(m.Width - listW - 1)
	left := m.renderArtifactList(bodyHeight, listW)
	right := m.renderEditorPane(bodyHeight, paneW, "Enter on an artifact to edit it")
	return m.joinColumns(left, right, listW, bodyHeight)
}

// renderArtifactList renders the left column of the artifacts split: a heading
// plus a scrolling, selectable list of the run's artifacts. The selection reads
// pink when the list has focus, dimmer when the editor pane does.
func (m Model) renderArtifactList(height, width int) []string {
	c := m.chrome()
	lines := make([]string, 0, height)
	title := "ARTIFACTS"
	if m.orch != nil && m.orch.Run() != nil {
		title = "ARTIFACTS  " + m.orch.Run().Stamp
	}
	lines = append(lines, c.heading.Render(fitText(title, width)))

	visible := max0(height - 1)
	start := 0
	if visible > 0 && m.ArtifactIndex >= visible {
		start = m.ArtifactIndex - visible + 1
	}
	for i := start; i < len(m.Artifacts) && len(lines) < height; i++ {
		row := m.Artifacts[i].Label
		if i == m.ArtifactIndex {
			style := c.focus
			if m.editorPaneFocused {
				style = c.suggest
			}
			lines = append(lines, style.Render(fitText("> "+row, width)))
			continue
		}
		lines = append(lines, fitText("  "+row, width))
	}
	for len(lines) < height {
		lines = append(lines, fitText("", width))
	}
	return lines
}
