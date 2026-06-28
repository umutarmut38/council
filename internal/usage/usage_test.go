package usage

import (
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

// A reported event must supersede the estimated event for the same (agent,
// phase) rather than be summed with it — otherwise reconciliation double-counts.
func TestAggregateReportedBeatsEstimated(t *testing.T) {
	s := Aggregate([]Event{
		{Agent: "codex", Phase: "build", Confidence: Estimated, InputTokens: 100, OutputTokens: 20},
		{Agent: "codex", Phase: "build", Confidence: Reported, InputTokens: 80, OutputTokens: 25},
	})
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

// A reported whole-session total supersedes the agent's per-prompt estimates
// (across all phases) instead of being summed with them.
func TestAggregateReportedSupersedesEstimatesAcrossPhases(t *testing.T) {
	s := Aggregate([]Event{
		{Agent: "codex", Phase: "plan", Confidence: Estimated, InputTokens: 10, OutputTokens: 2},
		{Agent: "codex", Phase: "build", Confidence: Estimated, InputTokens: 30, OutputTokens: 5},
		{Agent: "codex", Source: SourceProvider, Confidence: Reported, InputTokens: 99, OutputTokens: 88},
	})
	if s.Sessions[0].Confidence != Reported {
		t.Fatalf("confidence = %q, want reported", s.Sessions[0].Confidence)
	}
	if s.Sessions[0].Input != 99 || s.Sessions[0].Output != 88 {
		t.Fatalf("got %d/%d, want 99/88 (reported only)", s.Sessions[0].Input, s.Sessions[0].Output)
	}
}

// A reported total supersedes estimates only for ITS cwd; a cwd that never
// reconciled keeps its estimate (no undercount from a partial reconciliation).
func TestAggregateKeepsEstimatesForUnreconciledCWD(t *testing.T) {
	s := Aggregate([]Event{
		{Agent: "claude", CWD: "/a", Model: "m", Confidence: Estimated, InputTokens: 100},
		{Agent: "claude", CWD: "/a", Model: "m", Source: SourceProvider, Confidence: Reported, InputTokens: 80},
		{Agent: "claude", CWD: "/b", Model: "m", Confidence: Estimated, InputTokens: 50}, // never reconciled
	})
	if s.Input != 130 { // reported 80 (cwd /a) + estimated 50 (cwd /b); not 80 and not 230
		t.Fatalf("input = %d, want 130 (reported /a + estimated /b kept)", s.Input)
	}
	if s.Sessions[0].Confidence != Estimated { // weakest contributing tier
		t.Fatalf("confidence = %q, want estimated (weakest)", s.Sessions[0].Confidence)
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
