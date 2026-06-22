package config

import "strings"

// SchemaField documents one configuration key.
type SchemaField struct {
	Key         string
	Type        string
	Default     string
	Description string
}

// SchemaSection groups the keys under one config object.
type SchemaSection struct {
	// Title is the YAML path of the object (e.g. "ui" or
	// "agents.<name>.terminal"), rendered as the table heading.
	Title string
	// Intro is an optional one-line note shown above the table.
	Intro  string
	Fields []SchemaField
}

// Schema is the documented configuration reference. It is the single source of
// truth behind `council config schema` and the generated tables in
// docs/configuration.md. A test in this package walks the config structs with
// reflection and fails if a YAML field is missing here, so the schema cannot
// drift from the types.
func Schema() []SchemaSection {
	return []SchemaSection{
		{
			Title: "agents.<name>",
			Intro: "A map of agent name to config; the name labels the pane and artifacts.",
			Fields: []SchemaField{
				{"enabled", "bool", "`false`", "Launch this agent."},
				{"inherit", "string", "—", "Reuse another agent's definition by name (a preset, global, or local agent), then override the keys set here. A field overrides only when set to a non-zero value, so an inherited non-zero scalar or orchestration flag can't be reset to its zero value (e.g. you can't set `exclude_build: false` to undo a base's `true`); only `terminal.resize`/`terminal.color` are tri-state. `enabled` is never inherited; chains are allowed."},
				{"command", "list", "—", "argv used to start the interactive agent."},
				{"cwd", "string", "`\".\"`", "Working directory for the process."},
				{"color", "string", "—", "256-color index (`\"212\"`) or hex (`\"#ff5f87\"`) tinting the pane border; falls back to the personality color."},
				{"personality", "string", "—", "Personality name (must exist under `personalities`)."},
				{"role", "list", "`[worker, reviewer]`", "Orchestration phases the agent joins: `worker`, `reviewer`, or both."},
				{"env", "map", "—", "Extra environment for this agent, merged over the top-level `env` (this wins). Experimental: requires `experimental.setup_env`."},
				{"terminal", "object", "—", "Rendering and prompt-delivery settings (see `agents.<name>.terminal`)."},
				{"orchestration", "object", "—", "Per-phase behavior (see `agents.<name>.orchestration`)."},
			},
		},
		{
			Title: "agents.<name>.terminal",
			Intro: "Controls rendering and how prompts are delivered into the agent's live TUI.",
			Fields: []SchemaField{
				{"renderer", "string", "`screen`", "`screen` (terminal emulator) or `transcript` (cleaned scrollback)."},
				{"pty_size", "string", "`pane`", "`pane` (size the PTY to the pane) or `fixed` (use `cols`/`rows`)."},
				{"cols", "int", "`120`", "PTY width when `pty_size: fixed`."},
				{"rows", "int", "`40`", "PTY height when `pty_size: fixed`."},
				{"send_mode", "string", "`type`", "`type` (raw keystrokes) or `paste` (bracketed paste)."},
				{"before_send_sequence", "string", "—", "Sequence sent before the message, e.g. `ctrl+u` to clear the line."},
				{"submit_sequence", "string", "`cr`", "What submits the message (see the sequence names in the prose above)."},
				{"after_submit_sequence", "string", "—", "Sequence sent after submitting."},
				{"submit_delay_ms", "int", "`0`", "Send the submit key this many ms after the text, as its own write."},
				{"resize", "bool", "`true` (`false` if `fixed`)", "Resize the PTY when the pane resizes."},
				{"color", "bool", "`true`", "Pass a color-capable `TERM`; `false` sends `TERM=dumb`, `NO_COLOR=1`."},
			},
		},
		{
			Title: "agents.<name>.orchestration",
			Intro: "How the agent participates in the plan/vote/build phases.",
			Fields: []SchemaField{
				{"exclude", "bool", "`false`", "Exclude from all orchestration phases."},
				{"exclude_plan", "bool", "`false`", "Exclude from the plan phase."},
				{"exclude_vote", "bool", "`false`", "Exclude from the vote phase."},
				{"exclude_build", "bool", "`false`", "Exclude from the build phase."},
				{"plan_command", "list", "falls back to `command`", "Launch argv for the plan phase."},
				{"vote_command", "list", "falls back to `command`", "Launch argv for the vote phase."},
				{"build_command", "list", "falls back to `command`", "Launch argv for the build phase."},
				{"plan_prompt_in_command", "bool", "`false`", "Append the plan prompt as the final argv element instead of typing it into the TUI."},
				{"vote_prompt_in_command", "bool", "`false`", "Append the vote prompt as the final argv element."},
				{"build_prompt_in_command", "bool", "`false`", "Append the build prompt as the final argv element."},
			},
		},
		{
			Title: "ui",
			Fields: []SchemaField{
				{"layout", "string", "`grid`", "Pane layout."},
				{"max_scrollback_lines", "int", "`5000`", "Per-pane scrollback kept in memory."},
				{"page_rows", "int", "grid-derived", "Pane rows per page (for many agents)."},
				{"page_cols", "int", "grid-derived", "Pane columns per page (for many agents)."},
				{"adaptive_grid", "bool", "`true`", "Size the grid to the visible panes instead of always using `page_rows` x `page_cols`."},
				{"detect_approval_prompts", "bool", "`true`", "Experimental: auto-flag a pane as needs-input when an approval-looking prompt sits at the bottom and the agent has gone quiet."},
				{"group_by", "string", "`none`", "`none`, `personality`, or `category` — orders panes and the overview."},
				{"initial_prompt_delay_ms", "int", "`3000`", "Wait this long after launch before broadcasting the `ask` prompt."},
				{"editor", "string", "—", "Command (argv) to open files in `/artifacts`, `/compare`, and the integrated `/edit` pane; takes precedence over $VISUAL/$EDITOR/vim. e.g. `nvim` or `code -w`."},
				{"editor_open_cmd", "string", "`<Esc>:e {path}<CR>`", "Keystrokes sent to the live `/edit` editor to open a tree-selected file (`{path}` = repo-relative). Default suits vim/nvim; set empty to relaunch the editor per file instead."},
			},
		},
		{
			Title: "env, setup, experimental",
			Intro: "Experimental and off by default; `experimental.setup_env: true` is required to enable `env` and `setup`.",
			Fields: []SchemaField{
				{"experimental.setup_env", "bool", "`false`", "Required to enable `env`/`setup`. Both are ignored unless this is `true`."},
				{"env", "map", "—", "`KEY: value` map exported to every agent. Per-agent `agents.<name>.env` overrides it."},
				{"setup", "list", "—", "Commands run before agents launch (see `setup[]`)."},
			},
		},
		{
			Title: "setup[]",
			Intro: "Each entry is one pre-launch command.",
			Fields: []SchemaField{
				{"name", "string", "—", "Optional label shown in logs, `council doctor`, and `/setup`."},
				{"command", "list", "—", "argv to run before launching agents."},
				{"background", "bool", "`false`", "Keep the process alive for the session (a daemon/proxy) and stop it on exit. `false` runs to completion and aborts startup on a non-zero exit."},
				{"wait_for_port", "int", "—", "On a background command, block until `127.0.0.1:<port>` is listening (a readiness gate), up to ~10s."},
			},
		},
		{
			Title: "sessions",
			Fields: []SchemaField{
				{"root_dir", "string", "`.council/runs`", "Where run directories are written. Relative paths anchor to the launch directory."},
				{"private", "bool", "`true`", "Owner-only run artifacts (`0700` dirs, `0600` files)."},
				{"redact", "bool", "`false`", "Best-effort scrubbing of common secret patterns in saved transcripts."},
			},
		},
		{
			Title: "review",
			Fields: []SchemaField{
				{"check_command", "list", "—", "Run in each build worktree to gate implementations before the review vote; failures are dropped. Empty = no gate."},
				{"check_timeout_seconds", "int", "`600`", "Hard timeout per check run; a timeout is recorded as FAIL."},
				{"max_check_output_bytes", "int", "`1048576`", "Cap on each check log; longer output is truncated."},
			},
		},
		{
			Title: "files",
			Intro: "Limits for `@path` file-reference expansion in prompts and issues.",
			Fields: []SchemaField{
				{"allow_absolute", "bool", "`false`", "Allow expanding absolute paths and paths outside the working directory (ignored under `policy.mode: safe`)."},
				{"max_bytes", "int", "`262144`", "Per-file expansion cap; bigger files stay as `@tokens`. Binary files are always skipped."},
			},
		},
		{
			Title: "policy",
			Fields: []SchemaField{
				{"mode", "string", "`normal`", "`safe` | `normal` | `aggressive` — the automation risk posture."},
			},
		},
		{
			Title: "personality_categories.<name>",
			Fields: []SchemaField{
				{"label", "string", "—", "Display label for the category."},
				{"color", "string", "—", "Optional 256-color code."},
				{"order", "int", "`0`", "Sort order within groupings."},
			},
		},
		{
			Title: "personalities.<name>",
			Fields: []SchemaField{
				{"label", "string", "—", "Display label."},
				{"category", "string", "—", "Category name this personality links to."},
				{"color", "string", "—", "Optional 256-color code."},
				{"order", "int", "`0`", "Sort order within groupings."},
				{"prompt_prefix", "string", "—", "Text prepended to prompts sent to this agent."},
			},
		},
	}
}

// SchemaMarkdown renders Schema as the Markdown body used inside the generated
// region of docs/configuration.md and printed by `council config schema`.
func SchemaMarkdown() string {
	var b strings.Builder
	for i, sec := range Schema() {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("### `" + sec.Title + "`\n\n")
		if sec.Intro != "" {
			b.WriteString(sec.Intro + "\n\n")
		}
		b.WriteString("| Key | Type | Default | Description |\n")
		b.WriteString("|---|---|---|---|\n")
		for _, f := range sec.Fields {
			b.WriteString("| `" + markdownTableCell(f.Key) + "` | " + markdownTableCell(f.Type) + " | " + markdownTableCell(f.Default) + " | " + markdownTableCell(f.Description) + " |\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func markdownTableCell(value string) string {
	value = strings.ReplaceAll(value, "\n", "<br>")
	return strings.ReplaceAll(value, "|", `\|`)
}
