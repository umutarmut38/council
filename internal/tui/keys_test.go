package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
)

// TestKeyToPTYEscape guards the Esc → \x1b mapping that lets vim-like programs
// leave insert mode while a pane is focused in direct mode.
func TestKeyToPTYEscape(t *testing.T) {
	if got := keyToPTY(tea.KeyMsg{Type: tea.KeyEsc}, ""); got != "\x1b" {
		t.Fatalf("keyToPTY(esc) = %q, want %q", got, "\x1b")
	}
}

// TestDirectModeEscPassesThrough checks that Esc in direct mode does NOT exit to
// the composer (so it falls through to the pane), while F2 and Ctrl+O still exit.
func TestDirectModeEscPassesThrough(t *testing.T) {
	newDirectModel := func() Model {
		session := agent.NewSession("agent", config.AgentConfig{}, "")
		m := NewModel([]*agent.Session{session}, nil, 1000, "", 0, nil, nil)
		m.Width = 80
		m.Height = 24
		m.resizeAgents()
		m.InputMode = InputDirect
		return m
	}

	// Esc must keep us in direct mode.
	m := newDirectModel()
	updated, _ := m.handleDirectKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got := updated.(Model).InputMode; got != InputDirect {
		t.Fatalf("Esc in direct mode: InputMode = %v, want InputDirect", got)
	}

	// F2 and Ctrl+O must exit to the composer.
	for _, key := range []tea.KeyMsg{{Type: tea.KeyF2}, {Type: tea.KeyCtrlO}} {
		m := newDirectModel()
		updated, _ := m.handleDirectKey(key)
		if got := updated.(Model).InputMode; got != InputComposer {
			t.Fatalf("%s in direct mode: InputMode = %v, want InputComposer", key.String(), got)
		}
	}
}
