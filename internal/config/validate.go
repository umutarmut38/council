package config

import (
	"fmt"
	"sort"
	"strings"
)

// Validate checks a config for structural problems that would make a run
// misbehave. Call it on a normalized config (Load and SelectAgents normalize
// first). It returns the first hard error found, or nil when the config is
// usable.
//
// Validate is deliberately stricter than the lenient coercions in Normalize: it
// is the gate behind the documented validation examples and `council doctor
// --fix`, surfacing typos the runtime would otherwise paper over — an unknown
// policy.mode, an enabled agent with no command, or an unknown terminal
// renderer/send_mode/pty_size.
func (c Config) Validate() error {
	if err := ValidateAgentNames(c); err != nil {
		return err
	}
	if err := validateAgentInheritance(c); err != nil {
		return err
	}

	names := make([]string, 0, len(c.Agents))
	for name := range c.Agents {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		agent := c.Agents[name]
		if agent.Enabled && len(agent.Command) == 0 {
			return fmt.Errorf("agent %q is enabled but has no command", name)
		}
		if err := validateTerminal(name, agent.Terminal); err != nil {
			return err
		}
	}

	if mode := strings.TrimSpace(c.Policy.Mode); mode != "" && !knownPolicyMode(mode) {
		return fmt.Errorf("policy.mode %q is unknown (use safe|normal|aggressive)", c.Policy.Mode)
	}
	return nil
}

// validateAgentInheritance reports a broken `inherit` graph: a base that does
// not exist, an agent that inherits from itself, or a cycle. Resolution leaves
// `inherit` in place, so the graph is still walkable here.
func validateAgentInheritance(c Config) error {
	names := make([]string, 0, len(c.Agents))
	for name := range c.Agents {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		seen := map[string]bool{name: true}
		cur := name
		for depth := 0; ; depth++ {
			base := c.Agents[cur].Inherit
			if base == "" {
				break
			}
			if base == cur {
				return fmt.Errorf("agent %q cannot inherit from itself", cur)
			}
			if _, ok := c.Agents[base]; !ok {
				return fmt.Errorf("agent %q inherits from unknown agent %q", cur, base)
			}
			if seen[base] {
				return fmt.Errorf("agent %q has a circular inherit chain", name)
			}
			// Resolution stops after maxInheritDepth levels; report an over-deep
			// (but acyclic) chain rather than leaving it silently half-resolved.
			if depth >= maxInheritDepth {
				return fmt.Errorf("agent %q has an inherit chain deeper than %d", name, maxInheritDepth)
			}
			seen[base] = true
			cur = base
		}
	}
	return nil
}

func knownPolicyMode(mode string) bool {
	switch strings.ToLower(mode) {
	case PolicySafe, PolicyNormal, PolicyAggressive:
		return true
	default:
		return false
	}
}

func validateTerminal(agent string, t TerminalConfig) error {
	switch strings.ToLower(strings.TrimSpace(t.Renderer)) {
	case "", "screen", "transcript":
	default:
		return fmt.Errorf("agent %q: unknown terminal.renderer %q (use screen|transcript)", agent, t.Renderer)
	}
	switch strings.ToLower(strings.TrimSpace(t.SendMode)) {
	case "", "type", "paste":
	default:
		return fmt.Errorf("agent %q: unknown terminal.send_mode %q (use type|paste)", agent, t.SendMode)
	}
	switch strings.ToLower(strings.TrimSpace(t.PTYSize)) {
	case "", "pane", "fixed":
	default:
		return fmt.Errorf("agent %q: unknown terminal.pty_size %q (use pane|fixed)", agent, t.PTYSize)
	}
	return nil
}
