# Configuration

council reads `~/.council.yaml`. Create it with `council config init`. A repo may
also carry a local `.council.yaml` that layers on top (see
[Per-repo override](#per-repo-override)).

```yaml
agents: { … }                 # the agents and how to drive them
ui: { … }                     # layout, paging, grouping, timing
sessions: { root_dir: … }     # where runs are written
review: { check_command: … }  # the build gate
personality_categories: { … } # optional: persona groups
personalities: { … }          # optional: behavioral personas
```

---

## `agents`

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
| `personality` | — | Personality name (must exist under `personalities`). |

#### Roles

`role` is **structural** — it routes an agent to orchestration phases. It is a
list of `worker`, `reviewer`, or both:

| Role | Phases the agent joins |
|---|---|
| `worker` | `plan`, `build` (produces the solution) |
| `reviewer` | `vote`, `review` (judges the solution) |
| `[worker, reviewer]` *(or omitted)* | all phases — the default, backward compatible |

```yaml
agents:
  claude:  { role: [worker, reviewer] }   # plans, builds, votes, reviews
  codex:   { role: [worker] }             # only plans and builds
  copilot: { role: [reviewer] }           # only votes and reviews
```

Roles are independent of `personality` (which only injects prompt text) and
compose with it. Self-judging is always prevented: a reviewer never ranks its
own plan/diff, even when it also has the `worker` role. The legacy
`orchestration.exclude_*` flags still apply as overrides, and the `/target`
command can narrow within the role-eligible set at runtime.

### `agents.<name>.terminal`

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

#### Sequence names

`submit_sequence` / `before_send_sequence` / `after_submit_sequence` accept:
`cr` (`\r`), `lf` (`\n`), `crlf`, `esc`, `ctrl+c`, `ctrl+d`, `ctrl+u`,
`csi-enter` and `csi-…-enter` variants (kitty keyboard protocol), `none`, or
`raw:<bytes>` for an explicit sequence.

> Tip: most interactive agents work with `send_mode: type`, `submit_sequence:
> cr`, and `submit_delay_ms: 250`. `paste` is useful for multi-line input.

### `agents.<name>.orchestration`

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

---

## `ui`

| Key | Default | Meaning |
|---|---|---|
| `layout` | `grid` | Pane layout. |
| `max_scrollback_lines` | `5000` | Per-pane scrollback kept in memory. |
| `initial_prompt_delay_ms` | `3000` | Wait this long after launch before broadcasting (lets agents finish booting). Raise it if agents miss the prompt — codex's MCP load is the slowest factor; `8000` is a good value when running many agents. |
| `page_rows`, `page_cols` | grid-derived | Panes per page (for many agents). |
| `group_by` | `none` | `none`, `personality`, or `category` — orders panes and the overview. |

---

## `sessions`

| Key | Default | Meaning |
|---|---|---|
| `root_dir` | `.council/runs` | Where run directories are written. Relative paths are anchored to the directory council was launched from. |

---

## `review`

| Key | Default | Meaning |
|---|---|---|
| `check_command` | empty | Run in each build worktree to gate implementations before the review vote; ones that fail (non-zero exit) are dropped. Empty = no gate (language-agnostic; every changed build goes to the vote). Set it per stack, e.g. `["go","build","./..."]`, `["npm","test"]`, `["cargo","build"]`. |

---

## `personalities` and `personality_categories`

Optional. **Behavioral** dispositions (how an agent thinks) that drive UI
grouping and prompt injection. They are independent of [`role`](#roles), which is
structural (who builds vs. who judges).

```yaml
personality_categories:
  skeptical:
    label: Skeptical       # display label
    color: "203"           # optional 256-color code
    order: 20              # sort order within groupings

personalities:
  pessimist:
    label: Pessimist
    category: skeptical    # links to a category
    color: "203"
    order: 30
    prompt_prefix: |       # prepended to prompts sent to this agent
      Look for what can go wrong: risks, edge cases, missing tests…
```

Assign with `agents.<name>.personality: pessimist`. The `prompt_prefix` is
injected into broadcasts, `@agent`/`/send`, and orchestration prompts — but not
in direct mode. See [Workflows → Personalities](workflows.md#personalities-categories-and-targeting).

---

## Per-repo override

A repository can carry its own `.council.yaml` (or `.council.yml`) that
**layers over** the global `~/.council.yaml`. council searches from your current
directory up to the git repo root.

The merge is **deep**: top-level sections (`ui`, `sessions`, `review`,
`personalities`, …) and each agent are merged field-by-field, so a local file
only needs the keys it changes:

```yaml
# .council.yaml at a repo root — a Node project that only wants two agents
agents:
  codex: { enabled: false }
review:
  check_command: ["npm", "test"]
ui:
  initial_prompt_delay_ms: 5000
```

When a local file is applied, council prints `Using repo config <path>`.
