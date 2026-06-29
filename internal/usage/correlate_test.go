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
	rep := reconcileWith(events, lookup("claude", rd)).Events
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
	if rep := reconcileWith(events, lookup("claude", claudeRd)).Events; len(rep) != 0 {
		t.Fatalf("must not charge another tool's session, got %+v", rep)
	}
}

func TestReconcileRequiresConfiguredTool(t *testing.T) {
	events := []Event{
		{Agent: "claude-a", Tool: UnknownValue, At: "2026-06-27T10:00:00Z", CWD: "/w/a", Confidence: Estimated, InputTokens: 5},
	}
	rd := fakeReader{calls: map[string][]reader.Call{"/w/a": {{Model: "claude-sonnet-4-6", InputTokens: 100}}}}
	if rep := reconcileWith(events, lookup("claude", rd)).Events; len(rep) != 0 {
		t.Fatalf("missing usage.tool must not reconcile, got %+v", rep)
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
	rep := reconcileWith(events, lookup("copilot", rd)).Events
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

// Several same-tool agents sharing one cwd with no personality fingerprint can't
// be told apart → no reconcile, estimates stand (no pane charged for another's
// spend).
func TestReconcileMultipleSameToolSameCWD(t *testing.T) {
	events := []Event{
		{Agent: "claude-a", Tool: "claude", CWD: "/repo", At: "2026-06-27T10:00:00Z", Confidence: Estimated, InputTokens: 5},
		{Agent: "claude-b", Tool: "claude", CWD: "/repo", At: "2026-06-27T10:00:01Z", Confidence: Estimated, InputTokens: 5},
	}
	rd := fakeReader{calls: map[string][]reader.Call{"/repo": {
		{Model: "m", Timestamp: mustTime("2026-06-27T10:00:05Z"), InputTokens: 100},
	}}}
	if rep := reconcileWith(events, lookup("claude", rd)).Events; len(rep) != 0 {
		t.Fatalf("multiple same-tool agents must not reconcile, got %+v", rep)
	}
}

// Same-tool agents in one cwd are told apart by the personality prefix that
// starts each session's first user message.
func TestReconcileSameToolDisambiguatedByFingerprint(t *testing.T) {
	events := []Event{
		{Agent: "claude-architect", Tool: "claude", CWD: "/repo", At: "2026-06-27T10:00:00Z", Confidence: Estimated, Fingerprint: "You are The Architect. Think in systems.", InputTokens: 5},
		{Agent: "claude-skeptic", Tool: "claude", CWD: "/repo", At: "2026-06-27T10:00:01Z", Confidence: Estimated, Fingerprint: "You are The Skeptic. Question everything.", InputTokens: 5},
	}
	rd := fakeReader{calls: map[string][]reader.Call{"/repo": {
		{Model: "m", UserMessage: "You are The Architect. Think in systems.\n\nDo the plan.", InputTokens: 100, OutputTokens: 10},
		{Model: "m", UserMessage: "You are The Skeptic. Question everything.\n\nDo the plan.", InputTokens: 200, OutputTokens: 20},
	}}}
	rep := reconcileWith(events, lookup("claude", rd)).Events
	byAgent := map[string]Event{}
	for _, e := range rep {
		byAgent[e.Agent] = e
	}
	if byAgent["claude-architect"].InputTokens != 100 || byAgent["claude-skeptic"].InputTokens != 200 {
		t.Fatalf("fingerprint routing wrong: %+v", rep)
	}
}

func TestReconcileSharedFingerprintUsesUniquePromptMatch(t *testing.T) {
	fp := "You are The Architect. Think in systems."
	a := Event{Agent: "claude-a", Tool: "claude", CWD: "/repo", At: "2026-06-27T10:00:00Z", Confidence: Estimated, Fingerprint: fp, PromptPreview: fp + " Build the parser.", PromptHash: "a", InputTokens: 5}
	b := Event{Agent: "claude-b", Tool: "claude", CWD: "/repo", At: "2026-06-27T10:00:01Z", Confidence: Estimated, Fingerprint: fp, PromptPreview: fp + " Build the UI.", PromptHash: "b", InputTokens: 5}
	a.normalize()
	b.normalize()
	rd := fakeReader{calls: map[string][]reader.Call{"/repo": {
		{Model: "m", UserMessage: fp + "\n\nBuild the UI.", InputTokens: 200},
	}}}
	rep := reconcileWith([]Event{a, b}, lookup("claude", rd)).Events
	if len(rep) != 1 || rep[0].Agent != "claude-b" || rep[0].InputTokens != 200 {
		t.Fatalf("unique prompt match should pick claude-b, got %+v", rep)
	}
}

// Two panes with the SAME personality share a fingerprint → a session matches
// both → ambiguous → estimate stands rather than charge the wrong pane.
func TestReconcileSharedFingerprintStaysEstimated(t *testing.T) {
	fp := "You are The Architect. Think in systems."
	events := []Event{
		{Agent: "claude-a", Tool: "claude", CWD: "/repo", At: "2026-06-27T10:00:00Z", Confidence: Estimated, Fingerprint: fp},
		{Agent: "claude-b", Tool: "claude", CWD: "/repo", At: "2026-06-27T10:00:01Z", Confidence: Estimated, Fingerprint: fp},
	}
	rd := fakeReader{calls: map[string][]reader.Call{"/repo": {
		{Model: "m", UserMessage: fp + "\n\nGo.", InputTokens: 100},
	}}}
	if rep := reconcileWith(events, lookup("claude", rd)).Events; len(rep) != 0 {
		t.Fatalf("shared fingerprint must stay estimated, got %+v", rep)
	}
}

func TestProviderModelReplacesConfiguredEstimate(t *testing.T) {
	est := Event{RunID: "r", Agent: "claude-a", Tool: "claude", Model: "haiku", ModelSource: MetaSourceConfig, CWD: "/repo", Phase: "plan", At: "2026-06-27T10:00:00Z", Confidence: Estimated, PromptPreview: "Do the plan", PromptHash: "p", InputTokens: 5}
	est.normalize()
	rd := fakeReader{calls: map[string][]reader.Call{"/repo": {
		{Model: "claude-sonnet-4-6", UserMessage: "Do the plan", Timestamp: mustTime("2026-06-27T10:00:01Z"), InputTokens: 100},
	}}}
	rep := reconcileWith([]Event{est}, lookup("claude", rd)).Events
	if len(rep) != 1 {
		t.Fatalf("got %+v, want one reported event", rep)
	}
	if rep[0].Model != "claude-sonnet-4-6" || rep[0].ModelSource != MetaSourceProvider {
		t.Fatalf("reported model = %q/%q, want provider concrete model", rep[0].Model, rep[0].ModelSource)
	}
	s := Aggregate([]Event{est, rep[0]})
	if len(s.Sessions) != 1 || s.Sessions[0].Model != "claude-sonnet-4-6" || s.Input != 100 {
		t.Fatalf("reported provider model should replace estimate: %+v", s.Sessions)
	}
}

// One claude + one codex in the SAME cwd both reconcile — each is the only agent
// of its tool there, and the tools read different stores.
func TestReconcileCrossToolSameCWD(t *testing.T) {
	events := []Event{
		{Agent: "claude-w", Tool: "claude", CWD: "/repo", At: "2026-06-27T10:00:00Z", Confidence: Estimated, InputTokens: 5},
		{Agent: "codex-w", Tool: "codex", CWD: "/repo", At: "2026-06-27T10:00:00Z", Confidence: Estimated, InputTokens: 5},
	}
	claudeRd := fakeReader{calls: map[string][]reader.Call{"/repo": {{Model: "claude-sonnet-4-6", InputTokens: 100, OutputTokens: 10}}}}
	codexRd := fakeReader{calls: map[string][]reader.Call{"/repo": {{Model: "gpt-5", InputTokens: 200, OutputTokens: 20}}}}
	readerFor := func(tool string) reader.Reader {
		switch tool {
		case "claude":
			return claudeRd
		case "codex":
			return codexRd
		}
		return nil
	}
	rep := reconcileWith(events, readerFor).Events
	byAgent := map[string]Event{}
	for _, e := range rep {
		byAgent[e.Agent] = e
	}
	if byAgent["claude-w"].InputTokens != 100 || byAgent["codex-w"].InputTokens != 200 {
		t.Fatalf("cross-tool same-cwd should reconcile both: %+v", rep)
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
	if rep := reconcileWith(events, lookup("claude", rd)).Events; len(rep) != 0 {
		t.Fatalf("stale call must be ignored, got %+v", rep)
	}
}

func TestReconcileAndAppendIdempotent(t *testing.T) {
	runDir := t.TempDir()
	est := Event{RunID: "r", Agent: "claude-a", Tool: "claude", CWD: "/repo", Phase: "plan", At: "2026-06-27T10:00:00Z", Confidence: Estimated, PromptPreview: "Do the plan", PromptHash: "p", InputTokens: 5}
	if err := Append(runDir, est); err != nil {
		t.Fatal(err)
	}
	events, err := LoadEvents(runDir)
	if err != nil {
		t.Fatal(err)
	}
	rd := fakeReader{calls: map[string][]reader.Call{"/repo": {
		{Model: "claude-sonnet-4-6", SessionID: "s1", CallID: "c1", UserMessage: "Do the plan", Timestamp: mustTime("2026-06-27T10:00:01Z"), InputTokens: 100},
	}}}
	readerFor := lookup("claude", rd)
	events, rec, err := reconcileAndAppendWith(runDir, events, readerFor)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Events) != 1 {
		t.Fatalf("first reconcile appended %d events, want 1", len(rec.Events))
	}
	events, rec, err = reconcileAndAppendWith(runDir, events, readerFor)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Events) != 0 {
		t.Fatalf("second reconcile appended duplicates: %+v", rec.Events)
	}
	loaded, _ := LoadEvents(runDir)
	if len(loaded) != 2 {
		t.Fatalf("ledger events = %d, want estimate + one report", len(loaded))
	}
}
