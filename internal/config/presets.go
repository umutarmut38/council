package config

import (
	"fmt"
	"sort"
	"strings"
)

// Agent presets encode the terminal quirks of the common agent CLIs in one
// place, so adding a tool doesn't require hand-tuning YAML. Presets ship
// disabled and without auto-approval flags; AutoApproveCommand records the
// flags a user can opt into per phase.

type presetInfo struct {
	agent AgentConfig
	// autoApprove is the phase command (with auto-approval flags) users can
	// opt into for unattended plan/vote/build phases. Never applied by default.
	autoApprove []string
}

var agentPresets = map[string]presetInfo{
	"claude": {
		agent: AgentConfig{
			Command: []string{"claude"},
			CWD:     ".",
			Terminal: TerminalConfig{
				Renderer:       "screen",
				PTYSize:        "pane",
				SendMode:       "type",
				SubmitSequence: "cr",
				SubmitDelayMs:  250,
			},
		},
		autoApprove: []string{"claude", "--dangerously-skip-permissions"},
	},
	"codex": {
		agent: AgentConfig{
			Command: []string{"codex"},
			CWD:     ".",
			Terminal: TerminalConfig{
				Renderer:           "screen",
				PTYSize:            "pane",
				Cols:               120,
				Rows:               40,
				SendMode:           "paste",
				BeforeSendSequence: "ctrl+u",
				SubmitSequence:     "cr",
			},
		},
		autoApprove: []string{"codex", "--full-auto"},
	},
	"cursor": {
		agent: AgentConfig{
			Command: []string{"cursor-agent"},
			CWD:     ".",
			Terminal: TerminalConfig{
				Renderer:       "screen",
				PTYSize:        "pane",
				SendMode:       "type",
				SubmitSequence: "cr",
				SubmitDelayMs:  250,
			},
		},
		autoApprove: []string{"cursor-agent", "--force"},
	},
	"copilot": {
		agent: AgentConfig{
			Command: []string{"copilot"},
			CWD:     ".",
			Terminal: TerminalConfig{
				Renderer:       "screen",
				PTYSize:        "pane",
				SendMode:       "type",
				SubmitSequence: "cr",
				SubmitDelayMs:  250,
			},
			// Copilot is less suited to parallel build worktrees.
			Orchestration: OrchestrationConfig{ExcludeBuild: true},
		},
		autoApprove: []string{"copilot", "--allow-all-tools"},
	},
	"opencode": {
		agent: AgentConfig{
			Command: []string{"opencode"},
			CWD:     ".",
			Terminal: TerminalConfig{
				Renderer:       "screen",
				PTYSize:        "pane",
				SendMode:       "type",
				SubmitSequence: "cr",
			},
		},
	},
}

// PresetNames lists the known agent presets, sorted.
func PresetNames() []string {
	names := make([]string, 0, len(agentPresets))
	for name := range agentPresets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// AgentPreset returns a disabled, safe-by-default config for a known agent CLI.
func AgentPreset(name string) (AgentConfig, bool) {
	info, ok := agentPresets[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return AgentConfig{}, false
	}
	agent := info.agent
	agent.Enabled = false
	return agent, true
}

// PresetAutoApproveCommand returns the opt-in auto-approval phase command for
// a known preset ("" command slice if none is known).
func PresetAutoApproveCommand(name string) []string {
	info, ok := agentPresets[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return nil
	}
	return append([]string(nil), info.autoApprove...)
}

// riskyFlags are command flags that bypass tool/permission prompts. They are
// legitimate for sandboxed runs but should always be a visible, deliberate
// choice.
var riskyFlags = []string{
	"--dangerously-skip-permissions",
	"--allow-all-tools",
	"--full-auto",
	"--force",
	"--yolo",
	"--auto-approve",
	"--dangerously-bypass-approvals-and-sandbox",
}

// RiskyCommandFlags returns the known risky auto-approval flags found in cmd.
func RiskyCommandFlags(cmd []string) []string {
	var found []string
	for _, arg := range cmd {
		for _, flag := range riskyFlags {
			if arg == flag {
				found = append(found, flag)
			}
		}
	}
	return found
}

// AgentRiskyFlags reports the risky flags configured anywhere in an agent's
// commands, keyed by which command carries them.
func AgentRiskyFlags(agent AgentConfig) map[string][]string {
	out := map[string][]string{}
	add := func(label string, cmd []string) {
		if flags := RiskyCommandFlags(cmd); len(flags) > 0 {
			out[label] = flags
		}
	}
	add("command", agent.Command)
	add("plan_command", agent.Orchestration.PlanCommand)
	add("vote_command", agent.Orchestration.VoteCommand)
	add("build_command", agent.Orchestration.BuildCommand)
	return out
}

// ValidateAgentNames rejects agent names that would produce colliding or
// surprising artifact, branch, and worktree names. Names must match
// [A-Za-z0-9_-]+ and stay unique after safe-name normalization (e.g. "a/b"
// and "a_b" would collide).
func ValidateAgentNames(cfg Config) error {
	seen := map[string]string{}
	names := make([]string, 0, len(cfg.Agents))
	for name := range cfg.Agents {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !validAgentName(name) {
			return fmt.Errorf("invalid agent name %q: use only letters, digits, '-' and '_'", name)
		}
		key := strings.ToLower(name)
		if other, dup := seen[key]; dup {
			return fmt.Errorf("agent names %q and %q collide after normalization", other, name)
		}
		seen[key] = name
	}
	return nil
}

func validAgentName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}
