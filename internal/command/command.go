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
	// Group places the command in the help/docs layout.
	Group Group
	// RequiresRepo marks commands that drive a run and only work inside a git
	// repository.
	RequiresRepo bool
	// SynopsisOnly renders the command in `council help` as a bare synopsis
	// line without the inline description (the docs still describe it). It
	// matches the historical help layout for the launch/ask/version forms and
	// the argument-rich plan/adopt lines.
	SynopsisOnly bool
}

// cliCommands is the ordered CLI command reference. Order is significant: it is
// the order help and the generated docs render in.
var cliCommands = []CLI{
	{Name: "launch", Use: `[--agents claude,codex] [--no-local-config]`, Summary: "launch the interactive multiplexer", Group: GroupGeneral, SynopsisOnly: true},
	{Name: "ask", Use: `[--agents claude,codex] ask "<prompt>"`, Summary: "launch and broadcast a prompt", Group: GroupGeneral, SynopsisOnly: true},
	{Name: "config init", Use: `config init [--force]`, Summary: "write the default (safe) config", Group: GroupGeneral},
	{Name: "config wizard", Use: `config wizard`, Summary: "interactive setup", Group: GroupGeneral},
	{Name: "config add-agent", Use: `config add-agent <preset>`, Summary: "add a known agent CLI to the config", Group: GroupGeneral},
	{Name: "doctor", Use: `doctor`, Summary: "check config, commands, repo, run dirs", Group: GroupGeneral},
	{Name: "trust", Use: `trust [--revoke|--show]`, Summary: "trust this repo's .council.yaml", Group: GroupGeneral},
	{Name: "version", Aliases: []string{"--version", "-v"}, Use: `version`, Summary: "print build version, commit, and date", Group: GroupGeneral, SynopsisOnly: true},

	{Name: "plan", Use: `plan "<issue>" | --file issue.md | --issue 123`, Summary: "start a run; each agent drafts a plan", Group: GroupOrchestration, RequiresRepo: true, SynopsisOnly: true},
	{Name: "vote", Use: `vote [run]`, Summary: "tally ranked votes into a winner", Group: GroupOrchestration, RequiresRepo: true},
	{Name: "build", Use: `build [run]`, Summary: "all agents implement the winning plan", Group: GroupOrchestration, RequiresRepo: true},
	{Name: "review", Use: `review [run]`, Summary: "gate builds + reviewers pick the best", Group: GroupOrchestration, RequiresRepo: true},
	{Name: "adopt", Use: `adopt [run] [agent] [--dry-run] [--yes]`, Summary: "preview + apply a build's diff", Group: GroupOrchestration, RequiresRepo: true, SynopsisOnly: true},
	{Name: "run", Use: `run "<issue>"`, Summary: "plan -> vote -> build", Group: GroupOrchestration, RequiresRepo: true},
	{Name: "resume", Use: `resume [run]`, Summary: "reopen an older run with fresh agent processes", Group: GroupOrchestration, RequiresRepo: true},
	{Name: "status", Use: `status [run]`, Summary: "show a run's phase, artifacts, and winners", Group: GroupOrchestration, RequiresRepo: true},
	{Name: "report", Use: `report [run] [--post]`, Summary: "write report.md (--post comments on the issue)", Group: GroupOrchestration, RequiresRepo: true},
	{Name: "pr", Use: `pr [run] [agent]`, Summary: "open a PR from a build branch (via gh)", Group: GroupOrchestration, RequiresRepo: true},
	{Name: "scorecard", Use: `scorecard`, Summary: "agent performance across runs", Group: GroupOrchestration, RequiresRepo: true},
	{Name: "artifacts", Use: `artifacts scan [run] [--all]`, Summary: "scan run artifacts for likely secrets", Group: GroupOrchestration, RequiresRepo: true},
	{Name: "queue", Use: `queue add|list|run|clear`, Summary: "batch issues through council", Group: GroupOrchestration, RequiresRepo: true},
	{Name: "stack", Use: `stack detect|set <go|node|rust|python>`, Summary: "set review.check_command", Group: GroupOrchestration, RequiresRepo: true},
	{Name: "clean", Use: `clean [--dry-run] [--yes]`, Summary: "remove council worktrees + branches", Group: GroupOrchestration, RequiresRepo: true},
	{Name: "clean-runs", Use: `clean-runs [--keep N] [--dry-run]`, Summary: "prune old run artifacts", Group: GroupOrchestration, RequiresRepo: true},
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
	{Name: "setup", Args: "", Desc: "show pre-launch setup/env status"},
	{Name: "clean", Args: "", Desc: "preview then remove council worktrees (confirm to remove)"},
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
