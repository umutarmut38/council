package orchestrate

import "github.com/umutarmut38/council/internal/config"

func (c *Controller) ensureWorktrees(phase config.Phase) error {
	if c.manager == nil {
		c.manager = NewManager(c.repoRoot, c.run.Stamp)
	}
	for _, s := range c.specsForPhase(phase) {
		wt, err := c.manager.Add(s.Name, c.base)
		if err != nil {
			return err
		}
		c.worktrees[s.Name] = wt.Path
	}
	return nil
}

// BuildPrompt resets each worktree to pristine and returns the build broadcast.
func (c *Controller) BuildPrompt() (string, error) {
	issue, err := c.issue()
	if err != nil {
		return "", err
	}
	winnerAgent, _, err := c.Winner()
	if err != nil {
		return "", err
	}
	if err := c.ensureWorktrees(config.PhaseBuild); err != nil {
		return "", err
	}
	for _, name := range c.AgentsForPhase(config.PhaseBuild) {
		_ = c.manager.Reset(name)
	}
	// Record where the worktrees branched from so the review can diff against it.
	if sha, shaErr := revParse(c.repoRoot, "HEAD"); shaErr == nil {
		_ = c.run.SaveBaseSHA(sha)
	}
	// Reference the winning plan by (absolute) path so the broadcast prompt stays
	// small — inlining a multi-KB plan would block the PTY writes and freeze the UI.
	return BuildPrompt(issue, c.run.PlanPath(winnerAgent)), nil
}

// ResumeBuildPrompt reopens build worktrees without resetting them. It is used
// when resuming a run that was staged or interrupted during implementation.
func (c *Controller) ResumeBuildPrompt() (string, error) {
	issue, err := c.issue()
	if err != nil {
		return "", err
	}
	winnerAgent, _, err := c.Winner()
	if err != nil {
		return "", err
	}
	if c.manager == nil {
		c.manager = NewManager(c.repoRoot, c.run.Stamp)
	}
	for _, s := range c.specsForPhase(config.PhaseBuild) {
		wt, err := c.manager.Add(s.Name, c.base)
		if err != nil {
			return "", err
		}
		c.worktrees[s.Name] = wt.Path
	}
	if _, err := c.run.BaseSHA(); err != nil {
		if sha, shaErr := revParse(c.repoRoot, "HEAD"); shaErr == nil {
			_ = c.run.SaveBaseSHA(sha)
		}
	}
	return BuildPrompt(issue, c.run.PlanPath(winnerAgent)), nil
}

// Clean removes all council worktrees and branches.
func (c *Controller) Clean() ([]string, error) {
	mgr := c.manager
	if mgr == nil {
		mgr = NewManager(c.repoRoot, "")
	}
	return mgr.RemoveAll()
}

// ListWorktrees returns every council-managed worktree (all runs), for
// previews of what Clean would remove.
func (c *Controller) ListWorktrees() ([]Worktree, error) {
	mgr := c.manager
	if mgr == nil {
		mgr = NewManager(c.repoRoot, "")
	}
	return mgr.List()
}
