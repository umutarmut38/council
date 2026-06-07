package orchestrate

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Worktree is one agent's isolated checkout for a run.
type Worktree struct {
	Agent  string
	Path   string
	Branch string
}

// Manager creates and tears down per-agent git worktrees under
// <repo>/.council/worktrees, each on its own council/<agent>/<stamp> branch.
type Manager struct {
	RepoRoot string
	Stamp    string
}

// DetectRepoRoot returns the top-level git directory containing dir.
func DetectRepoRoot(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
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

func (m *Manager) pathFor(agent string) string {
	return filepath.Join(m.RepoRoot, ".council", "worktrees", agent)
}

// Add creates a worktree for the agent on a fresh branch from baseRef (HEAD if
// empty). If a worktree already exists at the path it is reused.
func (m *Manager) Add(agent, baseRef string) (Worktree, error) {
	wt := Worktree{Agent: agent, Path: m.pathFor(agent), Branch: m.branch(agent)}

	_ = exec.Command("git", "-C", m.RepoRoot, "worktree", "prune").Run()

	existing, err := m.List()
	if err != nil {
		return wt, err
	}
	for _, e := range existing {
		if e.Path == wt.Path {
			if _, statErr := os.Stat(e.Path); statErr == nil {
				return e, nil
			}
			break
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
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		return wt, fmt.Errorf("git worktree add %s: %v: %s", agent, err, strings.TrimSpace(string(out)))
	}
	return wt, nil
}

func branchExists(repoRoot string, branch string) bool {
	return exec.Command("git", "-C", repoRoot, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil
}

// Reset discards all changes in an agent's worktree, returning it to a pristine
// checkout (used before the build phase so planning side effects don't linger).
func (m *Manager) Reset(agent string) error {
	path := m.pathFor(agent)
	for _, args := range [][]string{
		{"-C", path, "reset", "--hard"},
		{"-C", path, "clean", "-fd"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// Remove deletes an agent's worktree and its branch.
func (m *Manager) Remove(wt Worktree) error {
	if out, err := exec.Command("git", "-C", m.RepoRoot, "worktree", "remove", "--force", wt.Path).CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove %s: %v: %s", wt.Agent, err, strings.TrimSpace(string(out)))
	}
	if wt.Branch != "" {
		// Best effort: the branch may already be gone or checked out elsewhere.
		_ = exec.Command("git", "-C", m.RepoRoot, "branch", "-D", wt.Branch).Run()
	}
	return nil
}

// List returns the council-managed worktrees (those under .council/worktrees).
func (m *Manager) List() ([]Worktree, error) {
	out, err := exec.Command("git", "-C", m.RepoRoot, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}

	prefix := filepath.Join(m.RepoRoot, ".council", "worktrees") + string(filepath.Separator)
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
	return removed, nil
}
