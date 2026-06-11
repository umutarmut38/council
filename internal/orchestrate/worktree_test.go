package orchestrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")

	root, err := DetectRepoRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestWorktreeAddResetRemove(t *testing.T) {
	root := initRepo(t)
	m := NewManager(root, "20260101-000000")

	wt, err := m.Add("claude", "")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if wt.Branch != "council/claude/20260101-000000" {
		t.Fatalf("branch = %q", wt.Branch)
	}
	if _, err := os.Stat(filepath.Join(wt.Path, "README.md")); err != nil {
		t.Fatalf("worktree not checked out: %v", err)
	}

	// Adding again reuses the existing worktree.
	again, err := m.Add("claude", "")
	if err != nil || again.Path != wt.Path {
		t.Fatalf("reuse failed: %v %q", err, again.Path)
	}

	if err := os.RemoveAll(wt.Path); err != nil {
		t.Fatal(err)
	}
	recreated, err := m.Add("claude", "")
	if err != nil {
		t.Fatalf("recreate after manual delete: %v", err)
	}
	if recreated.Path != wt.Path {
		t.Fatalf("recreated path = %q, want %q", recreated.Path, wt.Path)
	}
	if _, err := os.Stat(filepath.Join(recreated.Path, "README.md")); err != nil {
		t.Fatalf("recreated worktree not checked out: %v", err)
	}

	// List finds exactly our managed worktree.
	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Agent != "claude" {
		t.Fatalf("list = %+v", list)
	}

	// Reset discards stray changes.
	stray := filepath.Join(wt.Path, "junk.txt")
	if err := os.WriteFile(stray, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Reset("claude"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Fatalf("reset did not clean stray file")
	}

	if err := m.Remove(wt); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present after remove")
	}
}

func TestWorktreePathsAreStampScoped(t *testing.T) {
	root := initRepo(t)

	first := NewManager(root, "20260101-000000")
	wt1, err := first.Add("claude", "")
	if err != nil {
		t.Fatalf("add run1: %v", err)
	}

	second := NewManager(root, "20260102-000000")
	wt2, err := second.Add("claude", "")
	if err != nil {
		t.Fatalf("add run2: %v", err)
	}

	if wt1.Path == wt2.Path {
		t.Fatalf("two runs share a worktree path: %q", wt1.Path)
	}
	if filepath.Base(filepath.Dir(wt1.Path)) != "20260101-000000" {
		t.Fatalf("run1 path not stamp-scoped: %q", wt1.Path)
	}

	// Each manager's run view only contains its own worktree.
	runList, err := second.ListRun()
	if err != nil {
		t.Fatal(err)
	}
	if len(runList) != 1 || runList[0].Path != wt2.Path {
		t.Fatalf("ListRun = %+v, want only run2's worktree", runList)
	}

	// The global list sees both for cleanup.
	all, err := second.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("List = %+v, want both runs' worktrees", all)
	}
}

func TestWorktreeReuseRejectsBranchMismatch(t *testing.T) {
	root := initRepo(t)
	m := NewManager(root, "20260101-000000")
	wt, err := m.Add("claude", "")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	// Move the worktree onto a different branch, as a stale/confused state would.
	if out, err := exec.Command("git", "-C", wt.Path, "checkout", "-b", "council/claude/other").CombinedOutput(); err != nil {
		t.Fatalf("checkout: %v: %s", err, out)
	}

	if _, err := m.Add("claude", ""); err == nil {
		t.Fatal("expected branch-mismatch error, got nil")
	} else if !strings.Contains(err.Error(), "clean") {
		t.Fatalf("error should point at cleanup, got: %v", err)
	}
}

func TestRemoveAllCleansStampDirs(t *testing.T) {
	root := initRepo(t)
	m := NewManager(root, "20260101-000000")
	if _, err := m.Add("claude", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Add("codex", ""); err != nil {
		t.Fatal(err)
	}

	removed, err := m.RemoveAll()
	if err != nil {
		t.Fatalf("remove all: %v", err)
	}
	if len(removed) != 2 {
		t.Fatalf("removed = %v", removed)
	}
	stampDir := filepath.Join(root, ".council", "worktrees", "20260101-000000")
	if _, err := os.Stat(stampDir); !os.IsNotExist(err) {
		t.Fatalf("stamp dir still present after RemoveAll")
	}
}
