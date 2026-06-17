package orchestrate

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestVotePromptReferencesFilesNotInlinedPlans(t *testing.T) {
	refs := []PlanRef{{Letter: "A", Agent: "x"}, {Letter: "B", Agent: "y"}}
	paths := map[string]string{"A": "/run/votes/plan-a.md", "B": "/run/votes/plan-b.md"}

	p := VotePrompt("fix the bug", refs, paths, "/run/votes/x.md")

	if !strings.Contains(p, "plan-a.md") || !strings.Contains(p, "plan-b.md") {
		t.Fatalf("vote prompt should reference plan files by path: %q", p)
	}
	if strings.Contains(p, "=====") {
		t.Fatalf("vote prompt should not inline plan bodies: %q", p)
	}
	// Must stay small so it posts reliably instead of becoming a paste chip.
	if len(p) > 2000 {
		t.Fatalf("vote prompt unexpectedly large (%d bytes)", len(p))
	}
}

func TestAnonymizePlansAssignsLetters(t *testing.T) {
	refs := AnonymizePlans([]string{"claude", "codex", "cursor"}, func(a string) string { return a + ".md" })
	if len(refs) != 3 || refs[0].Letter != "A" || refs[2].Letter != "C" {
		t.Fatalf("refs = %+v", refs)
	}
	if refs[1].Agent != "codex" || refs[1].Path != "codex.md" {
		t.Fatalf("ref[1] = %+v", refs[1])
	}
}

func TestShufflePlanNamesReturnsCopiedPermutation(t *testing.T) {
	planned := []string{"claude", "codex", "cursor", "copilot"}
	original := append([]string(nil), planned...)

	shuffled := shufflePlanNames(planned)
	if !reflect.DeepEqual(planned, original) {
		t.Fatalf("shufflePlanNames mutated input: got %v, want %v", planned, original)
	}
	if len(shuffled) != len(original) {
		t.Fatalf("shuffled len = %d, want %d", len(shuffled), len(original))
	}
	if len(shuffled) > 0 {
		beforeAliasCheck := append([]string(nil), shuffled...)
		shuffled[0] = "modified"
		if planned[0] == "modified" {
			t.Fatal("shufflePlanNames returned a slice aliasing the input")
		}
		shuffled = beforeAliasCheck
	}

	sort.Strings(shuffled)
	sort.Strings(original)
	if !reflect.DeepEqual(shuffled, original) {
		t.Fatalf("shuffled names = %v, want permutation of %v", shuffled, original)
	}
}

func TestParseBallotRankingAndWinner(t *testing.T) {
	valid := []string{"A", "B", "C"}

	b := ParseBallot("claude", "I think\nRANKING: B > A > C\nbecause reasons", valid)
	if got := b.Ranking; len(got) != 3 || got[0] != "B" || got[2] != "C" {
		t.Fatalf("ranking = %v", got)
	}
	if b.Winner != "B" {
		t.Fatalf("winner inferred from ranking = %q", b.Winner)
	}

	// Winner line only, no ranking.
	b2 := ParseBallot("codex", "lots of text\nWINNER: A", valid)
	if b2.Winner != "A" || len(b2.Ranking) != 1 || b2.Ranking[0] != "A" {
		t.Fatalf("ballot2 = %+v", b2)
	}

	// Out-of-range letters are ignored.
	b3 := ParseBallot("x", "WINNER: Z", valid)
	if b3.Winner != "" {
		t.Fatalf("invalid winner should be dropped, got %q", b3.Winner)
	}
}

func TestTallyBordaCountPicksWinner(t *testing.T) {
	refs := []PlanRef{{Letter: "A", Agent: "claude"}, {Letter: "B", Agent: "codex"}, {Letter: "C", Agent: "cursor"}}
	ballots := []Ballot{
		ParseBallot("claude", "RANKING: B > A > C", []string{"A", "B", "C"}),
		ParseBallot("codex", "RANKING: B > C > A", []string{"A", "B", "C"}),
		ParseBallot("cursor", "RANKING: A > B > C", []string{"A", "B", "C"}),
	}
	res := Tally(ballots, refs)
	// B: 2+2+1=5, A: 1+0+2=3, C: 0+1+0=1
	if res.WinnerLetter != "B" || res.WinnerAgent != "codex" {
		t.Fatalf("winner = %q/%q, points=%v", res.WinnerLetter, res.WinnerAgent, res.Points)
	}
}

func TestTallyTieBreaksDeterministically(t *testing.T) {
	refs := []PlanRef{{Letter: "A", Agent: "x"}, {Letter: "B", Agent: "y"}}
	// One vote each way -> equal points; tie-break by first-place count is also
	// equal, so it falls to letter order (A).
	ballots := []Ballot{
		ParseBallot("x", "WINNER: A", []string{"A", "B"}),
		ParseBallot("y", "WINNER: B", []string{"A", "B"}),
	}
	if res := Tally(ballots, refs); res.WinnerLetter != "A" {
		t.Fatalf("tie winner = %q, want A", res.WinnerLetter)
	}
}

func TestBuildPromptReferencesPlanFileNotInlined(t *testing.T) {
	p := BuildPrompt("fix the bug", "/run/plans/claude.md")
	if !strings.Contains(p, "/run/plans/claude.md") {
		t.Fatalf("build prompt should reference the winning plan file: %q", p)
	}
	// Must stay small so the broadcast doesn't block the PTY writes / freeze the UI.
	if len(p) > 1200 {
		t.Fatalf("build prompt unexpectedly large (%d bytes)", len(p))
	}
}
