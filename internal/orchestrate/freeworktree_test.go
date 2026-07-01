package orchestrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/umutarmut38/council/internal/config"
)

func fwGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func fwWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fwExists(path string) bool { _, err := os.Stat(path); return err == nil }

// A freestyle worktree is created detached (no branch) and REUSED as-is on the
// next Ensure — agent work is never reset away.
func TestFreeWorktreesCreateReuseNoReset(t *testing.T) {
	root := initRepo(t)
	f := NewFreeWorktrees(root, config.WorktreesConfig{Freestyle: true})

	p1, err := f.Ensure("claude-a")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if p1 != f.Path("claude-a") {
		t.Fatalf("path = %q, want %q", p1, f.Path("claude-a"))
	}
	if !fwExists(filepath.Join(p1, "README.md")) {
		t.Fatal("worktree not checked out")
	}
	list := f.List()
	if len(list) != 1 || list[0].Branch != "" {
		t.Fatalf("expected one detached (no-branch) freestyle worktree, got %+v", list)
	}

	// In-progress agent work must survive a re-Ensure (no auto-reset).
	scratch := filepath.Join(p1, "work.txt")
	if err := os.WriteFile(scratch, []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	p2, err := f.Ensure("claude-a")
	if err != nil || p2 != p1 {
		t.Fatalf("reuse: %v %q", err, p2)
	}
	if !fwExists(scratch) {
		t.Fatal("reuse must NOT reset the worktree (lost work.txt)")
	}
}

// seed copies the instruction allowlist even when git-ignored, but never
// node_modules/.env.
func TestFreeWorktreesSeed(t *testing.T) {
	root := initRepo(t)
	fwWrite(t, root, ".gitignore", "AGENTS.md\n.env\nnode_modules/\n")
	fwGit(t, root, "add", ".gitignore")
	fwGit(t, root, "commit", "-m", "ignore")
	fwWrite(t, root, "AGENTS.md", "local instructions") // git-ignored
	fwWrite(t, root, ".env", "SECRET=1")                // git-ignored secret
	fwWrite(t, root, "node_modules/pkg/index.js", "junk")
	fwWrite(t, root, "extra.txt", "seed me")

	f := NewFreeWorktrees(root, config.WorktreesConfig{Freestyle: true, Seed: []string{"extra.txt"}})
	p, err := f.Ensure("a")
	if err != nil {
		t.Fatal(err)
	}
	if !fwExists(filepath.Join(p, "AGENTS.md")) {
		t.Error("git-ignored AGENTS.md should be seeded via the instruction allowlist")
	}
	if !fwExists(filepath.Join(p, "extra.txt")) {
		t.Error("configured Seed file should be copied")
	}
	if fwExists(filepath.Join(p, ".env")) {
		t.Error(".env must never be seeded")
	}
	if fwExists(filepath.Join(p, "node_modules")) {
		t.Error("node_modules must never be seeded")
	}
}

// Status reports dirty after an edit and behind after repo HEAD advances;
// Refresh refuses a dirty worktree without force, resets with force, and catches
// a behind worktree up to the repo HEAD.
func TestFreeWorktreesStatusAndRefresh(t *testing.T) {
	root := initRepo(t)
	f := NewFreeWorktrees(root, config.WorktreesConfig{Freestyle: true})
	p, err := f.Ensure("a")
	if err != nil {
		t.Fatal(err)
	}
	if behind, dirty, ok := f.Status("a"); !ok || behind || dirty {
		t.Fatalf("fresh worktree: behind=%v dirty=%v ok=%v (want false/false/true)", behind, dirty, ok)
	}

	// dirty
	if err := os.WriteFile(filepath.Join(p, "README.md"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, dirty, _ := f.Status("a"); !dirty {
		t.Fatal("expected dirty after editing a tracked file")
	}
	if err := f.Refresh("a", false); err == nil {
		t.Fatal("Refresh must refuse a dirty worktree without force")
	}
	if err := f.Refresh("a", true); err != nil {
		t.Fatalf("forced Refresh: %v", err)
	}
	if _, dirty, _ := f.Status("a"); dirty {
		t.Fatal("forced Refresh should have reset the dirty worktree")
	}

	// behind: advance the repo HEAD; the worktree stays at its creation commit.
	fwWrite(t, root, "new.txt", "second commit")
	fwGit(t, root, "add", "new.txt")
	fwGit(t, root, "commit", "-m", "advance")
	if behind, _, _ := f.Status("a"); !behind {
		t.Fatal("expected behind after the repo HEAD advanced")
	}
	if err := f.Refresh("a", false); err != nil {
		t.Fatalf("Refresh (clean): %v", err)
	}
	if behind, _, _ := f.Status("a"); behind {
		t.Fatal("Refresh should have caught the worktree up to repo HEAD")
	}
	if !fwExists(filepath.Join(p, "new.txt")) {
		t.Fatal("Refresh should have pulled in the new HEAD's file")
	}
}

// Remove tears a freestyle worktree down (for /clean).
func TestFreeWorktreesRemove(t *testing.T) {
	root := initRepo(t)
	f := NewFreeWorktrees(root, config.WorktreesConfig{Freestyle: true})
	if _, err := f.Ensure("a"); err != nil {
		t.Fatal(err)
	}
	if len(f.List()) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(f.List()))
	}
	if err := f.Remove("a"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(f.List()) != 0 {
		t.Fatalf("expected 0 workspaces after remove, got %d", len(f.List()))
	}
}
