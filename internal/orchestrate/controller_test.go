package orchestrate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
)

func TestVotePromptsExcludeOwnPlan(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)

	cfg := config.Config{
		Agents: map[string]config.AgentConfig{
			"a": {Enabled: true, Command: []string{"true"}},
			"b": {Enabled: true, Command: []string{"true"}},
			"c": {Enabled: true, Command: []string{"true"}},
		},
		Sessions: config.SessionConfig{RootDir: filepath.Join(root, ".council", "runs")},
	}
	cfg.Normalize()
	ctrl, err := NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.StartRun("do it"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(ctrl.Run().PlanPath(name), []byte("plan by "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	prompts, err := ctrl.VotePrompts()
	if err != nil {
		t.Fatal(err)
	}

	ownLetter := map[string]string{}
	for _, r := range ctrl.refs {
		ownLetter[r.Agent] = r.Letter
	}
	for _, voter := range []string{"a", "b", "c"} {
		p := prompts[voter]
		own := ctrl.Run().AnonPlanPath(ownLetter[voter])
		if strings.Contains(p, own) {
			t.Fatalf("%s's prompt references its own plan file %q", voter, own)
		}
		if got := strings.Count(p, "- Plan "); got != 2 {
			t.Fatalf("%s should rank 2 other plans, prompt lists %d: %q", voter, got, p)
		}
	}
}

func TestControllerFiltersAgentsPerPhase(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)

	cfg := config.Config{
		Agents: map[string]config.AgentConfig{
			"builder": {
				Enabled: true,
				Command: []string{"true"},
			},
			"planner": {
				Enabled: true,
				Command: []string{"true"},
				Orchestration: config.OrchestrationConfig{
					ExcludeBuild: true,
				},
			},
			"skip": {
				Enabled: true,
				Command: []string{"true"},
				Orchestration: config.OrchestrationConfig{
					Exclude: true,
				},
			},
		},
		Sessions: config.SessionConfig{RootDir: filepath.Join(root, ".council", "runs")},
	}
	cfg.Normalize()

	ctrl, err := NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := ctrl.Agents(); len(got) != 2 || got[0] != "builder" || got[1] != "planner" {
		t.Fatalf("agents = %v", got)
	}
	if err := ctrl.StartRun("do it"); err != nil {
		t.Fatal(err)
	}

	if got := ctrl.AgentsForPhase(config.PhasePlan); len(got) != 2 || got[0] != "builder" || got[1] != "planner" {
		t.Fatalf("plan agents = %v", got)
	}
	if got := ctrl.AgentsForPhase(config.PhaseBuild); len(got) != 1 || got[0] != "builder" {
		t.Fatalf("build agents = %v", got)
	}

	store, err := ctrl.Store(config.PhasePlan)
	if err != nil {
		t.Fatal(err)
	}
	prompts := map[string]string{"builder": "builder prompt", "planner": "planner prompt"}
	sessions := ctrl.PhaseSessions(config.PhasePlan, store, prompts)
	if len(sessions) != 2 {
		t.Fatalf("plan sessions = %+v", sessions)
	}
	for _, session := range sessions {
		if session.Config.CWD != root {
			t.Fatalf("%s plan cwd = %q, want repo root %q", session.Name, session.Config.CWD, root)
		}
	}
	artifacts := ctrl.ArtifactPaths(config.PhasePlan)
	if artifacts["builder"] != ctrl.Run().PlanPath("builder") {
		t.Fatalf("builder artifact = %q, want %q", artifacts["builder"], ctrl.Run().PlanPath("builder"))
	}
}

func TestControllerAppendsPromptForCommandPromptAgents(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)

	cfg := config.Config{
		Agents: map[string]config.AgentConfig{
			"argv": {
				Enabled: true,
				Command: []string{"agent"},
				Orchestration: config.OrchestrationConfig{
					PlanCommand:         []string{"agent", "-p"},
					PlanPromptInCommand: true,
				},
			},
			"tty": {
				Enabled: true,
				Command: []string{"agent"},
			},
		},
		Sessions: config.SessionConfig{RootDir: filepath.Join(root, ".council", "runs")},
	}
	cfg.Normalize()
	ctrl, err := NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.StartRun("do it"); err != nil {
		t.Fatal(err)
	}

	prompts := map[string]string{"argv": "argv prompt", "tty": "tty prompt"}
	store, err := ctrl.Store(config.PhasePlan)
	if err != nil {
		t.Fatal(err)
	}
	sessions := ctrl.PhaseSessions(config.PhasePlan, store, prompts)
	byName := map[string]*agent.Session{}
	for _, session := range sessions {
		byName[session.Name] = session
	}
	if got, want := byName["argv"].Config.Command, []string{"agent", "-p", "argv prompt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("argv command = %v, want %v", got, want)
	}
	if got, want := byName["tty"].Config.Command, []string{"agent"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tty command = %v, want %v", got, want)
	}
	if interactive := ctrl.InteractivePrompts(config.PhasePlan, prompts); len(interactive) != 1 || interactive["tty"] != "tty prompt" {
		t.Fatalf("interactive prompts = %v", interactive)
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(old)
	})
}

func TestRunArtifactsAreAbsoluteUnderLaunchDir(t *testing.T) {
	repo := initRepo(t)
	// Launch council from a subdirectory of the repo (the "poc" scenario).
	sub := filepath.Join(repo, "poc")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)

	cfg := config.Config{
		Agents:   map[string]config.AgentConfig{"a": {Enabled: true, Command: []string{"true"}}},
		Sessions: config.SessionConfig{RootDir: ".council/runs"}, // relative on purpose
	}
	cfg.Normalize()
	ctrl, err := NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.StartRun("do it"); err != nil {
		t.Fatal(err)
	}

	planPath := ctrl.Run().PlanPath("a")
	if !filepath.IsAbs(planPath) {
		t.Fatalf("plan artifact path must be absolute, got %q", planPath)
	}
	if !strings.HasPrefix(planPath, sub) {
		t.Fatalf("plan artifact %q should live under the launch dir %q, not the repo root", planPath, sub)
	}

	// Plan agents run in the launch dir, not the git root.
	store, err := ctrl.Store(config.PhasePlan)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range ctrl.PhaseSessions(config.PhasePlan, store, map[string]string{"a": "x"}) {
		if s.Config.CWD != sub {
			t.Fatalf("plan cwd = %q, want launch dir %q", s.Config.CWD, sub)
		}
	}
}

func TestScopeSelectsJudgesButCandidatesAreAllProduced(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)

	cfg := config.Config{
		Agents: map[string]config.AgentConfig{
			"a": {Enabled: true, Command: []string{"true"}},
			"b": {Enabled: true, Command: []string{"true"}},
			"c": {Enabled: true, Command: []string{"true"}},
		},
		Sessions: config.SessionConfig{RootDir: filepath.Join(root, ".council", "runs")},
	}
	cfg.Normalize()
	ctrl, err := NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.StartRun("issue"); err != nil {
		t.Fatal(err)
	}

	// a and b produced plans; c did not.
	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(ctrl.Run().PlanPath(name), []byte("plan "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Scope the vote to only c — c reviews a's and b's plans.
	ctrl.SetScope([]string{"c"})
	if got := ctrl.AgentsForPhase(config.PhaseVote); len(got) != 1 || got[0] != "c" {
		t.Fatalf("vote participants = %v, want [c]", got)
	}

	prompts, err := ctrl.VotePrompts()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := prompts["a"]; ok {
		t.Fatal("a is out of scope and should get no vote prompt")
	}
	if got := strings.Count(prompts["c"], "- Plan "); got != 2 {
		t.Fatalf("c should rank 2 produced plans (a,b), got %d: %q", got, prompts["c"])
	}

	// Scope nil restores all participants.
	ctrl.SetScope(nil)
	if got := ctrl.AgentsForPhase(config.PhasePlan); len(got) != 3 {
		t.Fatalf("cleared scope plan participants = %v, want all 3", got)
	}
}

func TestControllerRoutesByRole(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)

	cfg := config.Config{
		Agents: map[string]config.AgentConfig{
			"worker1":  {Enabled: true, Command: []string{"true"}, Role: []string{config.RoleWorker}},
			"reviewer": {Enabled: true, Command: []string{"true"}, Role: []string{config.RoleReviewer}},
			"both":     {Enabled: true, Command: []string{"true"}}, // empty role -> both
		},
		Sessions: config.SessionConfig{RootDir: filepath.Join(root, ".council", "runs")},
	}
	cfg.Normalize()
	ctrl, err := NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}

	// Workers (worker1, both) plan and build; reviewers (reviewer, both) vote and review.
	if got := ctrl.AgentsForPhase(config.PhasePlan); !reflect.DeepEqual(got, []string{"both", "worker1"}) {
		t.Fatalf("plan agents = %v, want [both worker1]", got)
	}
	if got := ctrl.AgentsForPhase(config.PhaseBuild); !reflect.DeepEqual(got, []string{"both", "worker1"}) {
		t.Fatalf("build agents = %v, want [both worker1]", got)
	}
	if got := ctrl.AgentsForPhase(config.PhaseVote); !reflect.DeepEqual(got, []string{"both", "reviewer"}) {
		t.Fatalf("vote agents = %v, want [both reviewer]", got)
	}
	if got := ctrl.AgentsForPhase(config.PhaseReview); !reflect.DeepEqual(got, []string{"both", "reviewer"}) {
		t.Fatalf("review agents = %v, want [both reviewer]", got)
	}
}

func TestResumeTargetRestoresActivePlanAndOnlyPromptsMissingAgents(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)

	cfg := config.Config{
		Agents: map[string]config.AgentConfig{
			"a": {Enabled: true, Command: []string{"true"}},
			"b": {Enabled: true, Command: []string{"true"}},
		},
		Sessions: config.SessionConfig{RootDir: filepath.Join(root, ".council", "runs")},
	}
	cfg.Normalize()
	ctrl, err := NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.StartRun("do it"); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.SaveActivePhase(config.PhasePlan, []string{"a", "b"}, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ctrl.Run().PlanPath("a"), []byte("done"), 0o644); err != nil {
		t.Fatal(err)
	}

	target, err := ctrl.ResumeTarget()
	if err != nil {
		t.Fatal(err)
	}
	if target.Phase != config.PhasePlan {
		t.Fatalf("phase = %q, want plan", target.Phase)
	}
	if _, ok := target.Prompts["a"]; ok {
		t.Fatalf("completed agent should not be prompted again: %v", target.Prompts)
	}
	if target.Prompts["b"] == "" {
		t.Fatalf("missing agent should receive a resume prompt: %v", target.Prompts)
	}
	if !reflect.DeepEqual(target.Participants, []string{"a", "b"}) {
		t.Fatalf("participants = %v, want [a b]", target.Participants)
	}
}

func TestVoteAssignmentsPersistAcrossResume(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)

	cfg := config.Config{
		Agents: map[string]config.AgentConfig{
			"a": {Enabled: true, Command: []string{"true"}},
			"b": {Enabled: true, Command: []string{"true"}},
			"c": {Enabled: true, Command: []string{"true"}},
		},
		Sessions: config.SessionConfig{RootDir: filepath.Join(root, ".council", "runs")},
	}
	cfg.Normalize()
	ctrl, err := NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.StartRun("do it"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(ctrl.Run().PlanPath(name), []byte("plan "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ctrl.VotePrompts(); err != nil {
		t.Fatal(err)
	}
	refs := append([]PlanRef(nil), ctrl.refs...)

	resumed, err := NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := resumed.UseRun(ctrl.Run().Stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := resumed.VotePrompts(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resumed.refs, refs) {
		t.Fatalf("resumed refs = %+v, want %+v", resumed.refs, refs)
	}
}

func TestResumeBuildDoesNotResetInterruptedWorktree(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)

	cfg := config.Config{
		Agents: map[string]config.AgentConfig{
			"worker": {Enabled: true, Command: []string{"true"}, Role: []string{config.RoleWorker}},
		},
		Sessions: config.SessionConfig{RootDir: filepath.Join(root, ".council", "runs")},
	}
	cfg.Normalize()
	ctrl, err := NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.StartRun("do it"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ctrl.Run().PlanPath("worker"), []byte("plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ctrl.Run().ResultPath(), []byte(`{"winner_agent":"worker"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.BuildPrompt(); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(ctrl.worktrees["worker"], "half-done.txt")
	if err := os.WriteFile(stray, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.SaveActivePhase(config.PhaseBuild, []string{"worker"}, true); err != nil {
		t.Fatal(err)
	}

	resumed, err := NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := resumed.UseRun(ctrl.Run().Stamp); err != nil {
		t.Fatal(err)
	}
	target, err := resumed.ResumeTarget()
	if err != nil {
		t.Fatal(err)
	}
	if target.Phase != config.PhaseBuild || !target.SendPrompts || target.PendingBuild {
		t.Fatalf("target = %+v, want active build with prompts sent", target)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Fatalf("resume reset or removed interrupted work: %v", err)
	}
}

// TestSetSinglePlanWinner: a lone plan is recorded as the vote winner without a
// vote, so Winner()/build proceed (the plan-phase twin of the single-build case).
func TestSetSinglePlanWinner(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)
	ctrl := resumeTestController(t, root)

	if err := os.WriteFile(ctrl.Run().PlanPath("a"), []byte("the only plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.SetSinglePlanWinner("a"); err != nil {
		t.Fatal(err)
	}
	name, plan, err := ctrl.Winner()
	if err != nil {
		t.Fatalf("Winner after single-plan select: %v", err)
	}
	if name != "a" {
		t.Fatalf("winner = %q, want a", name)
	}
	if plan != "the only plan" {
		t.Fatalf("winner plan = %q", plan)
	}
}

// TestRefineSinglePlanNoVotes: /refine works on the single auto-won plan even
// with no critiques, and folds in the user's note.
func TestRefineSinglePlanNoVotes(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)
	ctrl := resumeTestController(t, root)

	if err := os.WriteFile(ctrl.Run().PlanPath("a"), []byte("the only plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.SetSinglePlanWinner("a"); err != nil {
		t.Fatal(err)
	}
	prompts, err := ctrl.RefinePrompts("make it simpler")
	if err != nil {
		t.Fatalf("refine on a single plan should not error: %v", err)
	}
	p, ok := prompts["a"]
	if !ok {
		t.Fatal("the lone planner should be prompted to refine")
	}
	if !strings.Contains(p, "make it simpler") {
		t.Fatalf("refine prompt is missing the note:\n%s", p)
	}
	if strings.Contains(p, "REVIEWER CRITIQUES") {
		t.Fatalf("single-plan refine should have no critiques section:\n%s", p)
	}
}

// TestRefinePromptsAllPlanners: /refine fans out to every planner that produced
// a plan, backs each up to .orig.md, removes the live files, and tells each
// refiner the anonymized letter its plan was shown as.
func TestRefinePromptsAllPlanners(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)
	ctrl := resumeTestController(t, root)

	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(ctrl.Run().PlanPath(name), []byte("plan "+name), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ctrl.Run().VotePath(name), []byte("RANKING: A > B\nWINNER: A"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	refs := []PlanRef{{Letter: "A", Agent: "a"}, {Letter: "B", Agent: "b"}}
	if err := ctrl.Run().SaveVoteRefs(refs); err != nil {
		t.Fatal(err)
	}
	res := Tally([]Ballot{{Voter: "a", Ranking: []string{"B"}}, {Voter: "b", Ranking: []string{"A"}}}, refs)
	if err := ctrl.Run().WriteResult(res, refs); err != nil {
		t.Fatal(err)
	}

	prompts, err := ctrl.RefinePrompts("")
	if err != nil {
		t.Fatalf("RefinePrompts: %v", err)
	}
	if len(prompts) != 2 {
		t.Fatalf("want 2 refine prompts, got %d: %v", len(prompts), prompts)
	}
	wantLetter := map[string]string{"a": "Plan A", "b": "Plan B"}
	for _, name := range []string{"a", "b"} {
		p, ok := prompts[name]
		if !ok {
			t.Fatalf("planner %q was not prompted to refine", name)
		}
		if !strings.Contains(p, "REVIEWER CRITIQUES") {
			t.Fatalf("refine prompt for %q is missing critiques:\n%s", name, p)
		}
		if !strings.Contains(p, wantLetter[name]) {
			t.Fatalf("refine prompt for %q should name its own %s:\n%s", name, wantLetter[name], p)
		}
		// The live plan is moved aside so the rewrite can be watched.
		if fileExists(ctrl.Run().PlanPath(name)) {
			t.Fatalf("live plan for %q should be removed during refine", name)
		}
		origPath := strings.TrimSuffix(ctrl.Run().PlanPath(name), ".md") + ".orig.md"
		if !fileExists(origPath) {
			t.Fatalf("plan for %q should be backed up to %s", name, origPath)
		}
	}
}

// TestResetVote clears the prior vote's derived artifacts so a revote starts
// fresh, while leaving the plans themselves intact.
func TestResetVote(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)
	ctrl := resumeTestController(t, root)
	run := ctrl.Run()

	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(run.PlanPath(name), []byte("plan "+name), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(run.VotePath(name), []byte("RANKING: A\nWINNER: A"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A ballot left by a voter that is no longer in scope (e.g. an agent that was
	// excluded after voting). ResetVote must clear every derived artifact, not
	// just the current voter set, or this stale ballot could later be mistaken
	// for an already-cast vote when the agent re-enters scope.
	staleBallot := run.VotePath("stale-voter")
	if err := os.WriteFile(staleBallot, []byte("RANKING: A\nWINNER: A"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, letter := range []string{"A", "B"} {
		if err := os.WriteFile(run.AnonPlanPath(letter), []byte("anon "+letter), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	refs := []PlanRef{{Letter: "A", Agent: "a"}, {Letter: "B", Agent: "b"}}
	if err := run.SaveVoteRefs(refs); err != nil {
		t.Fatal(err)
	}
	if err := run.WriteResult(Tally([]Ballot{{Voter: "b", Ranking: []string{"A"}}}, refs), refs); err != nil {
		t.Fatal(err)
	}

	if err := ctrl.ResetVote(); err != nil {
		t.Fatalf("ResetVote: %v", err)
	}

	gone := []string{
		run.VoteRefsPath(), run.ResultPath(), run.SummaryPath(),
		run.VotePath("a"), run.VotePath("b"), staleBallot,
		run.AnonPlanPath("A"), run.AnonPlanPath("B"),
	}
	for _, p := range gone {
		if fileExists(p) {
			t.Fatalf("ResetVote should have removed %s", p)
		}
	}
	for _, name := range []string{"a", "b"} {
		if !fileExists(run.PlanPath(name)) {
			t.Fatalf("ResetVote must not remove the plan for %q", name)
		}
	}
}

// TestRevoteAfterResetReanonymizes: after a reset, VotePrompts re-anonymizes from
// the current (refined) plans rather than reusing the stale anonymized copies.
func TestRevoteAfterResetReanonymizes(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)
	ctrl := resumeTestController(t, root)
	run := ctrl.Run()

	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(run.PlanPath(name), []byte("plan "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ctrl.VotePrompts(); err != nil {
		t.Fatalf("first VotePrompts: %v", err)
	}

	if err := ctrl.ResetVote(); err != nil {
		t.Fatalf("ResetVote: %v", err)
	}
	// Refined plan for "a".
	if err := os.WriteFile(run.PlanPath("a"), []byte("refined plan a"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ctrl.VotePrompts(); err != nil {
		t.Fatalf("second VotePrompts: %v", err)
	}
	refs, err := run.LoadVoteRefs()
	if err != nil {
		t.Fatalf("refs should be regenerated: %v", err)
	}
	var letterForA string
	for _, r := range refs {
		if r.Agent == "a" {
			letterForA = r.Letter
		}
	}
	if letterForA == "" {
		t.Fatal("no letter assigned to a after revote")
	}
	anon, err := os.ReadFile(run.AnonPlanPath(letterForA))
	if err != nil {
		t.Fatalf("anonymized copy missing for a's revote letter: %v", err)
	}
	if string(anon) != "refined plan a" {
		t.Fatalf("anonymized plan for a = %q, want the refined content", string(anon))
	}
}

func resumeTestController(t *testing.T, root string) *Controller {
	t.Helper()
	cfg := config.Config{
		Agents: map[string]config.AgentConfig{
			"a": {Enabled: true, Command: []string{"true"}},
			"b": {Enabled: true, Command: []string{"true"}},
		},
		Sessions: config.SessionConfig{RootDir: filepath.Join(root, ".council", "runs")},
	}
	cfg.Normalize()
	ctrl, err := NewController(cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.StartRun("do it"); err != nil {
		t.Fatal(err)
	}
	return ctrl
}

func reopen(t *testing.T, ctrl *Controller) *Controller {
	t.Helper()
	resumed, err := NewController(ctrl.cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := resumed.UseRun(ctrl.Run().Stamp); err != nil {
		t.Fatal(err)
	}
	return resumed
}

// TestResumeTargetCoversEveryStage walks the pipeline and checks the inferred
// resume target at each point, so an interrupted run can always be reopened.
func TestResumeTargetCoversEveryStage(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)
	ctrl := resumeTestController(t, root)

	// Stage 1: nothing produced -> resume planning.
	target, err := reopen(t, ctrl).ResumeTarget()
	if err != nil || target.Phase != config.PhasePlan {
		t.Fatalf("fresh run: phase = %q (%v), want plan", target.Phase, err)
	}

	// Stage 2: plans exist, no vote result -> resume voting.
	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(ctrl.Run().PlanPath(name), []byte("plan "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	target, err = reopen(t, ctrl).ResumeTarget()
	if err != nil || target.Phase != config.PhaseVote {
		t.Fatalf("after plans: phase = %q (%v), want vote", target.Phase, err)
	}

	// Stage 3: vote tallied, build base recorded -> resume build (staged).
	refs := []PlanRef{{Letter: "A", Agent: "a"}, {Letter: "B", Agent: "b"}}
	res := Tally([]Ballot{{Voter: "b", Ranking: []string{"A"}}}, refs)
	if err := ctrl.Run().WriteResult(res, refs); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Run().SaveBaseSHA("deadbeef"); err != nil {
		t.Fatal(err)
	}
	target, err = reopen(t, ctrl).ResumeTarget()
	if err != nil || target.Phase != config.PhaseBuild {
		t.Fatalf("after vote: phase = %q (%v), want build", target.Phase, err)
	}
	if !target.PendingBuild {
		t.Fatal("inferred build resume should stage, not auto-send")
	}

	// Stage 4: review tallied -> idle resume (HUD takes over from here).
	if err := ctrl.SetSingleWinner("a"); err != nil {
		t.Fatal(err)
	}
	target, err = reopen(t, ctrl).ResumeTarget()
	if err != nil || target.Phase != "" {
		t.Fatalf("after review: phase = %q (%v), want idle", target.Phase, err)
	}
	if !strings.Contains(target.Status, "resumed") {
		t.Fatalf("idle resume status = %q", target.Status)
	}
}

// TestResumeRefineRoundKeepsRefinePrompt: an interrupted /refine must resume
// with the refine prompt (critiques + rewrite) for every refining planner, not a
// from-scratch plan prompt.
func TestResumeRefineRoundKeepsRefinePrompt(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)
	ctrl := resumeTestController(t, root)

	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(ctrl.Run().PlanPath(name), []byte("plan "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(ctrl.Run().VotePath("b"), []byte("RANKING: A\nWINNER: A"), 0o644); err != nil {
		t.Fatal(err)
	}
	refs := []PlanRef{{Letter: "A", Agent: "a"}, {Letter: "B", Agent: "b"}}
	res := Tally([]Ballot{{Voter: "b", Ranking: []string{"A"}}}, refs)
	if err := ctrl.Run().WriteResult(res, refs); err != nil {
		t.Fatal(err)
	}

	// Start the refine round (backs up every plan, removes the live files), save
	// the phase as the TUI would, then simulate a crash + resume.
	if _, err := ctrl.RefinePrompts(""); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.SaveActivePhase(config.PhasePlan, []string{"a", "b"}, true); err != nil {
		t.Fatal(err)
	}

	target, err := reopen(t, ctrl).ResumeTarget()
	if err != nil {
		t.Fatal(err)
	}
	if target.Phase != config.PhasePlan {
		t.Fatalf("phase = %q, want plan", target.Phase)
	}
	for _, name := range []string{"a", "b"} {
		prompt := target.Prompts[name]
		if prompt == "" {
			t.Fatalf("planner %q should be prompted on refine resume", name)
		}
		if !strings.Contains(prompt, "REVIEWER CRITIQUES") || !strings.Contains(prompt, "refine") {
			t.Fatalf("resume used a plain plan prompt for %q, not the refine prompt:\n%s", name, prompt)
		}
	}
	if !strings.Contains(target.Status, "refining") {
		t.Fatalf("status = %q, want refining", target.Status)
	}
}

// TestResumeRefineSkipsFinishedPlanner: if a refine round was interrupted after
// one planner already wrote its refined plan, resume re-prompts only the planner
// whose plan is still missing — the finished one is left untouched.
func TestResumeRefineSkipsFinishedPlanner(t *testing.T) {
	root := initRepo(t)
	chdir(t, root)
	ctrl := resumeTestController(t, root)

	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(ctrl.Run().PlanPath(name), []byte("plan "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	refs := []PlanRef{{Letter: "A", Agent: "a"}, {Letter: "B", Agent: "b"}}
	res := Tally([]Ballot{{Voter: "b", Ranking: []string{"A"}}}, refs)
	if err := ctrl.Run().WriteResult(res, refs); err != nil {
		t.Fatal(err)
	}
	if _, err := ctrl.RefinePrompts(""); err != nil {
		t.Fatal(err)
	}
	// Planner "a" finished refining: its live plan reappears.
	if err := os.WriteFile(ctrl.Run().PlanPath("a"), []byte("refined a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.SaveActivePhase(config.PhasePlan, []string{"a", "b"}, true); err != nil {
		t.Fatal(err)
	}

	target, err := reopen(t, ctrl).ResumeTarget()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := target.Prompts["a"]; ok {
		t.Fatal("finished planner a should not be re-prompted")
	}
	if target.Prompts["b"] == "" {
		t.Fatal("unfinished planner b should be re-prompted")
	}
}
