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
