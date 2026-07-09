---
title: Agents
parent: Configuration
nav_section: Reference
nav_order: 1
---

# `agents`

A map of agent name → config. The name is arbitrary (it labels the pane and the
artifacts).

```yaml
agents:
  claude:
    enabled: true
    command: ["claude"]        # argv to launch the interactive agent
    cwd: "."                   # working directory (default ".")
    role: [worker, reviewer]   # optional, which phases this agent joins
    personality: optimist      # optional, see personalities
    terminal: { … }            # how council renders/sends to this agent
    orchestration: { … }       # how this agent behaves in plan/vote/build
```

| Key | Default | Meaning |
|---|---|---|
| `enabled` | `false` | Launch this agent. |
| `command` | — | argv used to start the interactive agent. |
| `cwd` | `"."` | Working directory for the process. |
| `role` | `[worker, reviewer]` | Which orchestration phases the agent joins (see [Roles](#roles)). |
| `color` | — | 256-color index (`"212"`) or hex (`"#ff5f87"`) tinting this agent's pane **border**: full strength while focused, a computed darker shade otherwise. Rendering always uses indexed (non-themeable cube) colors, so VS Code, Apple Terminal, iTerm, Ghostty, etc. show the same shades; `council doctor` prints a color test strip if a terminal misbehaves. Falls back to the personality's `color`. |
| `env` | — | Extra environment for this agent's process (`KEY: value` map), merged over the top-level [`env`](config-files-policy.md#env-and-setup-experimental) — the per-agent value wins. Experimental: requires `experimental.setup_env: true`. |
| `personality` | — | Personality name (must exist under `personalities`). |
| `usage` | — | Cost-tracking binding for this agent (see [Usage & cost](config-usage.md)). |
| `worktree` | — | Override `worktrees.freestyle` for this agent (tri-state); see [Worktrees](config-worktrees.md). |

## Roles

`role` is **structural** — it routes an agent to orchestration phases. There is
one granular role per phase:

| Role | Phase the agent joins |
|---|---|
| `planner` | `plan` (writes a plan) |
| `builder` | `build` (implements the winning plan) |
| `voter` | `vote` (ranks the anonymized plans) |
| `review` | `review` (ranks the built diffs) — review-only |
| *(omitted)* | all phases — the default, backward compatible |

> **`reviewer` vs `review`.** For back-compat, a role list of just `[reviewer]`
> is the legacy alias for `voter` + `reviewer` (**vote + review**), *not*
> review-only. Use **`review`** for an agent that only ranks built diffs (and
> doesn't vote on plans). `reviewer` still selects the review phase when it
> appears next to a granular token, so `[voter, reviewer]` and `[voter, review]`
> are equivalent.

```yaml
agents:
  claude:  { role: [planner, builder, voter, review] }    # every phase
  codex:   { role: [planner, builder] }                   # only plans and builds
  copilot: { role: [voter, review] }                      # votes and reviews
  oracle:  { role: [planner] }                            # plan-only
  judge:   { role: [review] }                             # review-only
```

**Legacy aliases.** The old coarse roles still work, expanded automatically:
`worker` = `planner` + `builder`, and `reviewer` (when the list contains *only*
legacy tokens) = `voter` + `reviewer`. As soon as any granular token appears in
the list, every token is taken literally — so `[voter, reviewer]` is vote +
review. Don't mix a legacy `worker` with granular tokens; a lone `worker` beside
them is ignored.

Roles are independent of `personality` (which only injects prompt text) and
compose with it. Self-judging is always prevented: a reviewer never ranks its
own plan/diff, even when it also plans or builds. The legacy
`orchestration.exclude_*` flags still apply as overrides, and the `/target`
command can narrow within the role-eligible set at runtime.

## `agents.<name>.terminal`

Controls rendering and how prompts are delivered into the agent's live TUI.

| Key | Default | Meaning |
|---|---|---|
| `renderer` | `screen` | `screen` (terminal emulator) or `transcript` (cleaned scrollback). |
| `pty_size` | `pane` | `pane` (size the PTY to the pane) or `fixed` (use `cols`/`rows`). |
| `cols`, `rows` | `120`, `40` | PTY size when `pty_size: fixed`. |
| `send_mode` | `type` | `type` (raw keystrokes) or `paste` (bracketed paste). |
| `before_send_sequence` | — | A sequence sent before the message, e.g. `ctrl+u` to clear the line. |
| `submit_sequence` | `cr` | What submits the message (see [sequences](#sequence-names)). |
| `after_submit_sequence` | — | A sequence sent after submitting. |
| `submit_delay_ms` | `0` | Send the submit key this many ms *after* the text, as its own write. Needed by agents that treat an Enter glued onto pasted text as a newline (e.g. cursor, copilot). |
| `resize` | `true` (false if `fixed`) | Resize the PTY when the pane resizes. |
| `color` | `true` | Pass a color-capable `TERM`; `false` sends `TERM=dumb`, `NO_COLOR=1`. |

### Sequence names

`submit_sequence` / `before_send_sequence` / `after_submit_sequence` accept:
`cr` (`\r`), `lf` (`\n`), `crlf`, `esc`, `ctrl+c`, `ctrl+d`, `ctrl+u`,
`csi-enter` and `csi-…-enter` variants (kitty keyboard protocol), `none`, or
`raw:<bytes>` for an explicit sequence.

> Tip: most interactive agents work with `send_mode: type`,
> `submit_sequence: cr`, and `submit_delay_ms: 250`. `paste` is useful for
> multi-line input.

## `agents.<name>.orchestration`

How the agent participates in the plan/vote/build phases. Each phase can use a
different launch command (e.g. with auto-approval flags) and decide whether the
prompt is typed into the TUI or appended as an argv argument.

| Key | Default | Meaning |
|---|---|---|
| `exclude` | `false` | Exclude from **all** orchestration phases. |
| `exclude_plan` / `exclude_vote` / `exclude_build` | `false` | Exclude from one phase. |
| `plan_command` / `vote_command` / `build_command` | falls back to `command` | Launch argv for that phase (e.g. `["claude","--dangerously-skip-permissions"]`). |
| `plan_prompt_in_command` / `vote_…` / `build_…` | `false` | If true, append the phase prompt as the final argv element (headless `-p`-style) instead of typing it into the TUI. |

> The default presets keep agents **interactive** for every phase (no `-p`), so
> you watch them work in the panes. Copilot is excluded from build by default.
>
> **Auto-approval flags** (`--dangerously-skip-permissions`,
> `--allow-all-tools`, `--force`, …) are never configured by default. They make
> unattended phases possible but bypass each tool's own permission prompts —
> opt in per agent (the generated config has commented examples), and
> `council doctor` will warn whenever one is configured. `policy.mode: safe`
> refuses to run with them entirely.

The full generated field tables for `agents.<name>`, `.terminal`, and
`.orchestration` are in the [Schema reference](config-schema.md).
