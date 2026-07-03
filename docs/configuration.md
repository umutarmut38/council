---
title: Configuration
nav_section: Reference
nav_order: 1
---

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

> **Prefer a guided setup for a repo?** The
> [`council-config` Agent Skill](../skills/README.md) interviews you and writes a
> repo-local `.council.yaml` overlay (git-excluded via `.git/info/exclude`). It
> installs into Claude Code, Codex CLI, Cursor, GitHub Copilot, and OpenCode via
> `scripts/install-skill.sh`.

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
| `env` | — | Extra environment for this agent's process (`KEY: value` map), merged over the top-level [`env`](#env-and-setup-experimental) — the per-agent value wins. Experimental: requires `experimental.setup_env: true`. |
| `personality` | — | Personality name (must exist under `personalities`). |

#### Roles

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

> Tip: most interactive agents work with `send_mode: type`,
> `submit_sequence: cr`, and `submit_delay_ms: 250`. `paste` is useful for
> multi-line input.

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
> opt in per agent (the generated config has commented examples), and
> `council doctor` will warn whenever one is configured. `policy.mode: safe`
> refuses to run with them entirely.

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
| `theme` | `default` | Overall color palette — a built-in or a custom name from `themes`. See [Themes](#themes). |

### Themes

`ui.theme` recolors the whole UI chrome — header, footer, pane borders,
dividers, command suggestions, and the diff viewer. Four built-ins ship:

- `default` — the original palette.
- `nord` — cool: steel-blue brand, frost-cyan focus, slate borders.
- `solarized` — warm: yellow/orange brand, amber focus, teal rail.
- `mono` — high-contrast grayscale; red is reserved for warnings.

```yaml
ui:
  theme: nord
```

Per-agent and per-personality `color` settings still win for **pane borders**;
the theme drives everything else. `council doctor` prints its color test strip
in the active theme so you can preview it in your terminal.

Define your own palette under `ui.themes.<name>` and select it with `theme`.
Each role is an optional 256-color index (`"212"`) or hex (`"#ff5f87"`); any role
you omit inherits the `default` palette. Colors are indexed-256 only (no
truecolor), so they render identically across terminals.

```yaml
ui:
  theme: midnight
  themes:
    midnight:
      title: "117"
      focus: "213"
      warn: "203"
```

The full list of roles (`title`, `heading`, `status`, `rail`, `border`,
`focus`, `suggest`, `input`, `warn`, `faint`) is in the
[generated reference](#uithemesname) below.

---

## `env` and `setup` (experimental)

Council can export environment variables to agents and run commands before any
agent launches — a vendor-agnostic way to wire agents to a local service (a
context-compression proxy, a mock backend, a tunnel) without council knowing
anything about it. For a complete, runnable example, see
[examples/configs/headroom.yaml](https://github.com/umutarmut38/council/blob/main/examples/configs/headroom.yaml).

> **Experimental — off by default.** `setup` runs **arbitrary commands** and
> `env` mutates the agent environment, so the whole feature is opt-in. Set
> `experimental.setup_env: true` to turn it on in the merged effective config;
> otherwise any `env`/`setup` you configure is ignored and `council doctor`
> warns that it was. A trusted repo-local config can use `env`/`setup` when this
> flag is enabled globally.

```yaml
# Required: env/setup do nothing unless this is set.
experimental:
  setup_env: true

# exported to every agent process (merged under each agent's own env, which
# wins). Does NOT affect council's own subprocesses (git, gh).
env:
  OPENAI_BASE_URL: "http://127.0.0.1:8787"

# commands run once before agents launch (and re-run per one-shot CLI phase).
setup:
  - name: proxy                                   # optional label for logs/doctor
    command: ["headroom", "proxy", "--port", "8787"]
    background: true                              # supervised; stopped on exit
    wait_for_port: 8787                           # block until it's listening
  - command: ["docker", "compose", "up", "-d"]    # one-shot: run to completion
```

| Key | Meaning |
|---|---|
| `experimental.setup_env` | **Required to enable this feature.** `false` by default — `env`/`setup` are ignored unless this is `true`. |
| `env` | `KEY: value` map exported to every agent. Per-agent `agents.<name>.env` overrides it. |
| `setup[].command` | argv to run before launching agents. |
| `setup[].background` | `true` keeps the process alive for the session and terminates it on exit (a daemon/proxy). `false` (default) runs it to completion — a non-zero exit aborts startup. |
| `setup[].wait_for_port` | On a background command, block startup until `127.0.0.1:<port>` is listening (a readiness gate), up to ~10s. |
| `setup[].name` | Optional label shown in logs and `council doctor`. |

`council doctor` lists the exported env keys and setup commands and checks each
setup binary is on `PATH`. Setup runs once per interactive session and once per
`council run`; the standalone one-shot phases (`council plan`, etc.) each run it
for their own invocation.

> **Trust.** `setup` runs arbitrary commands, so from a **repo-local**
> `.council.yaml` it is gated exactly like the rest of the config: an untrusted
> or changed local file never runs setup or applies its env (`council trust` to
> approve, `--no-local-config` to ignore). Your global `~/.council.yaml` is
> always trusted.

See [`examples/configs/headroom.yaml`](https://github.com/umutarmut38/council/blob/main/examples/configs/headroom.yaml)
for routing agents through a local compression proxy.

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

## `worktrees`

Opt-in per-pane isolation for **freestyle** panes — the agents you chat with
directly, outside the orchestrated workflow. Off by default. Orchestration is
never affected: `/plan`, `/build`, and `/review` always use their own
run-stamped worktrees under `.council/worktrees/<stamp>/`.

```yaml
worktrees:
  freestyle: true          # each freestyle pane gets its own worktree
  seed:                    # extra files copied in, on top of the built-in allowlist
    - .env.example
    - config/*.yaml
```

With `freestyle: true`, each freestyle pane runs in its own **persistent,
repo-local** git worktree at `.council/workspaces/<agent>`, created on first use
as a detached checkout (no `council/<agent>` branch spam). This buys two things:

- **Per-pane cost.** Same-tool panes get distinct working directories, so
  provider-session [reconciliation](#usage) can attribute usage to each pane
  instead of collapsing several into one combined row.
- **File isolation.** Panes editing the same files no longer stomp each other.

Worktrees are **reused across sessions and never auto-reset**, so a pane keeps its
work between runs. The pane border shows a staleness marker — `⟳` when the
worktree is behind the repo HEAD, `*` when it has uncommitted changes.
[`/refresh`](commands.md) resets a worktree to HEAD and re-seeds it (it refuses
when the worktree is dirty unless you append `force`); [`/clean`](commands.md)
lists and removes them. Staleness is probed on demand, never on a timer.

`seed` copies additional files/globs (relative to the repo root) into each
worktree when it is created, on top of the built-in instruction-file allowlist:
`CLAUDE.md`, `AGENTS.md`, `GEMINI.md`, `QWEN.md`, `.cursorrules`,
`.github/copilot-instructions.md`, and `.mcp.json`. Use it for git-ignored files
an agent still needs (a local `.env`, machine config). Nothing is seeded that you
don't list, beyond that allowlist.

Some tools can't run in a clean detached checkout because they rely on the live
working tree (an installed `node_modules`, build output, uncommitted state). Keep
those in council's launch directory with a per-agent override:

```yaml
agents:
  copilot:
    worktree: false        # stay in the launch directory even when freestyle is on
```

Freestyle worktrees are a no-op outside a git repository.

---

## `usage`

A local, provider-agnostic cost ledger. Off by default; everything it records
stays under `.council/`, and it never needs a provider account, API key, or
billing API. Turn it on to see a run-cost total in the header, a per-pane cost in
each pane border, and the `/cost` breakdown:

```yaml
usage:
  enabled: true
agents:
  codex:
    enabled: true
    usage:
      tool: codex          # provider-session reader: claude|codex|copilot|cursor|opencode
      model: gpt-5-codex   # model for estimated pricing until a report supplies one
```

### How cost is calculated

council only knows what it can observe locally, so cost is built in two layers,
and every number carries a **confidence**:

1. **Estimated floor.** As council sends a prompt and the pane streams output, it
   counts the characters with a local estimator (`bytes4` — roughly 4 bytes per
   token — or `runes4`) and prices them immediately. Input is measured from the
   prompt the model actually sees (personality prefix + your text), *not* the
   terminal control bytes on the wire; output is the transcript delta. This is a
   live lower bound, shown as **estimated**.
2. **Reported totals.** When an agent declares a `usage.tool`, council reads that
   CLI's *own* session files (Claude Code, Codex, Copilot, opencode; cursor-agent
   records no token counts) and reconciles the real numbers over the estimate,
   shown as **reported**. Cached/reused context is kept in its own column and
   priced at the cheaper cache-read rate rather than double-charged as fresh
   input.

council never guesses an agent's tool or model from its `command` — set
`usage.tool` and `usage.model` explicitly, or the row stays an estimate. An
unknown `usage.tool` is rejected when the config loads.

Confidence shows as a prefix in the header, the pane badges, and the `/cost`
share bars, so it reads at a glance:

| Shown | Meaning |
|---|---|
| `~$0.02` | **estimated** — council's local character estimate |
| `$0.02` | **reported** — real token counts from the CLI's session files |
| `$?` | **unknown** — usage recorded, but no price could be resolved |

An unknown paid model stays `$?`; council never silently reports it as `$0`. Real
sub-cent costs show extra decimals (`~$0.0047`) instead of rounding to `$0.00`.

### Reading `/cost`

`/cost` in the TUI (or `council cost [run]` from the CLI) shows a per-session
table and a share bar of where the run's spend went:

```text
Agent          Phase    Tool   Model         Input  Cache  Output  Cost     Source           Confidence
codex-builder  session  codex  gpt-5.4-mini  8.8k   4.5k   36      $0.01    litellm-bundled  exact
codex-planner  session  codex  gpt-5.4-mini  5.3k   8.1k   36      $0.0047  litellm-bundled  exact
Total                                         14.1k  12.6k  72      $0.0147

Share:
codex-builder  ███████████░░░░░░░░░  68%  $0.01
codex-planner  ██████░░░░░░░░░░░░░░░  32%  $0.0047
```

**Input** is *fresh* input only; **Cache** is the reused context, priced at the
cache-read rate. That split is why two panes that sent nearly the same context can
cost different amounts — above, the planner sent less fresh input and hit cache
more, so it is cheaper despite the near-identical total. **Source** and
**Confidence** describe the *price* (where the rate came from and whether it was
an exact table match), not the token counts. The header carries the run total
(right-pinned), and while you type, the composer shows a live `~N tok` estimate of
what the send will cost before it fans out to every agent.

### Attribution and pricing

**Reconciliation** matches an estimate to a reported session by `(tool, cwd)`, and
credits only sessions whose activity overlaps the run. Two same-tool panes sharing
one directory can't be told apart, so they report as a single combined row; give
them distinct directories with [`worktrees.freestyle`](#worktrees) for a per-pane
breakdown. A few CLIs (notably Copilot) write their totals only when the process
exits, so their input shows the estimate live and upgrades to the reported number
once the pane — or council — exits.

**Pricing** resolves a model in order: a user `usage.prices` profile → a user or
built-in `usage.model_aliases` → a fresh LiteLLM cache → the bundled LiteLLM
snapshot → unknown. Refresh the cache with `council cost prices refresh` (the only
step that touches the network). A `usage.prices` profile sets input/output
per-million rates; cache-write/read rates derive from the input rate unless you
set them, so a cache-heavy session is never priced at `$0`. A profile older than
`stale_price_after_days` raises a warning.

View costs with `/cost` in the TUI, or `council cost [run]` / `council cost --since
30d` from the CLI. `council cost --source codeburn` relays machine-wide totals
from the optional `codeburn` CLI (`npm i -g codeburn`) for tools council doesn't
launch itself.

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
| `safe` | Refuses to run when enabled agents carry auto-approval flags; absolute `@file` refs never expand; destructive commands always confirm. |
| `normal` *(default)* | Doctor warns about risky flags; destructive commands ask for confirmation. |
| `aggressive` | Skips non-interactive adopt and clean confirmations — for sandboxed or fully-trusted environments. In-chat `/adopt` still confirms. |

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

---

## Schema reference (generated)

The table below is generated from the config structs by `council config schema`
(and `go generate ./...`); the prose above is the narrative version. A test fails
if this section drifts from the types, so it stays authoritative.

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
| `role` | list | all phases | Orchestration phases the agent joins, one token per phase: `planner`, `builder`, `voter`, `review`. Omit for all phases. Legacy aliases still work: `worker` = `planner`+`builder`, and a bare `reviewer` = `voter`+`reviewer` (use `review` for a review-only agent). |
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
