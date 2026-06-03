package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type AgentConfig struct {
	Enabled       bool                `yaml:"enabled"`
	Command       []string            `yaml:"command"`
	CWD           string              `yaml:"cwd"`
	Personality   string              `yaml:"personality,omitempty"`
	Role          []string            `yaml:"role,omitempty"`
	Terminal      TerminalConfig      `yaml:"terminal"`
	Orchestration OrchestrationConfig `yaml:"orchestration,omitempty"`
}

// Orchestration roles. Roles are structural (who builds vs. who judges) and are
// orthogonal to personalities (which only inject prompt text). An agent's role
// list selects which phases it participates in:
//
//	worker   -> plan, build      (produces the work)
//	reviewer -> vote, review     (judges the work)
//
// An empty role list means the agent has both roles (backward compatible).
const (
	RoleWorker   = "worker"
	RoleReviewer = "reviewer"
)

// HasRole reports whether the agent has a role. An empty role list defaults to
// all roles, so existing configs (no role field) behave exactly as before.
func (a AgentConfig) HasRole(role string) bool {
	if len(a.Role) == 0 {
		return true
	}
	for _, r := range a.Role {
		if strings.EqualFold(strings.TrimSpace(r), role) {
			return true
		}
	}
	return false
}

// OrchestrationConfig controls how an agent participates in the plan/vote/build
// phases. exclude disables all phases, while exclude_plan/exclude_vote/
// exclude_build disable one phase. Each *_command launches the agent
// interactively for that phase (the phases run in live panes), and should enable
// auto-approval so the agent can write its artifact file / edit code without
// blocking on permission prompts. An empty phase command falls back to the
// agent's normal Command.
type OrchestrationConfig struct {
	Exclude      bool     `yaml:"exclude,omitempty"`
	ExcludePlan  bool     `yaml:"exclude_plan,omitempty"`
	ExcludeVote  bool     `yaml:"exclude_vote,omitempty"`
	ExcludeBuild bool     `yaml:"exclude_build,omitempty"`
	PlanCommand  []string `yaml:"plan_command,omitempty"`
	VoteCommand  []string `yaml:"vote_command,omitempty"`
	BuildCommand []string `yaml:"build_command,omitempty"`
	// *_prompt_in_command appends the phase prompt as the final argv element
	// instead of typing it into the interactive TUI. This is useful for agents
	// with reliable non-interactive prompt flags such as `-p`.
	PlanPromptInCommand  bool `yaml:"plan_prompt_in_command,omitempty"`
	VotePromptInCommand  bool `yaml:"vote_prompt_in_command,omitempty"`
	BuildPromptInCommand bool `yaml:"build_prompt_in_command,omitempty"`
}

// Phase identifies an orchestration step.
type Phase string

const (
	PhasePlan   Phase = "plan"
	PhaseVote   Phase = "vote"
	PhaseBuild  Phase = "build"
	PhaseReview Phase = "review"
)

// CommandForPhase returns the launch command for the given phase, falling back
// to the agent's normal Command when no phase-specific command is configured.
func (a AgentConfig) CommandForPhase(phase Phase) []string {
	var cmd []string
	switch phase {
	case PhasePlan:
		cmd = a.Orchestration.PlanCommand
	case PhaseVote, PhaseReview:
		// Reviewing built diffs is a read-and-write-a-file step, like voting.
		cmd = a.Orchestration.VoteCommand
	case PhaseBuild:
		cmd = a.Orchestration.BuildCommand
	}
	if len(cmd) > 0 {
		return cmd
	}
	return a.Command
}

func (a AgentConfig) PromptInCommandForPhase(phase Phase) bool {
	switch phase {
	case PhasePlan:
		return a.Orchestration.PlanPromptInCommand
	case PhaseVote, PhaseReview:
		return a.Orchestration.VotePromptInCommand
	case PhaseBuild:
		return a.Orchestration.BuildPromptInCommand
	default:
		return false
	}
}

// ParticipatesIn reports whether the agent should be launched for a phase.
func (a AgentConfig) ParticipatesIn(phase Phase) bool {
	if a.Orchestration.Exclude {
		return false
	}
	// Role selects the phases; the legacy exclude_* flags remain as overrides.
	switch phase {
	case PhasePlan:
		return !a.Orchestration.ExcludePlan && a.HasRole(RoleWorker)
	case PhaseBuild:
		return !a.Orchestration.ExcludeBuild && a.HasRole(RoleWorker)
	case PhaseVote, PhaseReview:
		return !a.Orchestration.ExcludeVote && a.HasRole(RoleReviewer)
	default:
		return true
	}
}

type TerminalConfig struct {
	Renderer            string `yaml:"renderer"`
	PTYSize             string `yaml:"pty_size"`
	Cols                int    `yaml:"cols"`
	Rows                int    `yaml:"rows"`
	SendMode            string `yaml:"send_mode"`
	BeforeSendSequence  string `yaml:"before_send_sequence,omitempty"`
	SubmitSequence      string `yaml:"submit_sequence"`
	AfterSubmitSequence string `yaml:"after_submit_sequence,omitempty"`
	SubmitDelayMs       int    `yaml:"submit_delay_ms,omitempty"`
	Resize              *bool  `yaml:"resize,omitempty"`
	Color               *bool  `yaml:"color,omitempty"`
}

type UIConfig struct {
	Layout             string `yaml:"layout"`
	MaxScrollbackLines int    `yaml:"max_scrollback_lines"`
	PageRows           int    `yaml:"page_rows,omitempty"`
	PageCols           int    `yaml:"page_cols,omitempty"`
	GroupBy            string `yaml:"group_by,omitempty"`
	// InitialPromptDelayMs is how long to wait after launch before broadcasting
	// the `council ask` prompt, giving each agent's TUI time to finish booting
	// and start accepting input. Too short and the prompt is dropped.
	InitialPromptDelayMs int `yaml:"initial_prompt_delay_ms"`
}

type SessionConfig struct {
	RootDir string `yaml:"root_dir"`
}

// ReviewConfig controls the post-build review phase.
type ReviewConfig struct {
	// CheckCommand is run in each agent's build worktree to gate implementations
	// before voting; an implementation whose check fails is dropped. It is the
	// only language-specific part of review and is empty by default (no gate —
	// every changed implementation goes to the vote), so review is repo-agnostic
	// out of the box. Set it to your project's build/test command to enable the
	// gate, e.g. ["go","build","./..."], ["npm","test"], ["cargo","build"].
	CheckCommand []string `yaml:"check_command,omitempty"`
}

type PersonalityCategoryConfig struct {
	Label string `yaml:"label,omitempty"`
	Color string `yaml:"color,omitempty"`
	Order int    `yaml:"order,omitempty"`
}

type PersonalityConfig struct {
	Label        string `yaml:"label,omitempty"`
	Category     string `yaml:"category,omitempty"`
	Color        string `yaml:"color,omitempty"`
	Order        int    `yaml:"order,omitempty"`
	PromptPrefix string `yaml:"prompt_prefix,omitempty"`
}

type Config struct {
	Agents                map[string]AgentConfig               `yaml:"agents"`
	UI                    UIConfig                             `yaml:"ui"`
	Sessions              SessionConfig                        `yaml:"sessions"`
	Review                ReviewConfig                         `yaml:"review"`
	PersonalityCategories map[string]PersonalityCategoryConfig `yaml:"personality_categories,omitempty"`
	Personalities         map[string]PersonalityConfig         `yaml:"personalities,omitempty"`
}

type AgentSpec struct {
	Name   string
	Config AgentConfig
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".council.yaml"), nil
}

func Default() Config {
	return Config{
		Agents: map[string]AgentConfig{
			"claude": {
				Enabled: true,
				Command: []string{"claude"},
				CWD:     ".",
				Terminal: TerminalConfig{
					Renderer:       "screen",
					PTYSize:        "pane",
					SendMode:       "type",
					SubmitSequence: "cr",
					SubmitDelayMs:  250,
				},
				// Claude stays interactive for every phase; it posts a typed
				// prompt fine once loaded, and the delayed submit lands the Enter.
				Orchestration: OrchestrationConfig{
					PlanCommand:  []string{"claude", "--dangerously-skip-permissions"},
					VoteCommand:  []string{"claude", "--dangerously-skip-permissions"},
					BuildCommand: []string{"claude", "--dangerously-skip-permissions"},
				},
			},
			"codex": {
				Enabled: true,
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
			"cursor": {
				Enabled: true,
				Command: []string{"cursor-agent"},
				CWD:     ".",
				Terminal: TerminalConfig{
					Renderer:       "screen",
					PTYSize:        "pane",
					SendMode:       "type",
					SubmitSequence: "cr",
					SubmitDelayMs:  250,
				},
				Orchestration: OrchestrationConfig{
					PlanCommand:  []string{"cursor-agent", "--force"},
					VoteCommand:  []string{"cursor-agent", "--force"},
					BuildCommand: []string{"cursor-agent", "--force"},
				},
			},
			"copilot": {
				Enabled: true,
				Command: []string{"gh", "copilot"},
				CWD:     ".",
				Terminal: TerminalConfig{
					Renderer:       "screen",
					PTYSize:        "pane",
					SendMode:       "type",
					SubmitSequence: "cr",
					SubmitDelayMs:  250,
				},
				// Copilot stays interactive and posts via a typed prompt with a
				// delayed submit (like cursor). It is excluded from build by
				// default as it is less suited to parallel build worktrees.
				Orchestration: OrchestrationConfig{
					PlanCommand:  []string{"copilot", "--allow-all-tools"},
					VoteCommand:  []string{"copilot", "--allow-all-tools"},
					ExcludeBuild: true,
				},
			},
		},
		UI: UIConfig{
			Layout:               "grid",
			MaxScrollbackLines:   5000,
			PageRows:             2,
			PageCols:             2,
			GroupBy:              "none",
			InitialPromptDelayMs: 3000,
		},
		Sessions: SessionConfig{
			RootDir: ".council/runs",
		},
		// CheckCommand left empty: review is language-agnostic by default. Set
		// review.check_command in ~/.council.yaml to gate builds for your stack.
		Review: ReviewConfig{},
	}
}

func Load(path string) (Config, []byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, nil, err
	}

	cfg := Default()
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, nil, fmt.Errorf("load config: %w", err)
	}
	mergeDefaultAgentOrchestration(&cfg, raw, Default().Agents)
	cfg.Normalize()
	return cfg, raw, nil
}

// localConfigNames are the repo-local override filenames searched from the
// current directory up to the git repo root.
var localConfigNames = []string{".council.yaml", ".council.yml"}

// FindLocalConfig returns the path to a repo-local override config, or "" if
// none is found. It searches from the current directory upward, stopping at the
// git repo root, and never returns the global config path.
func FindLocalConfig() string {
	globalAbs := ""
	if p, err := DefaultPath(); err == nil {
		globalAbs, _ = filepath.Abs(p)
	}

	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		for _, name := range localConfigNames {
			p := filepath.Join(dir, name)
			if abs, _ := filepath.Abs(p); abs == globalAbs {
				continue
			}
			if fi, statErr := os.Stat(p); statErr == nil && !fi.IsDir() {
				return p
			}
		}
		if fi, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil && fi.IsDir() {
			return "" // reached the repo root without finding one
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// ApplyLocal layers a repo-local config (if present) over cfg, returning the
// merged config and the path that was applied ("" if none). Keys set locally
// override the global ones; everything else falls through.
func ApplyLocal(cfg Config) (Config, string, error) {
	path := FindLocalConfig()
	if path == "" {
		return cfg, "", nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, "", err
	}
	merged, err := ApplyLocalOverride(cfg, raw)
	if err != nil {
		return cfg, "", fmt.Errorf("%s: %w", path, err)
	}
	merged.Normalize()
	return merged, path, nil
}

// ApplyLocalOverride merges a local config document onto cfg. Top-level sections
// (ui, sessions, review) and each agent are deep-merged, so a local file only
// needs to specify the keys it wants to change.
func ApplyLocalOverride(cfg Config, raw []byte) (Config, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return cfg, fmt.Errorf("parse local config: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return cfg, nil
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value
		val := root.Content[i+1]
		switch key {
		case "ui":
			if err := val.Decode(&cfg.UI); err != nil {
				return cfg, err
			}
		case "sessions":
			if err := val.Decode(&cfg.Sessions); err != nil {
				return cfg, err
			}
		case "review":
			if err := val.Decode(&cfg.Review); err != nil {
				return cfg, err
			}
		case "personality_categories":
			if err := mergePersonalityCategories(&cfg, val); err != nil {
				return cfg, err
			}
		case "personalities":
			if err := mergePersonalities(&cfg, val); err != nil {
				return cfg, err
			}
		case "agents":
			if val.Kind != yaml.MappingNode {
				continue
			}
			if cfg.Agents == nil {
				cfg.Agents = map[string]AgentConfig{}
			}
			for j := 0; j+1 < len(val.Content); j += 2 {
				name := val.Content[j].Value
				// Decode onto the global agent so unspecified fields are kept.
				merged := cfg.Agents[name]
				if err := val.Content[j+1].Decode(&merged); err != nil {
					return cfg, err
				}
				cfg.Agents[name] = merged
			}
		}
	}
	return cfg, nil
}

func mergePersonalityCategories(cfg *Config, val *yaml.Node) error {
	if val.Kind != yaml.MappingNode {
		return nil
	}
	if cfg.PersonalityCategories == nil {
		cfg.PersonalityCategories = map[string]PersonalityCategoryConfig{}
	}
	for i := 0; i+1 < len(val.Content); i += 2 {
		name := val.Content[i].Value
		merged := cfg.PersonalityCategories[name]
		if err := val.Content[i+1].Decode(&merged); err != nil {
			return err
		}
		cfg.PersonalityCategories[name] = merged
	}
	return nil
}

func mergePersonalities(cfg *Config, val *yaml.Node) error {
	if val.Kind != yaml.MappingNode {
		return nil
	}
	if cfg.Personalities == nil {
		cfg.Personalities = map[string]PersonalityConfig{}
	}
	for i := 0; i+1 < len(val.Content); i += 2 {
		name := val.Content[i].Value
		merged := cfg.Personalities[name]
		if err := val.Content[i+1].Decode(&merged); err != nil {
			return err
		}
		cfg.Personalities[name] = merged
	}
	return nil
}

func mergeDefaultAgentOrchestration(cfg *Config, raw []byte, defaults map[string]AgentConfig) {
	explicit := explicitAgentOrchestration(raw)
	for name, def := range defaults {
		if explicit[name] {
			continue
		}
		agentCfg, ok := cfg.Agents[name]
		if !ok {
			continue
		}
		agentCfg.Orchestration = def.Orchestration
		cfg.Agents[name] = agentCfg
	}
}

func explicitAgentOrchestration(raw []byte) map[string]bool {
	explicit := map[string]bool{}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil || len(doc.Content) == 0 {
		return explicit
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return explicit
	}
	agents := mappingValue(root, "agents")
	if agents == nil || agents.Kind != yaml.MappingNode {
		return explicit
	}
	for i := 0; i+1 < len(agents.Content); i += 2 {
		name := agents.Content[i].Value
		body := agents.Content[i+1]
		explicit[name] = mappingValue(body, "orchestration") != nil
	}
	return explicit
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func WriteDefault(path string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; pass --force to overwrite", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	raw, err := yaml.Marshal(Default())
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func SelectAgents(cfg Config, overrides []string) ([]AgentSpec, []string, error) {
	cfg.Normalize()

	if len(overrides) > 0 {
		selected := make([]AgentSpec, 0, len(overrides))
		for _, name := range overrides {
			agentCfg, ok := cfg.Agents[name]
			if !ok {
				return nil, nil, fmt.Errorf("agent %q is not configured", name)
			}
			selected = append(selected, AgentSpec{Name: name, Config: agentCfg})
		}
		return selected, nil, nil
	}

	names := make([]string, 0, len(cfg.Agents))
	for name, agentCfg := range cfg.Agents {
		if agentCfg.Enabled {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	selected := make([]AgentSpec, 0, len(names))
	warnings := make([]string, 0)
	for _, name := range names {
		agentCfg := cfg.Agents[name]
		if len(agentCfg.Command) == 0 {
			warnings = append(warnings, fmt.Sprintf("warning: enabled agent %q has no command and will be skipped", name))
			continue
		}
		selected = append(selected, AgentSpec{Name: name, Config: agentCfg})
	}
	return selected, warnings, nil
}

func (c Config) PersonalityForAgent(agentName string) (string, PersonalityConfig, bool) {
	agentCfg, ok := c.Agents[agentName]
	if !ok || strings.TrimSpace(agentCfg.Personality) == "" {
		return "", PersonalityConfig{}, false
	}
	name := strings.TrimSpace(agentCfg.Personality)
	personality, ok := c.Personalities[name]
	return name, personality, ok
}

func (c Config) CategoryForPersonality(personalityName string) (string, PersonalityCategoryConfig, bool) {
	personality, ok := c.Personalities[personalityName]
	if !ok || strings.TrimSpace(personality.Category) == "" {
		return "", PersonalityCategoryConfig{}, false
	}
	name := strings.TrimSpace(personality.Category)
	category, ok := c.PersonalityCategories[name]
	return name, category, ok
}

func (c Config) AgentPromptPrefix(agentName string) string {
	_, personality, ok := c.PersonalityForAgent(agentName)
	if !ok {
		return ""
	}
	return strings.TrimSpace(personality.PromptPrefix)
}

func (c Config) PromptForAgent(agentName string, prompt string) string {
	if strings.TrimSpace(prompt) == "" {
		return prompt
	}
	prefix := c.AgentPromptPrefix(agentName)
	if prefix == "" {
		return prompt
	}
	return prefix + "\n\n" + strings.TrimLeft(prompt, "\n")
}

func (c *Config) Normalize() {
	if c.Agents == nil {
		c.Agents = map[string]AgentConfig{}
	}
	if c.UI.Layout == "" {
		c.UI.Layout = "grid"
	}
	if c.UI.PageRows <= 0 {
		c.UI.PageRows = 2
	}
	if c.UI.PageCols <= 0 {
		c.UI.PageCols = 2
	}
	switch strings.ToLower(strings.TrimSpace(c.UI.GroupBy)) {
	case "", "none", "personality", "category":
		if strings.TrimSpace(c.UI.GroupBy) == "" {
			c.UI.GroupBy = "none"
		} else {
			c.UI.GroupBy = strings.ToLower(strings.TrimSpace(c.UI.GroupBy))
		}
	default:
		c.UI.GroupBy = "none"
	}
	if c.UI.MaxScrollbackLines <= 0 {
		c.UI.MaxScrollbackLines = 5000
	}
	if c.UI.InitialPromptDelayMs <= 0 {
		c.UI.InitialPromptDelayMs = 3000
	}
	if c.Sessions.RootDir == "" {
		c.Sessions.RootDir = ".council/runs"
	}
	// Only tool-agnostic fallbacks live here. Per-agent behavior (how to type,
	// how to submit, paste vs raw, delays, etc.) is configured entirely from
	// ~/.council.yaml so new agents can be supported without code changes.
	for name, agentCfg := range c.Agents {
		if agentCfg.CWD == "" {
			agentCfg.CWD = "."
		}
		if agentCfg.Terminal.Renderer == "" {
			agentCfg.Terminal.Renderer = "screen"
		}
		if agentCfg.Terminal.PTYSize == "" {
			agentCfg.Terminal.PTYSize = "pane"
		}
		if agentCfg.Terminal.Cols <= 0 {
			agentCfg.Terminal.Cols = 120
		}
		if agentCfg.Terminal.Rows <= 0 {
			agentCfg.Terminal.Rows = 40
		}
		if agentCfg.Terminal.SendMode == "" {
			agentCfg.Terminal.SendMode = "type"
		}
		if agentCfg.Terminal.SubmitSequence == "" {
			agentCfg.Terminal.SubmitSequence = "cr"
		}
		if agentCfg.Terminal.Resize == nil {
			value := agentCfg.Terminal.PTYSize != "fixed"
			agentCfg.Terminal.Resize = &value
		}
		if agentCfg.Terminal.Color == nil {
			value := true
			agentCfg.Terminal.Color = &value
		}
		c.Agents[name] = agentCfg
	}
}
