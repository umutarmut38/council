package tui

import (
	"strings"
	"testing"

	"github.com/umutarmut38/council/internal/setup"
)

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

// TestSetupCommandReportsStatus checks that /setup reports an empty state when
// nothing was configured and otherwise opens the rendered observability report.
func TestSetupCommandReportsStatus(t *testing.T) {
	m := hudModel(t, "a")
	if handled, _ := m.handleCommand("/setup"); !handled {
		t.Fatal("/setup should be handled")
	}
	if m.Status != "no pre-launch setup or env configured" {
		t.Fatalf("empty-state status = %q", m.Status)
	}

	st := setup.New()
	st.SetEnvKeys(map[string]string{"API_KEY": "x"})
	h := st.Begin("api", []string{"npm", "start"}, setup.KindBackground, 8080)
	h.Running(7)
	h.Ready()

	m2 := hudModel(t, "a")
	m2.SetSetupStatus(st)
	if handled, _ := m2.handleCommand("/setup"); !handled {
		t.Fatal("/setup should be handled")
	}
	if !strings.Contains(m2.artifactView, "exported env keys") {
		t.Fatalf("artifact view missing setup report:\n%s", m2.artifactView)
	}
	if !strings.Contains(m2.Status, "1 command(s)") || !strings.Contains(m2.Status, "1 exported env key(s)") {
		t.Fatalf("status = %q", m2.Status)
	}
}
