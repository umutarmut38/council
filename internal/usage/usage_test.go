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

// Below a dime, costs render with four decimals so several small rows reconcile
// with their total ($0.0060 ×3 + $0.0019 = $0.0199, not three "$0.01" → "$0.03").
func TestFormatMoneyPrecision(t *testing.T) {
	for _, tc := range []struct {
		v    float64
		want string
	}{
		{0, "0.00"},
		{0.006, "0.0060"},
		{0.0199, "0.0199"},
		{0.099, "0.0990"},
		{0.10, "0.10"},
		{0.15, "0.15"},
		{1.2, "1.20"},
	} {
		if got := FormatMoney(tc.v); got != tc.want {
			t.Errorf("FormatMoney(%v) = %q, want %q", tc.v, got, tc.want)
		}
	}
}

// Yen has no minor unit, so the /cost table must not print fractional yen —
// including under the new sub-dime 4-decimal rule.
func TestCostJPYNoDecimals(t *testing.T) {
	big, small := 1234.0, 0.06
	if got := cost(&big, "JPY"); got != "JPY 1234" {
		t.Errorf("cost(1234, JPY) = %q, want %q", got, "JPY 1234")
	}
	if got := cost(&small, "JPY"); got != "JPY 0" {
		t.Errorf("cost(0.06, JPY) = %q, want %q (no fractional yen)", got, "JPY 0")
	}
}

// Cache writes ($1.25/M-class rates) and reads ($0.10/M-class) are priced an
// order of magnitude apart, so the /cost table shows them as separate CacheW /
// CacheR columns rather than one lumped Cache figure.
func TestFormatTableSplitsCacheWriteAndRead(t *testing.T) {
	out := FormatTable(Summary{
		Sessions: []SessionTotal{
			{Agent: "claude", Phase: "session", Confidence: Reported,
				Tokens: TokenTotals{Input: 20, Output: 486, CacheCreate: 30648, CacheRead: 27374}},
		},
		Tokens: TokenTotals{Input: 20, Output: 486, CacheCreate: 30648, CacheRead: 27374},
	})
	header := strings.SplitN(out, "\n", 2)[0]
	if !strings.Contains(header, "CacheW") || !strings.Contains(header, "CacheR") || strings.Contains(header, "Cache ") {
		t.Fatalf("header = %q, want separate CacheW and CacheR columns", header)
	}
	for _, prefix := range []string{"claude", "Total"} {
		for _, want := range []string{"30.6k", "27.4k"} {
			ok := false
			for _, l := range strings.Split(out, "\n") {
				if strings.HasPrefix(l, prefix) && strings.Contains(l, want) {
					ok = true
				}
			}
			if !ok {
				t.Fatalf("%s row missing %s:\n%s", prefix, want, out)
			}
		}
	}
}

// The /cost table Cost column carries the ~ estimate prefix for a still-estimated
// row (e.g. Copilot before it reports on exit); the Total is estimated whenever
// any session is.
func TestFormatTableEstimatePrefix(t *testing.T) {
	c := func(v float64) *float64 { return &v }
	out := FormatTable(Summary{
		Sessions: []SessionTotal{
			{Agent: "copilot", Phase: "session", Confidence: Estimated, Cost: c(0.006), Currency: "USD"},
			{Agent: "codex", Phase: "session", Confidence: Reported, Cost: c(0.006), Currency: "USD"},
		},
		Cost:     c(0.012),
		Currency: "USD",
	})
	row := func(prefix string) string {
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, prefix) {
				return l
			}
		}
		return ""
	}
	if r := row("copilot"); !strings.Contains(r, "~$0.0060") {
		t.Fatalf("estimated row = %q, want ~$0.0060:\n%s", r, out)
	}
	if r := row("codex"); !strings.Contains(r, "$0.0060") || strings.Contains(r, "~") {
		t.Fatalf("reported row = %q, want plain $0.0060 (no ~):\n%s", r, out)
	}
	if r := row("Total"); !strings.Contains(r, "~$0.0120") {
		t.Fatalf("Total = %q, want ~$0.0120 (any session estimated):\n%s", r, out)
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

// Repeated reconcile sweeps for the same (tool, cwd, model) group must not be
// summed. Each sweep re-computes the cumulative provider total; a later sweep
// carries a wider Replaces set (a new prompt/estimate appeared) and is persisted
// as a distinct event. Aggregate must keep only the richest, not add them — the
// real bug where "hi" showed ~18k input (8964+8964) instead of ~9k.
func TestAggregateSupersedesRepeatedReconcileSweeps(t *testing.T) {
	cwd := "/repo/.council/workspaces/codex-builder"
	est1 := Event{RunID: "r", Agent: "codex-builder", Phase: "session", Tool: "codex", CWD: cwd, Model: UnknownValue, PromptHash: "p1", Confidence: Estimated, InputTokens: 52}
	est1.normalize()
	est2 := Event{RunID: "r", Agent: "codex-builder", Phase: "session", Tool: "codex", CWD: cwd, Model: UnknownValue, PromptHash: "p2", Confidence: Estimated, InputTokens: 55}
	est2.normalize()

	// Sweep 1: after the first prompt (replaces est1 only).
	rep1 := Event{RunID: "r", Agent: "codex-builder", Phase: "session", Tool: "codex", CWD: cwd, Model: "gpt-5.4-mini", Source: SourceProvider, Confidence: Reported, InputTokens: 8964, OutputTokens: 29, Replaces: []string{est1.ReconcileKey}}
	rep1.normalize()
	// Sweep 2: after the second prompt (replaces est1+est2). Same cumulative
	// session total re-read; a distinct event because Replaces grew.
	rep2 := Event{RunID: "r", Agent: "codex-builder", Phase: "session", Tool: "codex", CWD: cwd, Model: "gpt-5.4-mini", Source: SourceProvider, Confidence: Reported, InputTokens: 8964, OutputTokens: 29, Replaces: []string{est1.ReconcileKey, est2.ReconcileKey}}
	rep2.normalize()

	s := Aggregate([]Event{est1, est2, rep1, rep2})
	if len(s.Sessions) != 1 {
		t.Fatalf("got %d sessions, want 1 combined row", len(s.Sessions))
	}
	if s.Sessions[0].Input != 8964 || s.Sessions[0].Output != 29 {
		t.Fatalf("got %d/%d, want 8964/29 (richest sweep, not summed)", s.Sessions[0].Input, s.Sessions[0].Output)
	}
	if s.Input != 8964 {
		t.Fatalf("grand input = %d, want 8964 (sweeps must not double-count)", s.Input)
	}
}

// When a later sweep genuinely observes more usage (e.g. a second turn added to
// the same session), the richest sweep wins — not the stale first one and not
// the sum.
func TestAggregateReconcileKeepsRichestSweep(t *testing.T) {
	cwd := "/repo/.council/workspaces/codex-planner"
	est := Event{RunID: "r", Agent: "codex-planner", Phase: "session", Tool: "codex", CWD: cwd, Model: UnknownValue, PromptHash: "p1", Confidence: Estimated, InputTokens: 52}
	est.normalize()
	rep1 := Event{RunID: "r", Agent: "codex-planner", Phase: "session", Tool: "codex", CWD: cwd, Model: "gpt-5.4-mini", Source: SourceProvider, Confidence: Reported, InputTokens: 5388, OutputTokens: 32, Replaces: []string{est.ReconcileKey}}
	rep1.normalize()
	rep2 := Event{RunID: "r", Agent: "codex-planner", Phase: "session", Tool: "codex", CWD: cwd, Model: "gpt-5.4-mini", Source: SourceProvider, Confidence: Reported, InputTokens: 14412, OutputTokens: 68, Replaces: []string{est.ReconcileKey}}
	rep2.normalize()

	s := Aggregate([]Event{est, rep1, rep2})
	if len(s.Sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(s.Sessions))
	}
	if s.Sessions[0].Input != 14412 || s.Sessions[0].Output != 68 {
		t.Fatalf("got %d/%d, want 14412/68 (richest sweep)", s.Sessions[0].Input, s.Sessions[0].Output)
	}
}

// Supersession is per-(agent,phase,tool,model,cwd): repeated sweeps of the same
// model collapse to the richest, but a second MODEL in the same cwd is a distinct
// group and must be kept (additive), not collapsed. Guards the invariant that the
// key needs no session id because reconcile emits one cumulative event per model
// per sweep.
func TestAggregateSupersedesPerModelKeepsDistinctModels(t *testing.T) {
	cwd := "/repo/.council/workspaces/codex-builder"
	mk := func(model string, in, out int, replaces ...string) Event {
		e := Event{RunID: "r", Agent: "codex-builder", Phase: "session", Tool: "codex", CWD: cwd, Model: model, Source: SourceProvider, Confidence: Reported, InputTokens: in, OutputTokens: out, Replaces: replaces}
		e.normalize()
		return e
	}
	s := Aggregate([]Event{
		mk("gpt-5.4-mini", 5000, 20),  // sweep 1, model A
		mk("gpt-5.4-mini", 12000, 55), // sweep 2, model A (cumulative) — supersedes
		mk("gpt-5.4", 800, 10),        // model B in the same cwd — additive
	})
	if len(s.Sessions) != 2 {
		t.Fatalf("got %d sessions, want 2 (one per model)", len(s.Sessions))
	}
	byModel := map[string]SessionTotal{}
	for _, ses := range s.Sessions {
		byModel[ses.Model] = ses
	}
	if a := byModel["gpt-5.4-mini"]; a.Input != 12000 || a.Output != 55 {
		t.Fatalf("model A = %d/%d, want 12000/55 (richest sweep, not summed)", a.Input, a.Output)
	}
	if b := byModel["gpt-5.4"]; b.Input != 800 {
		t.Fatalf("model B = %d, want 800 (distinct model kept, not collapsed)", b.Input)
	}
	if s.Input != 12800 {
		t.Fatalf("grand input = %d, want 12800 (12000 + 800)", s.Input)
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
