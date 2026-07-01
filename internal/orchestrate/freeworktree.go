package orchestrate

// FreeWorktrees implements the opt-in freestyle-worktree feature: each freestyle
// (non-orchestration) pane runs in its own persistent, repo-local git worktree
// at <repo>/.council/workspaces/<agent>. Unlike the run-stamped build Manager,
// these are reused across sessions and NEVER auto-reset — the only reset path is
// an explicit Refresh (guarded when dirty) or a /clean removal. A distinct cwd
// per pane gives per-pane cost attribution via the (tool, cwd) grouping in
// internal/usage/correlate.go, plus file isolation between same-tool panes.

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/umutarmut38/council/internal/cmdrun"
	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/fsperm"
)

// freeInstructionAllowlist is the curated set of agent-instruction files seeded
// into a freestyle worktree even when git-ignored, so an agent keeps its
// guidance the clean checkout would otherwise lack. Never includes
// node_modules/.env/secrets.
var freeInstructionAllowlist = []string{
	"CLAUDE.md",
	"AGENTS.md",
	"GEMINI.md",
	"QWEN.md",
	".cursorrules",
	".github/copilot-instructions.md",
	".mcp.json",
}

// FreeWorktrees manages the persistent freestyle worktrees for one repo.
type FreeWorktrees struct {
	RepoRoot string
	Cfg      config.WorktreesConfig
}

// NewFreeWorktrees builds a manager rooted at repoRoot.
func NewFreeWorktrees(repoRoot string, cfg config.WorktreesConfig) *FreeWorktrees {
	return &FreeWorktrees{RepoRoot: repoRoot, Cfg: cfg}
}

func (f *FreeWorktrees) root() string {
	return filepath.Join(f.RepoRoot, ".council", "workspaces")
}

// Path is the persistent worktree directory for an agent.
func (f *FreeWorktrees) Path(agent string) string {
	return filepath.Join(f.root(), safeName(agent))
}

// Ensure returns the agent's freestyle worktree, creating it (detached at HEAD,
// then seeded) on first use and REUSING it as-is afterward — it never resets an
// existing worktree, so an agent's in-progress work is preserved. Idempotent.
func (f *FreeWorktrees) Ensure(agent string) (string, error) {
	path := f.Path(agent)
	_, _ = cmdrun.Run(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"-C", f.RepoRoot, "worktree", "prune"}})
	if f.registered(path) {
		if _, err := os.Stat(path); err == nil {
			return path, nil // reuse as-is — NEVER reset
		}
	}
	// Detached HEAD keeps no council/<agent> branch spam in the repo.
	if out, err := cmdrun.CombinedOutput(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"-C", f.RepoRoot, "worktree", "add", "--detach", path, "HEAD"}}); err != nil {
		return "", fmt.Errorf("git worktree add --detach %s: %w (%s)", agent, err, strings.TrimSpace(string(out)))
	}
	f.seed(path)
	return path, nil
}

// seed copies the instruction-file allowlist (even when git-ignored) plus the
// configured Seed globs into a freshly created worktree. Called only on create.
func (f *FreeWorktrees) seed(path string) {
	rels := map[string]bool{}
	for _, rel := range freeInstructionAllowlist {
		if fileExists(filepath.Join(f.RepoRoot, rel)) {
			rels[rel] = true
		}
	}
	for _, pattern := range f.Cfg.Seed {
		matches, _ := filepath.Glob(filepath.Join(f.RepoRoot, pattern))
		for _, abs := range matches {
			if rel, err := filepath.Rel(f.RepoRoot, abs); err == nil {
				rels[rel] = true
			}
		}
	}
	for rel := range rels {
		_ = copyPathInto(filepath.Join(f.RepoRoot, rel), filepath.Join(path, rel))
	}
}

// Status is a read-only, bounded probe of an agent's worktree: behind = its HEAD
// differs from the repo HEAD; dirty = it has uncommitted changes. ok is false
// when the worktree is missing or a probe failed (caller shows no marker then).
func (f *FreeWorktrees) Status(agent string) (behind, dirty, ok bool) {
	path := f.Path(agent)
	if _, err := os.Stat(path); err != nil {
		return false, false, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	wtHead, ok1 := gitProbe(ctx, path, "rev-parse", "HEAD")
	repoHead, ok2 := gitProbe(ctx, f.RepoRoot, "rev-parse", "HEAD")
	if !ok1 || !ok2 {
		return false, false, false
	}
	behind = wtHead != repoHead
	status, ok3 := gitProbe(ctx, path, "status", "--porcelain")
	if !ok3 {
		return behind, false, true
	}
	return behind, strings.TrimSpace(status) != "", true
}

// Refresh resets an agent's worktree to the repo HEAD (the ONLY reset path) and
// re-seeds it. It refuses when the worktree is dirty unless force, so agent work
// is never silently discarded. A missing worktree is created instead.
func (f *FreeWorktrees) Refresh(agent string, force bool) error {
	path := f.Path(agent)
	if _, err := os.Stat(path); err != nil {
		_, err := f.Ensure(agent)
		return err
	}
	if _, dirty, ok := f.Status(agent); ok && dirty && !force {
		return fmt.Errorf("%s has uncommitted changes; run `/refresh %s force` to discard them", agent, agent)
	}
	// Reset to the REPO's current HEAD (not the worktree's own detached HEAD,
	// which is the stale commit it was created at) so /refresh actually catches
	// the worktree up to the latest repo state.
	target := "HEAD"
	if sha, err := revParse(f.RepoRoot, "HEAD"); err == nil && sha != "" {
		target = sha
	}
	for _, args := range [][]string{
		{"-C", path, "reset", "--hard", target},
		{"-C", path, "clean", "-fd", "-e", ".council-agent"},
	} {
		if out, err := cmdrun.CombinedOutput(context.Background(), cmdrun.Spec{Name: "git", Args: args}); err != nil {
			return fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	f.seed(path)
	return nil
}

// List returns every freestyle worktree under .council/workspaces (distinct from
// the run-stamped worktrees Manager owns).
func (f *FreeWorktrees) List() []Worktree {
	out, err := cmdrun.Output(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"-C", f.RepoRoot, "worktree", "list", "--porcelain"}})
	if err != nil {
		return nil
	}
	prefix := f.root() + string(filepath.Separator)
	var result []Worktree
	var cur Worktree
	flush := func() {
		if cur.Path != "" && strings.HasPrefix(cur.Path, prefix) {
			cur.Agent = filepath.Base(cur.Path)
			result = append(result, cur)
		}
		cur = Worktree{}
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.Path = normalizeGitPath(strings.TrimPrefix(line, "worktree "))
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()
	return result
}

func (f *FreeWorktrees) registered(path string) bool {
	for _, wt := range f.List() {
		if wt.Path == path {
			return true
		}
	}
	return false
}

// Remove tears down an agent's freestyle worktree.
func (f *FreeWorktrees) Remove(agent string) error {
	path := f.Path(agent)
	if out, err := cmdrun.CombinedOutput(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"-C", f.RepoRoot, "worktree", "remove", "--force", path}}); err != nil {
		return fmt.Errorf("git worktree remove %s: %w (%s)", agent, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RemoveAll tears down every freestyle worktree, for /clean.
func (f *FreeWorktrees) RemoveAll() ([]string, error) {
	var removed []string
	for _, wt := range f.List() {
		if err := f.Remove(wt.Agent); err != nil {
			return removed, err
		}
		removed = append(removed, wt.Agent)
	}
	return removed, nil
}

// ---- minimal file-copy helpers (re-added; the reverted revision deleted them) ----

// copyPathInto copies a file or (recursively) a directory from src to dst.
func copyPathInto(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFileInto(src, dst)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyPathInto(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyFileInto(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), fsperm.Dir()); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
