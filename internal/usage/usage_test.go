package usage

import (
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	if got := EstimateTokens("12345678"); got != 2 { // 8 chars / 4
		t.Fatalf("EstimateTokens = %d, want 2", got)
	}
}

// Two instances of the same CLI must stay separate sessions, not merge.
func TestAggregateKeepsSameToolInstancesApart(t *testing.T) {
	s := Aggregate([]Event{
		{Agent: "claude-a", Phase: "plan", Confidence: Estimated, InputTokens: 100, OutputTokens: 10, Model: "claude-sonnet-4-6"},
		{Agent: "claude-b", Phase: "plan", Confidence: Estimated, InputTokens: 200, OutputTokens: 20, Model: "claude-sonnet-4-6"},
	})
	if len(s.Sessions) != 2 {
		t.Fatalf("got %d sessions, want 2 (one per pane)", len(s.Sessions))
	}
	if s.Sessions[0].Agent != "claude-a" || s.Sessions[0].Input != 100 {
		t.Fatalf("claude-a wrong: %+v", s.Sessions[0])
	}
	if s.Input != 300 {
		t.Fatalf("grand input = %d, want 300", s.Input)
	}
}

// A reported event replaces only the estimated UsageKey it names.
func TestAggregateReportedReplacesSameUsageKey(t *testing.T) {
	est := Event{RunID: "r", Agent: "codex", Phase: "build", Tool: "codex", Model: UnknownValue, PromptHash: "p1", Confidence: Estimated, InputTokens: 100, OutputTokens: 20}
	est.normalize()
	rep := Event{RunID: "r", Agent: "codex", Phase: "build", Tool: "codex", Model: "gpt-5", Source: SourceProvider, Confidence: Reported, InputTokens: 80, OutputTokens: 25, Replaces: []string{est.ReconcileKey}}
	rep.normalize()
	s := Aggregate([]Event{est, rep})
	if len(s.Sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(s.Sessions))
	}
	if s.Sessions[0].Input != 80 || s.Sessions[0].Output != 25 {
		t.Fatalf("got %d/%d, want 80/25 (reported only, not summed)", s.Sessions[0].Input, s.Sessions[0].Output)
	}
	if s.Sessions[0].Confidence != Reported {
		t.Fatalf("confidence = %q, want reported", s.Sessions[0].Confidence)
	}
}

func TestFormatTableCompactsRepeatedHintsAndHidesGenericRowUnknowns(t *testing.T) {
	s := Aggregate([]Event{
		{Agent: "claude", Phase: "session", Confidence: Estimated, InputTokens: 40},
		{Agent: "codex", Phase: "session", Confidence: Estimated, InputTokens: 44},
	})
	s.Price(NewPricer(PricerOptions{}))
	out := FormatTable(s)
	if strings.Contains(out, "price unknown\t") || strings.Contains(out, "price unknown; price unknown") {
		t.Fatalf("generic row note leaked into table:\n%s", out)
	}
	if strings.Count(out, "usage.tool is not configured") != 1 {
		t.Fatalf("usage.tool hint should be compacted once:\n%s", out)
	}
	if strings.Count(out, "usage.model is not configured") != 1 {
		t.Fatalf("usage.model hint should be compacted once:\n%s", out)
	}
	if strings.Count(out, "price unknown for model --") != 1 {
		t.Fatalf("price unknown hint should be compacted once:\n%s", out)
	}
	if !strings.Contains(out, "2 sessions: usage.tool is not configured") {
		t.Fatalf("missing compact session count:\n%s", out)
	}
}

func TestAggregateDifferentPhasesRemainSeparate(t *testing.T) {
	plan := Event{RunID: "r", Agent: "codex", Phase: "plan", Tool: "codex", Model: UnknownValue, PromptHash: "p1", Confidence: Estimated, InputTokens: 10, OutputTokens: 2}
	build := Event{RunID: "r", Agent: "codex", Phase: "build", Tool: "codex", Model: UnknownValue, PromptHash: "p2", Confidence: Estimated, InputTokens: 30, OutputTokens: 5}
	plan.normalize()
	build.normalize()
	rep := Event{RunID: "r", Agent: "codex", Phase: "build", Tool: "codex", Model: "gpt-5", Source: SourceProvider, Confidence: Reported, InputTokens: 99, OutputTokens: 88, Replaces: []string{build.ReconcileKey}}
	rep.normalize()
	s := Aggregate([]Event{plan, build, rep})
	if len(s.Sessions) != 2 {
		t.Fatalf("got %d sessions, want plan estimate + build report", len(s.Sessions))
	}
	if s.Input != 109 {
		t.Fatalf("input = %d, want 109 (plan estimate + build report)", s.Input)
	}
}

// A reported total supersedes estimates only for ITS cwd; a cwd that never
// reconciled keeps its estimate (no undercount from a partial reconciliation).
func TestAggregateKeepsEstimatesForUnreconciledCWD(t *testing.T) {
	a := Event{Agent: "claude", CWD: "/a", Tool: "claude", Model: "m", Confidence: Estimated, InputTokens: 100}
	b := Event{Agent: "claude", CWD: "/b", Tool: "claude", Model: "m", Confidence: Estimated, InputTokens: 50}
	a.normalize()
	b.normalize()
	rep := Event{Agent: "claude", CWD: "/a", Tool: "claude", Model: "m", Source: SourceProvider, Confidence: Reported, InputTokens: 80, Replaces: []string{a.ReconcileKey}}
	rep.normalize()
	s := Aggregate([]Event{a, rep, b})
	if s.Input != 130 { // reported 80 (cwd /a) + estimated 50 (cwd /b); not 80 and not 230
		t.Fatalf("input = %d, want 130 (reported /a + estimated /b kept)", s.Input)
	}
}

// A session that used two models becomes two rows so each prices at its own rate.
func TestAggregateSplitsByModel(t *testing.T) {
	s := Aggregate([]Event{
		{Agent: "copilot", CWD: "/a", Model: "gpt-5", Source: SourceProvider, Confidence: Reported, InputTokens: 100, OutputTokens: 10},
		{Agent: "copilot", CWD: "/a", Model: "claude-opus-4-6", Source: SourceProvider, Confidence: Reported, InputTokens: 200, OutputTokens: 20},
	})
	if len(s.Sessions) != 2 {
		t.Fatalf("want 2 per-model rows, got %d (%+v)", len(s.Sessions), s.Sessions)
	}
	if s.Sessions[0].Model != "claude-opus-4-6" || s.Sessions[1].Model != "gpt-5" { // sorted by model
		t.Fatalf("rows not split/sorted by model: %+v", s.Sessions)
	}
	if s.Input != 300 {
		t.Fatalf("grand input = %d, want 300", s.Input)
	}
}

func TestLedgerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Record(Event{RunID: "r1", Agent: "claude", Phase: "plan", Source: SourcePrompt, Confidence: Estimated, InputTokens: 42}); err != nil {
		t.Fatal(err)
	}
	if l.Tokens["claude"].Input != 42 {
		t.Fatalf("live tally = %d, want 42", l.Tokens["claude"].Input)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	evs, err := LoadEvents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].InputTokens != 42 || evs[0].SchemaVersion != SchemaVersion {
		t.Fatalf("round-trip mismatch: %+v", evs)
	}
}

func TestLoadEventsMissingFile(t *testing.T) {
	evs, err := LoadEvents(t.TempDir())
	if err != nil || evs != nil {
		t.Fatalf("missing file should be empty/no-error, got %v / %v", evs, err)
	}
}
