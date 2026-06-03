package orchestrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/umutarmut38/council/internal/config"
)

// BuildCheck is the outcome of gating one agent's build implementation.
type BuildCheck struct {
	Agent   string
	Changed bool // produced a non-empty diff
	Passed  bool // check command exited 0 (true if no check command configured)
}

func revParse(repoRoot, ref string) (string, error) {
	out, err := exec.Command("git", "-C", repoRoot, "rev-parse", ref).Output()
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
	if err := os.MkdirAll(c.run.BuildsDir(), 0o755); err != nil {
		return nil, err
	}

	// Candidates are every implementation that was actually built (every council
	// worktree), independent of the current scope — the scope only picks the
	// reviewers.
	worktrees, err := c.manager.List()
	if err != nil {
		return nil, err
	}

	var results []BuildCheck
	for _, wt := range worktrees {
		c.worktrees[wt.Agent] = wt.Path
		res := BuildCheck{Agent: wt.Agent}

		// Stage everything first (respecting .gitignore) so newly-created files
		// are included — a plain `git diff` omits untracked files, which would
		// hide an implementation that builds a project from scratch.
		_ = exec.Command("git", "-C", wt.Path, "add", "-A").Run()
		diff, derr := exec.Command("git", "-C", wt.Path, "diff", "--cached", base).Output()
		if derr == nil && len(strings.TrimSpace(string(diff))) > 0 {
			res.Changed = true
			_ = os.WriteFile(c.run.BuildDiffPath(wt.Agent), diff, 0o644)
		}

		res.Passed = c.runCheck(wt.Path, wt.Agent)
		results = append(results, res)
	}
	return results, nil
}

// runCheck runs the configured check command in dir, logging output. Returns
// true if it passes (or if no check command is configured).
func (c *Controller) runCheck(dir, agent string) bool {
	cmd := c.cfg.Review.CheckCommand
	if len(cmd) == 0 {
		return true
	}
	out, err := runInDir(dir, cmd)
	header := fmt.Sprintf("$ %s\n\n", strings.Join(cmd, " "))
	status := "PASS"
	if err != nil {
		status = "FAIL: " + err.Error()
	}
	_ = os.WriteFile(c.run.CheckLogPath(agent), []byte(header+out+"\n"+status+"\n"), 0o644)
	return err == nil
}

func runInDir(dir string, args []string) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
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
			if werr := os.WriteFile(dest, data, 0o644); werr != nil {
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
	if err := os.MkdirAll(c.run.BuildsDir(), 0o755); err != nil {
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
	return os.WriteFile(c.run.BuildResultPath(), data, 0o644)
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

// Adopt applies the winning implementation's diff onto the repo's working tree
// as uncommitted changes for the user to review and commit.
// AdoptableBuilds lists the agents that produced a non-empty build diff (i.e.
// the candidates /adopt can apply).
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
		if fi, statErr := os.Stat(filepath.Join(c.run.BuildsDir(), name)); statErr == nil && fi.Size() > 0 {
			out = append(out, strings.TrimSuffix(name, ".diff"))
		}
	}
	sort.Strings(out)
	return out
}

// Adopt applies a build's diff onto the repo's working tree as uncommitted
// changes. With override == "" it adopts the reviewed winner; otherwise it
// adopts the named agent's build (overriding the recommendation).
func (c *Controller) Adopt(override string) (adopted string, err error) {
	adopted = strings.TrimSpace(override)
	if adopted == "" {
		adopted, err = c.BuildWinner()
		if err != nil {
			return "", err
		}
	}
	diffPath := c.run.BuildDiffPath(adopted)
	if fi, statErr := os.Stat(diffPath); statErr != nil || fi.Size() == 0 {
		avail := c.AdoptableBuilds()
		if len(avail) == 0 {
			return "", fmt.Errorf("no build diff for %q; run /review first", adopted)
		}
		return "", fmt.Errorf("no build diff for %q; available: %s", adopted, strings.Join(avail, ", "))
	}
	if out, applyErr := exec.Command("git", "-C", c.repoRoot, "apply", "--3way", diffPath).CombinedOutput(); applyErr != nil {
		return "", fmt.Errorf("git apply %s: %v: %s", adopted, applyErr, strings.TrimSpace(string(out)))
	}
	return adopted, nil
}
