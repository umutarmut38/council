package config

import (
	"reflect"
	"testing"
)

func TestNormalizeUIFillsDefaults(t *testing.T) {
	c := Config{}
	c.normalizeUI()
	if c.UI.Layout != "grid" {
		t.Fatalf("layout = %q, want grid", c.UI.Layout)
	}
	if c.UI.PageRows != 2 || c.UI.PageCols != 2 {
		t.Fatalf("page defaults = %dx%d, want 2x2", c.UI.PageRows, c.UI.PageCols)
	}
	if c.UI.GroupBy != "none" {
		t.Fatalf("group_by = %q, want none", c.UI.GroupBy)
	}
	if c.UI.MaxScrollbackLines != 5000 {
		t.Fatalf("max_scrollback = %d, want 5000", c.UI.MaxScrollbackLines)
	}
	if c.UI.InitialPromptDelayMs != 3000 {
		t.Fatalf("initial_prompt_delay_ms = %d, want 3000", c.UI.InitialPromptDelayMs)
	}
}

func TestNormalizeUICanonicalizes(t *testing.T) {
	// The legacy "paged-grid" alias collapses to "grid"; group_by is lowered.
	c := Config{UI: UIConfig{Layout: "paged-grid", GroupBy: "Category"}}
	c.normalizeUI()
	if c.UI.Layout != "grid" {
		t.Fatalf("paged-grid should canonicalize to grid, got %q", c.UI.Layout)
	}
	if c.UI.GroupBy != "category" {
		t.Fatalf("group_by = %q, want lower-cased category", c.UI.GroupBy)
	}

	// Unknown layout is left untouched so doctor can flag it.
	weird := Config{UI: UIConfig{Layout: "weird"}}
	weird.normalizeUI()
	if weird.UI.Layout != "weird" {
		t.Fatalf("unknown layout should be preserved, got %q", weird.UI.Layout)
	}

	// Unknown group_by falls back to none.
	bogus := Config{UI: UIConfig{GroupBy: "bogus"}}
	bogus.normalizeUI()
	if bogus.UI.GroupBy != "none" {
		t.Fatalf("unknown group_by should fall back to none, got %q", bogus.UI.GroupBy)
	}
}

func TestNormalizeUIPreservesExplicitValues(t *testing.T) {
	c := Config{UI: UIConfig{Layout: "grid", PageRows: 3, PageCols: 4, GroupBy: "personality", MaxScrollbackLines: 1000, InitialPromptDelayMs: 8000}}
	c.normalizeUI()
	want := UIConfig{Layout: "grid", PageRows: 3, PageCols: 4, GroupBy: "personality", MaxScrollbackLines: 1000, InitialPromptDelayMs: 8000}
	if !reflect.DeepEqual(c.UI, want) {
		t.Fatalf("explicit UI values changed: %+v", c.UI)
	}
}

func TestNormalizeSessionsDefault(t *testing.T) {
	c := Config{}
	c.normalizeSessions()
	if c.Sessions.RootDir != ".council/runs" {
		t.Fatalf("root_dir = %q, want .council/runs", c.Sessions.RootDir)
	}
	explicit := Config{Sessions: SessionConfig{RootDir: "/tmp/runs"}}
	explicit.normalizeSessions()
	if explicit.Sessions.RootDir != "/tmp/runs" {
		t.Fatalf("explicit root_dir changed to %q", explicit.Sessions.RootDir)
	}
}

func TestNormalizeTerminalDefaults(t *testing.T) {
	var term TerminalConfig
	normalizeTerminal(&term)
	if term.Renderer != "screen" || term.PTYSize != "pane" || term.Cols != 120 || term.Rows != 40 {
		t.Fatalf("terminal defaults = %+v", term)
	}
	if term.SendMode != "type" || term.SubmitSequence != "cr" {
		t.Fatalf("send defaults = %+v", term)
	}
	if term.Resize == nil || !*term.Resize {
		t.Fatalf("resize default = %v, want true for non-fixed pty", term.Resize)
	}
	if term.Color == nil || !*term.Color {
		t.Fatalf("color default = %v, want true", term.Color)
	}
}

func TestNormalizeTerminalResizeFollowsFixedPTY(t *testing.T) {
	term := TerminalConfig{PTYSize: "fixed"}
	normalizeTerminal(&term)
	if term.Resize == nil || *term.Resize {
		t.Fatalf("fixed pty should default resize to false, got %v", term.Resize)
	}
}

func TestNormalizeTerminalPreservesExplicit(t *testing.T) {
	color := false
	resize := true
	term := TerminalConfig{
		Renderer: "transcript", PTYSize: "fixed", Cols: 100, Rows: 30,
		SendMode: "paste", SubmitSequence: "lf", Resize: &resize, Color: &color,
	}
	normalizeTerminal(&term)
	if term.Renderer != "transcript" || term.PTYSize != "fixed" || term.Cols != 100 || term.Rows != 30 {
		t.Fatalf("explicit terminal values changed: %+v", term)
	}
	if term.SendMode != "paste" || term.SubmitSequence != "lf" {
		t.Fatalf("explicit send values changed: %+v", term)
	}
	if term.Resize == nil || !*term.Resize {
		t.Fatalf("explicit resize not preserved: %v", term.Resize)
	}
	if term.Color == nil || *term.Color {
		t.Fatalf("explicit color not preserved: %v", term.Color)
	}
}

func TestApplyExperimentalGateOnFoldsGlobalEnv(t *testing.T) {
	c := Config{
		Experimental: ExperimentalConfig{SetupEnv: true},
		Env:          map[string]string{"A": "global", "SHARED": "global"},
		Setup:        []SetupCommand{{Command: []string{"true"}}},
		Agents: map[string]AgentConfig{
			"x": {Env: map[string]string{"SHARED": "agent", "B": "agent"}},
			"y": {},
		},
	}
	c.applyExperimentalGate()

	if c.ExperimentalIgnored {
		t.Fatal("gate on should clear ExperimentalIgnored")
	}
	if len(c.Env) == 0 || len(c.Setup) == 0 {
		t.Fatal("gate on should preserve global env and setup")
	}
	x := c.Agents["x"].Env
	if x["A"] != "global" || x["SHARED"] != "agent" || x["B"] != "agent" {
		t.Fatalf("agent env fold wrong: %v", x)
	}
	if c.Agents["y"].Env["A"] != "global" {
		t.Fatalf("agent without its own env should receive the global env: %v", c.Agents["y"].Env)
	}
}

func TestApplyExperimentalGateOnKeepsEnvlessAgentNil(t *testing.T) {
	c := Config{
		Experimental: ExperimentalConfig{SetupEnv: true},
		Agents:       map[string]AgentConfig{"y": {}},
	}
	c.applyExperimentalGate()
	if c.Agents["y"].Env != nil {
		t.Fatalf("an agent with no env should stay nil, got %v", c.Agents["y"].Env)
	}
}

func TestApplyExperimentalGateOffDropsEverything(t *testing.T) {
	c := Config{
		Env:   map[string]string{"A": "1"},
		Setup: []SetupCommand{{Command: []string{"true"}}},
		Agents: map[string]AgentConfig{
			"x": {Env: map[string]string{"B": "2"}},
		},
	}
	c.applyExperimentalGate()
	if len(c.Env) != 0 || len(c.Setup) != 0 {
		t.Fatalf("gate off should drop env/setup: env=%v setup=%v", c.Env, c.Setup)
	}
	if c.Agents["x"].Env != nil {
		t.Fatalf("gate off should drop agent env, got %v", c.Agents["x"].Env)
	}
	if !c.ExperimentalIgnored {
		t.Fatal("gate off with configured env/setup should set ExperimentalIgnored")
	}
}

func TestApplyExperimentalGateOffWithNothingConfigured(t *testing.T) {
	c := Config{Agents: map[string]AgentConfig{"x": {}}}
	c.applyExperimentalGate()
	if c.ExperimentalIgnored {
		t.Fatal("nothing configured should not flag ExperimentalIgnored")
	}
}

func TestMergeEnv(t *testing.T) {
	if got := mergeEnv(nil, nil); got != nil {
		t.Fatalf("merging two empty maps should stay nil, got %v", got)
	}
	got := mergeEnv(map[string]string{"A": "base", "C": "base"}, map[string]string{"A": "over", "B": "over"})
	want := map[string]string{"A": "over", "B": "over", "C": "base"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeEnv = %v, want %v", got, want)
	}
	baseOnly := mergeEnv(map[string]string{"A": "1"}, nil)
	if !reflect.DeepEqual(baseOnly, map[string]string{"A": "1"}) {
		t.Fatalf("base-only merge = %v", baseOnly)
	}
}
