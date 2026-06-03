package orchestrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/umutarmut38/council/internal/config"
)

func TestSurvivorsFiltersChangedAndPassed(t *testing.T) {
	got := Survivors([]BuildCheck{
		{Agent: "a", Changed: true, Passed: true},
		{Agent: "b", Changed: true, Passed: false}, // failed check
		{Agent: "c", Changed: false, Passed: true}, // no changes
		{Agent: "d", Changed: true, Passed: true},
	})
	if len(got) != 2 || got[0] != "a" || got[1] != "d" {
		t.Fatalf("survivors = %v, want [a d]", got)
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestReviewFlowAndAdopt(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)

	cfg := config.Config{
		Agents: map[string]config.AgentConfig{
			"a": {Enabled: true, Command: []string{"true"}},
			"b": {Enabled: true, Command: []string{"true"}},
			"c": {Enabled: true, Command: []string{"true"}},
		},
		Sessions: config.SessionConfig{RootDir: filepath.Join(root, ".council", "runs")},
		Review:   config.ReviewConfig{},
	}
	cfg.Normalize()
	ctrl, err := NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.StartRun("do it"); err != nil {
		t.Fatal(err)
	}

	base, err := revParse(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.run.SaveBaseSHA(base); err != nil {
		t.Fatal(err)
	}

	// Give each agent a worktree with a distinct committed change.
	if err := ctrl.ensureWorktrees(config.PhaseBuild); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b", "c"} {
		wt := ctrl.worktrees[name]
		if err := os.WriteFile(filepath.Join(wt, name+".txt"), []byte("impl by "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, wt, "add", ".")
		gitIn(t, wt, "commit", "-m", "impl "+name)
	}

	prompts, survivors, err := ctrl.ReviewPrompts()
	if err != nil {
		t.Fatal(err)
	}
	if len(survivors) != 3 {
		t.Fatalf("survivors = %v, want 3", survivors)
	}
	if len(prompts) != 3 {
		t.Fatalf("expected a prompt per reviewer, got %d", len(prompts))
	}

	// Everyone ranks "a" first (a ranks others, since it can't vote for itself).
	letter := map[string]string{}
	for _, r := range ctrl.reviewRefs {
		letter[r.Agent] = r.Letter
	}
	write := func(reviewer, body string) {
		if err := os.WriteFile(ctrl.run.ReviewPath(reviewer), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("b", "RANKING: "+letter["a"]+" > "+letter["c"]+"\nWINNER: "+letter["a"])
	write("c", "RANKING: "+letter["a"]+" > "+letter["b"]+"\nWINNER: "+letter["a"])
	write("a", "RANKING: "+letter["b"]+" > "+letter["c"]+"\nWINNER: "+letter["b"])

	res, err := ctrl.CollectReviewsAndTally()
	if err != nil {
		t.Fatal(err)
	}
	if res.WinnerAgent != "a" {
		t.Fatalf("winner = %q, want a", res.WinnerAgent)
	}

	// Adopt applies a's diff to the repo working tree.
	winner, err := ctrl.Adopt("")
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if winner != "a" {
		t.Fatalf("adopted %q, want a", winner)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("winning change not applied to repo: %v", err)
	}
}

func TestRunBuildChecksCapturesUntrackedFiles(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)
	cfg := config.Config{
		Agents:   map[string]config.AgentConfig{"a": {Enabled: true, Command: []string{"true"}}, "b": {Enabled: true, Command: []string{"true"}}},
		Sessions: config.SessionConfig{RootDir: filepath.Join(root, ".council", "runs")},
		Review:   config.ReviewConfig{},
	}
	cfg.Normalize()
	ctrl, err := NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.StartRun("do it"); err != nil {
		t.Fatal(err)
	}
	base, _ := revParse(root, "HEAD")
	if err := ctrl.run.SaveBaseSHA(base); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.ensureWorktrees(config.PhaseBuild); err != nil {
		t.Fatal(err)
	}
	// "a" creates a NEW (untracked) file but does not commit; "b" does nothing.
	if err := os.WriteFile(filepath.Join(ctrl.worktrees["a"], "new.txt"), []byte("brand new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := ctrl.RunBuildChecks()
	if err != nil {
		t.Fatal(err)
	}
	changed := map[string]bool{}
	for _, r := range results {
		changed[r.Agent] = r.Changed
	}
	if !changed["a"] {
		t.Fatal("untracked new file should be captured as a change")
	}
	if changed["b"] {
		t.Fatal("agent with no changes should not be a candidate")
	}

	// /adopt a (override) applies the new file to the repo working tree.
	if err := ctrl.SetSingleWinner("b"); err != nil { // pretend b "won"
		t.Fatal(err)
	}
	adopted, err := ctrl.Adopt("a") // override the winner
	if err != nil {
		t.Fatalf("adopt override: %v", err)
	}
	if adopted != "a" {
		t.Fatalf("adopted %q, want a", adopted)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); err != nil {
		t.Fatalf("override adopt did not apply a's new file: %v", err)
	}
}
