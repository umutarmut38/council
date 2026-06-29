package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
	runstore "github.com/umutarmut38/council/internal/session"
	"github.com/umutarmut38/council/internal/usage"
)

func TestUsageMetadataIgnoresCommand(t *testing.T) {
	a := config.AgentConfig{Command: []string{"claude", "--model", "haiku"}}
	if tool, _, conf := usageTool(a); tool != usage.UnknownValue || conf != usage.Unknown {
		t.Fatalf("usageTool parsed command = %q/%q, want unknown", tool, conf)
	}
	if model, _, conf := usageModel(a); model != usage.UnknownValue || conf != usage.Unknown {
		t.Fatalf("usageModel parsed command = %q/%q, want unknown", model, conf)
	}
	a.Usage.Model = "sonnet"
	a.Usage.Tool = "claude"
	if model, _, _ := usageModel(a); model != "sonnet" {
		t.Fatalf("usageModel = %q, want explicit usage.model", model)
	}
	if tool, _, _ := usageTool(a); tool != "claude" {
		t.Fatalf("usageTool = %q, want explicit usage.tool", tool)
	}
}

func TestRecordUsageInputMetersWirePrompt(t *testing.T) {
	cfg := config.Config{
		Usage: usageConfigOn(),
		Agents: map[string]config.AgentConfig{
			"claude": {
				Command: []string{"claude", "--model", "haiku"},
				Usage:   config.AgentUsageConfig{Model: "haiku"},
				Terminal: config.TerminalConfig{
					SendMode:           "paste",
					BeforeSendSequence: "ctrl+u",
					SubmitSequence:     "cr",
				},
				Personality: "architect",
			},
		},
		Personalities: map[string]config.PersonalityConfig{
			"architect": {PromptPrefix: "You are The Architect."},
		},
	}
	cfg.Normalize()
	store := runstore.NewDeferred(t.TempDir(), []byte("cfg"), []byte("{}"))
	session := agent.NewSession("claude", cfg.Agents["claude"], "")
	m := NewModelWithConfig([]*agent.Session{session}, store, cfg, "", nil, time.Millisecond, nil, nil)
	if err := m.ensureRun(); err != nil {
		t.Fatal(err)
	}
	m.recordUsageInput(session, "plan", "Build it.")
	events, err := usage.LoadEvents(store.RunDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	e := events[0]
	if e.Model != "haiku" || e.ModelSource != usage.MetaSourceConfig {
		t.Fatalf("model = %q/%q, want config haiku", e.Model, e.ModelSource)
	}
	if e.Tool != usage.UnknownValue {
		t.Fatalf("tool = %q, want unknown without usage.tool", e.Tool)
	}
	if e.UserInputChars >= e.WireInputChars {
		t.Fatalf("wire chars should include personality/paste/submit bytes: user=%d wire=%d", e.UserInputChars, e.WireInputChars)
	}
	if !strings.Contains(e.PromptPreview, "Architect") || e.PromptHash == "" {
		t.Fatalf("prompt correlation fields missing: %+v", e)
	}
}

func usageConfigOn() config.UsageConfig {
	return config.UsageConfig{Enabled: true, Currency: "USD", Estimator: usage.EstimatorBytes4}
}

// /cost reconciliation rolls up per agent so the live header/badge can show the
// reported cost: a priced reported session, a mixed agent (weakest tier wins),
// and an agent whose model never priced.
func TestRollupReconciled(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	s := usage.Summary{Sessions: []usage.SessionTotal{
		{Agent: "claude-a", Confidence: usage.Reported, Cost: f(0.04), Currency: "USD"},
		{Agent: "claude-a", Confidence: usage.Estimated, Cost: f(0.01), Currency: "USD"},
		{Agent: "codex-a", Confidence: usage.Reported, Cost: nil}, // price unknown
	}}
	rc := rollupReconciled(s)
	a := rc["claude-a"]
	if !a.priced || a.cost < 0.0499 || a.cost > 0.0501 || a.confidence != usage.Estimated {
		t.Fatalf("claude-a = %+v, want priced ~0.05 with weakest (estimated) confidence", a)
	}
	if c := rc["codex-a"]; c.priced || !c.someUnknown {
		t.Fatalf("codex-a = %+v, want unpriced + someUnknown", c)
	}
}

func TestConfidenceSuffix(t *testing.T) {
	for conf, want := range map[string]string{
		usage.Exact: "x", usage.Reported: "r", usage.Estimated: "e", usage.Unknown: "?", "": "?",
	} {
		if got := confidenceSuffix(conf); got != want {
			t.Errorf("confidenceSuffix(%q) = %q, want %q", conf, got, want)
		}
	}
}

// After /cost reconciles, the pane badge shows the reported total (suffix r) and
// the header drops the "est" qualifier — not the stale estimated floor.
func TestBadgeAndHeaderPreferReconciled(t *testing.T) {
	m := Model{
		Config:     config.Config{Usage: usageConfigOn()},
		usageTally: map[string]usage.TokenPair{"claude-a": {Input: 20}},
		usageRate:  map[string]usage.Rate{"claude-a": {Currency: "USD", Found: true}},
		usageReconciled: map[string]reconciledCost{
			"claude-a": {cost: 0.04, currency: "USD", confidence: usage.Reported, priced: true},
		},
	}
	if got := m.usageBorderSuffix("claude-a"); got != " | $0.04r" {
		t.Fatalf("badge = %q, want \" | $0.04r\"", got)
	}
	if got := m.usageHeaderCost(); got != "Run $0.04" {
		t.Fatalf("header = %q, want \"Run $0.04\" (reported, no est)", got)
	}
}

func TestWeakerConfidence(t *testing.T) {
	if weakerConfidence("", usage.Reported) != usage.Reported {
		t.Error(`weakerConfidence("", reported) should seed with reported`)
	}
	if weakerConfidence(usage.Reported, usage.Estimated) != usage.Estimated {
		t.Error("estimated is weaker than reported")
	}
	if weakerConfidence(usage.Exact, usage.Reported) != usage.Reported {
		t.Error("reported is weaker than exact")
	}
}
