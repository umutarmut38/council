package tui

import "testing"

// TestHandleCommandResolvesAliases checks that the registry-backed dispatch
// routes alias spellings to the same handler as the canonical command, and
// reports genuinely unknown commands.
func TestHandleCommandResolvesAliases(t *testing.T) {
	cases := []struct {
		input  string
		status string
	}{
		{"/broadcast hi", "sent to all agents"}, // alias of /all
		{"/all hi", "sent to all agents"},
		{"/exit", "quit with Ctrl+X"}, // alias of /quit
		{"/quit", "quit with Ctrl+X"},
		{"/nope", "unknown command: /nope"},
	}
	for _, tc := range cases {
		m := hudModel(t, "a")
		handled, _ := m.handleCommand(tc.input)
		if !handled {
			t.Fatalf("handleCommand(%q) should be handled", tc.input)
		}
		if m.Status != tc.status {
			t.Fatalf("handleCommand(%q) status = %q, want %q", tc.input, m.Status, tc.status)
		}
	}
}
