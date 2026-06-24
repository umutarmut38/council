package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/umutarmut38/council/internal/cmdrun"
	"github.com/umutarmut38/council/internal/theme"
	"gopkg.in/yaml.v3"
)

type AgentConfig struct {
	Enabled bool `yaml:"enabled"`
	// Inherit names another agent whose definition this agent reuses: every
	// field is copied from the base, then the keys set here override it (see
	// ResolveInheritance). The base is resolved against the fully-merged config,
	// so it may be a preset, a global agent, or another local agent. `enabled`
	// is never inherited — each agent declares its own.
	Inherit string   `yaml:"inherit,omitempty"`
	Command []string `yaml:"command"`
	CWD     string   `yaml:"cwd"`
	// Color tints this agent's pane border and title (a 256-color code such
	// as "212", or an empty string for the default chrome color). Falls back
	// to the agent's personality color when unset.
	Color       string   `yaml:"color,omitempty"`
	Personality string   `yaml:"personality,omitempty"`
	Role        []string `yaml:"role,omitempty"`
	// Env is extra environment for this agent's process, merged over the
	// top-level env (this wins on conflicts). Populated from config; the
	// global env is folded in by Normalize.
	Env           map[string]string   `yaml:"env,omitempty"`
	Terminal      TerminalConfig      `yaml:"terminal"`
	Orchestration OrchestrationConfig `yaml:"orchestration,omitempty"`
}

// Orchestration roles. Roles are structural (which phase an agent joins) and are
// orthogonal to personalities (which only inject prompt text). There is one
// granular role per phase:
//
//	planner  -> plan      (writes a plan)
//	builder  -> build     (implements the winning plan)
//	voter    -> vote      (ranks the anonymized plans)
//	reviewer -> review    (ranks the built diffs)
//
// An empty role list means the agent joins every phase (backward compatible).
//
// The legacy coarse roles still work as aliases, expanded by expandRoles:
//
//	worker   -> planner, builder
//	reviewer -> voter, reviewer   (only when the list is legacy-only)
//
// See expandRoles for the precise legacy/literal rule.
const (
	RolePlanner  = "planner"
	RoleBuilder  = "builder"
	RoleVoter    = "voter"
	RoleReviewer = "reviewer"

	// RoleWorker is a legacy alias for planner+builder. RoleReviewer doubles as
	// the legacy alias for voter+reviewer when no granular token is present.
	RoleWorker = "worker"
)

// expandRoles resolves an agent's role list to canonical per-phase roles. It is
// pure and idempotent. The rule, applied case-insensitively after trimming:
//
//   - Empty list -> empty (means "all phases"; HasRole returns true for any).
//   - Any of planner/builder/voter present -> the whole list is literal and
//     returned as-is. A `reviewer` token then means review-only. Note `reviewer`
//     is deliberately NOT a trigger on its own, because it doubles as the legacy
//     alias below.
//   - Only legacy tokens (worker and/or reviewer, with no planner/builder/voter)
//     -> expand `worker -> planner, builder` and `reviewer -> voter, reviewer`
//     (the legacy judge-both meaning). So a bare `[reviewer]` is vote+review, not
//     review-only.
//
// Idempotency holds because the legacy branch always introduces a trigger token
// (planner or voter), so a second pass takes the literal branch. Mixing a lone
// legacy `worker` with granular tokens leaves it literal (and inert, since it
// matches no phase) — don't mix legacy and granular tokens.
func expandRoles(roles []string) []string {
	if len(roles) == 0 {
		return nil
	}
	hasGranular := false
	for _, r := range roles {
		switch strings.ToLower(strings.TrimSpace(r)) {
		case RolePlanner, RoleBuilder, RoleVoter:
			hasGranular = true
		}
	}
	if hasGranular {
		return roles
	}
	// Legacy-only list: expand the coarse aliases.
	out := make([]string, 0, len(roles)*2)
	for _, r := range roles {
		switch strings.ToLower(strings.TrimSpace(r)) {
		case RoleWorker:
			out = append(out, RolePlanner, RoleBuilder)
		case RoleReviewer:
			out = append(out, RoleVoter, RoleReviewer)
		default:
			out = append(out, r)
		}
	}
	return out
}

// HasRole reports whether the agent participates in the given canonical role.
// An empty role list defaults to all roles, so existing configs (no role field)
// behave exactly as before. Legacy aliases are expanded via expandRoles, so
// HasRole(RolePlanner) is true for both `role: [planner]` and `role: [worker]`.
func (a AgentConfig) HasRole(role string) bool {
	expanded := expandRoles(a.Role)
	if len(expanded) == 0 {
		return true
	}
	for _, r := range expanded {
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
	// PhaseReview shares the exclude_vote override with PhaseVote — there is no
	// exclude_review flag, and a legacy reviewer's exclude_vote disabled both.
	switch phase {
	case PhasePlan:
		return !a.Orchestration.ExcludePlan && a.HasRole(RolePlanner)
	case PhaseBuild:
		return !a.Orchestration.ExcludeBuild && a.HasRole(RoleBuilder)
	case PhaseVote:
		return !a.Orchestration.ExcludeVote && a.HasRole(RoleVoter)
	case PhaseReview:
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
	// AdaptiveGrid sizes the grid to the number of visible panes (1 pane →
	// full screen, 2 → side-by-side full height, 3-4 → 2x2) instead of always
	// using page_rows x page_cols. Defaults to true; page_rows/page_cols still
	// bound the page size for larger rosters, and adjusting rows/cols in the
	// in-app settings locks the layout for that session.
	AdaptiveGrid *bool `yaml:"adaptive_grid,omitempty"`
	// DetectApprovalPrompts (EXPERIMENTAL) flags a pane as "needs input" when
	// an approval-looking prompt sits at the bottom of its screen and the
	// agent has gone quiet. Detection is heuristic; /attention <agent> is the
	// manual, reliable path. Defaults to true; set false to disable.
	DetectApprovalPrompts *bool  `yaml:"detect_approval_prompts,omitempty"`
	GroupBy               string `yaml:"group_by,omitempty"`
	// InitialPromptDelayMs is how long to wait after launch before broadcasting
	// the `council ask` prompt, giving each agent's TUI time to finish booting
	// and start accepting input. Too short and the prompt is dropped.
	InitialPromptDelayMs int `yaml:"initial_prompt_delay_ms"`
	// Editor is the argv (space-split) used to open files: the external editor
	// from `/artifacts`/`/compare` and the integrated `/edit` pane. It takes
	// precedence over $VISUAL/$EDITOR; empty falls through to those and finally
	// to `vim`. e.g. "nvim" or "code -w".
	Editor string `yaml:"editor,omitempty"`
	// EditorOpenCmd is the keystroke template sent to the already-running
	// integrated editor to open a file selected in the tree ({path} is replaced
	// with the file's absolute path, vim-escaped). Nil (unset) uses the vim/nvim
	// default
	// "<Esc>:e {path}<CR>"; an explicit empty string disables in-place opening
	// and relaunches the editor on each file instead (for non-vim editors). A
	// pointer so unset and empty are distinguishable.
	EditorOpenCmd *string `yaml:"editor_open_cmd,omitempty"`
	// Mouse captures the mouse so the wheel scrolls a pane's history (or the
	// active list) and a click focuses a pane. Defaults to true. Mouse capture
	// disables native terminal text selection; it can be toggled at runtime with
	// Ctrl+W. A pointer so unset (default on) and an explicit false are
	// distinguishable.
	Mouse *bool `yaml:"mouse,omitempty"`
	// Theme selects the overall color palette: a built-in name (see
	// theme.BuiltinNames) or a key defined under Themes. Empty defaults to
	// "default" (the historical palette). The per-agent/personality pane color
	// still layers on top.
	Theme string `yaml:"theme,omitempty"`
	// Themes are custom palettes keyed by name. Each names a subset of color
	// roles (title, status, border, …) as a 256-index or "#rrggbb"; unset roles
	// inherit the default palette. Reference one via Theme.
	Themes map[string]theme.Palette `yaml:"themes,omitempty"`
}

// AdaptiveEnabled reports whether the adaptive grid is on (the default).
func (u UIConfig) AdaptiveEnabled() bool {
	return u.AdaptiveGrid == nil || *u.AdaptiveGrid
}

// ApprovalDetectionEnabled reports whether the experimental approval-prompt
// detection is on (the default).
func (u UIConfig) ApprovalDetectionEnabled() bool {
	return u.DetectApprovalPrompts == nil || *u.DetectApprovalPrompts
}

// MouseEnabled reports whether mouse capture starts on (the default).
func (u UIConfig) MouseEnabled() bool {
	return u.Mouse == nil || *u.Mouse
}

type SessionConfig struct {
	RootDir string `yaml:"root_dir"`
	// Private keeps run artifacts (logs, transcripts, prompts, diffs) readable
	// by the owner only (0700 dirs / 0600 files). Defaults to true.
	Private *bool `yaml:"private,omitempty"`
	// Redact rewrites common secret patterns (API keys, tokens, PEM blocks) in
	// saved transcripts. Off by default; redaction is best-effort.
	Redact bool `yaml:"redact,omitempty"`
}

// IsPrivate reports whether run artifacts use owner-only permissions.
func (s SessionConfig) IsPrivate() bool {
	return s.Private == nil || *s.Private
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
	// CheckTimeoutSeconds bounds one check command run; a hung test command
	// would otherwise block review forever. 0 uses the default (600s).
	CheckTimeoutSeconds int `yaml:"check_timeout_seconds,omitempty"`
	// MaxCheckOutputBytes caps a check log. 0 uses the default (1 MiB).
	MaxCheckOutputBytes int `yaml:"max_check_output_bytes,omitempty"`
}

// CheckTimeout returns the effective check command timeout.
func (r ReviewConfig) CheckTimeout() time.Duration {
	if r.CheckTimeoutSeconds > 0 {
		return time.Duration(r.CheckTimeoutSeconds) * time.Second
	}
	return 10 * time.Minute
}

// CheckOutputLimit returns the effective check log size cap in bytes.
func (r ReviewConfig) CheckOutputLimit() int {
	if r.MaxCheckOutputBytes > 0 {
		return r.MaxCheckOutputBytes
	}
	return 1 << 20
}

// FilesConfig constrains @file reference expansion.
type FilesConfig struct {
	// AllowAbsolute permits expanding absolute paths and paths outside the
	// working directory. Off by default so a pasted task can't quietly inline
	// ~/.ssh/id_rsa or /etc/passwd into an agent prompt.
	AllowAbsolute bool `yaml:"allow_absolute,omitempty"`
	// MaxBytes caps the size of a single expanded file. 0 uses the default
	// (256 KiB).
	MaxBytes int `yaml:"max_bytes,omitempty"`
}

// MaxRefBytes returns the effective per-file expansion cap.
func (f FilesConfig) MaxRefBytes() int {
	if f.MaxBytes > 0 {
		return f.MaxBytes
	}
	return 256 << 10
}

// Policy modes bundle the risk posture for automation features.
const (
	PolicySafe       = "safe"
	PolicyNormal     = "normal"
	PolicyAggressive = "aggressive"
)

// PolicyConfig selects how much automation council allows without asking.
type PolicyConfig struct {
	// Mode is one of safe, normal (default), aggressive.
	//   safe:       refuse risky auto-approval phase flags, never expand
	//               absolute @file refs, always confirm adopt/clean.
	//   normal:     warn about risky flags, confirm adopt/clean.
	//   aggressive: allow auto-approval flags and skip confirmations; meant
	//               for sandboxed or fully-trusted environments.
	Mode string `yaml:"mode,omitempty"`
}

func (p PolicyConfig) Normalized() string {
	switch strings.ToLower(strings.TrimSpace(p.Mode)) {
	case PolicySafe:
		return PolicySafe
	case PolicyAggressive:
		return PolicyAggressive
	default:
		return PolicyNormal
	}
}

func (p PolicyConfig) IsSafe() bool       { return p.Normalized() == PolicySafe }
func (p PolicyConfig) IsAggressive() bool { return p.Normalized() == PolicyAggressive }

// ConfirmDestructive reports whether adopt/clean should ask before acting.
func (p PolicyConfig) ConfirmDestructive() bool { return !p.IsAggressive() }

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
	Agents map[string]AgentConfig `yaml:"agents"`
	UI     UIConfig               `yaml:"ui"`
	// Env (EXPERIMENTAL — requires experimental.setup_env: true) is extra
	// environment exported to every agent process (merged under each agent's
	// own env, which wins). Use it to point agents at a local proxy, set
	// per-tool flags, etc. It does NOT affect council's own subprocesses
	// (git, gh). Ignored unless the experimental gate is on.
	Env map[string]string `yaml:"env,omitempty"`
	// Setup (EXPERIMENTAL — requires experimental.setup_env: true) runs commands
	// once before any agent launches — e.g. starting a proxy/daemon the agents
	// should use. See SetupCommand. Setup commands from a repo-local config are
	// subject to the same trust gate as the rest of the config (an untrusted
	// .council.yaml never runs them). Ignored unless the experimental gate is on.
	Setup                 []SetupCommand                       `yaml:"setup,omitempty"`
	Experimental          ExperimentalConfig                   `yaml:"experimental,omitempty"`
	Sessions              SessionConfig                        `yaml:"sessions"`
	Review                ReviewConfig                         `yaml:"review"`
	Files                 FilesConfig                          `yaml:"files,omitempty"`
	Policy                PolicyConfig                         `yaml:"policy,omitempty"`
	PersonalityCategories map[string]PersonalityCategoryConfig `yaml:"personality_categories,omitempty"`
	Personalities         map[string]PersonalityConfig         `yaml:"personalities,omitempty"`

	// ExperimentalIgnored is computed by Normalize: true when env/setup were
	// present but dropped because the experimental gate was off. Never
	// serialized; used to surface a single warning via SelectAgents.
	ExperimentalIgnored bool `yaml:"-"`
}

// ExperimentalConfig gates opt-in, not-yet-stable features.
type ExperimentalConfig struct {
	// SetupEnv enables the top-level/per-agent `env` and the pre-launch `setup`
	// commands. Off unless explicitly true: `setup` runs arbitrary commands and
	// `env` mutates the agent environment, so both are deliberately opt-in.
	SetupEnv bool `yaml:"setup_env,omitempty"`
}

// SetupEnvEnabled reports whether the experimental env/setup hooks are on.
// They are off unless explicitly enabled.
func (c Config) SetupEnvEnabled() bool { return c.Experimental.SetupEnv }

// SetupCommand is a command council runs before launching agents.
type SetupCommand struct {
	// Name is a short label for logs/doctor (defaults to the command).
	Name string `yaml:"name,omitempty"`
	// Command is the argv to run.
	Command []string `yaml:"command"`
	// Background keeps the process running for the whole council session and
	// terminates it on exit (e.g. a proxy). When false, council runs the
	// command to completion and aborts startup if it exits non-zero.
	Background bool `yaml:"background,omitempty"`
	// WaitForPort, when set on a background command, blocks startup until
	// 127.0.0.1:<port> accepts a connection (a readiness gate for proxies),
	// up to a short timeout.
	WaitForPort int `yaml:"wait_for_port,omitempty"`
}

// Label returns a human-friendly name for the setup command.
func (s SetupCommand) Label() string {
	if strings.TrimSpace(s.Name) != "" {
		return s.Name
	}
	return strings.Join(s.Command, " ")
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

// Default returns the baseline config: presets for the common agent CLIs with
// their terminal quirks encoded, all DISABLED and with no auto-approval flags.
// Users opt in per agent (enabled: true) and opt in to risky phase flags
// explicitly — first runs should never execute permission-bypassing commands
// the user has not seen. See AgentPreset for the known presets.
func Default() Config {
	agents := map[string]AgentConfig{}
	for _, name := range PresetNames() {
		preset, _ := AgentPreset(name)
		agents[name] = preset
	}
	return Config{
		Agents: agents,
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
		// review.check_command (or run `council stack detect`) to gate builds.
		Review: ReviewConfig{},
	}
}

// LoadRaw reads and decodes a config into the defaults but does NOT resolve
// `inherit` or normalize. Callers that merge a repo-local overlay use this so
// inheritance is resolved exactly once on the fully-merged agent map (see
// ResolveInheritance): resolving the global config first would bake a global
// child's inherited fields and stop a local override of its base from reaching
// the child.
func LoadRaw(path string) (Config, []byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, nil, err
	}

	cfg := Default()
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, nil, fmt.Errorf("load config: %w", err)
	}
	mergeDefaultAgentOrchestration(&cfg, raw, Default().Agents)
	return cfg, raw, nil
}

// Load reads, resolves inheritance, and normalizes a standalone config — the
// path a global ~/.council.yaml takes when no repo-local overlay is merged onto
// it. Inheritance is resolved before Normalize so a child inherits its base's
// terminal settings before generic defaults fill the gaps.
//
// Load is for runtime use; its result is resolved and normalized and so is NOT
// suitable for round-tripping (re-marshaling expands inherited fields and bakes
// defaults). Code that loads a config to write it back out, or that merges a
// repo-local overlay, should use LoadRaw and resolve/normalize itself.
func Load(path string) (Config, []byte, error) {
	cfg, raw, err := LoadRaw(path)
	if err != nil {
		return Config{}, nil, err
	}
	cfg.resolveAgentInheritance()
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
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return findLocalConfigFrom(cwd)
}

func findLocalConfigFrom(start string) string {
	globalAbs := ""
	if p, err := DefaultPath(); err == nil {
		globalAbs, _ = filepath.Abs(p)
	}

	// Prefer git's own notion of the repo root: it handles worktrees (where
	// .git is a file) and submodules correctly.
	repoRoot := gitToplevel(start)

	dir := start
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
		if repoRoot != "" && sameFile(dir, repoRoot) {
			return "" // reached the repo root without finding one
		}
		// Fallback boundary when git is unavailable: a .git entry (directory in
		// a normal checkout, file in a linked worktree) marks the repo root.
		if repoRoot == "" {
			if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil {
				return ""
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func gitToplevel(dir string) string {
	out, err := cmdrun.Output(context.Background(), cmdrun.Spec{Name: "git", Args: []string{"-C", dir, "rev-parse", "--show-toplevel"}})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func sameFile(a, b string) bool {
	fa, errA := os.Stat(a)
	fb, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(fa, fb)
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
		case "files":
			if err := val.Decode(&cfg.Files); err != nil {
				return cfg, err
			}
		case "policy":
			if err := val.Decode(&cfg.Policy); err != nil {
				return cfg, err
			}
		case "experimental":
			// A trusted local config can opt into the experimental hooks.
			if err := val.Decode(&cfg.Experimental); err != nil {
				return cfg, err
			}
		case "env":
			// Deep-merge: local keys add to / override the global env.
			merged := map[string]string{}
			if err := val.Decode(&merged); err != nil {
				return cfg, err
			}
			if cfg.Env == nil {
				cfg.Env = map[string]string{}
			}
			for k, v := range merged {
				cfg.Env[k] = v
			}
		case "setup":
			// A local setup list replaces the global one wholesale (running
			// both global and repo setup would be surprising).
			if err := val.Decode(&cfg.Setup); err != nil {
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
	content := defaultConfigHeader + string(raw) + defaultConfigExamples
	return os.WriteFile(path, []byte(content), 0o600)
}

const defaultConfigHeader = `# council configuration.
#
# All agent presets ship DISABLED and without auto-approval flags. Enable the
# CLIs you actually use (enabled: true), or run:
#
#   council config wizard
#
# Docs: https://github.com/umutarmut38/council
`

const defaultConfigExamples = `
# --- Opt-in examples (commented out on purpose) ---------------------------
#
# Auto-approval phase commands let orchestration phases run unattended, but
# they bypass each tool's permission prompts. Only enable them once you trust
# the repos you run council in (consider policy.mode: safe below instead):
#
# agents:
#   claude:
#     orchestration:
#       plan_command: ["claude", "--dangerously-skip-permissions"]
#       vote_command: ["claude", "--dangerously-skip-permissions"]
#       build_command: ["claude", "--dangerously-skip-permissions"]
#   copilot:
#     orchestration:
#       plan_command: ["copilot", "--allow-all-tools"]
#       vote_command: ["copilot", "--allow-all-tools"]
#
# Gate build review with your project's test command (or run
# ` + "`council stack detect`" + ` inside the repo):
#
# review:
#   check_command: ["go", "test", "./..."]
#
# Risk posture (safe | normal | aggressive):
#
# policy:
#   mode: normal
`

func SelectAgents(cfg Config, overrides []string) ([]AgentSpec, []string, error) {
	cfg.Normalize()

	if err := ValidateAgentNames(cfg); err != nil {
		return nil, nil, err
	}

	// Emitted on both selection paths: env/setup were configured but dropped
	// because the experimental gate is off.
	warnings := make([]string, 0)
	if cfg.ExperimentalIgnored {
		warnings = append(warnings, "warning: 'setup'/'env' are experimental and were ignored — set experimental.setup_env: true to enable")
	}

	if len(overrides) > 0 {
		selected := make([]AgentSpec, 0, len(overrides))
		for _, name := range overrides {
			agentCfg, ok := cfg.Agents[name]
			if !ok {
				return nil, nil, fmt.Errorf("agent %q is not configured", name)
			}
			selected = append(selected, AgentSpec{Name: name, Config: agentCfg})
		}
		return selected, warnings, nil
	}

	names := make([]string, 0, len(cfg.Agents))
	for name, agentCfg := range cfg.Agents {
		if agentCfg.Enabled {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	selected := make([]AgentSpec, 0, len(names))
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

// Normalize fills defaults and resolves the experimental gate so every
// downstream consumer sees a consistent config. It is split into focused,
// individually-tested helpers: UI defaults, session defaults, per-agent
// fallbacks, and the env/setup experimental gate.
func (c *Config) Normalize() {
	if c.Agents == nil {
		c.Agents = map[string]AgentConfig{}
	}
	c.normalizeUI()
	c.normalizeSessions()
	c.normalizeAgentDefaults()
	c.applyExperimentalGate()
}

// normalizeUI fills the UI defaults and canonicalizes its enum-like fields.
func (c *Config) normalizeUI() {
	// "paged-grid" appeared in older example configs; the paged grid is the
	// only layout, so both names mean the same thing. Unknown values are left
	// as-is for doctor to flag.
	switch strings.ToLower(strings.TrimSpace(c.UI.Layout)) {
	case "", "grid", "paged-grid":
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
}

// normalizeSessions fills the session defaults.
func (c *Config) normalizeSessions() {
	if c.Sessions.RootDir == "" {
		c.Sessions.RootDir = ".council/runs"
	}
}

// normalizeAgentDefaults applies the tool-agnostic per-agent fallbacks. Only
// generic fallbacks live here: per-agent behavior (how to type, how to submit,
// paste vs raw, delays, etc.) is configured entirely from ~/.council.yaml so
// new agents can be supported without code changes.
func (c *Config) normalizeAgentDefaults() {
	for name, agentCfg := range c.Agents {
		if agentCfg.CWD == "" {
			agentCfg.CWD = "."
		}
		normalizeTerminal(&agentCfg.Terminal)
		c.Agents[name] = agentCfg
	}
}

// normalizeTerminal fills the generic terminal-delivery defaults.
func normalizeTerminal(t *TerminalConfig) {
	if t.Renderer == "" {
		t.Renderer = "screen"
	}
	if t.PTYSize == "" {
		t.PTYSize = "pane"
	}
	if t.Cols <= 0 {
		t.Cols = 120
	}
	if t.Rows <= 0 {
		t.Rows = 40
	}
	if t.SendMode == "" {
		t.SendMode = "type"
	}
	if t.SubmitSequence == "" {
		t.SubmitSequence = "cr"
	}
	if t.Resize == nil {
		value := t.PTYSize != "fixed"
		t.Resize = &value
	}
	if t.Color == nil {
		value := true
		t.Color = &value
	}
}

// applyExperimentalGate resolves the experimental env/setup feature. When it is
// enabled, the global env is folded into each agent (the agent's own env wins),
// so the launch path only reads one map. When it is off (the default), env and
// setup are dropped entirely and ExperimentalIgnored records that something was
// configured but ignored, so SelectAgents can warn once.
func (c *Config) applyExperimentalGate() {
	if c.SetupEnvEnabled() {
		c.ExperimentalIgnored = false
		for name, agentCfg := range c.Agents {
			agentCfg.Env = mergeEnv(c.Env, agentCfg.Env)
			c.Agents[name] = agentCfg
		}
		return
	}

	droppedAgentEnv := false
	for name, agentCfg := range c.Agents {
		if len(agentCfg.Env) > 0 {
			droppedAgentEnv = true
			agentCfg.Env = nil
			c.Agents[name] = agentCfg
		}
	}
	if len(c.Env) > 0 || len(c.Setup) > 0 || droppedAgentEnv {
		c.ExperimentalIgnored = true
	}
	c.Env = nil
	c.Setup = nil
}

// mergeEnv overlays over onto base (over wins on conflicts) and returns the
// combined map. When both are empty it returns over unchanged, so an agent
// with no env keeps a nil map instead of gaining an empty one.
func mergeEnv(base, over map[string]string) map[string]string {
	if len(base) == 0 && len(over) == 0 {
		return over
	}
	merged := make(map[string]string, len(base)+len(over))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range over {
		merged[k] = v
	}
	return merged
}

// maxInheritDepth bounds an `inherit` chain so a cycle or a pathological config
// can't blow the stack; a chain longer than this is left unresolved for
// Validate to report.
const maxInheritDepth = 32

// ResolveInheritance resolves every agent's `inherit` chain in place. It must
// run once on the fully-merged agent map, before Normalize, so a child inherits
// its base's terminal settings before generic defaults fill the gaps. The run
// path and `doctor` load the global config with LoadRaw, apply the repo-local
// overlay, then call this — resolving the global config first (as Load does for
// standalone use) would bake a global child and stop a local override of its
// base from reaching it.
//
// Resolution is idempotent only while the agent map is unchanged: re-running it
// on an unmodified map is a no-op, but once a base changes you must re-resolve
// from the unresolved children to pick the change up (hence LoadRaw + resolve
// once after overlays). `inherit` is left in place (not cleared) so Validate can
// still report a dangling base or a cycle and so config.effective.yaml shows
// intent.
func (c *Config) ResolveInheritance() { c.resolveAgentInheritance() }

// resolveAgentInheritance overlays each agent that sets `inherit` onto a copy of
// its fully-resolved base, so the child reuses the base's definition and only
// the keys it declares win. It is tolerant: an unknown base, a self-reference,
// a cycle, or an over-deep chain leaves the child as-is (with `inherit` intact)
// for Validate to flag, rather than erroring on the run path.
func (c *Config) resolveAgentInheritance() {
	if len(c.Agents) == 0 {
		return
	}
	names := make([]string, 0, len(c.Agents))
	for name := range c.Agents {
		names = append(names, name)
	}
	sort.Strings(names) // deterministic order; resolution is independent of it
	for _, name := range names {
		c.resolveAgentOne(name, map[string]bool{})
	}
}

// resolveAgentOne resolves a single agent, recursing into its base first so a
// multi-level chain (c inherits b inherits a) resolves fully. visiting tracks
// the active chain for cycle detection. The bool result reports whether the
// agent was fully resolved; an unresolvable agent (self-reference, a base
// already in the active chain — i.e. a direct or indirect cycle, an unknown
// base, or an over-deep chain) is left untouched with `inherit` intact for
// Validate to report, and the false result stops a caller from overlaying a
// half-resolved base onto itself.
func (c *Config) resolveAgentOne(name string, visiting map[string]bool) (AgentConfig, bool) {
	agent := c.Agents[name]
	if agent.Inherit == "" {
		return agent, true
	}
	base := agent.Inherit
	if base == name || visiting[name] || visiting[base] || len(visiting) >= maxInheritDepth {
		return agent, false
	}
	if _, ok := c.Agents[base]; !ok {
		return agent, false // unknown base
	}
	visiting[name] = true
	resolvedBase, ok := c.resolveAgentOne(base, visiting)
	delete(visiting, name)
	if !ok {
		return agent, false // base unresolvable: leave this agent as-is too
	}

	merged := overlayAgent(resolvedBase, agent)
	c.Agents[name] = merged
	return merged, true
}

// overlayAgent returns base with child's explicitly-set fields applied on top.
// Enabled is taken from the child unconditionally so inheriting a base never
// silently activates (or deactivates) the child. A child cannot reset an
// inherited non-zero scalar back to its zero value (the terminal *bool fields
// are the tri-state exception); overriding to a different value always works.
func overlayAgent(base, child AgentConfig) AgentConfig {
	out := base
	out.Enabled = child.Enabled
	out.Inherit = child.Inherit
	if len(child.Command) > 0 {
		out.Command = child.Command
	}
	if child.CWD != "" {
		out.CWD = child.CWD
	}
	if child.Color != "" {
		out.Color = child.Color
	}
	if child.Personality != "" {
		out.Personality = child.Personality
	}
	if len(child.Role) > 0 {
		out.Role = child.Role
	}
	out.Env = mergeEnv(base.Env, child.Env)
	out.Terminal = overlayTerminal(base.Terminal, child.Terminal)
	out.Orchestration = overlayOrchestration(base.Orchestration, child.Orchestration)
	return out
}

// overlayTerminal merges child's set terminal fields over base's. The *bool
// fields (Resize, Color) are tri-state: nil inherits, non-nil overrides — so a
// child can override an inherited true back to false.
func overlayTerminal(base, child TerminalConfig) TerminalConfig {
	out := base
	if child.Renderer != "" {
		out.Renderer = child.Renderer
	}
	if child.PTYSize != "" {
		out.PTYSize = child.PTYSize
	}
	if child.Cols != 0 {
		out.Cols = child.Cols
	}
	if child.Rows != 0 {
		out.Rows = child.Rows
	}
	if child.SendMode != "" {
		out.SendMode = child.SendMode
	}
	if child.BeforeSendSequence != "" {
		out.BeforeSendSequence = child.BeforeSendSequence
	}
	if child.SubmitSequence != "" {
		out.SubmitSequence = child.SubmitSequence
	}
	if child.AfterSubmitSequence != "" {
		out.AfterSubmitSequence = child.AfterSubmitSequence
	}
	if child.SubmitDelayMs != 0 {
		out.SubmitDelayMs = child.SubmitDelayMs
	}
	if child.Resize != nil {
		out.Resize = child.Resize
	}
	if child.Color != nil {
		out.Color = child.Color
	}
	return out
}

// overlayOrchestration merges child's orchestration over base's. Exclusions and
// prompt-in-command flags are OR-ed (a child can add an exclusion but, by the
// zero-value rule, cannot remove one it inherits); phase commands override when
// the child sets them.
func overlayOrchestration(base, child OrchestrationConfig) OrchestrationConfig {
	out := base
	if child.Exclude {
		out.Exclude = true
	}
	if child.ExcludePlan {
		out.ExcludePlan = true
	}
	if child.ExcludeVote {
		out.ExcludeVote = true
	}
	if child.ExcludeBuild {
		out.ExcludeBuild = true
	}
	if len(child.PlanCommand) > 0 {
		out.PlanCommand = child.PlanCommand
	}
	if len(child.VoteCommand) > 0 {
		out.VoteCommand = child.VoteCommand
	}
	if len(child.BuildCommand) > 0 {
		out.BuildCommand = child.BuildCommand
	}
	if child.PlanPromptInCommand {
		out.PlanPromptInCommand = true
	}
	if child.VotePromptInCommand {
		out.VotePromptInCommand = true
	}
	if child.BuildPromptInCommand {
		out.BuildPromptInCommand = true
	}
	return out
}
