package tui

import (
	"strings"
	"testing"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
)

// TestVimEscapePath: filenames with spaces or Ex metacharacters are escaped so
// `:e {path}` opens the right file; ordinary paths are untouched.
func TestVimEscapePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/repo/src/app.go", "/repo/src/app.go"},
		{"/repo/my file.md", "/repo/my\\ file.md"},
		{"/repo/a|b.txt", "/repo/a\\|b.txt"},
		{"/repo/100%.md", "/repo/100\\%.md"},
		{"/repo/a#b.md", "/repo/a\\#b.md"},
	}
	for _, c := range cases {
		if got := vimEscapePath(c.in); got != c.want {
			t.Errorf("vimEscapePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestEditorOpenSequence: the default in-place command escapes the (absolute)
// path, and an explicit empty ui.editor_open_cmd disables in-place opening.
func TestEditorOpenSequence(t *testing.T) {
	session := agent.NewSession("nvim", config.AgentConfig{}, "")
	m := NewModel([]*agent.Session{session}, nil, 1000, "", 0, nil, nil)

	seq, inPlace := m.editorOpenSequence("/repo/my file.md")
	if !inPlace {
		t.Fatal("default template should open in place")
	}
	if !strings.Contains(seq, "my\\ file.md") {
		t.Fatalf("expected the path escaped in the open sequence, got %q", seq)
	}

	empty := ""
	m.Config.UI.EditorOpenCmd = &empty
	if _, inPlace := m.editorOpenSequence("/x"); inPlace {
		t.Fatal("empty editor_open_cmd should disable in-place open (relaunch instead)")
	}
}
