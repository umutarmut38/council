// Package command is the single source of truth for council's commands: the
// CLI subcommands run from the shell and the in-chat composer commands typed
// into the TUI. The CLI dispatcher, the `council help` usage text, the command
// palette, and the generated docs all read their metadata from here so they
// cannot drift apart.
package command

import "strings"

// Group buckets CLI commands for help and docs rendering.
type Group int

const (
	// GroupGeneral covers the everyday, config, and maintenance commands.
	GroupGeneral Group = iota
	// GroupOrchestration covers the plan/vote/build pipeline run from the
	// shell; every one needs a git repository.
	GroupOrchestration
)

// CLI describes one `council …` subcommand: its dispatch identity, the synopsis
// shown in help, and where it belongs in the reference.
type CLI struct {
	// Name is the canonical token (or tokens, e.g. "config init") that select
	// this command. It also identifies the handler the dispatcher wires up.
	Name string
	// Aliases are alternate spellings accepted on the command line (e.g.
	// "--version", "-v").
	Aliases []string
	// Use is the full invocation shown after "council " in help and docs,
	// including arguments (e.g. `plan "<issue>" | --file issue.md`).
	Use string
	// Summary is the one-line description used by the docs and, unless
	// SynopsisOnly is set, by `council help`.
	Summary string
	// Group places the command in the help/docs layout. Every
	// GroupOrchestration command is repo-scoped — it only works inside a git
	// repository (enforced by orchestrate.NewController, not this metadata).
	Group Group
	// SynopsisOnly renders the command in `council help` as a bare synopsis
	// line without the inline description (the docs still describe it). It
	// matches the historical help layout for the launch/ask/version forms and
	// the argument-rich plan/adopt lines.
	SynopsisOnly bool
	// Long is an optional paragraph shown in `council <cmd> --help` and the
	// generated CLI reference, below the synopsis. Verb-style commands (cost,
	// queue, stack, artifacts) use it to enumerate their sub-verbs.
	Long string
	// Flags documents the command's own flags for `council <cmd> --help` and
	// the CLI reference. ponytail: hand-kept in sync with the stdlib FlagSets in
	// cmd/council (the FlagSets live inside handler bodies in package main and
	// can't be reflected from here); TestSubcommandHelpExitsZero exercises every
	// rendered entry. The shared --no-local-config is appended by the renderer
	// for the commands that accept it, so it is not repeated here.
	Flags []Flag
}

// Flag documents one flag a CLI command accepts.
type Flag struct {
	// Name is the flag as typed, e.g. "--agents".
	Name string
	// Arg is a value placeholder shown after the name (e.g. "a,b"); empty for
	// boolean flags.
	Arg string
	// Desc is the one-line description.
	Desc string
}

// noLocalConfigFlag is the shared flag every orchestration command (and doctor,
// launch, ask) accepts; the renderer appends it so it isn't repeated on each
// registry entry.
var noLocalConfigFlag = Flag{Name: "--no-local-config", Desc: "ignore the repo-local .council.yaml"}

// AcceptsNoLocalConfig reports whether the command accepts --no-local-config
// (every orchestration command via newOrchFlagSet, plus doctor and the bare
// launch/ask forms).
func (c CLI) AcceptsNoLocalConfig() bool {
	if c.Group == GroupOrchestration {
		return true
	}
	switch c.Name {
	case "doctor", "launch", "ask":
		return true
	}
	return false
}

// HelpFlags returns the command's documented flags with the shared
// --no-local-config appended when it applies.
func (c CLI) HelpFlags() []Flag {
	if !c.AcceptsNoLocalConfig() {
		return c.Flags
	}
	return append(append([]Flag(nil), c.Flags...), noLocalConfigFlag)
}

// cliCommands is the ordered CLI command reference. Order is significant: it is
// the order help and the generated docs render in.
var cliCommands = []CLI{
	{Name: "launch", Use: `[--agents claude,codex] [--no-local-config]`, Summary: "launch the interactive multiplexer", Group: GroupGeneral, SynopsisOnly: true,
		Long:  "The bare `council` command. Opens the interactive multiplexer with every enabled agent in its own live pane. `--agents` narrows the launch to a subset.",
		Flags: []Flag{{Name: "--agents", Arg: "a,b", Desc: "only launch these agents (comma-separated)"}}},
	{Name: "ask", Use: `[--agents claude,codex] ask "<prompt>"`, Summary: "launch and broadcast a prompt", Group: GroupGeneral, SynopsisOnly: true,
		Long:  "Launch the multiplexer and immediately broadcast one prompt to every launched agent.",
		Flags: []Flag{{Name: "--agents", Arg: "a,b", Desc: "only launch these agents (comma-separated)"}}},
	{Name: "config init", Use: `config init [--force] [--interactive]`, Summary: "write the default (safe) config", Group: GroupGeneral,
		Flags: []Flag{
			{Name: "--force", Desc: "overwrite an existing config"},
			{Name: "--interactive", Desc: "run the setup wizard instead (alias: -i)"},
		}},
	{Name: "config wizard", Use: `config wizard`, Summary: "interactive setup", Group: GroupGeneral,
		Long: "Detect installed agent CLIs, pick which to enable and their roles, optionally opt into auto-approval, detect the project stack for the review gate, and write ~/.council.yaml."},
	{Name: "config add-agent", Use: `config add-agent <preset> [--name x] [--role planner,builder,voter,review]`, Summary: "add a known agent CLI to the config", Group: GroupGeneral, SynopsisOnly: true,
		Flags: []Flag{
			{Name: "--name", Arg: "x", Desc: "agent name in the config (defaults to the preset name)"},
			{Name: "--role", Arg: "planner,builder,voter,review", Desc: "comma-separated roles (review = review-only; empty = all phases)"},
		}},
	{Name: "config schema", Use: `config schema [--json]`, Summary: "print the configuration reference (Markdown, or JSON Schema)", Group: GroupGeneral,
		Flags: []Flag{{Name: "--json", Desc: "emit a JSON Schema (draft 2020-12) instead of Markdown"}}},
	{Name: "doctor", Use: `doctor [--fix]`, Summary: "check config, commands, repo, run dirs (--fix applies safe fixes)", Group: GroupGeneral,
		Flags: []Flag{{Name: "--fix", Desc: "apply safe, local, reversible fixes (default is read-only)"}}},
	{Name: "trust", Use: `trust [--revoke|--show]`, Summary: "trust this repo's .council.yaml", Group: GroupGeneral,
		Flags: []Flag{
			{Name: "--revoke", Desc: "revoke trust for this repo's .council.yaml"},
			{Name: "--show", Desc: "print the trust status without changing it"},
		}},
	{Name: "version", Aliases: []string{"--version", "-v"}, Use: `version`, Summary: "print build version, commit, and date", Group: GroupGeneral, SynopsisOnly: true},

	{Name: "plan", Use: `plan "<issue>" | --file issue.md | --issue 123 [--agents a,b] [--base ref]`, Summary: "start a run; each agent drafts a plan", Group: GroupOrchestration, SynopsisOnly: true,
		Flags: []Flag{
			{Name: "--file", Arg: "issue.md", Desc: "read the issue from a markdown file"},
			{Name: "--issue", Arg: "123", Desc: "fetch the issue from GitHub by number (via gh)"},
			{Name: "--agents", Arg: "a,b", Desc: "comma-separated agent names"},
			{Name: "--base", Arg: "ref", Desc: "base ref for worktrees (default HEAD)"},
		}},
	{Name: "vote", Use: `vote [run] [--agents a,b]`, Summary: "tally ranked votes into a winner", Group: GroupOrchestration,
		Flags: []Flag{{Name: "--agents", Arg: "a,b", Desc: "comma-separated agent names"}}},
	{Name: "build", Use: `build [run] [--agents a,b]`, Summary: "all agents implement the winning plan", Group: GroupOrchestration,
		Flags: []Flag{{Name: "--agents", Arg: "a,b", Desc: "comma-separated agent names"}}},
	{Name: "review", Use: `review [run] [--agents a,b]`, Summary: "gate builds + reviewers pick the best", Group: GroupOrchestration,
		Flags: []Flag{{Name: "--agents", Arg: "a,b", Desc: "comma-separated agent names"}}},
	{Name: "adopt", Use: `adopt [run] [agent] [--dry-run] [--yes]`, Summary: "preview + apply a build's diff", Group: GroupOrchestration, SynopsisOnly: true,
		Flags: []Flag{
			{Name: "--dry-run", Desc: "show what would be applied without touching the tree"},
			{Name: "--yes", Desc: "skip the confirmation prompt"},
		}},
	{Name: "run", Use: `run "<issue>" | --file issue.md | --issue 123 [--agents a,b] [--base ref]`, Summary: "plan -> vote -> build", Group: GroupOrchestration, SynopsisOnly: true,
		Flags: []Flag{
			{Name: "--file", Arg: "issue.md", Desc: "read the issue from a markdown file"},
			{Name: "--issue", Arg: "123", Desc: "fetch the issue from GitHub by number (via gh)"},
			{Name: "--agents", Arg: "a,b", Desc: "comma-separated agent names"},
			{Name: "--base", Arg: "ref", Desc: "base ref for worktrees (default HEAD)"},
		}},
	{Name: "resume", Use: `resume [run] [--agents a,b]`, Summary: "reopen an older run with fresh agent processes", Group: GroupOrchestration,
		Flags: []Flag{{Name: "--agents", Arg: "a,b", Desc: "comma-separated agent names"}}},
	{Name: "status", Use: `status [run]`, Summary: "show a run's phase, artifacts, and winners", Group: GroupOrchestration},
	{Name: "cost", Use: `cost [run] [--since 30d] [--source ledger|codeburn] | cost prices refresh | cost models [filter]`, Summary: "per-session usage and estimated cost", Group: GroupOrchestration, SynopsisOnly: true,
		Long: "Verbs:\n  cost [run]            usage/cost for one run (default: latest)\n  cost prices refresh   refresh the LiteLLM price cache\n  cost models [filter]  list price-table model names + aliases",
		Flags: []Flag{
			{Name: "--since", Arg: "30d", Desc: "aggregate across runs newer than this (e.g. 30d, 7d)"},
			{Name: "--source", Arg: "ledger|codeburn", Desc: "ledger, or codeburn for machine-wide cross-tool totals"},
		}},
	{Name: "report", Use: `report [run] [--post N]`, Summary: "write report.md (--post N comments on issue #N)", Group: GroupOrchestration,
		Flags: []Flag{{Name: "--post", Arg: "N", Desc: "post the report as a comment on GitHub issue #N (via gh)"}}},
	{Name: "pr", Use: `pr [run] [agent]`, Summary: "open a PR from a build branch (via gh)", Group: GroupOrchestration},
	{Name: "scorecard", Use: `scorecard`, Summary: "agent performance across runs", Group: GroupOrchestration},
	{Name: "artifacts", Use: `artifacts scan [run] [--all]`, Summary: "scan run artifacts for likely secrets", Group: GroupOrchestration,
		Long:  "Verbs:\n  artifacts scan [run]  scan one run (latest by default) for likely secrets",
		Flags: []Flag{{Name: "--all", Desc: "scan every run under the sessions root, not just one"}}},
	{Name: "queue", Use: `queue add|list|run|clear`, Summary: "batch issues through council", Group: GroupOrchestration,
		Long: "Verbs:\n  queue add [--issue N | --file task.md | \"<text>\"]  append a task\n  queue list                                        show queued tasks\n  queue run                                         run each task as a full `council run`\n  queue clear                                       empty the queue",
		Flags: []Flag{
			{Name: "--issue", Arg: "N", Desc: "(queue add) GitHub issue number"},
			{Name: "--file", Arg: "task.md", Desc: "(queue add) issue file"},
		}},
	{Name: "stack", Use: `stack detect|set <go|node|rust|python>`, Summary: "set review.check_command", Group: GroupOrchestration,
		Long: "Verbs:\n  stack detect                     detect the project stack and set the review gate\n  stack set <go|node|rust|python>  set the review gate for a named stack"},
	{Name: "clean", Use: `clean [--dry-run] [--yes]`, Summary: "remove council worktrees + branches", Group: GroupOrchestration,
		Flags: []Flag{
			{Name: "--dry-run", Desc: "list what would be removed"},
			{Name: "--yes", Desc: "skip the confirmation prompt"},
		}},
	{Name: "clean-runs", Use: `clean-runs [--keep N] [--dry-run] [--yes]`, Summary: "prune old run artifacts", Group: GroupOrchestration,
		Flags: []Flag{
			{Name: "--keep", Arg: "N", Desc: "number of most recent runs to keep (default 10)"},
			{Name: "--dry-run", Desc: "list what would be removed"},
			{Name: "--yes", Desc: "skip the confirmation prompt"},
		}},
}

// CLIs returns the ordered CLI command reference.
func CLIs() []CLI { return cliCommands }

// LookupCLI resolves a command-line token (a name or an alias) to its CLI
// entry.
func LookupCLI(token string) (CLI, bool) {
	for _, c := range cliCommands {
		if c.Name == token {
			return c, true
		}
		for _, a := range c.Aliases {
			if a == token {
				return c, true
			}
		}
	}
	return CLI{}, false
}

// IsOrchestration reports whether the token selects an orchestration
// subcommand. It is how the dispatcher decides to route into the orchestration
// handlers instead of the default launch path.
func IsOrchestration(token string) bool {
	c, ok := LookupCLI(token)
	return ok && c.Group == GroupOrchestration
}

// Composer describes one in-chat command typed into the TUI composer after a
// leading "/". The fields drive the palette, /help, and Tab completion.
type Composer struct {
	// Name is the canonical command word (without the "/").
	Name string
	// Aliases are alternate words that dispatch to the same handler.
	Aliases []string
	// Args is the argument hint shown in the palette (e.g. "agent msg").
	Args string
	// Desc is the short description shown in the palette and the suggestion
	// line.
	Desc string
	// Key is the global keybinding that triggers the same action outside the
	// composer (e.g. "Ctrl+G"), shown next to the command in the palette.
	// Empty when the command has no direct shortcut.
	Key string
}

// composerCommands drives both /help text and the command palette. Order is
// the palette's declaration order; keep it in sync with the handlers in the
// TUI's handleCommand.
var composerCommands = []Composer{
	{Name: "all", Aliases: []string{"broadcast"}, Args: "msg", Desc: "send to every agent"},
	{Name: "send", Args: "agent msg", Desc: "send to one agent"},
	{Name: "direct", Aliases: []string{"window"}, Args: "[agent]", Desc: "type straight into a pane", Key: "F2"},
	{Name: "zoom", Aliases: []string{"full"}, Args: "[agent]", Desc: "fullscreen the focused pane", Key: "Ctrl+F"},
	{Name: "page", Args: "next|prev|n", Desc: "switch pane pages", Key: "Ctrl+N/P"},
	{Name: "overview", Aliases: []string{"agents"}, Args: "", Desc: "show all agents", Key: "Ctrl+G"},
	{Name: "settings", Aliases: []string{"prefs"}, Args: "", Desc: "adjust layout for this session"},
	{Name: "runs", Args: "", Desc: "browse previous runs"},
	{Name: "resume", Args: "[run]", Desc: "resume an older run"},
	{Name: "focus", Args: "agent", Desc: "focus a pane"},
	{Name: "target", Args: "all|focus|personality|category", Desc: "scope messages AND phases (plan/vote/build)", Key: "Ctrl+B"},
	{Name: "show", Args: "all|personality|category", Desc: "choose displayed personalities"},
	{Name: "hide", Args: "personality", Desc: "hide a personality"},
	{Name: "clear", Args: "[agent]", Desc: "clear pane output"},
	{Name: "save", Args: "", Desc: "save transcripts", Key: "Ctrl+S"},
	{Name: "plan", Args: "<issue>", Desc: "council: each agent drafts a plan"},
	{Name: "vote", Args: "", Desc: "council: agents rank the plans (auto-skipped when there's only one plan)"},
	{Name: "build", Args: "", Desc: "council: stage winning plan in worktrees (no start)"},
	{Name: "start-build", Aliases: []string{"startbuild"}, Args: "", Desc: "council: send the build prompt staged by /build"},
	{Name: "review", Args: "", Desc: "council: check + vote the best build"},
	{Name: "adopt", Args: "[agent]", Desc: "council: preview then apply a build (confirm to apply)"},
	{Name: "preview", Args: "[agent]", Desc: "council: show what /adopt would change"},
	{Name: "compare", Args: "", Desc: "council: inspect builds — files, git-style diffs vs base or between builds"},
	{Name: "judge", Args: "plan|build <agent|letter>", Desc: "council: pick a winner yourself"},
	{Name: "refine", Args: "[note]", Desc: "council: every planner revises its plan from the vote critiques (or a note), then the council revotes (works on a single plan)"},
	{Name: "artifacts", Args: "", Desc: "browse this run's plans/votes/diffs/logs"},
	{Name: "edit", Args: "[path]", Desc: "integrated editor + file tree"},
	{Name: "report", Args: "", Desc: "write report.md for the run"},
	{Name: "restart", Args: "agent", Desc: "terminate + relaunch one pane"},
	{Name: "resend", Args: "agent", Desc: "resend the current phase prompt"},
	{Name: "nudge", Args: "[agent]", Desc: "remind agent(s) to write their artifact"},
	{Name: "attention", Args: "agent [off]", Desc: "flag a pane as needing your input"},
	{Name: "finish", Args: "", Desc: "force-collect the current phase now"},
	{Name: "status", Args: "", Desc: "show the active run/phase"},
	{Name: "cost", Args: "", Desc: "per-session usage and estimated cost"},
	{Name: "setup", Args: "", Desc: "show pre-launch setup/env status"},
	{Name: "clean", Args: "", Desc: "preview then remove council worktrees (confirm to remove)"},
	{Name: "refresh", Args: "[agent|all] [force]", Desc: "reset a freestyle pane's worktree to HEAD (force discards uncommitted changes)"},
	{Name: "help", Args: "", Desc: "list commands"},
	{Name: "quit", Aliases: []string{"exit"}, Args: "", Desc: "quit council", Key: "Ctrl+X"},
}

// Composers returns the ordered in-chat command reference.
func Composers() []Composer { return composerCommands }

// LookupComposer resolves an in-chat command word (a name or an alias) to its
// canonical Composer entry. The word should already be lower-cased and have its
// leading "/" stripped.
func LookupComposer(word string) (Composer, bool) {
	word = strings.ToLower(word)
	for _, c := range composerCommands {
		if c.Name == word {
			return c, true
		}
		for _, a := range c.Aliases {
			if a == word {
				return c, true
			}
		}
	}
	return Composer{}, false
}
