package orchestrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"

	"github.com/umutarmut38/council/internal/config"
)

// PlanPrompts returns one prompt per planning agent. Plan/vote phases run in
// the repo root and write directly into the run directory, avoiding per-agent
// worktree trust prompts for read-only phases.
func (c *Controller) PlanPrompts() (map[string]string, error) {
	issue, err := c.issue()
	if err != nil {
		return nil, err
	}
	prompts := map[string]string{}
	for _, name := range c.AgentsForPhase(config.PhasePlan) {
		prompts[name] = PlanPrompt(issue, c.run.PlanPath(name))
	}
	return prompts, nil
}

// CollectPlans returns plans written directly into the run and reports missing
// agents. It reports every eligible planner (ignoring the active scope) so the
// produced set is independent of who later votes.
func (c *Controller) CollectPlans() (found map[string]string, missing []string, err error) {
	return CollectRunArtifacts(c.allAgentsForPhase(config.PhasePlan), c.run.PlanPath)
}

// VotePrompts builds anonymized voting prompts from the plans already
// collected into the run, and remembers the letter assignment for tallying.
func (c *Controller) VotePrompts() (map[string]string, error) {
	issue, err := c.issue()
	if err != nil {
		return nil, err
	}
	// Candidates are every plan produced (all eligible planners), independent of
	// the active scope — so a differently-scoped set of voters can judge them.
	planned := make([]string, 0, len(c.allSpecsForPhase(config.PhasePlan)))
	byAgent := map[string]string{}
	for _, name := range c.allAgentsForPhase(config.PhasePlan) {
		data, rerr := os.ReadFile(c.run.PlanPath(name))
		if rerr == nil {
			planned = append(planned, name)
			byAgent[name] = string(data)
		}
	}
	if len(planned) == 0 {
		return nil, errors.New("no plans found; run plan first")
	}

	if refs, loadErr := c.run.LoadVoteRefs(); loadErr == nil && len(refs) > 0 {
		c.refs = refs
	} else {
		// Randomize the letter assignment per run so position carries no signal
		// about who wrote which plan.
		shuffled := append([]string(nil), planned...)
		rand.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		c.refs = AnonymizePlans(shuffled, c.run.PlanPath)
		if err := c.run.SaveVoteRefs(c.refs); err != nil {
			return nil, err
		}
	}

	// Write anonymized copies the reviewers read from, so the prompt can stay
	// small (referencing files) instead of inlining tens of KB of plans.
	planPaths := map[string]string{}
	for _, ref := range c.refs {
		dest := c.run.AnonPlanPath(ref.Letter)
		if _, statErr := os.Stat(dest); statErr != nil {
			body, ok := byAgent[ref.Agent]
			if !ok {
				return nil, fmt.Errorf("missing plan for assignment %s (%s)", ref.Letter, ref.Agent)
			}
			if err := os.WriteFile(dest, []byte(body), 0o644); err != nil {
				return nil, fmt.Errorf("write anonymized plan %s: %w", ref.Letter, err)
			}
		}
		planPaths[ref.Letter] = dest
	}

	// Each voter ranks only the OTHER plans, so it can't favor (or even see) its
	// own — a structural guard against self-bias.
	prompts := map[string]string{}
	for _, voter := range c.AgentsForPhase(config.PhaseVote) {
		prompts[voter] = VotePrompt(issue, excludeOwnRefs(c.refs, voter), planPaths, c.run.VotePath(voter))
	}
	return prompts, nil
}

func excludeOwnRefs(refs []PlanRef, voter string) []PlanRef {
	out := make([]PlanRef, 0, len(refs))
	for _, r := range refs {
		if r.Agent != voter {
			out = append(out, r)
		}
	}
	return out
}

func lettersExcludingOwn(refs []PlanRef, voter string) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.Agent != voter {
			out = append(out, r.Letter)
		}
	}
	return out
}

// CollectVotesAndTally gathers VOTE.md files, tallies, and writes the result.
func (c *Controller) CollectVotesAndTally() (Result, error) {
	if len(c.refs) == 0 {
		refs, err := c.run.LoadVoteRefs()
		if err != nil || len(refs) == 0 {
			return Result{}, errors.New("missing vote plan assignments; rerun vote")
		}
		c.refs = refs
	}
	votes, _, err := CollectRunArtifacts(c.AgentsForPhase(config.PhaseVote), c.run.VotePath)
	if err != nil {
		return Result{}, err
	}
	ballots := make([]Ballot, 0, len(votes))
	for _, name := range c.AgentsForPhase(config.PhaseVote) {
		if text, ok := votes[name]; ok {
			// Only the letters this voter was shown are valid, so a stray
			// self-vote (its own plan) is dropped rather than counted.
			ballots = append(ballots, ParseBallot(name, text, lettersExcludingOwn(c.refs, name)))
		}
	}
	if len(ballots) == 0 {
		return Result{}, errors.New("no votes were cast")
	}
	res := Tally(ballots, c.refs)
	if err := c.run.WriteResult(res, c.refs); err != nil {
		return res, err
	}
	return res, nil
}

// Winner returns the winning agent and its plan text from a tallied run.
func (c *Controller) Winner() (agentName, plan string, err error) {
	agentName, err = c.winnerName()
	if err != nil {
		return "", "", err
	}
	planBytes, err := os.ReadFile(c.run.PlanPath(agentName))
	if err != nil {
		return "", "", fmt.Errorf("winning plan missing: %w", err)
	}
	return agentName, string(planBytes), nil
}

// winnerName resolves the vote winner without requiring the plan file to
// still exist (a refine round temporarily removes it).
func (c *Controller) winnerName() (string, error) {
	data, err := os.ReadFile(c.run.ResultPath())
	if err != nil {
		return "", errors.New("no result yet; run vote first")
	}
	var payload struct {
		Winner string `json:"winner_agent"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", err
	}
	if payload.Winner == "" {
		return "", errors.New("result has no winner")
	}
	return payload.Winner, nil
}
