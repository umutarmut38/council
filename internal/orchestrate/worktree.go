package orchestrate

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/umutarmut38/council/internal/cmdrun"
)

// Worktree is one agent's isolated checkout for a run.
type Worktree struct {
	Agent  string
	Path   string
	Branch string
}

// Manager creates and tears down per-agent git worktrees under
// <repo>/.council/worktrees/<stamp>, each on its own council/<agent>/<stamp>
// branch. Scoping the path by run stamp guarantees a new run can never build
// inside a stale worktree left behind by an earlier run.
type Manager struct {
	RepoRoot string
	Stamp    string
}

// DetectRepoRoot returns the top-level git directory containing dir.
func DetectRepoRoot(dir string) (string, error) {
	out, err := cmdrun.Output(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"-C", dir, "rev-parse", "--show-toplevel"}})
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	return normalizeGitPath(strings.TrimSpace(string(out))), nil
}

// normalizeGitPath converts a path emitted by git (which always uses forward
// slashes, even on Windows) into the host's native form so it compares equal to
// paths built with filepath. On Unix this is a no-op.
func normalizeGitPath(p string) string {
	if p == "" {
		return p
	}
	return filepath.Clean(filepath.FromSlash(p))
}

func NewManager(repoRoot, stamp string) *Manager {
	return &Manager{RepoRoot: repoRoot, Stamp: stamp}
}

func (m *Manager) branch(agent string) string {
	return fmt.Sprintf("council/%s/%s", agent, m.Stamp)
}

func (m *Manager) worktreesRoot() string {
	return filepath.Join(m.RepoRoot, ".council", "worktrees")
}

func (m *Manager) pathFor(agent string) string {
	if m.Stamp == "" {
		return filepath.Join(m.worktreesRoot(), agent)
	}
	return filepath.Join(m.worktreesRoot(), m.Stamp, agent)
}

// Add creates a worktree for the agent on a fresh branch from baseRef (HEAD if
// empty). An existing worktree at the run's path is reused only when it is
// checked out on this run's branch; anything else fails with a pointer to
// /clean rather than silently building in a stale checkout.
func (m *Manager) Add(agent, baseRef string) (Worktree, error) {
	wt := Worktree{Agent: agent, Path: m.pathFor(agent), Branch: m.branch(agent)}

	_, _ = cmdrun.Run(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"-C", m.RepoRoot, "worktree", "prune"}})

	existing, err := m.List()
	if err != nil {
		return wt, err
	}
	for _, e := range existing {
		if e.Path == wt.Path {
			if _, statErr := os.Stat(e.Path); statErr != nil {
				break // pruned/deleted on disk; recreate below
			}
			if e.Branch != wt.Branch {
				return wt, fmt.Errorf("worktree %s is on branch %q, expected %q; run /clean (or `council clean`) and retry", e.Path, e.Branch, wt.Branch)
			}
			return e, nil
		}
	}

	args := []string{"-C", m.RepoRoot, "worktree", "add"}
	if branchExists(m.RepoRoot, wt.Branch) {
		args = append(args, wt.Path, wt.Branch)
	} else {
		args = append(args, "-b", wt.Branch, wt.Path)
		if baseRef != "" {
			args = append(args, baseRef)
		}
	}
	if _, err := cmdrun.CombinedOutput(context.Background(), cmdrun.Spec{Name: "git", Args: args}); err != nil {
		return wt, fmt.Errorf("git worktree add %s: %w", agent, err)
	}
	return wt, nil
}

func branchExists(repoRoot string, branch string) bool {
	_, err := cmdrun.Run(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"-C", repoRoot, "rev-parse", "--verify", "--quiet", "refs/heads/" + branch}})
	return err == nil
}

// Reset discards all changes in an agent's worktree, returning it to a pristine
// checkout (used before the build phase so planning side effects don't linger).
func (m *Manager) Reset(agent string) error {
	path := m.pathFor(agent)
	for _, args := range [][]string{
		{"-C", path, "reset", "--hard"},
		{"-C", path, "clean", "-fd"},
	} {
		if _, err := cmdrun.CombinedOutput(context.Background(), cmdrun.Spec{Name: "git", Args: args}); err != nil {
			return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
	}
	return nil
}

// Remove deletes an agent's worktree and its branch.
func (m *Manager) Remove(wt Worktree) error {
	if _, err := cmdrun.CombinedOutput(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"-C", m.RepoRoot, "worktree", "remove", "--force", wt.Path}}); err != nil {
		return fmt.Errorf("git worktree remove %s: %w", wt.Agent, err)
	}
	if wt.Branch != "" {
		// Best effort: the branch may already be gone or checked out elsewhere.
		_, _ = cmdrun.Run(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"-C", m.RepoRoot, "branch", "-D", wt.Branch}})
	}
	m.removeEmptyStampDirs()
	return nil
}

// List returns every council-managed worktree (those under .council/worktrees),
// across all runs and including pre-stamp legacy paths. Use ListRun for the
// worktrees that belong to this manager's run.
func (m *Manager) List() ([]Worktree, error) {
	out, err := cmdrun.Output(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"-C", m.RepoRoot, "worktree", "list", "--porcelain"}})
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}

	prefix := m.worktreesRoot() + string(filepath.Separator)
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
	return result, nil
}

// ListRun returns only the worktrees that belong to this manager's run stamp,
// so a review never picks up implementations left behind by an older run.
func (m *Manager) ListRun() ([]Worktree, error) {
	all, err := m.List()
	if err != nil {
		return nil, err
	}
	if m.Stamp == "" {
		return all, nil
	}
	runPrefix := filepath.Join(m.worktreesRoot(), m.Stamp) + string(filepath.Separator)
	branchSuffix := "/" + m.Stamp
	out := make([]Worktree, 0, len(all))
	for _, wt := range all {
		// Legacy worktrees (pre-stamp layout) still count when their branch
		// carries this run's stamp.
		if strings.HasPrefix(wt.Path, runPrefix) || strings.HasSuffix(wt.Branch, branchSuffix) {
			out = append(out, wt)
		}
	}
	return out, nil
}

// RemoveAll tears down every council-managed worktree for cleanup.
func (m *Manager) RemoveAll() ([]string, error) {
	worktrees, err := m.List()
	if err != nil {
		return nil, err
	}
	removed := make([]string, 0, len(worktrees))
	for _, wt := range worktrees {
		if err := m.Remove(wt); err != nil {
			return removed, err
		}
		removed = append(removed, wt.Agent)
	}
	m.removeEmptyStampDirs()
	return removed, nil
}

// removeEmptyStampDirs prunes empty per-run directories left under
// .council/worktrees after their agent worktrees are removed.
func (m *Manager) removeEmptyStampDirs() {
	entries, err := os.ReadDir(m.worktreesRoot())
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			// os.Remove only deletes empty directories, which is exactly what
			// we want here.
			_ = os.Remove(filepath.Join(m.worktreesRoot(), e.Name()))
		}
	}
}
