package orchestrate

// Inspecting and comparing build worktrees: locating an agent's live
// worktree, diffing an implementation against the run base, and diffing two
// implementations against each other.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/umutarmut38/council/internal/cmdrun"
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

// BuildProgress reports how many of the run's build worktrees show activity —
// committed work (HEAD past the recorded base) or uncommitted/untracked changes
// — out of the total worktrees. It is a cheap, read-only probe (no staging, no
// writes), so the Build rail can climb during the build instead of snapping to
// N/N only once /review captures the diffs.
func (c *Controller) BuildProgress() (active, total int) {
	if c.run == nil {
		return 0, 0
	}
	base, err := c.run.BaseSHA()
	if err != nil {
		return 0, 0
	}
	// Read the cached manager only — never write c.manager from this off-thread
	// probe. UI-thread code (EnsureManager) initializes it before the first tick;
	// the local fallback covers an early/edge call without racing the field or
	// allocating per tick in the normal case.
	mgr := c.manager
	if mgr == nil {
		mgr = NewManager(c.repoRoot, c.run.Stamp)
	}
	worktrees, err := mgr.ListRun()
	if err != nil {
		return 0, 0
	}
	total = len(worktrees)
	// Bound the whole scan so a slow/hung git can't make the probe run long. The
	// per-probe gitProbe timeout still applies within this budget.
	ctx, cancel := context.WithTimeout(context.Background(), buildProbeBudget)
	defer cancel()
	for i, wt := range worktrees {
		if ctx.Err() != nil {
			// Budget spent mid-scan: treat the unprobed remainder as active
			// (inconclusive), matching how a single inconclusive probe counts.
			active += len(worktrees) - i
			break
		}
		changed, ok := worktreeProbe(ctx, wt.Path, base)
		// Be conservative about progress: an inconclusive probe (a transient git
		// error or an index.lock) counts as active, so the Build rail doesn't
		// stall at 0 while work is actually happening.
		if changed || !ok {
			active++
		}
	}
	return active, total
}

// buildProbeBudget bounds an entire BuildProgress scan so a slow git can't make
// the off-thread probe run unboundedly; the per-probe gitProbe timeout still
// applies within it.
const buildProbeBudget = 6 * time.Second

// worktreeProbe reports, read-only, whether a build worktree differs from base
// (changed) and whether the probe was conclusive (ok). A moved HEAD or a
// non-empty status means changed; ok is false when any git probe failed, so a
// caller can fall back to a full capture instead of trusting a "clean" answer.
// ctx bounds the probe so a scan-wide deadline is honored across both git calls.
func worktreeProbe(ctx context.Context, wtPath, base string) (changed, ok bool) {
	head, headOK := gitProbe(ctx, wtPath, "rev-parse", "HEAD")
	if !headOK {
		return false, false
	}
	if head != base {
		return true, true // committed work
	}
	status, statusOK := gitProbe(ctx, wtPath, "status", "--porcelain")
	if !statusOK {
		return false, false
	}
	return status != "", true
}

// gitProbe runs a short read-only git command in wtPath, capping it at 2s but no
// later than the caller's ctx deadline, so one slow probe can't starve the next
// and a scan-wide budget is honored. On error/timeout it reports failure rather
// than blocking.
func gitProbe(ctx context.Context, wtPath string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := cmdrun.Output(ctx, cmdrun.Spec{Name: "git", Args: append([]string{"-C", wtPath}, args...)})
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
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
	out, err := cmdrun.Output(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"-C", c.repoRoot, "diff", treeA, treeB}})
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
	if _, err := cmdrun.CombinedOutput(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"-C", wt, "add", "-A"}}); err != nil {
		return "", fmt.Errorf("git add -A in %s: %w", agent, err)
	}
	out, err := cmdrun.Output(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"-C", wt, "write-tree"}})
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
