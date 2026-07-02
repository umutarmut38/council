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

// lookup returns rd for the given tool (the cumulative/global reader), nil otherwise.
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

// Cumulative mode: several same-tool agents in one cwd are NOT split per pane —
// the shared/global store can't tell them apart, so their usage is reported as
// one combined row (labeled by tool) that replaces both estimates.
func TestReconcileCumulativeCombinesSameTool(t *testing.T) {
	events := []Event{
		{Agent: "claude-a", Tool: "claude", CWD: "/repo", At: "2026-06-27T10:00:00Z", Confidence: Estimated, InputTokens: 5},
		{Agent: "claude-b", Tool: "claude", CWD: "/repo", At: "2026-06-27T10:00:01Z", Confidence: Estimated, InputTokens: 5},
	}
	rd := fakeReader{calls: map[string][]reader.Call{"/repo": {
		{Model: "m", Timestamp: mustTime("2026-06-27T10:00:05Z"), InputTokens: 100, OutputTokens: 10},
		{Model: "m", Timestamp: mustTime("2026-06-27T10:00:06Z"), InputTokens: 200, OutputTokens: 20},
	}}}
	rep := reconcileWith(events, lookup("claude", rd)).Events
	if len(rep) != 1 || rep[0].Agent != "claude" || rep[0].InputTokens != 300 || rep[0].OutputTokens != 30 {
		t.Fatalf("cumulative should combine same-tool into one 300/30 row, got %+v", rep)
	}
	if len(rep[0].Replaces) != 2 {
		t.Fatalf("combined row should replace both pane estimates, got replaces=%v", rep[0].Replaces)
	}
}

// Isolated mode: each pane runs in its OWN worktree (distinct cwd), so the same
// (tool,cwd) grouping attributes per pane — no shared store, no fingerprint.
func TestReconcileIsolatedDistinctCWD(t *testing.T) {
	events := []Event{
		{Agent: "claude-a", Tool: "claude", CWD: "/wt/a", At: "2026-06-27T10:00:00Z", Confidence: Estimated, InputTokens: 5},
		{Agent: "claude-b", Tool: "claude", CWD: "/wt/b", At: "2026-06-27T10:00:01Z", Confidence: Estimated, InputTokens: 5},
	}
	rd := fakeReader{calls: map[string][]reader.Call{
		"/wt/a": {{Model: "m", InputTokens: 100, OutputTokens: 10}},
		"/wt/b": {{Model: "m", InputTokens: 200, OutputTokens: 20}},
	}}
	rep := reconcileWith(events, lookup("claude", rd)).Events
	byAgent := map[string]Event{}
	for _, e := range rep {
		byAgent[e.Agent] = e
	}
	if byAgent["claude-a"].InputTokens != 100 || byAgent["claude-b"].InputTokens != 200 {
		t.Fatalf("distinct worktree cwds should attribute per pane, got %+v", rep)
	}
}

// Codex records are per-session cumulative snapshots that the reader collapses to
// one fresh-input Call per session (see codex reader). Reconcile then sums the
// distinct sessions in a cwd. This proves the reconciled Input is the sum of the
// FRESH inputs with cache kept in its own column — never folding cache back into
// Input (the 39.9k over-count). It also documents that combining several codex
// sessions sharing a cwd sums them (what distinct stable workspaces then split
// into per-pane rows).
func TestReconcileCodexInputExcludesCacheAndSumsSessions(t *testing.T) {
	events := []Event{
		{Agent: "codex-a", Tool: "codex", CWD: "/repo", At: "2026-06-27T10:00:00Z", Confidence: Estimated, InputTokens: 55},
		{Agent: "codex-b", Tool: "codex", CWD: "/repo", At: "2026-06-27T10:00:01Z", Confidence: Estimated, InputTokens: 55},
	}
	// Two codex sessions in one cwd; each Call already carries FRESH input +
	// separate cache (post-reader-fix shape): 8807 fresh + 4480 cache, 9837 + 3456.
	rd := fakeReader{calls: map[string][]reader.Call{"/repo": {
		{Model: "gpt-5.4-mini", SessionID: "s1", Timestamp: mustTime("2026-06-27T10:00:05Z"), InputTokens: 8807, CacheRead: 4480, OutputTokens: 25},
		{Model: "gpt-5.4-mini", SessionID: "s2", Timestamp: mustTime("2026-06-27T10:00:06Z"), InputTokens: 9837, CacheRead: 3456, OutputTokens: 61},
	}}}
	rep := reconcileWith(events, lookup("codex", rd)).Events
	if len(rep) != 1 {
		t.Fatalf("want one combined codex row, got %+v", rep)
	}
	e := rep[0]
	// Input is the sum of the FRESH inputs (8807+9837), NOT the cache-inflated
	// totals (13287+13293=26580) and NOT with cache folded in.
	if e.InputTokens != 18644 {
		t.Fatalf("Input = %d, want 18644 (fresh 8807+9837, no cache)", e.InputTokens)
	}
	if e.CacheReadTokens != 7936 {
		t.Fatalf("CacheRead = %d, want 7936 (4480+3456), tracked separately", e.CacheReadTokens)
	}
	if e.OutputTokens != 86 {
		t.Fatalf("Output = %d, want 86", e.OutputTokens)
	}
	// The aggregated Input column must show the fresh total, never Input+cache.
	s := Aggregate(append(events, rep...))
	if s.Tokens.Input != 18644 {
		t.Fatalf("aggregated Input = %d, want 18644 (cache excluded from the Input column)", s.Tokens.Input)
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
		"/w/a": {
			// Session over long before the run.
			{Timestamp: mustTime("2020-01-01T00:00:00Z"), LastActivity: mustTime("2020-01-01T01:00:00Z"), InputTokens: 999},
			// Session started long after the run's window.
			{Timestamp: mustTime("2026-06-27T11:00:00Z"), InputTokens: 999},
		},
	}}
	if rep := reconcileWith(events, lookup("claude", rd)).Events; len(rep) != 0 {
		t.Fatalf("out-of-window calls must be ignored, got %+v", rep)
	}
}

// Council launches panes at startup, so a session can START long before the
// first prompt. As long as it was still ACTIVE inside the run window, its
// reported totals count — a point-in-time start check would drop it (the
// "codex reconcile silently dead after 60s of idle" bug).
func TestReconcileIncludesSessionStartedBeforeWindow(t *testing.T) {
	events := []Event{
		{Agent: "codex-a", Tool: "codex", CWD: "/w/a", At: "2026-06-27T10:00:00Z", Confidence: Estimated, InputTokens: 5},
	}
	rd := fakeReader{calls: map[string][]reader.Call{
		"/w/a": {{
			Model:        "gpt-5",
			Timestamp:    mustTime("2026-06-27T09:30:00Z"), // pane launched, user idled 30m
			LastActivity: mustTime("2026-06-27T10:00:20Z"), // then worked during the run
			InputTokens:  100, OutputTokens: 10,
		}},
	}}
	rep := reconcileWith(events, lookup("codex", rd)).Events
	if len(rep) != 1 || rep[0].InputTokens != 100 {
		t.Fatalf("active-in-window session must reconcile despite early start, got %+v", rep)
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

// A (tool, cwd) group whose label flips from the pane name (single agent) to
// the tool name (combined) between two sweeps must not keep the stale
// single-agent sweep alive: both events re-compute the same cumulative store
// total, so Aggregate keeps only the richest.
func TestSingleToCombinedFlipSupersedes(t *testing.T) {
	runDir := t.TempDir()
	estA := Event{RunID: "r", Agent: "claude-a", Tool: "claude", Phase: "plan",
		At: "2026-06-27T10:00:00Z", CWD: "/repo", Confidence: Estimated, InputTokens: 5}
	if err := Append(runDir, estA); err != nil {
		t.Fatal(err)
	}
	events, _ := LoadEvents(runDir)
	rd1 := fakeReader{calls: map[string][]reader.Call{"/repo": {
		{Model: "m", Timestamp: mustTime("2026-06-27T10:00:03Z"), InputTokens: 100, OutputTokens: 10},
	}}}
	events, _, err := reconcileAndAppendWith(runDir, events, lookup("claude", rd1))
	if err != nil {
		t.Fatal(err)
	}

	// A second same-tool pane sends its first prompt; the store now has both.
	estB := Event{RunID: "r", Agent: "claude-b", Tool: "claude", Phase: "plan",
		At: "2026-06-27T10:00:05Z", CWD: "/repo", Confidence: Estimated, InputTokens: 5}
	if err := Append(runDir, estB); err != nil {
		t.Fatal(err)
	}
	events, _ = LoadEvents(runDir)
	rd2 := fakeReader{calls: map[string][]reader.Call{"/repo": {
		{Model: "m", Timestamp: mustTime("2026-06-27T10:00:03Z"), InputTokens: 100, OutputTokens: 10},
		{Model: "m", Timestamp: mustTime("2026-06-27T10:00:08Z"), InputTokens: 200, OutputTokens: 20},
	}}}
	events, _, err = reconcileAndAppendWith(runDir, events, lookup("claude", rd2))
	if err != nil {
		t.Fatal(err)
	}

	s := Aggregate(events)
	if s.Tokens.Input != 300 || s.Tokens.Output != 30 {
		t.Fatalf("total = %d/%d, want 300/30 (stale single-agent sweep must be superseded); sessions: %+v",
			s.Tokens.Input, s.Tokens.Output, s.Sessions)
	}
}

// Provider events from DIFFERENT runs are independent totals: supersession is
// scoped per run, so a cross-run aggregation (council cost --since) sums them.
func TestProviderSupersessionScopedPerRun(t *testing.T) {
	mk := func(runID string, in int) Event {
		e := Event{RunID: runID, Agent: "claude", Tool: "claude", Model: "m", CWD: "/repo",
			Source: SourceProvider, Confidence: Reported, InputTokens: in}
		e.normalize()
		return e
	}
	s := Aggregate([]Event{mk("run1", 100), mk("run2", 200)})
	if s.Tokens.Input != 300 {
		t.Fatalf("cross-run total = %d, want 300 (runs are additive, not superseded)", s.Tokens.Input)
	}
}
