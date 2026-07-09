# `.council.yaml` schema reference

Source of truth: `internal/config/config.go`, `schema.go`, and `presets.go` in
the [council](https://github.com/umutarmut38/council) repo. The **Fields** section
below is generated from those structs (identical to `council config schema` and
`docs/config-schema.md`), so it never drifts. `.council.yaml` is a **repo-local
overlay** deep-merged over `~/.council.yaml`: top-level sections (`ui`,
`sessions`, `review`, `worktrees`, `usage`, `files`, `policy`, `env`,
`experimental`) and each entry under `agents`, `personalities`, and
`personality_categories` are deep-merged, so a local file only specifies the keys
it changes.

## Top-level structure

```yaml
agents:                 # map: name -> agent config
personalities:          # map: name -> personality (optional)
personality_categories: # map: name -> category, for grouped layouts (optional)
ui:                     # layout, paging, grouping, timing, themes
sessions:               # run storage, privacy, redaction
review:                 # post-build check gate (+ timeout/output caps)
worktrees:              # per-pane git worktrees for freestyle panes (optional)
usage:                  # local cost/usage ledger (optional, off by default)
files:                  # @file expansion limits (optional)
policy:                 # risk posture: safe | normal | aggressive (optional)
env:                    # experimental: env exported to agents (optional)
setup:                  # experimental: pre-launch commands (optional)
experimental:           # feature gates (setup_env)
```

## Fields

Every field, per config struct. `role` selects orchestration phases (one token
per phase: `planner`, `builder`, `voter`, `review`; omit for all phases; legacy
`worker` = `planner`+`builder`, bare `reviewer` = `voter`+`review`). Self-judging
is always prevented. Personalities are orthogonal — they only inject prompt text.

<!-- BEGIN GENERATED: config-schema -->
### `agents.<name>`

A map of agent name to config; the name labels the pane and artifacts.

| Key | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Launch this agent. |
| `inherit` | string | — | Reuse another agent's definition by name (a preset, global, or local agent), then override the keys set here. A field overrides only when set to a non-zero value, so an inherited non-zero scalar or orchestration flag can't be reset to its zero value (e.g. you can't set `exclude_build: false` to undo a base's `true`); only `terminal.resize`/`terminal.color` are tri-state. `enabled` is never inherited; chains are allowed. |
| `command` | list | — | argv used to start the interactive agent. |
| `cwd` | string | `"."` | Working directory for the process. |
| `color` | string | — | 256-color index (`"212"`) or hex (`"#ff5f87"`) tinting the pane border; falls back to the personality color. |
| `personality` | string | — | Personality name (must exist under `personalities`). |
| `role` | list | all phases | Orchestration phases the agent joins, one token per phase: `planner`, `builder`, `voter`, `review`. Omit for all phases. Legacy aliases still work: `worker` = `planner`+`builder`, and a bare `reviewer` = `voter`+`review` (use `review` for a review-only agent). |
| `env` | map | — | Extra environment for this agent, merged over the top-level `env` (this wins). Experimental: requires `experimental.setup_env`. |
| `terminal` | object | — | Rendering and prompt-delivery settings (see `agents.<name>.terminal`). |
| `orchestration` | object | — | Per-phase behavior (see `agents.<name>.orchestration`). |
| `usage` | object | — | Cost-tracking binding for this agent (see `agents.<name>.usage`). |
| `worktree` | bool | — | Override `worktrees.freestyle` for this agent (tri-state). `false` keeps the agent in council's working directory even when freestyle worktrees are on — use it for tools that need the live tree (installed `node_modules`, build output, uncommitted state) that a clean detached worktree lacks (e.g. `copilot`). Unset follows `worktrees.freestyle`. |

### `agents.<name>.terminal`

Controls rendering and how prompts are delivered into the agent's live TUI.

| Key | Type | Default | Description |
|---|---|---|---|
| `renderer` | string | `screen` | `screen` (terminal emulator) or `transcript` (cleaned scrollback). |
| `pty_size` | string | `pane` | `pane` (size the PTY to the pane) or `fixed` (use `cols`/`rows`). |
| `cols` | int | `120` | PTY width when `pty_size: fixed`. |
| `rows` | int | `40` | PTY height when `pty_size: fixed`. |
| `send_mode` | string | `type` | `type` (raw keystrokes) or `paste` (bracketed paste). |
| `before_send_sequence` | string | — | Sequence sent before the message, e.g. `ctrl+u` to clear the line. |
| `submit_sequence` | string | `cr` | What submits the message (see the sequence names in the prose above). |
| `after_submit_sequence` | string | — | Sequence sent after submitting. |
| `submit_delay_ms` | int | `0` | Send the submit key this many ms after the text, as its own write. |
| `resize` | bool | `true` (`false` if `fixed`) | Resize the PTY when the pane resizes. |
| `color` | bool | `true` | Pass a color-capable `TERM`; `false` sends `TERM=dumb`, `NO_COLOR=1`. |

### `agents.<name>.orchestration`

How the agent participates in the plan/vote/build phases.

| Key | Type | Default | Description |
|---|---|---|---|
| `exclude` | bool | `false` | Exclude from all orchestration phases. |
| `exclude_plan` | bool | `false` | Exclude from the plan phase. |
| `exclude_vote` | bool | `false` | Exclude from the vote phase. |
| `exclude_build` | bool | `false` | Exclude from the build phase. |
| `plan_command` | list | falls back to `command` | Launch argv for the plan phase. |
| `vote_command` | list | falls back to `command` | Launch argv for the vote phase. |
| `build_command` | list | falls back to `command` | Launch argv for the build phase. |
| `plan_prompt_in_command` | bool | `false` | Append the plan prompt as the final argv element instead of typing it into the TUI. |
| `vote_prompt_in_command` | bool | `false` | Append the vote prompt as the final argv element. |
| `build_prompt_in_command` | bool | `false` | Append the build prompt as the final argv element. |

### `ui`

| Key | Type | Default | Description |
|---|---|---|---|
| `layout` | string | `grid` | Pane layout. |
| `max_scrollback_lines` | int | `5000` | Per-pane scrollback kept in memory. |
| `page_rows` | int | grid-derived | Pane rows per page (for many agents). |
| `page_cols` | int | grid-derived | Pane columns per page (for many agents). |
| `adaptive_grid` | bool | `true` | Size the grid to the visible panes instead of always using `page_rows` x `page_cols`. |
| `detect_approval_prompts` | bool | `true` | Experimental: auto-flag a pane as needs-input when an approval-looking prompt sits at the bottom and the agent has gone quiet. |
| `group_by` | string | `none` | `none`, `personality`, or `category` — orders panes and the overview. |
| `initial_prompt_delay_ms` | int | `3000` | Wait this long after launch before broadcasting the `ask` prompt. |
| `editor` | string | — | Command (argv) to open files in `/artifacts`, `/compare`, and the integrated `/edit` pane; takes precedence over $VISUAL/$EDITOR/vim. e.g. `nvim` or `code -w`. |
| `editor_open_cmd` | string | `<Esc>:e {path}<CR>` | Keystrokes sent to the live `/edit` editor to open a tree-selected file (`{path}` = the file's absolute path, vim-escaped). Default suits vim/nvim; set empty to relaunch the editor per file instead. |
| `mouse` | bool | `true` | Capture the mouse: wheel scrolls a pane's history / the active list, click focuses a pane. Disables native terminal text selection; toggle at runtime with Ctrl+W. |
| `theme` | string | `default` | Overall color palette: a built-in (`default`, `mono`, `nord`, `solarized`) or a name defined under `themes`. The per-agent/personality pane color still layers on top. |
| `themes` | map | — | Custom palettes keyed by name; reference one via `theme`. See `ui.themes.<name>`. |

### `ui.themes.<name>`

A custom palette. Each role is an optional 256-color index (`"212"`) or hex (`"#ff5f87"`); unset roles inherit the `default` palette. Indexed-256 only (no truecolor).

| Key | Type | Default | Description |
|---|---|---|---|
| `title` | string | default | Brand/header wordmark. |
| `heading` | string | default | Section headings (artifacts, settings). |
| `status` | string | default | Nominal/success readouts and diff additions. |
| `rail` | string | default | Progress rail, diff hunk headers, idle next-action. |
| `border` | string | default | Unfocused pane borders and dividers (the muted tone). |
| `focus` | string | default | Focused pane border / active selection. |
| `suggest` | string | default | Command suggestions. |
| `input` | string | default | Composer input text. |
| `warn` | string | default | Warnings/alerts and diff deletions. |
| `faint` | string | default | De-emphasized secondary text. |

### `env, setup, experimental`

Experimental and off by default; `experimental.setup_env: true` is required to enable `env` and `setup`.

| Key | Type | Default | Description |
|---|---|---|---|
| `experimental.setup_env` | bool | `false` | Required to enable `env`/`setup`. Both are ignored unless this is `true`. |
| `env` | map | — | `KEY: value` map exported to every agent. Per-agent `agents.<name>.env` overrides it. |
| `setup` | list | — | Commands run before agents launch (see `setup[]`). |

### `setup[]`

Each entry is one pre-launch command.

| Key | Type | Default | Description |
|---|---|---|---|
| `name` | string | — | Optional label shown in logs, `council doctor`, and `/setup`. |
| `command` | list | — | argv to run before launching agents. |
| `background` | bool | `false` | Keep the process alive for the session (a daemon/proxy) and stop it on exit. `false` runs to completion and aborts startup on a non-zero exit. |
| `wait_for_port` | int | — | On a background command, block until `127.0.0.1:<port>` is listening (a readiness gate), up to ~10s. |

### `sessions`

| Key | Type | Default | Description |
|---|---|---|---|
| `root_dir` | string | `.council/runs` | Where run directories are written. Relative paths anchor to the launch directory. |
| `private` | bool | `true` | Owner-only run artifacts (`0700` dirs, `0600` files). |
| `redact` | bool | `false` | Best-effort scrubbing of common secret patterns in saved transcripts. |

### `review`

| Key | Type | Default | Description |
|---|---|---|---|
| `check_command` | list | — | Run in each build worktree to gate implementations before the review vote; failures are dropped. Empty = no gate. |
| `check_timeout_seconds` | int | `600` | Hard timeout per check run; a timeout is recorded as FAIL. |
| `max_check_output_bytes` | int | `1048576` | Cap on each check log; longer output is truncated. |

### `files`

Limits for `@path` file-reference expansion in prompts and issues.

| Key | Type | Default | Description |
|---|---|---|---|
| `allow_absolute` | bool | `false` | Allow expanding absolute paths and paths outside the working directory (ignored under `policy.mode: safe`). |
| `max_bytes` | int | `262144` | Per-file expansion cap; bigger files stay as `@tokens`. Binary files are always skipped. |

### `policy`

| Key | Type | Default | Description |
|---|---|---|---|
| `mode` | string | `normal` | `safe` \| `normal` \| `aggressive` — the automation risk posture. |

### `personality_categories.<name>`

| Key | Type | Default | Description |
|---|---|---|---|
| `label` | string | — | Display label for the category. |
| `color` | string | — | Optional 256-color code. |
| `order` | int | `0` | Sort order within groupings. |

### `personalities.<name>`

| Key | Type | Default | Description |
|---|---|---|---|
| `label` | string | — | Display label. |
| `category` | string | — | Category name this personality links to. |
| `color` | string | — | Optional 256-color code. |
| `order` | int | `0` | Sort order within groupings. |
| `prompt_prefix` | string | — | Text prepended to prompts sent to this agent. |

### `worktrees`

Opt-in per-pane isolation for freestyle (non-orchestration) panes. Off by default; orchestration (`/plan` and its run-stamped build worktrees) is unaffected.

| Key | Type | Default | Description |
|---|---|---|---|
| `freestyle` | bool | `false` | Give each freestyle pane its own persistent, repo-local git worktree at `.council/workspaces/<agent>` (a distinct cwd — enables per-pane cost when `usage.enabled`, plus file isolation). Reused across sessions and never auto-reset; a stale marker (`⟳` behind HEAD, `*` dirty) shows drift and `/refresh` resets it. No-op outside a git repo. |
| `seed` | list | — | Extra files/globs (relative to the repo root) copied into each worktree on create, on top of the built-in instruction-file allowlist (`CLAUDE.md`, `AGENTS.md`, `GEMINI.md`, `QWEN.md`, `.cursorrules`, `.github/copilot-instructions.md`, `.mcp.json`). |

### `usage`

Local, provider-agnostic cost/usage ledger. Off by default; all data stays under `.council/`.

| Key | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Record usage and show the cost UI (header total, pane-border cost, `/cost`). |
| `estimator` | string | `bytes4` | Local token estimator: `bytes4` (UTF-8 bytes / 4) or `runes4` (Unicode code points / 4). |
| `show_total_in_header` | bool | `true` | Show a compact run cost total in the top status line (on by default when usage is enabled; set `false` to hide). |
| `show_agent_cost_in_border` | bool | `true` | Show each session's cost in its pane border (on by default when usage is enabled; set `false` to hide). |
| `stale_price_after_days` | int | `60` | Warn when a user price profile's `reviewed_at` is older than this. |
| `model_aliases` | map | — | Map a model name council sees to one the price tables know. |
| `prices` | map | — | User-reviewed price profiles (see `usage.prices.<name>`). |

### `usage.prices.<name>`

A user-defined price profile, in per-million-token units. Wins over the bundled/cached price tables.

| Key | Type | Default | Description |
|---|---|---|---|
| `input_per_million` | float | `0` | Input price per 1M tokens. |
| `output_per_million` | float | `0` | Output price per 1M tokens. |
| `cache_write_per_million` | float | input × 1.25 | Cache-write price per 1M tokens; derives from the input rate when unset. |
| `cache_read_per_million` | float | input × 0.1 | Cache-read price per 1M tokens; derives from the input rate when unset. |
| `currency` | string | — | Currency for this profile (informational). |
| `source` | string | — | Where the price came from (informational, e.g. `user`). |
| `reviewed_at` | string | — | Date (`YYYY-MM-DD`) the price was last reviewed; drives the stale warning. |

### `agents.<name>.usage`

Binds explicit usage metadata for cost tracking. Council never inspects `agents.<name>.command` for tool or model inference.

| Key | Type | Default | Description |
|---|---|---|---|
| `model` | string | — | Observed model name to use for estimated pricing until a provider-session report supplies a concrete model. |
| `price_profile` | string | — | A `usage.prices` entry; when set it wins over the price tables. |
| `tool` | string | — | Native provider-session reader to enable for this agent, e.g. `claude`, `codex`, `copilot`, `cursor`, `opencode`. |
<!-- END GENERATED: config-schema -->

## Sequence names

`submit_sequence` / `before_send_sequence` / `after_submit_sequence` accept:
`cr` (`\r`), `lf` (`\n`), `crlf`, `esc`, `ctrl+c`, `ctrl+d`, `ctrl+u`,
`csi-enter` and `csi-…-enter` (kitty keyboard protocol), `none`, or
`raw:<bytes>`.

## Known per-agent quirks (preset defaults that work)

From `internal/config/presets.go`. Presets ship **disabled** and **without**
auto-approval flags.

| Agent   | `command`         | terminal                                              | auto-approval (opt-in) | orchestration |
|---------|-------------------|------------------------------------------------------|------------------------|---------------|
| claude  | `["claude"]`      | `type`, `cr`, `submit_delay_ms: 250`                 | `["claude","--dangerously-skip-permissions"]` | all phases |
| codex   | `["codex"]`       | `paste`, `before_send_sequence: ctrl+u`, `cr`        | `["codex","--full-auto"]` | all phases |
| cursor  | `["cursor-agent"]`| `type`, `cr`, `submit_delay_ms: 250`                 | `["cursor-agent","--force"]` | all phases |
| copilot | `["copilot"]`     | `type`, `cr`, `submit_delay_ms: 250`                 | `["copilot","--allow-all-tools"]` | `exclude_build: true` |
| opencode| `["opencode"]`    | `type`, `cr`                                         | —                      | all phases |

> Flags council recognizes as risky auto-approval (warned by `council doctor`,
> refused under `policy.mode: safe`): `--dangerously-skip-permissions`,
> `--allow-all-tools`, `--full-auto`, `--force`, `--yolo`, `--auto-approve`,
> `--dangerously-bypass-approvals-and-sandbox`.

## Minimal overlay example (planner/builder + voter/review)

```yaml
# .council.yaml — git-excluded locally via .git/info/exclude; do not commit.
ui:
  group_by: category
  initial_prompt_delay_ms: 8000

review:
  check_command: ["go", "test", "./..."]

# Optional: local cost tracking (off by default). Shows a run total in the
# header, a per-pane cost in each border, and the /cost breakdown.
# usage:
#   enabled: true

# Optional: give each freestyle (non-orchestration) pane its own git worktree,
# so same-tool panes get isolated files and a per-pane cost split.
# worktrees:
#   freestyle: true

personality_categories:
  builders: { label: Builders, color: "81", order: 10 }
  reviewers: { label: Reviewers, color: "203", order: 20 }

personalities:
  pragmatist:
    label: Pragmatist
    category: builders
    color: "81"
    prompt_prefix: |
      Prefer the smallest maintainable change that fully solves the problem.
  critic:
    label: Critic
    category: reviewers
    color: "212"
    prompt_prefix: |
      Scrutinize for correctness, regressions, and brittle assumptions.

agents:
  codex-worker:
    enabled: true
    command: ["codex"]
    role: [planner, builder]
    personality: pragmatist
    terminal:
      send_mode: paste
      before_send_sequence: ctrl+u
      submit_sequence: cr
    # usage:                 # enable per-agent cost tracking (needs usage.enabled)
    #   tool: codex          # session reader: claude|codex|copilot|cursor|opencode
    #   model: gpt-5-codex   # model for estimated pricing until a report supplies one
  copilot-reviewer:
    enabled: true
    command: ["copilot"]
    role: [voter, review]
    personality: critic
    # worktree: false        # copilot needs the live tree; keep it in the launch dir
    terminal:
      submit_sequence: cr
      submit_delay_ms: 250
```
