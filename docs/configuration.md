# Configuration

council reads `~/.council.yaml`. Create it with `council config init`. A repo may
also carry a local `.council.yaml` that layers on top (see
[Per-repo override](#per-repo-override)).

```yaml
agents: { … }                 # the agents and how to drive them
ui: { … }                     # layout, paging, grouping, timing
sessions: { root_dir: … }     # where runs are written, privacy, redaction
review: { check_command: … }  # the build gate (+ timeout and output caps)
files: { … }                  # optional: @file expansion limits
policy: { mode: … }           # optional: safe | normal | aggressive
personality_categories: { … } # optional: persona groups
personalities: { … }          # optional: behavioral personas
```

The generated default config ships every agent preset **disabled** and without
auto-approval flags — enable what you use, or run `council config wizard`.

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
| `color` | — | 256-color index (`"212"`) or hex (`"#ff5f87"`) tinting this agent's pane **border**: full strength while focused, a computed darker shade otherwise. Rendering always uses indexed (non-themeable cube) colors, so VS Code, Apple Terminal, iTerm, Ghostty, etc. show the same shades; `council doctor` prints a color test strip if a terminal misbehaves. Falls back to the personality's `color`. |
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
>
> **Auto-approval flags** (`--dangerously-skip-permissions`,
> `--allow-all-tools`, `--force`, …) are never configured by default. They make
> unattended phases possible but bypass each tool's own permission prompts —
> opt in per agent (the generated config has commented examples), and `council
> doctor` will warn whenever one is configured. `policy.mode: safe` refuses to
> run with them entirely.

---

## `ui`

| Key | Default | Meaning |
|---|---|---|
| `layout` | `grid` | Pane layout. |
| `adaptive_grid` | `true` | Size the grid to the visible panes: 1 pane fills the screen, 2 sit side by side at full height, 3-4 use a 2x2. Larger rosters page with `page_rows` x `page_cols`. Adjusting rows/cols in `/settings` locks the layout for that session; set `false` to always use the configured grid. |
| `detect_approval_prompts` | `true` | **Experimental.** Auto-flag a pane as "needs input" when an approval-looking prompt sits at the bottom of its screen and the agent has been quiet for ~2s. Heuristic by nature — `/attention <agent>` is the manual, reliable path. Set `false` to disable. |
| `max_scrollback_lines` | `5000` | Per-pane scrollback kept in memory. |
| `initial_prompt_delay_ms` | `3000` | Wait this long after launch before broadcasting (lets agents finish booting). Raise it if agents miss the prompt — codex's MCP load is the slowest factor; `8000` is a good value when running many agents. |
| `page_rows`, `page_cols` | grid-derived | Panes per page (for many agents). |
| `group_by` | `none` | `none`, `personality`, or `category` — orders panes and the overview. |

---

## `sessions`

| Key | Default | Meaning |
|---|---|---|
| `root_dir` | `.council/runs` | Where run directories are written. Relative paths are anchored to the directory council was launched from. |
| `private` | `true` | Run artifacts (raw logs, transcripts, prompts, diffs, check logs) are written owner-only: `0700` directories, `0600` files. Set `false` for shared-machine workflows that need group reads. |
| `redact` | `false` | Best-effort scrubbing of common secret patterns (AWS/GitHub/OpenAI/Slack keys, bearer tokens, PEM blocks, `api_key=` assignments) from **saved transcripts**. Raw PTY logs are a live stream and are not redacted — keep `private: true`. |

Old runs accumulate; prune them with `council clean-runs --keep 10`.

---

## `review`

| Key | Default | Meaning |
|---|---|---|
| `check_command` | empty | Run in each build worktree to gate implementations before the review vote; ones that fail (non-zero exit) are dropped. Empty = no gate (language-agnostic; every changed build goes to the vote). Set it per stack, e.g. `["go","build","./..."]`, `["npm","test"]`, `["cargo","build"]` — or let `council stack detect` write it for you. |
| `check_timeout_seconds` | `600` | Hard timeout per check run, so a hung test can't block review forever. A timeout is recorded as FAIL in the check log. |
| `max_check_output_bytes` | `1048576` | Cap on each check log; longer output is truncated. |

---

## `files`

Limits for `@path` file-reference expansion in prompts and issues.

| Key | Default | Meaning |
|---|---|---|
| `allow_absolute` | `false` | By default only paths **inside the working directory** expand — a pasted task can't quietly inline `~/.ssh/id_rsa` into an agent prompt. Set `true` to allow absolute/outside paths (ignored under `policy.mode: safe`). |
| `max_bytes` | `262144` | Per-file size cap; bigger files stay as `@tokens`. Binary files are always skipped. |

---

## `policy`

```yaml
policy:
  mode: normal   # safe | normal | aggressive
```

| Mode | Behavior |
|---|---|
| `safe` | Refuses to run when enabled agents carry auto-approval flags; absolute `@file` refs never expand; adopt/clean always confirm. |
| `normal` *(default)* | Doctor warns about risky flags; adopt/clean ask for confirmation. |
| `aggressive` | Skips the adopt/clean confirmations — for sandboxed or fully-trusted environments. |

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

### Trust

A repo-local config can change **which commands council executes** (including
auto-approval flags), so it is only applied once you trust it:

- The first time council sees a repo's `.council.yaml` (or whenever its
  content changes), it asks before applying it and remembers your answer by
  content hash.
- `council trust` trusts the current repo's config explicitly (useful in
  scripts); `council trust --show` audits it; `council trust --revoke` forgets it.
- `--no-local-config` ignores repo-local config for one invocation.
- The trust store lives under your user config directory
  (`council/trust.json`). `council stack` writes are auto-trusted — you just
  authored them.
