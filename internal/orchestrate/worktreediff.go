package orchestrate

// Inspecting and comparing build worktrees: locating an agent's live
// worktree, diffing an implementation against the run base, and diffing two
// implementations against each other.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// WorktreePath returns the live worktree directory for an agent's build in
// the current run, when it still exists on disk (i.e. before /clean).
func (c *Controller) WorktreePath(agent string) (string, bool) {
	if c.run == nil {
		return "", false
	}
	mgr := c.manager
	if mgr == nil {
		mgr = NewManager(c.repoRoot, c.run.Stamp)
	}
	worktrees, err := mgr.ListRun()
	if err != nil {
		return "", false
	}
	for _, wt := range worktrees {
		if wt.Agent == agent {
			if _, statErr := os.Stat(wt.Path); statErr == nil {
				return wt.Path, true
			}
		}
	}
	return "", false
}

// DiffVsBase returns an agent's captured implementation diff (worktree
// against the recorded build base). It reads the run artifact, so it works
// even after the worktrees were cleaned.
func (c *Controller) DiffVsBase(agent string) (string, error) {
	if c.run == nil {
		return "", errors.New("no active run")
	}
	data, err := os.ReadFile(c.run.BuildDiffPath(agent))
	if err != nil {
		return "", fmt.Errorf("no captured diff for %q; run /review first", agent)
	}
	return string(data), nil
}

// DiffBuilds returns a git-native diff between two agents' implementations.
// Both worktrees share the repo's object database, so each index is written
// as a tree object and the trees are diffed — no .git noise, real rename and
// mode handling, exactly what `git diff` would say.
func (c *Controller) DiffBuilds(agentA, agentB string) (string, error) {
	treeA, err := c.worktreeTree(agentA)
	if err != nil {
		return "", err
	}
	treeB, err := c.worktreeTree(agentB)
	if err != nil {
		return "", err
	}
	out, err := exec.Command("git", "-C", c.repoRoot, "diff", treeA, treeB).Output()
	if err != nil {
		return "", fmt.Errorf("git diff %s..%s: %w", agentA, agentB, err)
	}
	return string(out), nil
}

// worktreeTree stages everything in the agent's worktree and writes its index
// as a tree object, returning the tree SHA.
func (c *Controller) worktreeTree(agent string) (string, error) {
	wt, ok := c.WorktreePath(agent)
	if !ok {
		return "", fmt.Errorf("%s's worktree is gone (cleaned?); only the captured diff vs base is available", agent)
	}
	if out, err := exec.Command("git", "-C", wt, "add", "-A").CombinedOutput(); err != nil {
		return "", fmt.Errorf("git add -A in %s: %v: %s", agent, err, strings.TrimSpace(string(out)))
	}
	out, err := exec.Command("git", "-C", wt, "write-tree").Output()
	if err != nil {
		return "", fmt.Errorf("git write-tree in %s: %w", agent, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// DiffFile is one file's entry in a unified diff.
type DiffFile struct {
	Path    string
	Status  string // M, A, D, R
	Added   int
	Deleted int
	Patch   string // this file's section of the unified diff
}

var diffHeaderPattern = regexp.MustCompile(`(?m)^diff --git a/(.*) b/(.*)$`)

// SplitUnifiedDiff breaks a unified diff into per-file entries, preserving
// order. Counts are computed from the +/- lines; status from the headers.
func SplitUnifiedDiff(diff string) []DiffFile {
	matches := diffHeaderPattern.FindAllStringSubmatchIndex(diff, -1)
	files := make([]DiffFile, 0, len(matches))
	for i, m := range matches {
		start := m[0]
		end := len(diff)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		section := diff[start:end]
		// Prefer the b/ path (post-image); fall back to a/ for deletions.
		pathA := diff[m[2]:m[3]]
		pathB := diff[m[4]:m[5]]
		file := DiffFile{Path: pathB, Status: "M", Patch: section}
		switch {
		case strings.Contains(section, "\nnew file mode "):
			file.Status = "A"
		case strings.Contains(section, "\ndeleted file mode "):
			file.Status = "D"
			file.Path = pathA
		case strings.Contains(section, "\nrename from "):
			file.Status = "R"
		}
		for _, line := range strings.Split(section, "\n") {
			switch {
			case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			case strings.HasPrefix(line, "+"):
				file.Added++
			case strings.HasPrefix(line, "-"):
				file.Deleted++
			}
		}
		files = append(files, file)
	}
	return files
}

// StatLine renders a compact git-like stat for a file entry: "M app.js +12 -3".
func (f DiffFile) StatLine() string {
	return fmt.Sprintf("%s %s  +%s -%s", f.Status, f.Path, strconv.Itoa(f.Added), strconv.Itoa(f.Deleted))
}
