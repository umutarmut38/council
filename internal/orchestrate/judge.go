package orchestrate

import (
	"errors"
	"fmt"
	"strings"
)

// JudgePlan records a human-selected plan winner, overriding (or standing in
// for) the reviewers' vote. choice is an agent name or an assignment letter.
func (c *Controller) JudgePlan(choice string) (string, error) {
	if c.run == nil {
		return "", errors.New("no active run")
	}
	choice = strings.TrimSpace(choice)
	if choice == "" {
		return "", errors.New("usage: /judge plan <agent|letter>")
	}
	refs, _ := c.run.LoadVoteRefs()

	winnerAgent, winnerLetter := "", ""
	for _, ref := range refs {
		if strings.EqualFold(ref.Letter, choice) || ref.Agent == choice {
			winnerAgent, winnerLetter = ref.Agent, ref.Letter
			break
		}
	}
	if winnerAgent == "" {
		// No assignments yet (vote not started): accept any agent with a plan.
		if !fileExists(c.run.PlanPath(choice)) {
			return "", fmt.Errorf("no plan found for %q", choice)
		}
		winnerAgent = choice
	}
	res := Result{WinnerAgent: winnerAgent, WinnerLetter: winnerLetter}
	if err := c.run.WriteResult(res, refs); err != nil {
		return "", err
	}
	return winnerAgent, nil
}

// JudgeBuild records a human-selected build winner. choice must name an agent
// with a captured build diff.
func (c *Controller) JudgeBuild(choice string) (string, error) {
	if c.run == nil {
		return "", errors.New("no active run")
	}
	choice = strings.TrimSpace(choice)
	avail := c.AdoptableBuilds()
	for _, a := range avail {
		if a == choice {
			return choice, c.writeBuildResult(Result{WinnerAgent: choice})
		}
	}
	if len(avail) == 0 {
		return "", errors.New("no build diffs captured yet; run /review first")
	}
	return "", fmt.Errorf("no build diff for %q; available: %s", choice, strings.Join(avail, ", "))
}
