package orchestrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/umutarmut38/council/internal/cmdrun"
	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/fsperm"
)

// BuildCheck is the outcome of gating one agent's build implementation.
type BuildCheck struct {
	Agent   string
	Changed bool // produced a non-empty diff
	Passed  bool // check command exited 0 (true if no check command configured)
	// Warnings collects errors from best-effort steps (e.g. staging before the
	// diff capture) that were ignored but are worth surfacing in /review.
	Warnings []string
}

func revParse(repoRoot, ref string) (string, error) {
	out, err := cmdrun.Output(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"-C", repoRoot, "rev-parse", ref}})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// RunBuildChecks captures each build worktree's diff (against the recorded base)
// and runs the configured check command in it. An implementation survives if it
// changed something and its check passed.
func (c *Controller) RunBuildChecks() ([]BuildCheck, error) {
	if c.run == nil {
		return nil, errors.New("no active run")
	}
	if c.manager == nil {
		c.manager = NewManager(c.repoRoot, c.run.Stamp)
	}
	base, err := c.run.BaseSHA()
	if err != nil {
		return nil, fmt.Errorf("no recorded build base; run build first: %w", err)
	}
	if err := os.MkdirAll(c.run.BuildsDir(), fsperm.Dir()); err != nil {
		return nil, err
	}

	// Candidates are every implementation that was actually built for THIS run
	// (the run's worktrees), independent of the current scope — the scope only
	// picks the reviewers.
	worktrees, err := c.manager.ListRun()
	if err != nil {
		return nil, err
	}

	var results []BuildCheck
	for _, wt := range worktrees {
		c.worktrees[wt.Agent] = wt.Path
		res := BuildCheck{Agent: wt.Agent}
		ctx, cancel := context.WithTimeout(context.Background(), reviewCaptureTimeout)
		res.Changed, res.Warnings = c.captureBuildDiff(ctx, wt.Agent, wt.Path, base)
		cancel()
		res.Passed = c.runCheck(wt.Path, wt.Agent)
		results = append(results, res)
	}
	c.logCheckWarnings(results)
	return results, nil
}

// Git budgets for captureBuildDiff. /review gives each worktree a generous
// per-worktree budget; an on-demand /compare bounds the WHOLE scan (across every
// worktree) so the UI can never stall for long, no matter how many there are.
const (
	reviewCaptureTimeout = 30 * time.Second
	compareScanBudget    = 10 * time.Second
)

// captureBuildDiff stages an agent's build worktree and writes its diff against
// the recorded base to BuildDiffPath when non-empty. It mutates only the
// worktree index (git add -A) — exactly what /review already does — and never
// runs the check command, so it is safe to call on demand (e.g. /compare during
// the build). It always re-captures, so a later /review records a fresh diff.
// ctx bounds the git work; callers (especially /compare on the UI thread) pass a
// deadline so a stuck git can't freeze rendering.
func (c *Controller) captureBuildDiff(ctx context.Context, agent, wtPath, base string) (changed bool, warnings []string) {
	// Stage everything first (respecting .gitignore) so newly-created files are
	// included — a plain `git diff` omits untracked files, which would hide an
	// implementation that builds a project from scratch. Staging is best-effort,
	// but a failure here can hide work, so record it.
	if _, addErr := cmdrun.CombinedOutput(ctx, cmdrun.Spec{Name: "git", Args: []string{"-C", wtPath, "add", "-A"}}); addErr != nil {
		warnings = append(warnings, fmt.Sprintf("git add -A: %v", addErr))
	}
	diff, derr := cmdrun.Output(ctx, cmdrun.Spec{Name: "git", Args: []string{"-C", wtPath, "diff", "--cached", base}})
	switch {
	case derr != nil:
		warnings = append(warnings, fmt.Sprintf("git diff --cached %s: %v", base, derr))
		// A failed recapture must not silently fall back to a stale diff from an
		// earlier /compare capture — /review is meant to be authoritative.
		if rmErr := os.Remove(c.run.BuildDiffPath(agent)); rmErr != nil && !os.IsNotExist(rmErr) {
			warnings = append(warnings, fmt.Sprintf("remove stale %s diff: %v", agent, rmErr))
		}
	case len(strings.TrimSpace(string(diff))) > 0:
		// A failed write would silently hide the work, so surface it and treat
		// the build as unchanged (a diff we can't persist can't be reviewed).
		// Drop any prior diff too, so a stale capture isn't left to surface.
		if werr := os.WriteFile(c.run.BuildDiffPath(agent), diff, fsperm.File()); werr != nil {
			warnings = append(warnings, fmt.Sprintf("write %s diff: %v", agent, werr))
			_ = os.Remove(c.run.BuildDiffPath(agent))
		} else {
			changed = true
		}
	default:
		// The worktree now matches the base (e.g. an agent reverted work that an
		// earlier /compare captured). Drop any stale diff so /compare and
		// AdoptableBuilds don't keep showing changes that no longer exist.
		if rmErr := os.Remove(c.run.BuildDiffPath(agent)); rmErr != nil && !os.IsNotExist(rmErr) {
			warnings = append(warnings, fmt.Sprintf("remove stale %s diff: %v", agent, rmErr))
		}
	}
	return changed, warnings
}

// ensureBuildDiffs captures any build worktree's diff that has not been written
// yet, so /compare works during or right after the build — before /review runs
// the checks that normally write them. Best-effort: it needs a recorded base
// (saved at /build) and live worktrees, and it leaves agents that already have a
// diff untouched so a later /review (which re-captures all) stays authoritative.
func (c *Controller) ensureBuildDiffs() {
	if c.run == nil {
		return
	}
	base, err := c.run.BaseSHA()
	if err != nil {
		return
	}
	if c.manager == nil {
		c.manager = NewManager(c.repoRoot, c.run.Stamp)
	}
	worktrees, err := c.manager.ListRun()
	if err != nil {
		return
	}
	if err := os.MkdirAll(c.run.BuildsDir(), fsperm.Dir()); err != nil {
		return
	}
	// One deadline for the WHOLE scan: /compare runs on the TUI thread, so bound
	// the total git work regardless of how many worktrees need capturing.
	ctx, cancel := context.WithTimeout(context.Background(), compareScanBudget)
	defer cancel()
	var warned []BuildCheck
	for _, wt := range worktrees {
		c.worktrees[wt.Agent] = wt.Path
		if ctx.Err() != nil {
			break // budget spent; keep /compare responsive even with many worktrees
		}
		diffPath := c.run.BuildDiffPath(wt.Agent)
		hasDiff := false
		if fi, statErr := os.Stat(diffPath); statErr == nil && fi.Size() > 0 {
			hasDiff = true
		}
		// Cheap read-only probe first: it decides whether to skip, refresh, or
		// drop the heavier stage+diff so /compare doesn't stage idle worktrees.
		changed, ok := worktreeProbe(ctx, wt.Path, base)
		if ok && !changed {
			// Conclusively back at base: drop any stale diff (best-effort) so
			// /compare doesn't keep showing changes that no longer exist, then
			// skip the capture.
			if hasDiff {
				_ = os.Remove(diffPath)
			}
			continue
		}
		if !ok && hasDiff {
			continue // keep the last captured diff when the live probe is inconclusive
		}
		// Surface best-effort capture failures: without this, a failed git
		// add/diff would leave /compare reporting stale or missing changes with
		// no way to diagnose. Logged to the same warnings.log /review uses.
		if _, warnings := c.captureBuildDiff(ctx, wt.Agent, wt.Path, base); len(warnings) > 0 {
			warned = append(warned, BuildCheck{Agent: wt.Agent, Warnings: warnings})
		}
	}
	c.logCheckWarnings(warned)
}

// refreshBuildDiff re-captures one live build worktree's diff when available,
// falling back to the last captured artifact if the worktree has been cleaned.
func (c *Controller) refreshBuildDiff(agent string) {
	if c.run == nil || strings.TrimSpace(agent) == "" {
		return
	}
	base, err := c.run.BaseSHA()
	if err != nil {
		return
	}
	wtPath, ok := c.WorktreePath(agent)
	if !ok {
		return
	}
	if err := os.MkdirAll(c.run.BuildsDir(), fsperm.Dir()); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), compareScanBudget)
	defer cancel()
	if _, warnings := c.captureBuildDiff(ctx, agent, wtPath, base); len(warnings) > 0 {
		c.logCheckWarnings([]BuildCheck{{Agent: agent, Warnings: warnings}})
	}
}

// logCheckWarnings appends ignored best-effort errors to a per-run warnings
// log so they stay inspectable after the fact.
func (c *Controller) logCheckWarnings(results []BuildCheck) {
	var b strings.Builder
	for _, res := range results {
		for _, w := range res.Warnings {
			fmt.Fprintf(&b, "%s: %s\n", res.Agent, w)
		}
	}
	if b.Len() == 0 {
		return
	}
	path := filepath.Join(c.run.BuildsDir(), "warnings.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, fsperm.File())
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(b.String())
}

// runCheck runs the configured check command in dir, logging output. Returns
// true if it passes (or if no check command is configured).
func (c *Controller) runCheck(dir, agent string) bool {
	cmd := c.cfg.Review.CheckCommand
	if len(cmd) == 0 {
		return true
	}
	out, timedOut, err := runInDir(dir, cmd, c.cfg.Review.CheckTimeout(), c.cfg.Review.CheckOutputLimit())
	header := fmt.Sprintf("$ %s\n\n", strings.Join(cmd, " "))
	status := "PASS"
	switch {
	case timedOut:
		status = fmt.Sprintf("FAIL: timed out after %s (review.check_timeout_seconds)", c.cfg.Review.CheckTimeout())
		err = errors.New(status)
	case err != nil:
		status = "FAIL: " + err.Error()
	}
	_ = os.WriteFile(c.run.CheckLogPath(agent), []byte(header+out+"\n"+status+"\n"), fsperm.File())
	return err == nil
}

// runInDir runs a command with a timeout and an output cap, so a hung or
// log-spewing check command can't block review or fill the disk.
func runInDir(dir string, args []string, timeout time.Duration, maxOutput int) (out string, timedOut bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	raw, err := cmdrun.CombinedOutput(ctx, cmdrun.Spec{Name: args[0], Args: args[1:], Dir: dir, MaxOutput: maxOutput})
	// Unwrap to the underlying exec error so the per-agent check log keeps its
	// compact "FAIL: exit status N" line (the captured output is logged
	// separately).
	return string(raw), ctx.Err() == context.DeadlineExceeded, errors.Unwrap(err)
}

// Survivors returns the agents whose implementation changed something and passed.
func Survivors(results []BuildCheck) []string {
	var out []string
	for _, r := range results {
		if r.Changed && r.Passed {
			out = append(out, r.Agent)
		}
	}
	return out
}

// ReviewPrompts gates the builds and, when more than one survives, builds an
// anonymized peer-review prompt per reviewer (each excluding its own build).
// It returns the per-agent prompts and the surviving agents. With 0 or 1
// survivors there is nothing to vote on (prompts is empty).
func (c *Controller) ReviewPrompts() (prompts map[string]string, survivors []string, err error) {
	issue, err := c.issue()
	if err != nil {
		return nil, nil, err
	}
	if refs, loadErr := c.run.LoadReviewRefs(); loadErr == nil && len(refs) > 0 {
		c.reviewRefs = refs
		return c.reviewPromptsFromRefs(issue, refs)
	}

	results, err := c.RunBuildChecks()
	if err != nil {
		return nil, nil, err
	}
	survivors = Survivors(results)
	if len(survivors) < 2 {
		return map[string]string{}, survivors, nil
	}

	// Randomize letters and write anonymized diff copies for reviewers to read.
	shuffled := append([]string(nil), survivors...)
	rand.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	c.reviewRefs = AnonymizePlans(shuffled, c.run.BuildDiffPath)
	if err := c.run.SaveReviewRefs(c.reviewRefs); err != nil {
		return nil, nil, err
	}
	return c.reviewPromptsFromRefs(issue, c.reviewRefs)
}

func (c *Controller) reviewPromptsFromRefs(issue string, refs []PlanRef) (map[string]string, []string, error) {
	diffPaths := map[string]string{}
	survivors := make([]string, 0, len(refs))
	for _, ref := range refs {
		survivors = append(survivors, ref.Agent)
		dest := c.run.AnonDiffPath(ref.Letter)
		if _, statErr := os.Stat(dest); statErr != nil {
			data, rerr := os.ReadFile(c.run.BuildDiffPath(ref.Agent))
			if rerr != nil {
				return nil, nil, fmt.Errorf("read build diff %s: %w", ref.Agent, rerr)
			}
			if werr := os.WriteFile(dest, data, fsperm.File()); werr != nil {
				return nil, nil, werr
			}
		}
		diffPaths[ref.Letter] = dest
	}

	prompts := map[string]string{}
	for _, reviewer := range c.AgentsForPhase(config.PhaseReview) {
		prompts[reviewer] = ReviewPrompt(issue, excludeOwnRefs(refs, reviewer), diffPaths, c.run.ReviewPath(reviewer))
	}
	return prompts, survivors, nil
}

// CollectReviewsAndTally gathers review files, tallies, and writes the build
// result naming the winning implementation.
func (c *Controller) CollectReviewsAndTally() (Result, error) {
	if len(c.reviewRefs) == 0 {
		refs, err := c.run.LoadReviewRefs()
		if err != nil || len(refs) == 0 {
			return Result{}, errors.New("missing review assignments; rerun /review")
		}
		c.reviewRefs = refs
	}
	reviews, _, err := CollectRunArtifacts(c.AgentsForPhase(config.PhaseReview), c.run.ReviewPath)
	if err != nil {
		return Result{}, err
	}
	ballots := make([]Ballot, 0, len(reviews))
	for _, name := range c.AgentsForPhase(config.PhaseReview) {
		if text, ok := reviews[name]; ok {
			ballots = append(ballots, ParseBallot(name, text, lettersExcludingOwn(c.reviewRefs, name)))
		}
	}
	if len(ballots) == 0 {
		return Result{}, errors.New("no reviews were submitted")
	}
	res := Tally(ballots, c.reviewRefs)
	if err := c.writeBuildResult(res); err != nil {
		return res, err
	}
	return res, nil
}

// SetSingleWinner records a winner directly (used when only one build survived
// the checks, so there is nothing to vote on).
func (c *Controller) SetSingleWinner(agent string) error {
	return c.writeBuildResult(Result{WinnerAgent: agent})
}

func (c *Controller) writeBuildResult(res Result) error {
	if err := os.MkdirAll(c.run.BuildsDir(), fsperm.Dir()); err != nil {
		return err
	}
	payload := struct {
		Winner string         `json:"winner_agent"`
		Letter string         `json:"winner_letter,omitempty"`
		Points map[string]int `json:"points,omitempty"`
	}{res.WinnerAgent, res.WinnerLetter, res.Points}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.run.BuildResultPath(), data, fsperm.File())
}

// BuildWinner returns the winning agent recorded by the review.
func (c *Controller) BuildWinner() (string, error) {
	data, err := os.ReadFile(c.run.BuildResultPath())
	if err != nil {
		return "", errors.New("no build result yet; run /review first")
	}
	var payload struct {
		Winner string `json:"winner_agent"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", err
	}
	if payload.Winner == "" {
		return "", errors.New("build result has no winner")
	}
	return payload.Winner, nil
}

// anonDiffName matches the anonymized review copies (diff-a.diff, diff-b.diff,
// …) that live next to the per-agent diffs in the builds dir. They are inputs
// for reviewers, not adoptable candidates.
var anonDiffName = regexp.MustCompile(`^diff-[a-z][0-9]*$`)

// AdoptableBuilds lists the agents that produced a non-empty build diff (i.e.
// the candidates /adopt can apply). Anonymized reviewer copies are excluded.
func (c *Controller) AdoptableBuilds() []string {
	var out []string
	entries, err := os.ReadDir(c.run.BuildsDir())
	if err != nil {
		return nil
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".diff") || e.IsDir() {
			continue
		}
		agent := strings.TrimSuffix(name, ".diff")
		if anonDiffName.MatchString(agent) {
			continue
		}
		if fi, statErr := os.Stat(filepath.Join(c.run.BuildsDir(), name)); statErr == nil && fi.Size() > 0 {
			out = append(out, agent)
		}
	}
	sort.Strings(out)
	return out
}

// BuildComparison is one candidate build's row in the /compare view.
type BuildComparison struct {
	Agent        string
	Letter       string // anonymized review letter, if assigned
	Files        int    // files touched by the diff
	CheckStatus  string // PASS, FAIL, or "—" when no check ran
	Points       int    // review Borda points (0 when not reviewed)
	Winner       bool
	DiffPath     string
	CheckLogPath string
}

// CompareBuilds summarizes every candidate build from the artifacts on disk:
// diffstat, check outcome, review points, and the recommended winner.
func (c *Controller) CompareBuilds() ([]BuildComparison, error) {
	if c.run == nil {
		return nil, errors.New("no active run")
	}
	// Capture any not-yet-written diffs so /compare works during the build,
	// before /review runs the checks that normally write them.
	c.ensureBuildDiffs()
	agents := c.AdoptableBuilds()
	if len(agents) == 0 {
		return nil, errors.New("no build changes yet")
	}
	letters := map[string]string{}
	if refs, err := c.run.LoadReviewRefs(); err == nil {
		for _, ref := range refs {
			letters[ref.Agent] = ref.Letter
		}
	}
	points := map[string]int{}
	if data, err := os.ReadFile(c.run.BuildResultPath()); err == nil {
		var payload struct {
			Points map[string]int `json:"points"`
		}
		if json.Unmarshal(data, &payload) == nil {
			points = payload.Points
		}
	}
	winner, _ := c.BuildWinner()

	rows := make([]BuildComparison, 0, len(agents))
	for _, agentName := range agents {
		row := BuildComparison{
			Agent:        agentName,
			Letter:       letters[agentName],
			DiffPath:     c.run.BuildDiffPath(agentName),
			CheckLogPath: c.run.CheckLogPath(agentName),
			CheckStatus:  "—",
			Winner:       agentName == winner,
		}
		if data, err := os.ReadFile(row.DiffPath); err == nil {
			row.Files = strings.Count(string(data), "\ndiff --git ")
			if strings.HasPrefix(string(data), "diff --git ") {
				row.Files++
			}
		}
		if log, err := os.ReadFile(row.CheckLogPath); err == nil {
			row.CheckStatus = "FAIL"
			if strings.HasSuffix(strings.TrimSpace(string(log)), "PASS") {
				row.CheckStatus = "PASS"
			}
		}
		if row.Letter != "" {
			row.Points = points[row.Letter]
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Winner != rows[j].Winner {
			return rows[i].Winner
		}
		if rows[i].Points != rows[j].Points {
			return rows[i].Points > rows[j].Points
		}
		return rows[i].Agent < rows[j].Agent
	})
	return rows, nil
}

// AdoptPlan describes what /adopt would do, so the user can inspect the
// change before any file in the working tree is touched.
type AdoptPlan struct {
	Agent      string
	DiffPath   string
	Files      []string // files the diff touches
	DirtyFiles []string // uncommitted working-tree files (overlap risk)
	CheckError string   // non-empty when `git apply --check --3way` failed
}

// PlanAdopt resolves which build would be adopted and preflights it: the
// touched files, the current working-tree dirt, and a `git apply --check`
// result. It never modifies the working tree.
func (c *Controller) PlanAdopt(override string) (AdoptPlan, error) {
	agentName, err := c.resolveAdoptAgent(override)
	if err != nil {
		return AdoptPlan{}, err
	}
	c.refreshBuildDiff(agentName)
	diffPath, err := c.adoptDiffPath(agentName)
	if err != nil {
		return AdoptPlan{}, err
	}
	plan := AdoptPlan{Agent: agentName, DiffPath: diffPath}

	if out, err := cmdrun.Output(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"-C", c.repoRoot, "apply", "--numstat", diffPath}}); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				plan.Files = append(plan.Files, fields[2])
			}
		}
	}
	plan.DirtyFiles = c.DirtyFiles()
	if out, checkErr := cmdrun.CombinedOutput(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"-C", c.repoRoot, "apply", "--check", "--3way", diffPath}}); checkErr != nil {
		plan.CheckError = strings.TrimSpace(string(out))
		if plan.CheckError == "" {
			plan.CheckError = checkErr.Error()
		}
	}
	return plan, nil
}

// DirtyFiles lists uncommitted changes in the repo's working tree.
func (c *Controller) DirtyFiles() []string {
	out, err := cmdrun.Output(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"-C", c.repoRoot, "status", "--porcelain"}})
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if len(line) > 3 {
			files = append(files, strings.TrimSpace(line[3:]))
		}
	}
	return files
}

func (c *Controller) resolveAdoptAgent(override string) (string, error) {
	if c.run == nil {
		return "", errors.New("no active run")
	}
	agentName := strings.TrimSpace(override)
	if agentName == "" {
		winner, err := c.BuildWinner()
		if err != nil {
			return "", err
		}
		agentName = winner
	}
	return agentName, nil
}

func (c *Controller) adoptDiffPath(agentName string) (string, error) {
	diffPath := c.run.BuildDiffPath(agentName)
	if fi, statErr := os.Stat(diffPath); statErr != nil || fi.Size() == 0 {
		avail := c.AdoptableBuilds()
		if len(avail) == 0 {
			return "", fmt.Errorf("no build diff for %q; run /compare or /review first", agentName)
		}
		return "", fmt.Errorf("no build diff for %q; available: %s", agentName, strings.Join(avail, ", "))
	}
	return diffPath, nil
}

// Adopt applies a build's diff onto the repo's working tree as uncommitted
// changes. With override == "" it adopts the reviewed winner; otherwise it
// adopts the named agent's build (overriding the recommendation). The diff is
// preflighted with `git apply --check --3way` first so a conflicting patch
// fails cleanly instead of half-applying.
func (c *Controller) Adopt(override string) (adopted string, files []string, err error) {
	plan, err := c.PlanAdopt(override)
	if err != nil {
		return "", nil, err
	}
	if plan.CheckError != "" {
		return "", nil, fmt.Errorf("diff for %s does not apply cleanly: %s", plan.Agent, plan.CheckError)
	}
	if _, applyErr := cmdrun.CombinedOutput(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"-C", c.repoRoot, "apply", "--3way", plan.DiffPath}}); applyErr != nil {
		return "", nil, fmt.Errorf("git apply %s: %w", plan.Agent, applyErr)
	}
	_ = c.run.RecordAdoption(plan.Agent, plan.Files)
	return plan.Agent, plan.Files, nil
}
