package config

import "testing"

func TestFreestyleWorktree(t *testing.T) {
	no, yes := false, true
	base := func(freestyle bool, override *bool) Config {
		return Config{
			Worktrees: WorktreesConfig{Freestyle: freestyle},
			Agents:    map[string]AgentConfig{"a": {Worktree: override}},
		}
	}
	// Feature off → never, regardless of the per-agent override.
	if base(false, nil).FreestyleWorktree("a") || base(false, &yes).FreestyleWorktree("a") {
		t.Error("feature off should never enable a worktree")
	}
	// Feature on, no override → follows the global (on).
	if !base(true, nil).FreestyleWorktree("a") {
		t.Error("feature on with no override should enable")
	}
	// Feature on, opt-out → off for that agent.
	if base(true, &no).FreestyleWorktree("a") {
		t.Error("worktree: false should opt the agent out")
	}
	// Feature on, explicit true → on.
	if !base(true, &yes).FreestyleWorktree("a") {
		t.Error("worktree: true should enable")
	}
	// Unknown agent falls back to the global.
	if !base(true, nil).FreestyleWorktree("missing") {
		t.Error("unknown agent should follow the global default")
	}
}
