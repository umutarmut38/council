package orchestrate

import (
	"strings"
	"testing"
)

func TestStatusReportIncludesVoteBreakdownAndBallots(t *testing.T) {
	root := t.TempDir()
	run, err := NewRun(root)
	if err != nil {
		t.Fatal(err)
	}
	refs := []PlanRef{{Letter: "A", Agent: "alice"}, {Letter: "B", Agent: "bob"}}
	res := Result{
		WinnerAgent:  "bob",
		WinnerLetter: "B",
		Points:       map[string]int{"A": 1, "B": 2},
		Firsts:       map[string]int{"A": 0, "B": 1},
		Ballots:      []Ballot{{Voter: "carol", Ranking: []string{"B", "A"}}},
	}
	if err := run.WriteResult(res, refs); err != nil {
		t.Fatal(err)
	}
	summary, err := SummarizeRun(run.RootDir, run.Stamp)
	if err != nil {
		t.Fatal(err)
	}

	out := StatusReport(run, summary)
	for _, want := range []string{"Winner: **bob**", "(Plan B)", "carol: B > A"} {
		if !strings.Contains(out, want) {
			t.Errorf("StatusReport missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestStatusReportEmptyBeforeAnyOutcome(t *testing.T) {
	root := t.TempDir()
	run, err := NewRun(root)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := SummarizeRun(run.RootDir, run.Stamp)
	if err != nil {
		t.Fatal(err)
	}
	if out := StatusReport(run, summary); out != "" {
		t.Errorf("expected empty StatusReport before any outcome, got:\n%s", out)
	}
}
