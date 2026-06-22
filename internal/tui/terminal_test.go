package tui

import (
	"strings"
	"testing"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
)

// TestTerminalCharsetDesignationNotRendered guards the emulator against leaking
// the final byte of an intermediate-byte escape sequence onto the screen as
// literal text. nvim emits "ESC ( B" (designate G0 = ASCII) after nearly every
// SGR reset; before the fix the emulator consumed only "ESC (" and rendered the
// trailing "B", corrupting the integrated-editor pane.
func TestTerminalCharsetDesignationNotRendered(t *testing.T) {
	session := agent.NewSession("nvim", config.AgentConfig{}, "")
	model := NewModel([]*agent.Session{session}, nil, 1000, "", 0, nil, nil)
	model.Width = 40
	model.Height = 6
	model.resizeAgents()

	view := model.Agents[0]
	// The exact real-world pattern: charset reset + SGR reset, then text.
	model.appendOutput(view, "\x1b(B\x1b[mpackage tui")

	plain := ansiRE.ReplaceAllString(strings.Join(view.screenLines(view.Height, view.Width), "\n"), "")
	if !strings.Contains(plain, "package tui") {
		t.Fatalf("expected 'package tui' in output, got %q", plain)
	}
	if strings.Contains(plain, "Bpackage") {
		t.Fatalf("charset-designation final byte leaked as literal text: %q", plain)
	}
}

// TestTerminalCharsetDesignationSplitBuffer checks the same sequence when it is
// cut across two output chunks (the final byte arrives in the next read), which
// must be buffered rather than dropped or leaked.
func TestTerminalCharsetDesignationSplitBuffer(t *testing.T) {
	session := agent.NewSession("nvim", config.AgentConfig{}, "")
	model := NewModel([]*agent.Session{session}, nil, 1000, "", 0, nil, nil)
	model.Width = 40
	model.Height = 6
	model.resizeAgents()

	view := model.Agents[0]
	model.appendOutput(view, "hi\x1b(") // sequence cut after the intermediate byte
	model.appendOutput(view, "Bthere")  // final byte + text arrive next

	plain := ansiRE.ReplaceAllString(strings.Join(view.screenLines(view.Height, view.Width), "\n"), "")
	if !strings.Contains(plain, "hithere") {
		t.Fatalf("expected 'hithere' across split buffers, got %q", plain)
	}
}

// TestEditorCursorRendered checks the integrated editor's cursor render draws a
// reverse-video block at the cursor, while the plain pane render does not.
func TestEditorCursorRendered(t *testing.T) {
	session := agent.NewSession("nvim", config.AgentConfig{}, "")
	model := NewModel([]*agent.Session{session}, nil, 1000, "", 0, nil, nil)
	model.Width = 40
	model.Height = 6
	model.resizeAgents()

	view := model.Agents[0]
	model.appendOutput(view, "\x1b[1;1Hhello\x1b[1;3H") // text, cursor to row1 col3

	withCursor := strings.Join(view.screenLinesCursor(view.Height, view.Width), "\n")
	if !strings.Contains(withCursor, "\x1b[7m") {
		t.Fatalf("expected a reverse-video cursor, got %q", withCursor)
	}
	if plain := strings.Join(view.screenLines(view.Height, view.Width), "\n"); strings.Contains(plain, "\x1b[7m") {
		t.Fatalf("plain screenLines must not draw a cursor: %q", plain)
	}
}

// TestEditorCursorHiddenByDECTCEM checks ESC[?25l/h toggles the cursor so the
// editor render honors a program that hides its cursor.
func TestEditorCursorHiddenByDECTCEM(t *testing.T) {
	session := agent.NewSession("nvim", config.AgentConfig{}, "")
	model := NewModel([]*agent.Session{session}, nil, 1000, "", 0, nil, nil)
	model.Width = 40
	model.Height = 6
	model.resizeAgents()

	view := model.Agents[0]
	model.appendOutput(view, "\x1b[1;1Hhi\x1b[?25l")
	if !view.CursorHidden {
		t.Fatal("expected CursorHidden after ESC[?25l")
	}
	if out := strings.Join(view.screenLinesCursor(view.Height, view.Width), "\n"); strings.Contains(out, "\x1b[7m") {
		t.Fatalf("cursor must not render while hidden: %q", out)
	}
	model.appendOutput(view, "\x1b[?25h")
	if view.CursorHidden {
		t.Fatal("expected cursor visible after ESC[?25h")
	}
}
