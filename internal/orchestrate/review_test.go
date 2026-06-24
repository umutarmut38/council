package orchestrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	winner, _, err := ctrl.Adopt("")
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
	adopted, _, err := ctrl.Adopt("a") // override the winner
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

func newTestController(t *testing.T, root string, agents []string, review config.ReviewConfig) *Controller {
	t.Helper()
	agentMap := map[string]config.AgentConfig{}
	for _, name := range agents {
		agentMap[name] = config.AgentConfig{Enabled: true, Command: []string{"true"}}
	}
	cfg := config.Config{
		Agents:   agentMap,
		Sessions: config.SessionConfig{RootDir: filepath.Join(root, ".council", "runs")},
		Review:   review,
	}
	cfg.Normalize()
	ctrl, err := NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.StartRun("do it"); err != nil {
		t.Fatal(err)
	}
	return ctrl
}

func buildOneDiff(t *testing.T, ctrl *Controller, root, agent, file, content string) {
	t.Helper()
	base, err := revParse(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.run.SaveBaseSHA(base); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.ensureWorktrees(config.PhaseBuild); err != nil {
		t.Fatal(err)
	}
	wt := ctrl.worktrees[agent]
	if err := os.WriteFile(filepath.Join(wt, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wt, "add", ".")
	gitIn(t, wt, "commit", "-m", "impl")
	if _, err := ctrl.RunBuildChecks(); err != nil {
		t.Fatal(err)
	}
}

func TestPlanAdoptReportsDirtyTreeAndFiles(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)
	ctrl := newTestController(t, root, []string{"a"}, config.ReviewConfig{})
	buildOneDiff(t, ctrl, root, "a", "a.txt", "impl by a\n")

	// Dirty the working tree.
	if err := os.WriteFile(filepath.Join(root, "WIP.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := ctrl.PlanAdopt("a")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Agent != "a" || len(plan.Files) != 1 || plan.Files[0] != "a.txt" {
		t.Fatalf("plan = %+v", plan)
	}
	if len(plan.DirtyFiles) == 0 {
		t.Fatal("dirty working tree not reported")
	}
	if plan.CheckError != "" {
		t.Fatalf("clean diff flagged as failing: %s", plan.CheckError)
	}
	// PlanAdopt must not touch the tree.
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("PlanAdopt applied the diff")
	}
}

func TestAdoptRefusesConflictingDiff(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)
	ctrl := newTestController(t, root, []string{"a"}, config.ReviewConfig{})
	// The agent edits README.md...
	base, _ := revParse(root, "HEAD")
	if err := ctrl.run.SaveBaseSHA(base); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.ensureWorktrees(config.PhaseBuild); err != nil {
		t.Fatal(err)
	}
	wt := ctrl.worktrees["a"]
	if err := os.WriteFile(filepath.Join(wt, "README.md"), []byte("agent version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wt, "add", ".")
	gitIn(t, wt, "commit", "-m", "impl")
	if _, err := ctrl.RunBuildChecks(); err != nil {
		t.Fatal(err)
	}
	// ...and the user rewrites the same file incompatibly.
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("conflicting local edit\nmore\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := ctrl.Adopt("a"); err == nil {
		t.Fatal("conflicting diff should be refused")
	}
	// The conflicting local edit survives untouched.
	data, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil || string(data) != "conflicting local edit\nmore\n" {
		t.Fatalf("working tree was modified by a failed adopt: %q, %v", data, err)
	}
}

func TestRunCheckTimesOut(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)
	ctrl := newTestController(t, root, []string{"a"}, config.ReviewConfig{
		CheckCommand:        []string{"sleep", "5"},
		CheckTimeoutSeconds: 1,
	})
	buildOneDiff(t, ctrl, root, "a", "a.txt", "impl\n")

	log, err := os.ReadFile(ctrl.run.CheckLogPath("a"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "timed out") {
		t.Fatalf("check log should record the timeout, got:\n%s", log)
	}
}

func TestWriteReportSummarizesRun(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)
	ctrl := newTestController(t, root, []string{"a", "b"}, config.ReviewConfig{})
	if err := os.WriteFile(ctrl.run.PlanPath("a"), []byte("plan a"), 0o644); err != nil {
		t.Fatal(err)
	}
	refs := []PlanRef{{Letter: "A", Agent: "a"}, {Letter: "B", Agent: "b"}}
	res := Tally([]Ballot{{Voter: "b", Ranking: []string{"A"}}}, refs)
	if err := ctrl.run.WriteResult(res, refs); err != nil {
		t.Fatal(err)
	}

	path, err := WriteReport(ctrl.run)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	report := string(data)
	for _, want := range []string{"# Council run", "## Issue", "do it", "## Plan vote", "**a**"} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
}

func TestJudgePlanAndBuild(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)
	ctrl := newTestController(t, root, []string{"a", "b"}, config.ReviewConfig{})
	if err := os.WriteFile(ctrl.run.PlanPath("b"), []byte("plan b"), 0o644); err != nil {
		t.Fatal(err)
	}

	winner, err := ctrl.JudgePlan("b")
	if err != nil || winner != "b" {
		t.Fatalf("judge plan = %q, %v", winner, err)
	}
	gotWinner, _, err := ctrl.Winner()
	if err != nil || gotWinner != "b" {
		t.Fatalf("recorded winner = %q, %v", gotWinner, err)
	}

	if _, err := ctrl.JudgeBuild("b"); err == nil {
		t.Fatal("judge build without a diff should fail")
	}
	buildOneDiff(t, ctrl, root, "b", "b.txt", "impl\n")
	if _, err := ctrl.JudgeBuild("b"); err != nil {
		t.Fatalf("judge build: %v", err)
	}
	if got, err := ctrl.BuildWinner(); err != nil || got != "b" {
		t.Fatalf("build winner = %q, %v", got, err)
	}
}

func TestAdoptableBuildsExcludesAnonymizedCopies(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)
	ctrl := newTestController(t, root, []string{"a", "b"}, config.ReviewConfig{})
	if err := os.MkdirAll(ctrl.run.BuildsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// Real candidate diffs plus the anonymized copies reviewers read.
	for _, name := range []string{"a.diff", "b.diff", "diff-a.diff", "diff-b.diff"} {
		if err := os.WriteFile(filepath.Join(ctrl.run.BuildsDir(), name), []byte("diff --git x x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := ctrl.AdoptableBuilds()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("AdoptableBuilds = %v, want [a b] (anonymized diff-a/diff-b excluded)", got)
	}
}

func TestDiffBuildsComparesTwoWorktrees(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)
	ctrl := newTestController(t, root, []string{"a", "b"}, config.ReviewConfig{})
	base, _ := revParse(root, "HEAD")
	if err := ctrl.run.SaveBaseSHA(base); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.ensureWorktrees(config.PhaseBuild); err != nil {
		t.Fatal(err)
	}
	// Two implementations: a shared file with different contents plus one
	// unique file each — all uncommitted, like a real build.
	write := func(agent, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(ctrl.worktrees[agent], name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a", "app.js", "version A\n")
	write("a", "only-a.txt", "a\n")
	write("b", "app.js", "version B\n")
	write("b", "only-b.txt", "b\n")

	diff, err := ctrl.DiffBuilds("a", "b")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-version A", "+version B", "only-a.txt", "only-b.txt"} {
		if !strings.Contains(diff, want) {
			t.Fatalf("pairwise diff missing %q:\n%s", want, diff)
		}
	}
	if strings.Contains(diff, ".git") {
		t.Fatalf("pairwise diff leaked .git noise:\n%s", diff)
	}

	// The worktree lookup also works.
	if path, ok := ctrl.WorktreePath("a"); !ok || path != ctrl.worktrees["a"] {
		t.Fatalf("WorktreePath = %q (%v)", path, ok)
	}
	if _, ok := ctrl.WorktreePath("nope"); ok {
		t.Fatal("unknown agent should have no worktree")
	}
}

func TestSplitUnifiedDiff(t *testing.T) {
	diff := "diff --git a/app.js b/app.js\nindex 1..2 100644\n--- a/app.js\n+++ b/app.js\n@@ -1 +1 @@\n-old\n+new\n" +
		"diff --git a/added.txt b/added.txt\nnew file mode 100644\n--- /dev/null\n+++ b/added.txt\n@@ -0,0 +1 @@\n+hi\n" +
		"diff --git a/gone.txt b/gone.txt\ndeleted file mode 100644\n--- a/gone.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-bye\n"
	files := SplitUnifiedDiff(diff)
	if len(files) != 3 {
		t.Fatalf("files = %d, want 3", len(files))
	}
	if files[0].Path != "app.js" || files[0].Status != "M" || files[0].Added != 1 || files[0].Deleted != 1 {
		t.Fatalf("modified entry = %+v", files[0])
	}
	if files[1].Status != "A" || files[1].Added != 1 {
		t.Fatalf("added entry = %+v", files[1])
	}
	if files[2].Status != "D" || files[2].Path != "gone.txt" {
		t.Fatalf("deleted entry = %+v", files[2])
	}
	if !strings.Contains(files[0].Patch, "+new") || strings.Contains(files[0].Patch, "+hi") {
		t.Fatalf("per-file patch slicing wrong: %q", files[0].Patch)
	}
}

func TestCompareBuildsBeforeReview(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)
	ctrl := newTestController(t, root, []string{"a", "b"}, config.ReviewConfig{})
	base, _ := revParse(root, "HEAD")
	if err := ctrl.run.SaveBaseSHA(base); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.ensureWorktrees(config.PhaseBuild); err != nil {
		t.Fatal(err)
	}

	// Nothing changed yet: /compare has nothing to show (and must not claim the
	// user has to run /review first).
	if _, err := ctrl.CompareBuilds(); err == nil {
		t.Fatal("compare with no build changes should error")
	}

	// Both agents change their worktrees, but /review never runs — so no .diff
	// files exist on disk.
	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(ctrl.worktrees[name], name+".txt"), []byte("impl by "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := ctrl.CompareBuilds()
	if err != nil {
		t.Fatalf("compare before review: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	// The diffs were captured on demand, with non-empty file counts.
	for _, name := range []string{"a", "b"} {
		if fi, statErr := os.Stat(ctrl.run.BuildDiffPath(name)); statErr != nil || fi.Size() == 0 {
			t.Fatalf("on-demand diff for %q not written", name)
		}
	}
	for _, row := range rows {
		if row.Files == 0 {
			t.Fatalf("row %q has no files: %+v", row.Agent, row)
		}
	}
}

// TestCompareDropsStaleDiff: if an agent reverts its worktree back to the base
// after a /compare captured its diff, a later /compare must drop the stale diff
// instead of continuing to show changes that no longer exist.
func TestCompareDropsStaleDiff(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)
	ctrl := newTestController(t, root, []string{"a", "b"}, config.ReviewConfig{})
	base, _ := revParse(root, "HEAD")
	if err := ctrl.run.SaveBaseSHA(base); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.ensureWorktrees(config.PhaseBuild); err != nil {
		t.Fatal(err)
	}

	// Both agents change their worktrees; /compare captures both diffs.
	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(ctrl.worktrees[name], name+".txt"), []byte("impl by "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ctrl.CompareBuilds(); err != nil {
		t.Fatalf("first compare: %v", err)
	}
	if fi, statErr := os.Stat(ctrl.run.BuildDiffPath("a")); statErr != nil || fi.Size() == 0 {
		t.Fatal("a's diff should have been captured")
	}

	// "a" reverts back to the base (unstage + remove the file) while "b" keeps
	// its change. A fresh /compare must drop a's now-stale diff but keep b's.
	wtA := ctrl.worktrees["a"]
	gitIn(t, wtA, "reset", "--hard", "HEAD")
	_ = os.Remove(filepath.Join(wtA, "a.txt"))

	if _, err := ctrl.CompareBuilds(); err != nil {
		t.Fatalf("second compare: %v", err)
	}
	if _, statErr := os.Stat(ctrl.run.BuildDiffPath("a")); !os.IsNotExist(statErr) {
		t.Fatal("a's stale diff should have been dropped once its worktree returned to base")
	}
	if fi, statErr := os.Stat(ctrl.run.BuildDiffPath("b")); statErr != nil || fi.Size() == 0 {
		t.Fatal("b's diff should still be present")
	}
}

func TestBuildProgressCountsActiveWorktrees(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)
	ctrl := newTestController(t, root, []string{"a", "b"}, config.ReviewConfig{})
	base, _ := revParse(root, "HEAD")
	if err := ctrl.run.SaveBaseSHA(base); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.ensureWorktrees(config.PhaseBuild); err != nil {
		t.Fatal(err)
	}

	// Fresh worktrees: no activity.
	if active, total := ctrl.BuildProgress(); active != 0 || total != 2 {
		t.Fatalf("idle build: %d/%d, want 0/2", active, total)
	}

	// "a" makes an uncommitted change — counts as active.
	if err := os.WriteFile(filepath.Join(ctrl.worktrees["a"], "a.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if active, total := ctrl.BuildProgress(); active != 1 || total != 2 {
		t.Fatalf("one active: %d/%d, want 1/2", active, total)
	}

	// "b" commits its work (HEAD moves past base) — also counts as active.
	wt := ctrl.worktrees["b"]
	if err := os.WriteFile(filepath.Join(wt, "b.txt"), []byte("done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wt, "add", ".")
	gitIn(t, wt, "commit", "-m", "impl b")
	if active, total := ctrl.BuildProgress(); active != 2 || total != 2 {
		t.Fatalf("both active: %d/%d, want 2/2", active, total)
	}
}
