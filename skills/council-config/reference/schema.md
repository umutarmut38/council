# `.council.yaml` schema reference

Source of truth: `internal/config/config.go`, `schema.go`, and `presets.go` in
the [council](https://github.com/umutarmut38/council) repo (mirrored in
`docs/configuration.md`). `.council.yaml` is a **repo-local overlay**
deep-merged over `~/.council.yaml`. Top-level sections (`ui`, `sessions`,
`review`, `files`, `policy`, `env`, `experimental`) and each entry under
`agents`, `personalities`, and `personality_categories` are deep-merged, so a
local file only specifies the keys it changes.

## Top-level structure

```yaml
agents:                 # map: name -> agent config
personalities:          # map: name -> personality (optional)
personality_categories: # map: name -> category, for grouped layouts (optional)
ui:                     # layout, paging, grouping, timing
sessions:               # run storage, privacy, redaction
review:                 # post-build check gate (+ timeout/output caps)
files:                  # @file expansion limits (optional)
policy:                 # risk posture: safe | normal | aggressive (optional)
env:                    # experimental: env exported to agents (optional)
setup:                  # experimental: pre-launch commands (optional)
experimental:           # feature gates (setup_env)
```

## `agents.<name>`

The name labels the pane, branch, and artifacts; use `[A-Za-z0-9_-]+` and keep
names unique after case-folding.

| Key            | Type        | Default              | Notes |
|----------------|-------------|----------------------|-------|
| `enabled`      | bool        | `false`              | Include in default runs. |
| `command`      | list[str]   | —                    | argv to launch the CLI, e.g. `["claude"]`. |
| `cwd`          | str         | `"."`                | Working dir. |
| `color`        | str         | —                    | 256-color index (`"212"`) or hex (`"#ff5f87"`) tinting the pane border; falls back to the personality color. |
| `personality`  | str         | —                    | Name of a personality (injects `prompt_prefix`). |
| `role`         | list[str]   | `[worker, reviewer]` | `[worker]`, `[reviewer]`, or both. **Empty = both roles.** |
| `env`          | map         | —                    | Extra env merged over top-level `env` (this wins). Experimental: needs `experimental.setup_env`. |
| `terminal`     | map         | —                    | See terminal table. |
| `orchestration`| map         | —                    | See orchestration table. |

**Roles** select phases: `worker` → plan + build (produces work); `reviewer` →
vote + review (judges work). Self-judging is always prevented. Personalities are
orthogonal — they only inject prompt text.

### `agents.<name>.terminal`

| Key                    | Type   | Default | Notes |
|------------------------|--------|---------|-------|
| `renderer`             | str    | `screen`| `screen` (terminal emulator) or `transcript` (cleaned scrollback). |
| `pty_size`             | str    | `pane`  | `pane` (resize to pane) or `fixed` (use `cols`/`rows`). |
| `cols` / `rows`        | int    | 120/40  | Used with `pty_size: fixed`. |
| `send_mode`            | str    | `type`  | `type` (keystrokes) or `paste` (bracketed paste). |
| `before_send_sequence` | str    | —       | Sequence before the message, e.g. `ctrl+u` to clear the line. |
| `submit_sequence`      | str    | `cr`    | What submits the message (see sequence names). |
| `after_submit_sequence`| str    | —       | Sequence sent after submit. |
| `submit_delay_ms`      | int    | `0`     | Send the submit key this many ms after the text, as its own write. |
| `resize`               | bool   | `true` (`false` if `fixed`) | Resize the PTY when the pane resizes. |
| `color`                | bool   | `true`  | `false` sends `TERM=dumb`, `NO_COLOR=1`. |

**Sequence names** (`submit_sequence` / `before_send_sequence` /
`after_submit_sequence`): `cr` (`\r`), `lf` (`\n`), `crlf`, `esc`, `ctrl+c`,
`ctrl+d`, `ctrl+u`, `csi-enter` and `csi-…-enter` (kitty keyboard protocol),
`none`, or `raw:<bytes>`.

### `agents.<name>.orchestration`

Phases run in live interactive panes. A phase command should enable
auto-approval so the agent can write its artifact / edit code without blocking on
a permission prompt. An empty phase command falls back to `command`. **No
auto-approval flags are set by default** — opt in per agent, per phase.

| Key                        | Type      | Default            | Notes |
|----------------------------|-----------|--------------------|-------|
| `exclude`                  | bool      | `false`            | Drop from all phases. |
| `exclude_plan`             | bool      | `false`            | Drop from plan. |
| `exclude_vote`             | bool      | `false`            | Drop from vote/review. |
| `exclude_build`            | bool      | `false`            | Drop from build. |
| `plan_command`             | list[str] | falls back to `command` | Override launch for plan. |
| `vote_command`             | list[str] | falls back to `command` | Override launch for vote + review. |
| `build_command`            | list[str] | falls back to `command` | Override launch for build. |
| `plan_prompt_in_command`   | bool      | `false`            | Append the prompt as final argv instead of typing it (for `-p`-style flags). |
| `vote_prompt_in_command`   | bool      | `false`            | Same, for vote/review. |
| `build_prompt_in_command`  | bool      | `false`            | Same, for build. |

## `ui`

| Key                       | Type | Default       | Notes |
|---------------------------|------|---------------|-------|
| `layout`                  | str  | `grid`        | The only layout (`paged-grid` is accepted as an alias and normalized to `grid`). |
| `adaptive_grid`           | bool | `true`        | Size the grid to the visible panes (1 = full screen, 2 = side-by-side, 3-4 = 2x2); larger rosters page with `page_rows`/`page_cols`. `false` always uses the configured grid. |
| `detect_approval_prompts` | bool | `true`        | **Experimental.** Auto-flag a pane as needs-input when an approval-looking prompt sits at the bottom and the agent has gone quiet. |
| `page_rows` / `page_cols` | int  | grid-derived (2/2) | Page dimensions for many agents. |
| `group_by`                | str  | `none`        | `none`, `personality`, or `category`. |
| `max_scrollback_lines`    | int  | `5000`        | Per-pane scrollback kept in memory. |
| `initial_prompt_delay_ms` | int  | `3000`        | Wait after launch before broadcasting the prompt (too short = dropped; `8000` is good for many agents). |

## `sessions`

| Key        | Type | Default          | Notes |
|------------|------|------------------|-------|
| `root_dir` | str  | `.council/runs`  | Where run artifacts are stored (relative anchors to the launch dir). |
| `private`  | bool | `true`           | Owner-only artifacts (`0700` dirs, `0600` files). |
| `redact`   | bool | `false`          | Best-effort scrubbing of common secret patterns in saved transcripts. |

## `review`

| Key                      | Type      | Default     | Notes |
|--------------------------|-----------|-------------|-------|
| `check_command`          | list[str] | empty       | Build/test gate run in each build worktree before voting; a failing build is dropped. Empty = no gate. e.g. `["go","build","./..."]`, `["npm","test"]`, `["cargo","build"]`. |
| `check_timeout_seconds`  | int       | `600`       | Hard timeout per check run; a timeout is recorded as FAIL. |
| `max_check_output_bytes` | int       | `1048576`   | Cap on each check log; longer output is truncated. |

## `files`

| Key              | Type | Default   | Notes |
|------------------|------|-----------|-------|
| `allow_absolute` | bool | `false`   | Allow expanding absolute / outside-cwd `@path` refs (ignored under `policy.mode: safe`). |
| `max_bytes`      | int  | `262144`  | Per-file expansion cap; bigger files stay as `@tokens`. Binary files are always skipped. |

## `policy`

| Key    | Type | Default  | Notes |
|--------|------|----------|-------|
| `mode` | str  | `normal` | `safe` (refuse auto-approval flags, never expand absolute `@file`, always confirm adopt/clean), `normal` (warn + confirm), `aggressive` (allow flags, skip confirmations — for sandboxed/trusted environments). |

## `env`, `setup`, `experimental` (experimental, off by default)

`env` and `setup` are ignored unless `experimental.setup_env: true`. From a
repo-local file they are also subject to the trust gate. `env` does **not**
affect council's own subprocesses (git, gh).

| Key                      | Type      | Notes |
|--------------------------|-----------|-------|
| `experimental.setup_env` | bool      | Required to enable `env`/`setup`. |
| `env`                    | map       | `KEY: value` exported to every agent; per-agent `env` overrides it. |
| `setup`                  | list      | Pre-launch commands (see below). |

### `setup[]`

| Key             | Type      | Notes |
|-----------------|-----------|-------|
| `name`          | str       | Optional label shown in logs, `council doctor`, and `/setup`. |
| `command`       | list[str] | argv to run before launching agents. |
| `background`    | bool      | `true` keeps a daemon/proxy alive for the session and stops it on exit; `false` runs to completion and aborts startup on non-zero exit. |
| `wait_for_port` | int       | On a background command, block until `127.0.0.1:<port>` is listening, up to ~10s. |

## `personalities.<name>`

| Key            | Type | Notes |
|----------------|------|-------|
| `label`        | str  | Display label. |
| `category`     | str  | Key into `personality_categories`. |
| `color`        | str  | 256-color code as a string, e.g. `"114"`. |
| `order`        | int  | Sort order within groupings. |
| `prompt_prefix`| str  | Block scalar text prepended to this agent's prompts. |

## `personality_categories.<name>`

`label` (str), `color` (str, 256-color code), `order` (int). Used when
`ui.group_by: category`.

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

## Minimal overlay example (worker + reviewer)

```yaml
# .council.yaml — git-excluded locally via .git/info/exclude; do not commit.
ui:
  group_by: category
  initial_prompt_delay_ms: 8000

review:
  check_command: ["go", "test", "./..."]

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
    role: [worker]
    personality: pragmatist
    terminal:
      send_mode: paste
      before_send_sequence: ctrl+u
      submit_sequence: cr
  copilot-reviewer:
    enabled: true
    command: ["copilot"]
    role: [reviewer]
    personality: critic
    terminal:
      submit_sequence: cr
      submit_delay_ms: 250
```
