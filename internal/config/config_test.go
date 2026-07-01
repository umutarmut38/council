package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCommandForPhaseFallsBackToCommand(t *testing.T) {
	a := AgentConfig{
		Command: []string{"agent"},
		Orchestration: OrchestrationConfig{
			BuildCommand: []string{"agent", "--force"},
		},
	}
	if got := a.CommandForPhase(PhaseBuild); !reflect.DeepEqual(got, []string{"agent", "--force"}) {
		t.Fatalf("build command = %v", got)
	}
	// plan has no override, so it falls back to Command.
	if got := a.CommandForPhase(PhasePlan); !reflect.DeepEqual(got, []string{"agent"}) {
		t.Fatalf("plan command = %v, want fallback to Command", got)
	}
}

func TestDefaultsAreSafe(t *testing.T) {
	d := Default().Agents
	if len(d) == 0 {
		t.Fatal("default config should ship agent presets")
	}
	for name, agentCfg := range d {
		if agentCfg.Enabled {
			t.Fatalf("%s preset must ship disabled", name)
		}
		if flags := AgentRiskyFlags(agentCfg); len(flags) > 0 {
			t.Fatalf("%s preset must not carry auto-approval flags, got %v", name, flags)
		}
	}
	// Copilot remains excluded from build (worktree quirk), and the opt-in
	// auto-approval commands stay available but never applied by default.
	if d["copilot"].ParticipatesIn(PhaseBuild) {
		t.Fatal("copilot should be excluded from build by default")
	}
	if got := PresetAutoApproveCommand("claude"); !reflect.DeepEqual(got, []string{"claude", "--dangerously-skip-permissions"}) {
		t.Fatalf("claude auto-approve command = %v", got)
	}
	if flags := RiskyCommandFlags([]string{"claude", "--dangerously-skip-permissions"}); len(flags) != 1 {
		t.Fatalf("risky flag detection = %v", flags)
	}
}

func TestValidateAgentNames(t *testing.T) {
	good := Config{Agents: map[string]AgentConfig{"claude": {}, "codex-2": {}, "a_b": {}}}
	if err := ValidateAgentNames(good); err != nil {
		t.Fatalf("valid names rejected: %v", err)
	}
	bad := Config{Agents: map[string]AgentConfig{"a/b": {}}}
	if err := ValidateAgentNames(bad); err == nil {
		t.Fatal("name with '/' should be rejected")
	}
	dup := Config{Agents: map[string]AgentConfig{"Claude": {}, "claude": {}}}
	if err := ValidateAgentNames(dup); err == nil {
		t.Fatal("names colliding after normalization should be rejected")
	}
}

func TestParticipatesInHonorsPhaseExclusions(t *testing.T) {
	agent := AgentConfig{
		Orchestration: OrchestrationConfig{
			ExcludeVote: true,
		},
	}
	if !agent.ParticipatesIn(PhasePlan) {
		t.Fatal("agent should participate in plan")
	}
	if agent.ParticipatesIn(PhaseVote) {
		t.Fatal("agent should be excluded from vote")
	}
	if !agent.ParticipatesIn(PhaseBuild) {
		t.Fatal("agent should participate in build")
	}

	agent.Orchestration.Exclude = true
	if agent.ParticipatesIn(PhasePlan) || agent.ParticipatesIn(PhaseVote) || agent.ParticipatesIn(PhaseBuild) {
		t.Fatal("global exclude should disable every phase")
	}
}

func TestLoadMergesDefaultOrchestrationWhenBlockIsOmitted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := []byte(`agents:
  copilot:
    enabled: true
    command: ["gh", "copilot"]
`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	copilot := cfg.Agents["copilot"]
	if !copilot.ParticipatesIn(PhasePlan) || !copilot.ParticipatesIn(PhaseVote) || copilot.ParticipatesIn(PhaseBuild) {
		t.Fatalf("copilot phase participation = %+v", copilot.Orchestration)
	}
	// No auto-approval flags are inherited from defaults; phase commands fall
	// back to the agent's own command.
	if got := copilot.CommandForPhase(PhasePlan); !reflect.DeepEqual(got, []string{"gh", "copilot"}) {
		t.Fatalf("copilot plan command = %v", got)
	}
	if copilot.PromptInCommandForPhase(PhasePlan) {
		t.Fatal("copilot should inherit interactive (non-argv) prompt delivery")
	}
}

func TestLoadPreservesExplicitEmptyOrchestrationBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := []byte(`agents:
  copilot:
    enabled: true
    command: ["gh", "copilot"]
    orchestration: {}
`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	copilot := cfg.Agents["copilot"]
	if !copilot.ParticipatesIn(PhaseBuild) {
		t.Fatalf("explicit empty orchestration should preserve zero values: %+v", copilot.Orchestration)
	}
	if got := copilot.CommandForPhase(PhasePlan); !reflect.DeepEqual(got, []string{"gh", "copilot"}) {
		t.Fatalf("copilot plan command = %v", got)
	}
}

func TestFindLocalConfigWalksUpToGitRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".council.yaml"), []byte("ui:\n  layout: grid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	old, _ := os.Getwd()
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	got := FindLocalConfig()
	if filepath.Base(got) != ".council.yaml" {
		t.Fatalf("FindLocalConfig() = %q, want a .council.yaml at the repo root", got)
	}
}

// Regression: the top-level usage block (and per-agent usage) must survive the
// repo-overlay merge. ApplyLocalOverride is a key-by-key switch, so a new
// top-level section silently vanishes unless it has a case.
func TestApplyLocalOverrideUsage(t *testing.T) {
	base := Default()
	local := []byte(`
usage:
  enabled: true
  show_total_in_header: true
  prices:
    gpt5-user:
      input_per_million: 1.25
      output_per_million: 10.0
      reviewed_at: "2026-06-20"
agents:
  claude:
    usage:
      model: claude-sonnet-4-6
      price_profile: gpt5-user
`)
	merged, err := ApplyLocalOverride(base, local)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.Usage.Enabled || !merged.Usage.HeaderTotalEnabled() {
		t.Fatalf("usage block dropped: %+v", merged.Usage)
	}
	if p, ok := merged.Usage.Prices["gpt5-user"]; !ok || p.InputPerMillion != 1.25 {
		t.Fatalf("price profile not merged: %+v", merged.Usage.Prices)
	}
	if merged.Agents["claude"].Usage.Model != "claude-sonnet-4-6" {
		t.Fatalf("per-agent usage dropped: %+v", merged.Agents["claude"].Usage)
	}
}

// Regression: the opt-in worktrees block must survive the repo-overlay merge.
// ApplyLocalOverride is a key-by-key switch, so without a "worktrees" case a
// repo-local worktrees.freestyle silently vanished when a global config was
// present and the feature never activated.
func TestApplyLocalOverrideWorktrees(t *testing.T) {
	base := Default() // global config has no worktrees block
	local := []byte("worktrees:\n  freestyle: true\n  seed:\n    - .env\n")
	merged, err := ApplyLocalOverride(base, local)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.Worktrees.Freestyle {
		t.Fatalf("worktrees.freestyle dropped by overlay: %+v", merged.Worktrees)
	}
	if len(merged.Worktrees.Seed) != 1 || merged.Worktrees.Seed[0] != ".env" {
		t.Fatalf("worktrees.seed dropped by overlay: %+v", merged.Worktrees.Seed)
	}
}

// Guardrail: every top-level Config section must be reachable by
// ApplyLocalOverride so a new section can never silently vanish from the repo
// overlay (the worktrees bug). A section is reachable if it has a bespoke merge
// case or is covered by the generic topLevelOverlayFields map.
func TestApplyLocalOverrideReachesEverySection(t *testing.T) {
	special := map[string]bool{ // sections with bespoke merge handlers
		"agents":                 true,
		"personalities":          true,
		"personality_categories": true,
	}
	covered := topLevelOverlayFields(&Config{})
	ct := reflect.TypeOf(Config{})
	for i := 0; i < ct.NumField(); i++ {
		name := strings.SplitN(ct.Field(i).Tag.Get("yaml"), ",", 2)[0]
		if name == "" || name == "-" || special[name] {
			continue
		}
		if _, ok := covered[name]; !ok {
			t.Errorf("top-level section %q is not reachable by ApplyLocalOverride; it would silently vanish from the repo overlay", name)
		}
	}
}

// An inheriting agent keeps the base's usage bindings, overriding only what it sets.
func TestOverlayAgentUsageInherits(t *testing.T) {
	base := AgentConfig{Usage: AgentUsageConfig{Model: "gpt-5", Tool: "codex", PriceProfile: "p"}}
	child := AgentConfig{Usage: AgentUsageConfig{Model: "claude-opus-4-6"}} // override model only
	out := overlayAgent(base, child)
	if out.Usage.Model != "claude-opus-4-6" {
		t.Fatalf("child model override lost: %+v", out.Usage)
	}
	if out.Usage.Tool != "codex" || out.Usage.PriceProfile != "p" {
		t.Fatalf("base tool/profile not inherited: %+v", out.Usage)
	}
}

// The per-agent worktree opt-out is a *bool tri-state: an inheriting agent must
// be able to override an inherited true back to false, and an unset child must
// keep the base's value. Guards the overlay propagation.
func TestOverlayAgentWorktreeOverride(t *testing.T) {
	yes, no := true, false
	if out := overlayAgent(AgentConfig{Worktree: &yes}, AgentConfig{Worktree: &no}); out.Worktree == nil || *out.Worktree {
		t.Fatalf("child worktree:false override lost: %v", out.Worktree)
	}
	if out := overlayAgent(AgentConfig{Worktree: &yes}, AgentConfig{}); out.Worktree == nil || !*out.Worktree {
		t.Fatalf("base worktree not inherited when child unset: %v", out.Worktree)
	}
	if out := overlayAgent(AgentConfig{}, AgentConfig{Worktree: &yes}); out.Worktree == nil || !*out.Worktree {
		t.Fatalf("child worktree:true not applied over unset base: %v", out.Worktree)
	}
}

func TestApplyLocalOverrideDeepMerges(t *testing.T) {
	base := Default()
	claude := base.Agents["claude"]
	claude.Enabled = true
	base.Agents["claude"] = claude
	cursor := base.Agents["cursor"]
	cursor.Enabled = true
	base.Agents["cursor"] = cursor
	wantDelay := base.Agents["cursor"].Terminal.SubmitDelayMs
	base.PersonalityCategories = map[string]PersonalityCategoryConfig{
		"strategy": {Label: "Strategy", Order: 10},
	}
	base.Personalities = map[string]PersonalityConfig{
		"architect": {Label: "Architect", Category: "strategy", PromptPrefix: "Think in systems."},
	}

	local := []byte(`
agents:
  cursor:
    enabled: false
    personality: critic
ui:
  initial_prompt_delay_ms: 1234
  page_rows: 3
  group_by: category
review:
  check_command: ["make", "test"]
personality_categories:
  review:
    label: Review
    order: 30
personalities:
  critic:
    label: Critic
    category: review
    prompt_prefix: Find bugs.
`)
	merged, err := ApplyLocalOverride(base, local)
	if err != nil {
		t.Fatal(err)
	}

	cursor = merged.Agents["cursor"]
	if cursor.Enabled {
		t.Fatal("local override should disable cursor")
	}
	// Fields not set locally are preserved from the global config.
	if cursor.Terminal.SubmitDelayMs != wantDelay {
		t.Fatalf("cursor terminal lost: submit_delay=%d, want %d", cursor.Terminal.SubmitDelayMs, wantDelay)
	}
	if len(cursor.Command) == 0 || cursor.Command[0] != "cursor-agent" {
		t.Fatalf("cursor command lost: %v", cursor.Command)
	}
	// Other agents untouched.
	if !merged.Agents["claude"].Enabled {
		t.Fatal("claude should stay enabled")
	}
	// Top-level overrides applied; sibling keys preserved.
	if merged.UI.InitialPromptDelayMs != 1234 {
		t.Fatalf("ui delay = %d, want 1234", merged.UI.InitialPromptDelayMs)
	}
	if merged.UI.PageRows != 3 || merged.UI.GroupBy != "category" {
		t.Fatalf("ui page/group settings = %+v", merged.UI)
	}
	if merged.UI.MaxScrollbackLines != base.UI.MaxScrollbackLines {
		t.Fatalf("ui max_scrollback should be preserved, got %d", merged.UI.MaxScrollbackLines)
	}
	if len(merged.Review.CheckCommand) != 2 || merged.Review.CheckCommand[0] != "make" {
		t.Fatalf("review check_command = %v", merged.Review.CheckCommand)
	}
	if _, ok := merged.Personalities["architect"]; !ok {
		t.Fatal("base personality should be preserved")
	}
	if merged.Personalities["critic"].PromptPrefix != "Find bugs." {
		t.Fatalf("critic personality = %+v", merged.Personalities["critic"])
	}
	if _, ok := merged.PersonalityCategories["strategy"]; !ok {
		t.Fatal("base personality category should be preserved")
	}
	if merged.Agents["cursor"].Personality != "critic" {
		t.Fatalf("cursor personality = %q, want critic", merged.Agents["cursor"].Personality)
	}
}

func TestNormalizeAppliesGenericDefaultsOnly(t *testing.T) {
	// Normalize must stay tool-agnostic: an agent with no terminal config gets
	// the same generic fallbacks regardless of its name. Per-agent behavior is
	// expected to come from the config file, not from Normalize.
	cfg := Config{
		Agents: map[string]AgentConfig{
			"cursor":   {Enabled: true, Command: []string{"cursor-agent"}},
			"anything": {Enabled: true, Command: []string{"some-cli"}},
		},
	}

	cfg.Normalize()

	if cfg.UI.PageRows != 2 || cfg.UI.PageCols != 2 || cfg.UI.GroupBy != "none" {
		t.Fatalf("ui page defaults = %+v", cfg.UI)
	}

	for name, agentCfg := range cfg.Agents {
		term := agentCfg.Terminal
		if term.Renderer != "screen" || term.PTYSize != "pane" || term.Cols != 120 || term.Rows != 40 {
			t.Fatalf("%s generic defaults = %+v", name, term)
		}
		if term.SendMode != "type" {
			t.Fatalf("%s send_mode = %q, want type", name, term.SendMode)
		}
		if term.SubmitSequence != "cr" {
			t.Fatalf("%s submit_sequence = %q, want cr", name, term.SubmitSequence)
		}
		if term.BeforeSendSequence != "" || term.AfterSubmitSequence != "" || term.SubmitDelayMs != 0 {
			t.Fatalf("%s should not get tool-specific sequences injected: %+v", name, term)
		}
		if term.Resize == nil || !*term.Resize {
			t.Fatalf("%s resize default = %v, want true", name, term.Resize)
		}
	}
}

func TestPromptForAgentPrependsPersonalityPrefix(t *testing.T) {
	cfg := Config{
		Agents: map[string]AgentConfig{
			"claude": {Personality: "architect"},
			"codex":  {},
		},
		Personalities: map[string]PersonalityConfig{
			"architect": {PromptPrefix: "Think in systems."},
		},
	}

	if got, want := cfg.PromptForAgent("claude", "\nBuild it."), "Think in systems.\n\nBuild it."; got != want {
		t.Fatalf("personality prompt = %q, want %q", got, want)
	}
	if got, want := cfg.PromptForAgent("codex", "Build it."), "Build it."; got != want {
		t.Fatalf("plain prompt = %q, want %q", got, want)
	}
	if got := cfg.PromptForAgent("claude", "   "); got != "   " {
		t.Fatalf("empty prompt should not be replaced, got %q", got)
	}
}

func TestDefaultCursorPresetSubmitsWithDelayedCR(t *testing.T) {
	// cursor-agent treats an Enter that arrives in the same burst as the text
	// as a literal newline, and council does not negotiate the kitty keyboard
	// protocol (so csi-enter is unreliable). The preset therefore types raw and
	// submits a plain CR on its own a moment later.
	cursor := Default().Agents["cursor"].Terminal
	if cursor.SendMode != "type" {
		t.Fatalf("cursor send_mode = %q, want type", cursor.SendMode)
	}
	if cursor.SubmitSequence != "cr" {
		t.Fatalf("cursor submit_sequence = %q, want cr", cursor.SubmitSequence)
	}
	if cursor.SubmitDelayMs <= 0 {
		t.Fatalf("cursor submit_delay_ms = %d, want > 0", cursor.SubmitDelayMs)
	}
	if cursor.BeforeSendSequence != "" || cursor.AfterSubmitSequence != "" {
		t.Fatalf("cursor preset should not wrap input in ctrl+u: %+v", cursor)
	}
}

func TestNormalizePreservesExplicitTerminalSettings(t *testing.T) {
	resize := true
	cfg := Config{
		Agents: map[string]AgentConfig{
			"codex": {
				Enabled: true,
				Command: []string{"codex"},
				Terminal: TerminalConfig{
					Renderer:       "transcript",
					PTYSize:        "pane",
					Cols:           100,
					Rows:           30,
					SendMode:       "type",
					SubmitSequence: "lf",
					Resize:         &resize,
				},
			},
		},
	}

	cfg.Normalize()

	terminal := cfg.Agents["codex"].Terminal
	if terminal.Renderer != "transcript" || terminal.PTYSize != "pane" || terminal.Cols != 100 || terminal.Rows != 30 || terminal.SendMode != "type" || terminal.SubmitSequence != "lf" {
		t.Fatalf("explicit terminal settings were not preserved: %+v", terminal)
	}
	if terminal.Resize == nil || !*terminal.Resize {
		t.Fatalf("explicit resize was not preserved: %v", terminal.Resize)
	}
}

func TestRolePhaseRouting(t *testing.T) {
	worker := AgentConfig{Role: []string{RoleWorker}}
	reviewer := AgentConfig{Role: []string{RoleReviewer}}
	both := AgentConfig{Role: []string{RoleWorker, RoleReviewer}}
	def := AgentConfig{} // empty role -> all

	// Legacy worker expands to planner+builder; it is not a planner-only agent.
	if !worker.HasRole(RolePlanner) || !worker.HasRole(RoleBuilder) || worker.HasRole(RoleVoter) {
		t.Fatal("legacy worker should expand to planner+builder")
	}
	if !def.HasRole(RolePlanner) || !def.HasRole(RoleReviewer) {
		t.Fatal("empty role must default to all roles")
	}

	cases := []struct {
		name                      string
		a                         AgentConfig
		plan, vote, build, review bool
	}{
		// Legacy aliases (expanded).
		{"worker", worker, true, false, true, false},
		{"reviewer", reviewer, false, true, false, true},
		{"both", both, true, true, true, true},
		{"default", def, true, true, true, true},
		// Granular per-phase roles.
		{"planner", AgentConfig{Role: []string{RolePlanner}}, true, false, false, false},
		{"builder", AgentConfig{Role: []string{RoleBuilder}}, false, false, true, false},
		{"voter", AgentConfig{Role: []string{RoleVoter}}, false, true, false, false},
		{"granular-vote+review", AgentConfig{Role: []string{RoleVoter, RoleReviewer}}, false, true, false, true},
		// A granular token makes `reviewer` literal (review-only, no vote),
		// unlike the legacy `[reviewer]` alias above.
		{"granular-reviewer-is-literal", AgentConfig{Role: []string{RolePlanner, RoleReviewer}}, true, false, false, true},
		// The unambiguous review-only token: review without voting.
		{"review-only", AgentConfig{Role: []string{RoleReview}}, false, false, false, true},
		{"review+vote", AgentConfig{Role: []string{RoleVoter, RoleReview}}, false, true, false, true},
	}
	for _, c := range cases {
		got := []bool{
			c.a.ParticipatesIn(PhasePlan),
			c.a.ParticipatesIn(PhaseVote),
			c.a.ParticipatesIn(PhaseBuild),
			c.a.ParticipatesIn(PhaseReview),
		}
		want := []bool{c.plan, c.vote, c.build, c.review}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: plan/vote/build/review = %v, want %v", c.name, got, want)
		}
	}

	// Legacy exclude_* still overrides the role.
	w := AgentConfig{Role: []string{RoleWorker}, Orchestration: OrchestrationConfig{ExcludePlan: true}}
	if w.ParticipatesIn(PhasePlan) {
		t.Fatal("exclude_plan should override the worker role")
	}
}

func TestExpandRoles(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", nil, nil},
		{"legacy worker", []string{"worker"}, []string{"planner", "builder"}},
		{"legacy reviewer", []string{"reviewer"}, []string{"voter", "reviewer"}},
		{"legacy both", []string{"worker", "reviewer"}, []string{"planner", "builder", "voter", "reviewer"}},
		{"granular literal", []string{"planner"}, []string{"planner"}},
		{"granular vote+review", []string{"voter", "reviewer"}, []string{"voter", "reviewer"}},
		// `review` is the unambiguous review-only token and is itself a trigger,
		// so it stays literal (no legacy expansion).
		{"review-only literal", []string{"review"}, []string{"review"}},
		// A granular token present makes every token literal, so `reviewer`
		// keeps its review-only meaning instead of expanding to voter+reviewer.
		{"mixed keeps reviewer literal", []string{"planner", "reviewer"}, []string{"planner", "reviewer"}},
		{"case and spaces", []string{" Worker "}, []string{"planner", "builder"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := expandRoles(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("expandRoles(%v) = %v, want %v", c.in, got, c.want)
			}
			// Idempotent: expanding the result again is a no-op.
			if again := expandRoles(got); !reflect.DeepEqual(again, got) {
				t.Fatalf("expandRoles not idempotent: %v -> %v", got, again)
			}
		})
	}
}

func TestGlobalEnvMergesIntoAgents(t *testing.T) {
	cfg := Config{
		Experimental: ExperimentalConfig{SetupEnv: true},
		Env:          map[string]string{"OPENAI_BASE_URL": "http://proxy:8787", "SHARED": "global"},
		Agents: map[string]AgentConfig{
			"codex":  {Enabled: true, Command: []string{"codex"}, Env: map[string]string{"SHARED": "agent", "EXTRA": "x"}},
			"claude": {Enabled: true, Command: []string{"claude"}},
		},
	}
	cfg.Normalize()

	codex := cfg.Agents["codex"]
	if codex.Env["OPENAI_BASE_URL"] != "http://proxy:8787" {
		t.Fatalf("global env not folded into codex: %v", codex.Env)
	}
	if codex.Env["SHARED"] != "agent" {
		t.Fatalf("agent env should win over global: %v", codex.Env)
	}
	if codex.Env["EXTRA"] != "x" {
		t.Fatalf("agent-only env lost: %v", codex.Env)
	}
	claude := cfg.Agents["claude"]
	if claude.Env["OPENAI_BASE_URL"] != "http://proxy:8787" || claude.Env["SHARED"] != "global" {
		t.Fatalf("global env not applied to agent without its own env: %v", claude.Env)
	}
}

func TestExperimentalGateClearsSetupEnvWhenDisabled(t *testing.T) {
	// With the gate off (the default), Normalize must drop global env, per-agent
	// env, and setup, and flag that it did so for the SelectAgents warning.
	cfg := Config{
		Env:   map[string]string{"OPENAI_BASE_URL": "http://proxy:8787"},
		Setup: []SetupCommand{{Name: "proxy", Command: []string{"true"}}},
		Agents: map[string]AgentConfig{
			"codex": {Enabled: true, Command: []string{"codex"}, Env: map[string]string{"X": "y"}},
		},
	}
	cfg.Normalize()

	if len(cfg.Env) != 0 {
		t.Fatalf("global env should be dropped when gate is off: %v", cfg.Env)
	}
	if len(cfg.Setup) != 0 {
		t.Fatalf("setup should be dropped when gate is off: %v", cfg.Setup)
	}
	if env := cfg.Agents["codex"].Env; len(env) != 0 {
		t.Fatalf("agent env should be dropped when gate is off: %v", env)
	}
	if !cfg.ExperimentalIgnored {
		t.Fatal("ExperimentalIgnored should be true after dropping configured env/setup")
	}

	// SelectAgents must surface a single warning about the ignored hooks.
	_, warnings, err := SelectAgents(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, w := range warnings {
		if strings.Contains(w, "experimental.setup_env") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an experimental.setup_env warning, got %v", warnings)
	}
}

func TestExperimentalGateClearsIgnoredStateWhenEnabled(t *testing.T) {
	cfg := Config{
		Env:   map[string]string{"OPENAI_BASE_URL": "http://proxy:8787"},
		Setup: []SetupCommand{{Name: "proxy", Command: []string{"true"}}},
		Agents: map[string]AgentConfig{
			"codex": {Enabled: true, Command: []string{"codex"}, Env: map[string]string{"X": "y"}},
		},
	}

	cfg.Normalize()
	if !cfg.ExperimentalIgnored {
		t.Fatal("ExperimentalIgnored should be true after dropping configured env/setup")
	}

	cfg.Experimental.SetupEnv = true
	cfg.Env = map[string]string{"OPENAI_BASE_URL": "http://proxy:8787"}
	cfg.Setup = []SetupCommand{{Name: "proxy", Command: []string{"true"}}}
	agent := cfg.Agents["codex"]
	agent.Env = map[string]string{"X": "y"}
	cfg.Agents["codex"] = agent

	cfg.Normalize()
	if cfg.ExperimentalIgnored {
		t.Fatal("ExperimentalIgnored should reset once env/setup are enabled")
	}
}

func TestApplyLocalOverrideEnvAndSetup(t *testing.T) {
	base := Default()
	base.Env = map[string]string{"A": "1"}
	local := []byte(`
env:
  A: "2"
  B: "3"
setup:
  - name: proxy
    command: ["headroom", "proxy", "--port", "8787"]
    background: true
    wait_for_port: 8787
`)
	merged, err := ApplyLocalOverride(base, local)
	if err != nil {
		t.Fatal(err)
	}
	if merged.Env["A"] != "2" || merged.Env["B"] != "3" {
		t.Fatalf("env override = %v, want A=2 B=3", merged.Env)
	}
	if len(merged.Setup) != 1 {
		t.Fatalf("setup not decoded: %+v", merged.Setup)
	}
	sc := merged.Setup[0]
	if sc.Name != "proxy" || !sc.Background || sc.WaitForPort != 8787 ||
		len(sc.Command) != 4 || sc.Command[0] != "headroom" {
		t.Fatalf("setup decoded wrong: %+v", sc)
	}
	if sc.Label() != "proxy" {
		t.Fatalf("Label() = %q, want proxy", sc.Label())
	}
}
