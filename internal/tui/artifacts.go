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

// openInEditor suspends the TUI and opens path in $VISUAL/$EDITOR (vim by
// default), so diffs and artifacts can be inspected with full tooling.
func (m *Model) openInEditor(path string) tea.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vim"
	}
	parts := strings.Fields(editor)
	parts = append(parts, path)
	cmd := exec.Command(parts[0], parts[1:]...)
	m.Status = "opened " + path + " in " + parts[0]
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return editorDoneMsg{err: err} })
}

func (m *Model) handleArtifactsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	viewing := m.artifactView != ""
	pageStep := max0(m.Height-m.chromeLines()) - 1
	if pageStep < 1 {
		pageStep = 10
	}
	switch msg.String() {
	case "esc", "q":
		if viewing {
			m.closeArtifactView()
		} else {
			m.ScreenMode = ScreenPanes
			m.resizeAgents()
		}
		return m, nil
	case "up", "k":
		if viewing {
			m.artifactTop = max0(m.artifactTop - 1)
		} else if m.ArtifactIndex > 0 {
			m.ArtifactIndex--
		}
		return m, nil
	case "down", "j":
		if viewing {
			m.artifactTop++
		} else if m.ArtifactIndex < len(m.Artifacts)-1 {
			m.ArtifactIndex++
		}
		return m, nil
	case "pgup":
		if viewing {
			m.artifactTop = max0(m.artifactTop - pageStep)
		}
		return m, nil
	case "pgdown", " ":
		if viewing {
			m.artifactTop += pageStep
		}
		return m, nil
	case "g", "home":
		m.artifactTop = 0
		return m, nil
	case "enter":
		if !viewing && len(m.Artifacts) > 0 {
			m.openArtifactFile(m.Artifacts[m.ArtifactIndex])
		}
		return m, nil
	case "e", "o":
		// Open the file behind the view (or the selected list entry) in
		// $EDITOR — vim/neovim for real inspection instead of the pager.
		if viewing && m.artifactFile != "" {
			return m, m.openInEditor(m.artifactFile)
		}
		if !viewing && len(m.Artifacts) > 0 {
			return m, m.openInEditor(m.Artifacts[m.ArtifactIndex].Path)
		}
		if viewing {
			m.Status = "no file behind this view — /preview <agent> has the diff, or open from /artifacts"
		}
		return m, nil
	case "y":
		// In a staged adopt preview, `y` applies the diff right here.
		if viewing && m.viewingAdoptPreview() {
			cmd := m.applyAdopt(m.pendingAdopt.Agent)
			m.closeArtifactView()
			m.ScreenMode = ScreenPanes
			return m, cmd
		}
		return m, nil
	case "n":
		if viewing && m.viewingAdoptPreview() {
			m.pendingAdopt = nil
			m.closeArtifactView()
			m.ScreenMode = ScreenPanes
			m.Status = "adopt cancelled"
		}
		return m, nil
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

// colorDiffLine renders one unified-diff line with git-style colors.
func colorDiffLine(line string, width int) string {
	text := fitText(line, width)
	switch {
	case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		return faintStyle.Render(text)
	case strings.HasPrefix(line, "+"):
		return statusStyle.Render(text)
	case strings.HasPrefix(line, "-"):
		return warnStyle.Render(text)
	case strings.HasPrefix(line, "@@"):
		return railStyle.Render(text)
	case strings.HasPrefix(line, "diff --git"), strings.HasPrefix(line, "index "),
		strings.HasPrefix(line, "new file"), strings.HasPrefix(line, "deleted file"),
		strings.HasPrefix(line, "rename "):
		return headingStyle.Render(text)
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
			hint = "e open in $EDITOR · " + hint
		}
		if m.viewingAdoptPreview() {
			hint = "y apply · n cancel · e edit diff · ↑/↓ scroll · Esc back"
		}
		header := fmt.Sprintf("%s (%d lines) — %s", m.artifactPath, len(wrapped), hint)
		lines = append(lines, c.heading.Render(fitText(header, m.Width)))
		for i := top; i < len(wrapped) && len(lines) < bodyHeight; i++ {
			line := fitText(wrapped[i], m.Width)
			if m.artifactIsDiff {
				line = colorDiffLine(wrapped[i], m.Width)
			}
			lines = append(lines, line)
		}
		return fitBlock(lines, m.Width, bodyHeight)
	}

	lines = append(lines, c.heading.Render(fitText("Run artifacts", m.Width)))
	lines = append(lines, c.faint.Render(fitText(transcriptPrivacyNote(m.Config.Sessions.Redact), m.Width)))
	start := 0
	visible := bodyHeight - 1
	if visible > 0 && m.ArtifactIndex >= visible {
		start = m.ArtifactIndex - visible + 1
	}
	for i := start; i < len(m.Artifacts) && len(lines) < bodyHeight; i++ {
		entry := m.Artifacts[i]
		marker := "  "
		text := fmt.Sprintf("%s%s", marker, entry.Label)
		if i == m.ArtifactIndex {
			text = c.focus.Render(fitText("> "+entry.Label+"  ·  "+entry.Path, m.Width))
		} else {
			text = fitText(text, m.Width)
		}
		lines = append(lines, text)
	}
	return fitBlock(lines, m.Width, bodyHeight)
}
