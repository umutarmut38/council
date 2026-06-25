package orchestrate

// End-to-end regression test for the whole council pipeline. It drives the
// Controller through exactly the operations the TUI issues for
//
//	/plan → /vote → /refine → /vote (revote) → /build → /compare → /review → /adopt
//
// simulating each agent by writing the artifact it would produce, then asserting
// every phase's outcome. This is the guard that the full workflow still hangs
// together — including the refine→revote loop and compare-before-review — without
// needing the TUI/PTY layer or a real agent CLI.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/umutarmut38/council/internal/config"
)

func TestFullWorkflowEndToEnd(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)

	// poc-style review gate: a build must produce the three files and persist via
	// localStorage. gamma deliberately fails it, to exercise the drop path.
	check := config.ReviewConfig{CheckCommand: []string{
		"sh", "-c",
		"test -f index.html && test -f styles.css && test -f app.js && grep -q localStorage app.js",
	}}
	agents := []string{"alpha", "beta", "gamma"}
	ctrl := newTestController(t, root, agents, check)

	// ---- PLAN ---------------------------------------------------------------
	if _, err := ctrl.PlanPrompts(); err != nil {
		t.Fatalf("PlanPrompts: %v", err)
	}
	for _, a := range agents {
		wfWrite(t, ctrl.run.PlanPath(a), "# Plan by "+a+"\n\nApproach.\n")
	}
	found, missing, err := ctrl.CollectPlans()
	if err != nil || len(found) != 3 || len(missing) != 0 {
		t.Fatalf("CollectPlans: found=%d missing=%v err=%v", len(found), missing, err)
	}

	// ---- VOTE (winner: alpha) ----------------------------------------------
	if _, err := ctrl.VotePrompts(); err != nil {
		t.Fatalf("VotePrompts: %v", err)
	}
	wfCastBallots(t, ctrl.run.VotePath, wfLetters(ctrl.refs), ctrl.AgentsForPhase(config.PhaseVote), "alpha")
	res, err := ctrl.CollectVotesAndTally()
	if err != nil {
		t.Fatalf("CollectVotesAndTally: %v", err)
	}
	if res.WinnerAgent != "alpha" {
		t.Fatalf("vote winner = %q, want alpha", res.WinnerAgent)
	}

	// ---- REFINE (every planner revises, then the council revotes) ----------
	refinePrompts, err := ctrl.RefinePrompts("tighten it")
	if err != nil {
		t.Fatalf("RefinePrompts: %v", err)
	}
	if len(refinePrompts) != 3 {
		t.Fatalf("refine should prompt every planner, got %d", len(refinePrompts))
	}
	for _, a := range agents {
		orig := strings.TrimSuffix(ctrl.run.PlanPath(a), ".md") + ".orig.md"
		if !fileExists(orig) {
			t.Fatalf("refine should back %s's plan up to %s", a, orig)
		}
		if fileExists(ctrl.run.PlanPath(a)) {
			t.Fatalf("refine should remove %s's live plan until the rewrite lands", a)
		}
		wfWrite(t, ctrl.run.PlanPath(a), "# Refined plan by "+a+"\n")
	}
	if _, _, err := ctrl.CollectPlans(); err != nil {
		t.Fatalf("CollectPlans after refine: %v", err)
	}
	ctrl.ClearRefineBackups()
	for _, a := range agents {
		if orig := strings.TrimSuffix(ctrl.run.PlanPath(a), ".md") + ".orig.md"; fileExists(orig) {
			t.Fatalf("ClearRefineBackups should remove %s", orig)
		}
	}
	if err := ctrl.ResetVote(); err != nil {
		t.Fatalf("ResetVote: %v", err)
	}

	// ---- REVOTE (the field shifts to beta) ---------------------------------
	if _, err := ctrl.VotePrompts(); err != nil {
		t.Fatalf("revote VotePrompts: %v", err)
	}
	wfCastBallots(t, ctrl.run.VotePath, wfLetters(ctrl.refs), ctrl.AgentsForPhase(config.PhaseVote), "beta")
	res, err = ctrl.CollectVotesAndTally()
	if err != nil {
		t.Fatalf("revote tally: %v", err)
	}
	if res.WinnerAgent != "beta" {
		t.Fatalf("revote winner = %q, want beta (field should shift)", res.WinnerAgent)
	}

	// ---- BUILD --------------------------------------------------------------
	base, err := revParse(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.run.SaveBaseSHA(base); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.ensureWorktrees(config.PhaseBuild); err != nil {
		t.Fatalf("ensureWorktrees: %v", err)
	}
	for _, a := range agents {
		wt := ctrl.worktrees[a]
		wfWriteApp(t, wt, a != "gamma") // gamma omits localStorage → fails the gate
		gitIn(t, wt, "add", ".")
		gitIn(t, wt, "commit", "-m", "impl "+a)
	}
	// Live build progress (off-thread probe) sees all three worktrees as active.
	if active, total := ctrl.BuildProgress(); active != 3 || total != 3 {
		t.Fatalf("BuildProgress = %d/%d, want 3/3", active, total)
	}

	// ---- COMPARE (before review: diffs captured on demand, no checks yet) ---
	rows, err := ctrl.CompareBuilds()
	if err != nil {
		t.Fatalf("CompareBuilds: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("compare rows = %d, want 3", len(rows))
	}
	for _, r := range rows {
		if r.Files == 0 {
			t.Fatalf("compare row %q has no files (diff not captured on demand): %+v", r.Agent, r)
		}
		if r.CheckStatus != "—" {
			t.Fatalf("compare before review must show no check status, got %q for %s", r.CheckStatus, r.Agent)
		}
	}

	// ---- REVIEW (gate drops gamma, then reviewers rank; winner: alpha) ------
	_, survivors, err := ctrl.ReviewPrompts()
	if err != nil {
		t.Fatalf("ReviewPrompts: %v", err)
	}
	sort.Strings(survivors)
	if len(survivors) != 2 || survivors[0] != "alpha" || survivors[1] != "beta" {
		t.Fatalf("survivors = %v, want [alpha beta] (gamma fails the check)", survivors)
	}
	wfCastBallots(t, ctrl.run.ReviewPath, wfLetters(ctrl.reviewRefs), ctrl.AgentsForPhase(config.PhaseReview), "alpha")
	bres, err := ctrl.CollectReviewsAndTally()
	if err != nil {
		t.Fatalf("CollectReviewsAndTally: %v", err)
	}
	if bres.WinnerAgent != "alpha" {
		t.Fatalf("review winner = %q, want alpha", bres.WinnerAgent)
	}

	// ---- ADOPT --------------------------------------------------------------
	adopted, files, err := ctrl.Adopt("")
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if adopted != "alpha" {
		t.Fatalf("adopted %q, want alpha", adopted)
	}
	if len(files) == 0 {
		t.Fatal("adopt reported no files applied")
	}
	if _, err := os.Stat(filepath.Join(root, "app.js")); err != nil {
		t.Fatalf("winning build was not applied to the repo: %v", err)
	}
}

// wfWrite writes a file, failing the test on error.
func wfWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// wfWriteApp writes the minimal app the review gate validates. withStorage=false
// omits the localStorage call so the check command fails for that build.
func wfWriteApp(t *testing.T, dir string, withStorage bool) {
	t.Helper()
	wfWrite(t, filepath.Join(dir, "index.html"), "<!doctype html><link rel=stylesheet href=styles.css><script src=app.js></script>")
	wfWrite(t, filepath.Join(dir, "styles.css"), "body{margin:0}")
	app := "const board=[];"
	if withStorage {
		app = "localStorage.setItem('kanban','[]');"
	}
	wfWrite(t, filepath.Join(dir, "app.js"), app)
}

// wfLetters maps each agent to its current anonymized ballot letter.
func wfLetters(refs []PlanRef) map[string]string {
	m := map[string]string{}
	for _, r := range refs {
		m[r.Agent] = r.Letter
	}
	return m
}

// wfCastBallots writes a ballot per voter that makes `winner` the top pick.
// Candidate letters come from `letters`; `voters` is the full voter set (a
// superset of the candidates — e.g. a dropped builder still reviews). A voter
// that owns the winning letter ranks some other candidate instead, mirroring the
// "can't vote for yourself" rule, so the winner still nets the most first-places.
func wfCastBallots(t *testing.T, pathFor func(string) string, letters map[string]string, voters []string, winner string) {
	t.Helper()
	winLetter := letters[winner]
	if winLetter == "" {
		t.Fatalf("winner %q has no candidate letter", winner)
	}
	for _, v := range voters {
		top := winLetter
		if letters[v] == winLetter { // this voter is the winning candidate
			top = ""
			for a, l := range letters {
				if a != winner {
					top = l
					break
				}
			}
			if top == "" {
				t.Fatal("need at least two candidates")
			}
		}
		wfWrite(t, pathFor(v), "RANKING: "+top+"\nWINNER: "+top+"\n")
	}
}
