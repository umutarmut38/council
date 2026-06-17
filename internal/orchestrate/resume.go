package orchestrate

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
	runstore "github.com/umutarmut38/council/internal/session"
)

// ResumeTarget describes how the TUI should reopen a run. Empty Phase means the
// run is not mid-phase; resume should just reopen normal agent sessions.
type ResumeTarget struct {
	Phase        config.Phase
	Participants []string
	Prompts      map[string]string
	SendPrompts  bool
	PendingBuild bool
	Status       string
}

func (c *Controller) ResumeSessions(store *runstore.Store) []*agent.Session {
	sessions := make([]*agent.Session, 0, len(c.specs))
	for _, spec := range c.specs {
		ac := spec.Config
		ac.CWD = c.workDir
		sessions = append(sessions, agent.NewSession(spec.Name, ac, store.RawLogPath(spec.Name)))
	}
	return sessions
}

// ResumeTarget returns the best phase to reopen. It prefers the explicit
// in-progress phase saved by the TUI; older runs without state are inferred from
// artifacts on disk.
func (c *Controller) ResumeTarget() (ResumeTarget, error) {
	if c.run == nil {
		return ResumeTarget{}, errors.New("no active run")
	}
	if state, err := c.run.LoadState(); err == nil && state.Phase != "" {
		return c.resumePhaseTarget(config.Phase(state.Phase), state.Participants, state.PromptSent)
	}
	c.SetScope(nil)
	return c.inferResumeTarget()
}

func (c *Controller) inferResumeTarget() (ResumeTarget, error) {
	plans, _, err := CollectRunArtifacts(c.AgentsForPhase(config.PhasePlan), c.run.PlanPath)
	if err != nil {
		return ResumeTarget{}, err
	}
	if len(plans) == 0 {
		return c.resumePhaseTarget(config.PhasePlan, nil, true)
	}
	if !fileExists(c.run.ResultPath()) {
		return c.resumePhaseTarget(config.PhaseVote, nil, true)
	}
	if !fileExists(c.run.BuildResultPath()) {
		if fileExists(filepath.Join(c.run.BuildsDir(), "base.txt")) {
			return c.resumePhaseTarget(config.PhaseBuild, nil, false)
		}
	}
	return ResumeTarget{Status: "resumed run " + c.run.Stamp}, nil
}

func (c *Controller) resumePhaseTarget(phase config.Phase, participants []string, promptSent bool) (ResumeTarget, error) {
	c.SetScope(participants)
	switch phase {
	case config.PhasePlan:
		// A refine round looks like a plan phase, but the winner's plan file
		// was moved to .orig.md so the rewrite can be watched. Resume it with
		// the refine prompt, not a from-scratch plan prompt.
		if refinePrompts, ok := c.resumeRefinePrompts(participants); ok {
			return ResumeTarget{
				Phase:        phase,
				Participants: c.AgentsForPhase(phase),
				Prompts:      refinePrompts,
				SendPrompts:  true,
				Status:       resumeStatus(c.run.Stamp, "refining", refinePrompts),
			}, nil
		}
		prompts, err := c.PlanPrompts()
		if err != nil {
			return ResumeTarget{}, err
		}
		prompts = c.filterPromptsForMissing(config.PhasePlan, prompts)
		return ResumeTarget{
			Phase:        phase,
			Participants: c.AgentsForPhase(phase),
			Prompts:      prompts,
			SendPrompts:  true,
			Status:       resumeStatus(c.run.Stamp, "planning", prompts),
		}, nil
	case config.PhaseVote:
		prompts, err := c.VotePrompts()
		if err != nil {
			return ResumeTarget{}, err
		}
		prompts = c.filterPromptsForMissing(config.PhaseVote, prompts)
		return ResumeTarget{
			Phase:        phase,
			Participants: c.AgentsForPhase(phase),
			Prompts:      prompts,
			SendPrompts:  true,
			Status:       resumeStatus(c.run.Stamp, "voting", prompts),
		}, nil
	case config.PhaseBuild:
		prompt, err := c.ResumeBuildPrompt()
		if err != nil {
			return ResumeTarget{}, err
		}
		prompts := map[string]string{}
		for _, name := range c.AgentsForPhase(config.PhaseBuild) {
			prompts[name] = prompt
		}
		return ResumeTarget{
			Phase:        phase,
			Participants: c.AgentsForPhase(phase),
			Prompts:      prompts,
			SendPrompts:  promptSent,
			PendingBuild: !promptSent,
			Status:       buildResumeStatus(c.run.Stamp, promptSent),
		}, nil
	case config.PhaseReview:
		prompts, survivors, err := c.ReviewPrompts()
		if err != nil {
			return ResumeTarget{}, err
		}
		if len(survivors) < 2 {
			return ResumeTarget{Status: fmt.Sprintf("resumed run %s — review has %d survivor(s)", c.run.Stamp, len(survivors))}, nil
		}
		prompts = c.filterPromptsForMissing(config.PhaseReview, prompts)
		return ResumeTarget{
			Phase:        phase,
			Participants: c.AgentsForPhase(phase),
			Prompts:      prompts,
			SendPrompts:  true,
			Status:       resumeStatus(c.run.Stamp, "reviewing", prompts),
		}, nil
	default:
		return ResumeTarget{Status: "resumed run " + c.run.Stamp}, nil
	}
}

// resumeRefinePrompts detects an interrupted /refine: exactly one participant
// whose plan was backed up to .orig.md and whose live plan file is missing.
func (c *Controller) resumeRefinePrompts(participants []string) (map[string]string, bool) {
	if len(participants) != 1 {
		return nil, false
	}
	agentName := participants[0]
	planPath := c.run.PlanPath(agentName)
	origPath := strings.TrimSuffix(planPath, ".md") + ".orig.md"
	if fileExists(planPath) || !fileExists(origPath) {
		return nil, false
	}
	prompts, err := c.RefinePrompts()
	if err != nil {
		return nil, false
	}
	return prompts, true
}

func (c *Controller) filterPromptsForMissing(phase config.Phase, prompts map[string]string) map[string]string {
	paths := c.ArtifactPaths(phase)
	if len(paths) == 0 {
		return prompts
	}
	filtered := map[string]string{}
	for name, prompt := range prompts {
		if path := paths[name]; path != "" && fileExists(path) {
			continue
		}
		filtered[name] = prompt
	}
	return filtered
}

func resumeStatus(stamp, verb string, prompts map[string]string) string {
	if len(prompts) == 0 {
		return "resumed run " + stamp + " — collecting completed " + verb + " phase"
	}
	return fmt.Sprintf("resumed run %s — %s with %d agent(s)", stamp, verb, len(prompts))
}

func buildResumeStatus(stamp string, promptSent bool) string {
	if promptSent {
		return "resumed run " + stamp + " — continuing build"
	}
	return "resumed run " + stamp + " — build staged; run /start-build"
}
