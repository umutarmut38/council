package orchestrate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
	"github.com/umutarmut38/council/internal/fsperm"
	runstore "github.com/umutarmut38/council/internal/session"
)

// Controller is the orchestration engine shared by the CLI subcommands and the
// in-chat phase commands. It owns the run, the per-agent worktrees, and the
// logic to build phase sessions, collect artifacts, and tally votes.
type Controller struct {
	cfg          config.Config
	repoRoot     string
	workDir      string // absolute dir council was launched from; where plan/vote/review run
	sessionsRoot string // absolute runs dir (workDir + cfg.Sessions.RootDir)
	specs        []config.AgentSpec
	base         string
	run          *Run
	manager      *Manager
	worktrees    map[string]string
	scope        map[string]bool // nil = all; else only these agents participate/judge
	refs         []PlanRef       // anonymized plan letters, set when a vote prompt is built
	reviewRefs   []PlanRef       // anonymized build letters, set when a review prompt is built
}

// NewController selects the participating agents (enabled, not globally
// orchestration.exclude) and locates the git repo. Per-phase exclusions are
// applied when sessions/artifacts are built. baseRef is the worktree base
// (HEAD if "").
func NewController(cfg config.Config, agentsOverride []string, baseRef string) (*Controller, error) {
	selected, _, err := config.SelectAgents(cfg, agentsOverride)
	if err != nil {
		return nil, err
	}
	specs := make([]config.AgentSpec, 0, len(selected))
	for _, s := range selected {
		if s.Config.Orchestration.Exclude {
			continue
		}
		specs = append(specs, s)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	if len(specs) == 0 {
		return nil, errors.New("no orchestration agents; enable agents and ensure they aren't orchestration.exclude")
	}

	cwd, _ := os.Getwd()
	workDir := cwd
	if abs, absErr := filepath.Abs(cwd); absErr == nil {
		workDir = abs
	}
	repoRoot, err := DetectRepoRoot(cwd)
	if err != nil {
		return nil, err
	}
	return &Controller{
		cfg:          cfg,
		repoRoot:     repoRoot,
		workDir:      workDir,
		sessionsRoot: resolveSessionsRoot(workDir, cfg.Sessions.RootDir),
		specs:        specs,
		base:         baseRef,
		worktrees:    map[string]string{},
	}, nil
}

// resolveSessionsRoot anchors the runs directory to the launch dir so artifact
// paths handed to agents are absolute and unambiguous regardless of each tool's
// own working directory.
func resolveSessionsRoot(workDir, rootDir string) string {
	if rootDir == "" {
		rootDir = ".council/runs"
	}
	if filepath.IsAbs(rootDir) {
		return rootDir
	}
	return filepath.Join(workDir, rootDir)
}

func (c *Controller) Agents() []string {
	names := make([]string, 0, len(c.specs))
	for _, s := range c.specs {
		names = append(names, s.Name)
	}
	return names
}

func (c *Controller) AgentsForPhase(phase config.Phase) []string {
	specs := c.specsForPhase(phase)
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		names = append(names, s.Name)
	}
	return names
}

func (c *Controller) Run() *Run { return c.run }

// StartRun begins a fresh run from the resolved issue text. Planning and voting
// run from the trusted repo root; build lazily prepares worktrees.
func (c *Controller) StartRun(issueText string) error {
	run, err := NewRun(c.sessionsRoot)
	if err != nil {
		return err
	}
	if err := run.SaveIssue(issueText); err != nil {
		return err
	}
	c.run = run
	return nil
}

// UseRun attaches to an existing run (latest if stamp is empty).
func (c *Controller) UseRun(stamp string) error {
	run, err := OpenRun(c.sessionsRoot, stamp)
	if err != nil {
		return err
	}
	c.run = run
	return nil
}

// SetScope restricts which agents participate in (and judge) subsequent phases.
// A nil/empty list clears the scope (all agents participate). Candidates for
// vote/review are read from disk and are not affected by the scope.
func (c *Controller) SetScope(names []string) {
	if len(names) == 0 {
		c.scope = nil
		return
	}
	c.scope = make(map[string]bool, len(names))
	for _, n := range names {
		c.scope[n] = true
	}
}

// SaveActivePhase persists the phase that is currently open in the TUI.
func (c *Controller) SaveActivePhase(phase config.Phase, participants []string, promptSent bool) error {
	if c.run == nil {
		return errors.New("no active run")
	}
	c.run.RecordPhaseStart(string(phase), participants)
	return c.run.SaveState(string(phase), participants, promptSent)
}

func (c *Controller) MarkPhasePromptSent(phase config.Phase) error {
	if c.run == nil {
		return nil
	}
	state, err := c.run.LoadState()
	if err != nil || string(state.Phase) != string(phase) {
		return nil
	}
	return c.run.SaveState(string(phase), state.Participants, true)
}

func (c *Controller) ClearActivePhase() error {
	if c.run == nil {
		return nil
	}
	if state, err := c.run.LoadState(); err == nil && state.Phase != "" {
		c.run.RecordPhaseEnd(string(state.Phase))
	}
	return c.run.ClearState()
}

// allSpecsForPhase returns every agent eligible for a phase, ignoring the active
// scope (used to enumerate produced artifacts / candidates).
func (c *Controller) allSpecsForPhase(phase config.Phase) []config.AgentSpec {
	specs := make([]config.AgentSpec, 0, len(c.specs))
	for _, spec := range c.specs {
		if spec.Config.ParticipatesIn(phase) {
			specs = append(specs, spec)
		}
	}
	return specs
}

func (c *Controller) allAgentsForPhase(phase config.Phase) []string {
	specs := c.allSpecsForPhase(phase)
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		names = append(names, s.Name)
	}
	return names
}

// specsForPhase returns the agents that run/judge a phase: eligible for the
// phase and within the active scope (if any).
func (c *Controller) specsForPhase(phase config.Phase) []config.AgentSpec {
	specs := make([]config.AgentSpec, 0, len(c.specs))
	for _, spec := range c.allSpecsForPhase(phase) {
		if c.scope == nil || c.scope[spec.Name] {
			specs = append(specs, spec)
		}
	}
	return specs
}

func (c *Controller) worktreesForPhase(phase config.Phase) map[string]string {
	paths := map[string]string{}
	for _, spec := range c.specsForPhase(phase) {
		if wt, ok := c.worktrees[spec.Name]; ok {
			paths[spec.Name] = wt
		}
	}
	return paths
}

func (c *Controller) issue() (string, error) {
	if c.run == nil {
		return "", errors.New("no active run")
	}
	return c.run.Issue()
}

// Store opens a per-phase log store under the current run.
func (c *Controller) Store(phase config.Phase) (*runstore.Store, error) {
	if c.run == nil {
		return nil, errors.New("no active run")
	}
	return runstore.OpenAt(c.run.Dir, string(phase))
}

// PhaseSessions builds fresh agent sessions for a phase: phase-specific command,
// plan/vote in the trusted repo root, build in isolated worktrees.
func (c *Controller) PhaseSessions(phase config.Phase, store *runstore.Store, prompts map[string]string) []*agent.Session {
	specs := c.specsForPhase(phase)
	sessions := make([]*agent.Session, 0, len(specs))
	for _, spec := range specs {
		ac := spec.Config
		ac.Command = spec.Config.CommandForPhase(phase)
		if prompt, ok := prompts[spec.Name]; ok && prompt != "" && spec.Config.PromptInCommandForPhase(phase) {
			ac.Command = append([]string(nil), ac.Command...)
			ac.Command = append(ac.Command, c.cfg.PromptForAgent(spec.Name, prompt))
		}
		// Plan/vote/review run where council was launched; build runs in the
		// agent's isolated worktree.
		ac.CWD = c.workDir
		if phase == config.PhaseBuild {
			ac.CWD = c.worktrees[spec.Name]
		}
		sessions = append(sessions, agent.NewSession(spec.Name, ac, store.RawLogPath(spec.Name)))
	}
	return sessions
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (c *Controller) InteractivePrompts(phase config.Phase, prompts map[string]string) map[string]string {
	filtered := map[string]string{}
	for _, spec := range c.specsForPhase(phase) {
		if spec.Config.PromptInCommandForPhase(phase) {
			continue
		}
		if prompt := prompts[spec.Name]; prompt != "" {
			filtered[spec.Name] = prompt
		}
	}
	return filtered
}

// ArtifactPaths maps each agent to the file council watches to know that agent
// finished a phase. Build has no artifact (the code is the result).
func (c *Controller) ArtifactPaths(phase config.Phase) map[string]string {
	switch phase {
	case config.PhasePlan, config.PhaseVote, config.PhaseReview:
	default:
		return nil
	}
	paths := map[string]string{}
	for _, agentName := range c.AgentsForPhase(phase) {
		switch phase {
		case config.PhasePlan:
			paths[agentName] = c.run.PlanPath(agentName)
		case config.PhaseVote:
			paths[agentName] = c.run.VotePath(agentName)
		case config.PhaseReview:
			paths[agentName] = c.run.ReviewPath(agentName)
		}
	}
	return paths
}

// RefinePrompts builds the consensus round: every planner that produced a plan
// reads the council's critiques and rewrites its plan before the council
// revotes. Each plan is preserved as <agent>.orig.md and the watched plan file
// is removed so the phase completes when the refined plan lands. The file
// handling is idempotent so an interrupted /refine resumes cleanly.
func (c *Controller) RefinePrompts(note string) (map[string]string, error) {
	issue, err := c.issue()
	if err != nil {
		return nil, err
	}

	// Critiques are optional: a single auto-won plan has no votes, so refine
	// proceeds on the note alone (or a generic tighten-up instruction). Ballots
	// are free-form rankings of every plan, so the whole set is shared by each
	// refiner rather than split per plan.
	votePaths := []string{}
	for _, voter := range c.allAgentsForPhase(config.PhaseVote) {
		if path := c.run.VotePath(voter); fileExists(path) {
			votePaths = append(votePaths, path)
		}
	}

	// Map each planner to the letter its plan was shown as, so the prompt can
	// point it at the critiques aimed at it. Absent for an auto-won single plan.
	letterByAgent := map[string]string{}
	if refs, err := c.run.LoadVoteRefs(); err == nil {
		for _, r := range refs {
			letterByAgent[r.Agent] = r.Letter
		}
	}

	prompts := map[string]string{}
	for _, agent := range c.allAgentsForPhase(config.PhasePlan) {
		planPath := c.run.PlanPath(agent)
		origPath := strings.TrimSuffix(planPath, ".md") + ".orig.md"
		// A planner is in the refine set if it produced a plan: it has a live
		// plan (fresh start) or an .orig.md backup (mid-refine resume).
		if !fileExists(planPath) && !fileExists(origPath) {
			continue
		}
		if !fileExists(origPath) {
			// Fresh start: back the plan up, then remove the watched artifact so
			// the phase finishes when the agent writes the refined version. On
			// resume the backup already exists, so we touch nothing.
			data, err := os.ReadFile(planPath)
			if err != nil {
				return nil, fmt.Errorf("read plan for %s: %w", agent, err)
			}
			if err := os.WriteFile(origPath, data, fsperm.File()); err != nil {
				return nil, err
			}
			_ = os.Remove(planPath)
		}
		prompts[agent] = RefinePrompt(issue, origPath, votePaths, planPath, note, letterByAgent[agent])
	}
	if len(prompts) == 0 {
		return nil, errors.New("no plans to refine")
	}
	return prompts, nil
}

// ResetVote clears the derived artifacts of a prior vote (anonymized plan
// copies, ballots, letter assignments, and the tally) so the next /vote
// re-anonymizes and re-tallies from the current plans. Used after a refine round.
func (c *Controller) ResetVote() error {
	c.refs = nil
	return c.run.ResetVote(c.allAgentsForPhase(config.PhaseVote))
}

// ClearRefineBackups removes the <agent>.orig.md plan backups created by a
// refine round, called once the round completes so a later refine round in the
// same run re-snapshots the current plan rather than the first one.
func (c *Controller) ClearRefineBackups() {
	for _, agent := range c.allAgentsForPhase(config.PhasePlan) {
		origPath := strings.TrimSuffix(c.run.PlanPath(agent), ".md") + ".orig.md"
		_ = os.Remove(origPath)
	}
}

// RefineRoundActive reports whether a refine round is still mid-flight: at least
// one planner has an <agent>.orig.md backup. The backups are written when a
// refine round starts and removed by ClearRefineBackups when it completes, so
// their presence is the ground-truth signal that a finishing plan phase is in
// fact a refine round. finishPhase keys the revote reset off this rather than
// the TUI phase label, because a resumed refine round is reopened under the
// "plan" label (config.PhasePlan), not "refine".
func (c *Controller) RefineRoundActive() bool {
	if c.run == nil {
		return false
	}
	for _, agent := range c.allAgentsForPhase(config.PhasePlan) {
		origPath := strings.TrimSuffix(c.run.PlanPath(agent), ".md") + ".orig.md"
		if fileExists(origPath) {
			return true
		}
	}
	return false
}
