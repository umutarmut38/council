package tui

import (
	"strings"
	"testing"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
)

func TestTranscriptPrivacyNote(t *testing.T) {
	on := transcriptPrivacyNote(true)
	off := transcriptPrivacyNote(false)

	if !strings.Contains(on, "transcripts here are redacted") {
		t.Fatalf("redact-on note should say transcripts are redacted: %q", on)
	}
	if !strings.Contains(off, "NOT redacted") {
		t.Fatalf("redact-off note should warn transcripts are not redacted: %q", off)
	}
	for _, note := range []string{on, off} {
		if !strings.Contains(note, "raw PTY logs") {
			t.Fatalf("note should distinguish raw PTY logs: %q", note)
		}
	}
}

func newArtifactsModel() Model {
	session := agent.NewSession("shell", config.AgentConfig{}, "")
	m := NewModel([]*agent.Session{session}, nil, 1000, "", 0, nil, nil)
	m.Width = 100
	m.Height = 24
	m.resizeAgents()
	return m
}

// TestArtifactsSplitLayout: the artifact list renders as a two-column split with
// the selectable list on the left and the editor pane (placeholder) on the right.
func TestArtifactsSplitLayout(t *testing.T) {
	m := newArtifactsModel()
	m.ScreenMode = ScreenArtifacts
	m.Artifacts = []artifactEntry{
		{Label: "report", Path: "/run/report.md"},
		{Label: "plan: claude.md", Path: "/run/plans/claude.md"},
	}

	plain := ansiRE.ReplaceAllString(strings.Join(m.renderArtifacts(m.Height-m.chromeLines()), "\n"), "")
	if !strings.Contains(plain, "report") || !strings.Contains(plain, "plan: claude.md") {
		t.Fatalf("expected artifact labels in the left column, got:\n%s", plain)
	}
	if !strings.Contains(plain, "Enter on an artifact to edit it") {
		t.Fatalf("expected the editor placeholder in the right column, got:\n%s", plain)
	}
	if !strings.Contains(plain, "│") {
		t.Fatalf("expected a column separator, got:\n%s", plain)
	}
}

// TestArtifactsSyntheticViewerStillPager: a synthetic view (preview/diff, no file
// behind it) keeps the full-width read-only pager, not the split.
func TestArtifactsSyntheticViewerStillPager(t *testing.T) {
	m := newArtifactsModel()
	m.openArtifactText("compare: claude vs base", "line one\nline two\nline three")

	plain := ansiRE.ReplaceAllString(strings.Join(m.renderArtifacts(m.Height-m.chromeLines()), "\n"), "")
	if !strings.Contains(plain, "line one") || !strings.Contains(plain, "compare: claude vs base") {
		t.Fatalf("expected pager content for the synthetic view, got:\n%s", plain)
	}
	if strings.Contains(plain, "Enter on an artifact to edit it") {
		t.Fatalf("synthetic view must not render the split placeholder:\n%s", plain)
	}
}
