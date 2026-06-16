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
