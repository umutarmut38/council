package config

import (
	"os"
	"path/filepath"
	"reflect"
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

func TestDefaultOrchestrationPresets(t *testing.T) {
	d := Default().Agents
	// Claude and Copilot stay interactive in every phase: no -p, prompt
	// delivered through the live pane (not argv).
	if got := d["claude"].CommandForPhase(PhasePlan); !reflect.DeepEqual(got, []string{"claude", "--dangerously-skip-permissions"}) {
		t.Fatalf("claude plan command = %v", got)
	}
	if d["claude"].PromptInCommandForPhase(PhasePlan) {
		t.Fatal("claude should receive plan prompts interactively, not via argv")
	}
	if !d["copilot"].ParticipatesIn(PhasePlan) || !d["copilot"].ParticipatesIn(PhaseVote) {
		t.Fatal("copilot should participate in planning and voting by default")
	}
	if d["copilot"].ParticipatesIn(PhaseBuild) {
		t.Fatal("copilot should be excluded from build by default")
	}
	if got := d["copilot"].CommandForPhase(PhasePlan); !reflect.DeepEqual(got, []string{"copilot", "--allow-all-tools"}) {
		t.Fatalf("copilot plan command = %v", got)
	}
	if d["copilot"].PromptInCommandForPhase(PhasePlan) {
		t.Fatal("copilot should receive plan prompts interactively, not via argv")
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
	if got := copilot.CommandForPhase(PhasePlan); !reflect.DeepEqual(got, []string{"copilot", "--allow-all-tools"}) {
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

func TestApplyLocalOverrideDeepMerges(t *testing.T) {
	base := Default()
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

	cursor := merged.Agents["cursor"]
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

	if !worker.HasRole(RoleWorker) || worker.HasRole(RoleReviewer) {
		t.Fatal("worker should have only the worker role")
	}
	if !def.HasRole(RoleWorker) || !def.HasRole(RoleReviewer) {
		t.Fatal("empty role must default to all roles")
	}

	cases := []struct {
		name                      string
		a                         AgentConfig
		plan, vote, build, review bool
	}{
		{"worker", worker, true, false, true, false},
		{"reviewer", reviewer, false, true, false, true},
		{"both", both, true, true, true, true},
		{"default", def, true, true, true, true},
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
