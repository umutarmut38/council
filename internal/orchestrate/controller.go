package orchestrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
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

// RefinePrompts builds the consensus-round prompt: the winning planner reads
// the reviewers' critiques and rewrites its plan before the build starts. The
// original plan is preserved as <agent>.orig.md and the watched plan file is
// removed so the phase completes when the refined plan lands.
func (c *Controller) RefinePrompts() (map[string]string, error) {
	issue, err := c.issue()
	if err != nil {
		return nil, err
	}
	winner, err := c.winnerName()
	if err != nil {
		return nil, err
	}

	votePaths := []string{}
	for _, voter := range c.allAgentsForPhase(config.PhaseVote) {
		if path := c.run.VotePath(voter); fileExists(path) {
			votePaths = append(votePaths, path)
		}
	}
	if len(votePaths) == 0 {
		return nil, errors.New("no votes on disk to refine from")
	}

	planPath := c.run.PlanPath(winner)
	origPath := strings.TrimSuffix(planPath, ".md") + ".orig.md"
	if !fileExists(origPath) {
		data, err := os.ReadFile(planPath)
		if err != nil {
			return nil, fmt.Errorf("read winning plan: %w", err)
		}
		if err := os.WriteFile(origPath, data, fsperm.File()); err != nil {
			return nil, err
		}
	}
	// Remove the watched artifact so the refine phase finishes when the agent
	// writes the new version (the original stays in .orig.md).
	_ = os.Remove(planPath)

	return map[string]string{
		winner: RefinePrompt(issue, origPath, votePaths, planPath),
	}, nil
}
