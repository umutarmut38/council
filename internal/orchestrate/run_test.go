package orchestrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListRunsSummarizesArtifacts(t *testing.T) {
	root := t.TempDir()
	run, err := NewRun(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := run.SaveIssue("Fix the bug in the formatter"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(run.PlanPath("claude"), []byte("plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(run.VotePath("codex"), []byte("vote"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run.WriteResult(Result{WinnerAgent: "claude", WinnerLetter: "A", Points: map[string]int{}, Firsts: map[string]int{}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(run.BuildsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(run.ReviewPath("cursor"), []byte("review"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(run.BuildDiffPath("claude"), []byte("diff"), 0o644); err != nil {
		t.Fatal(err)
	}

	summaries, err := ListRuns(root, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries = %d, want 1", len(summaries))
	}
	s := summaries[0]
	if s.Stamp != run.Stamp || s.Winner != "claude" {
		t.Fatalf("summary identity = %+v", s)
	}
	if len(s.Plans) != 1 || s.Plans[0] != "claude" {
		t.Fatalf("plans = %v", s.Plans)
	}
	if len(s.Votes) != 1 || s.Votes[0] != "codex" {
		t.Fatalf("votes = %v", s.Votes)
	}
	if len(s.Reviews) != 1 || s.Reviews[0] != "cursor" {
		t.Fatalf("reviews = %v", s.Reviews)
	}
	if len(s.Diffs) != 1 || s.Diffs[0] != "claude" {
		t.Fatalf("diffs = %v", s.Diffs)
	}
}

func TestLoadTranscriptsCombinesPhaseLogs(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, "20260605-120000")
	planDir := filepath.Join(runDir, "transcripts", "plan")
	voteDir := filepath.Join(runDir, "transcripts", "vote")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(voteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "claude.md.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "claude.txt"), []byte("plan transcript\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(voteDir, "claude.txt"), []byte("vote transcript\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	transcripts := LoadTranscripts(runDir, []string{"claude"})
	got := transcripts["claude"]
	if got == "" {
		t.Fatal("missing claude transcript")
	}
	if want := "--- transcript: plan ---"; !strings.Contains(got, want) {
		t.Fatalf("transcript missing %q: %q", want, got)
	}
	if want := "--- transcript: vote ---"; !strings.Contains(got, want) {
		t.Fatalf("transcript missing %q: %q", want, got)
	}
}
