package usage

import (
	"testing"
	"time"

	"github.com/umutarmut38/council/internal/usage/internal/reader"
)

type fakeReader struct{ calls map[string][]reader.Call }

func (fakeReader) Name() string { return "fake" }
func (r fakeReader) ReadForCWD(cwd string) ([]reader.Call, error) {
	return r.calls[cwd], nil
}
func (r fakeReader) LatestModel(cwd string) (string, error) {
	if c := r.calls[cwd]; len(c) > 0 {
		return c[0].Model, nil
	}
	return "", nil
}

func mustTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// lookup returns rd for the given tool, nil otherwise.
func lookup(tool string, rd reader.Reader) func(string) reader.Reader {
	return func(t string) reader.Reader {
		if t == tool {
			return rd
		}
		return nil
	}
}

// One agent owns a cwd → its tool calls reconcile to a reported total.
func TestReconcileSingleAgentCWD(t *testing.T) {
	events := []Event{
		{RunID: "r", Agent: "claude-a", Tool: "claude", Phase: "plan", At: "2026-06-27T10:00:00Z", CWD: "/w/a", Confidence: Estimated, InputTokens: 5, OutputTokens: 1},
	}
	rd := fakeReader{calls: map[string][]reader.Call{
		"/w/a": {{Provider: "claude", Model: "claude-sonnet-4-6", Timestamp: mustTime("2026-06-27T10:00:03Z"), InputTokens: 100, OutputTokens: 40}},
	}}
	rep := reconcileWith(events, lookup("claude", rd))
	if len(rep) != 1 || rep[0].Confidence != Reported || rep[0].InputTokens != 100 || rep[0].CWD != "/w/a" {
		t.Fatalf("reconcile = %+v, want one reported 100-in event in /w/a", rep)
	}
	s := Aggregate(append(events, rep...))
	if s.Sessions[0].Confidence != Reported || s.Sessions[0].Input != 100 || s.Sessions[0].Output != 40 {
		t.Fatalf("aggregated session = %+v, want reported 100/40", s.Sessions[0])
	}
}

// A reader for a different tool must never be charged to this agent.
func TestReconcileOnlyUsesAgentsOwnTool(t *testing.T) {
	events := []Event{
		{Agent: "codex-a", Tool: "codex", At: "2026-06-27T10:00:00Z", CWD: "/w/a", Confidence: Estimated, InputTokens: 5},
	}
	claudeRd := fakeReader{calls: map[string][]reader.Call{"/w/a": {{Model: "claude", InputTokens: 100}}}}
	// Only a claude reader exists; the agent's tool is codex → no reconcile.
	if rep := reconcileWith(events, lookup("claude", claudeRd)); len(rep) != 0 {
		t.Fatalf("must not charge another tool's session, got %+v", rep)
	}
}

// A session that used several models reconciles to one reported event per model.
func TestReconcilePerModel(t *testing.T) {
	events := []Event{
		{Agent: "copilot-a", Tool: "copilot", At: "2026-06-27T10:00:00Z", CWD: "/w/a", Confidence: Estimated, InputTokens: 5},
	}
	rd := fakeReader{calls: map[string][]reader.Call{"/w/a": {
		{Model: "gpt-5", Timestamp: mustTime("2026-06-27T10:00:03Z"), InputTokens: 100, OutputTokens: 10},
		{Model: "claude-opus-4-6", Timestamp: mustTime("2026-06-27T10:00:04Z"), InputTokens: 200, OutputTokens: 20},
	}}}
	rep := reconcileWith(events, lookup("copilot", rd))
	if len(rep) != 2 {
		t.Fatalf("want 2 per-model reported events, got %+v", rep)
	}
	byModel := map[string]Event{}
	for _, e := range rep {
		byModel[e.Model] = e
	}
	if byModel["gpt-5"].InputTokens != 100 || byModel["claude-opus-4-6"].InputTokens != 200 {
		t.Fatalf("per-model split wrong: %+v", rep)
	}
}

// Two agents shared a cwd → ambiguous → no reconcile, estimates stand.
func TestReconcileAmbiguousSharedCWD(t *testing.T) {
	events := []Event{
		{Agent: "claude-a", Tool: "claude", CWD: "/repo", At: "2026-06-27T10:00:00Z", Confidence: Estimated, InputTokens: 5},
		{Agent: "claude-b", Tool: "claude", CWD: "/repo", At: "2026-06-27T10:00:00Z", Confidence: Estimated, InputTokens: 5},
	}
	rd := fakeReader{calls: map[string][]reader.Call{"/repo": {{InputTokens: 100}}}}
	if rep := reconcileWith(events, lookup("claude", rd)); len(rep) != 0 {
		t.Fatalf("ambiguous shared cwd must not reconcile, got %+v", rep)
	}
}

// A call outside the run's time window is not credited to the agent.
func TestReconcileIgnoresOutOfWindowCalls(t *testing.T) {
	events := []Event{
		{Agent: "claude-a", Tool: "claude", CWD: "/w/a", At: "2026-06-27T10:00:00Z", Confidence: Estimated, InputTokens: 5},
	}
	rd := fakeReader{calls: map[string][]reader.Call{
		"/w/a": {{Timestamp: mustTime("2020-01-01T00:00:00Z"), InputTokens: 999}},
	}}
	if rep := reconcileWith(events, lookup("claude", rd)); len(rep) != 0 {
		t.Fatalf("stale call must be ignored, got %+v", rep)
	}
}
