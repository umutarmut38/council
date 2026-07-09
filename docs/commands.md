---
title: Commands
nav_section: Usage
nav_order: 2
---

# Commands

council has two command surfaces: **in-chat commands** typed into the composer
while the app is running, and **CLI subcommands** run from your shell.

---

## In-chat composer

The box at the bottom of the screen is the **composer**. What you type is sent
when you press `Enter`.

### Message syntax

| You type | Effect |
|---|---|
| `some text` | Broadcast to the current target (all agents by default). |
| `@all message` | Send to every agent. |
| `@claude message` | Send to one agent by name. |
| `@path/to/file` | Expand the file's contents inline when the message is sent. Typing `@` opens a **vertical file picker** — `↑/↓` selects, `Enter` inserts, `Esc` closes. |
| `/command …` | Run a command (below). Typing `/` opens the **command palette**: a vertical list of matching commands, with the ones suggested for the current pipeline stage (●) on top. `↑/↓` selects, `Tab`/`Enter` completes, then `Enter` runs. |

`@agent` only targets a pane when the word after `@` is a known agent (or
`all`); otherwise `@path` is treated as a file reference.

### Pane & view commands

| Command | What it does |
|---|---|
| `/all <msg>` | Broadcast a message to every agent. |
| `/send <agent> <msg>` | Send a message to one agent. |
| `/direct [agent]` | Enter **direct mode** — keystrokes go straight to the focused pane (optionally focus `agent` first). `Esc`/`F2` returns. |
| `/focus <agent>` | Focus a pane by name. |
| `/zoom [agent]` | Toggle full-screen for the focused (or named) pane. |
| `/page next\|prev\|<n>` | Switch the visible page of panes. |
| `/overview` (`/agents`) | Open the overview list of all agents. |
| `/settings` | Open the layout settings view (grid size, grouping) for this session. |
| `/clear [agent]` | Clear pane output (all panes, or one). |
| `/save` | Save transcripts to the run directory. |

### Targeting & personalities

| Command | What it does |
|---|---|
| `/target all\|focus\|personality <name>\|category <name>` | Scope **both** broadcast messages **and** orchestration phases. e.g. `/target category review` then `/vote`. |
| `/show all\|personality <names>\|category <name>` | Choose which personalities are displayed (visibility filter). |
| `/hide personality <name>` | Hide a personality's panes. |

See [Workflows → Personalities](workflows.md#personalities-categories-and-targeting).

### Orchestration (the council flow)

| Command | What it does |
|---|---|
| `/plan <issue or @file>` | Start a run; each in-scope agent drafts a plan to `plans/<agent>.md`. |
| `/vote` | In-scope agents rank the (anonymized) plans; a winner is tallied. Auto-skipped when only one plan was produced — that plan wins and you go straight to `/build` (or `/refine`), mirroring how `/review` collapses to a single surviving build. |
| `/build` | **Stage** the build: create a git worktree per agent and relaunch the panes there — but do not send the prompt yet. |
| `/start-build` | Send the build prompt staged by `/build`. |
| `/review` | Run the check command in each build worktree, drop failures, then in-scope agents vote the best diff. |
| `/refine [note]` | Consensus round: **every planner that produced a plan** reads the council's critiques and rewrites its plan, then you're prompted to `/vote` again (the field can shift). Pass a `note` (`/refine make it simpler`) to add your own guidance. Works on a single auto-won plan too — with no critiques it just revises the plan from your note. |
| `/compare` | **Interactive build inspector.** ↑/↓ selects a build (files touched, check result, review points, live/cleaned worktree, ★ winner). `Enter` drills into its changed files vs the run base; `Enter` on a file shows its git-style colored diff; `e` opens the worktree file (or the whole worktree from the build list) in the configured editor (`ui.editor`, else `$VISUAL`/`$EDITOR`/`vim`) (`i` opens it in the integrated editor instead). Press `x` to mark a build, then `Enter` on another to diff the **two implementations against each other** (computed natively via git trees). `d` shows the full diff vs base. Esc unwinds: diff → files → builds → panes. |
| `/preview [agent]` | Show exactly what `/adopt` would change: files, dirty-tree overlap, the `git apply --check` result, **and the full diff**. This is read-only; `e` opens the diff in `$EDITOR`. |
| `/adopt [agent]` | Opens a full-screen confirmation preview and waits for `y` to apply the diff as uncommitted changes (`n` cancels; `Esc` keeps it staged so `/adopt confirm` applies it later). Name an agent to override the reviewed winner. |
| `/judge plan <agent\|letter>` | Record a human-picked plan winner (override or stand in for the vote). |
| `/judge build <agent>` | Record a human-picked build winner. |
| `/finish` | Force-collect the current phase now (use if a pane finished but auto-detect didn't fire). |
| `/status` | Show the active run and phase. |
| `/cost` | Show per-session token usage and estimated cost for the active run (requires `usage.enabled`). |
| `/report` | Write `report.md` for the run and open it in the viewer. |
| `/artifacts` | Browse the run's plans, votes, diffs, check logs, reviews, and transcripts in a split view: the list on the left, the selected file open in the **integrated editor** (`ui.editor`) on the right. `↑/↓` select · `Enter` opens it in the editor pane (editable) · `Tab` jumps into the pane · `F2`/`Ctrl+O` back to the list · `e` opens it in the external configured editor (`ui.editor`, else `$VISUAL`/`$EDITOR`/`vim`) instead. |
| `/edit [path]` | **Integrated editor.** A collapsible file-tree sidebar on the left and the configured editor (`ui.editor`, e.g. `nvim`) running inside council as a PTY pane on the right. `↑/↓` move, `→`/`Enter` expand a folder or open a file, `←` collapse, `Tab` jump to the editor pane. In the pane, keystrokes (including `Esc`) go to the editor; `F2`/`Ctrl+O` returns to the tree. Selecting another file opens it in the live editor via `ui.editor_open_cmd` (default `:e {path}`, tuned for vim/nvim). With a `path` argument that file is opened immediately. |
| `/clean` | Two-step removal: first call previews the run-stamped worktrees/branches **and** any persistent freestyle worktrees (`.council/workspaces`, when `worktrees.freestyle` is on); `/clean confirm` removes them. |
| `/refresh [agent\|all] [force]` | Reset a **freestyle worktree** (`worktrees.freestyle`) to the repo HEAD and re-seed it — the only reset path for a persistent worktree. Defaults to the focused pane; `/refresh all` does every freestyle pane. Refuses when the worktree has uncommitted changes unless you append `force` (which discards them). The pane border shows a stale marker (`⟳` when behind HEAD, `*` when dirty). |

### Recovery

| Command | What it does |
|---|---|
| `/restart <agent>` | Terminate and relaunch one pane with its current phase command. |
| `/resend [agent]` | Resend the current phase prompt — to one agent, or to everyone still missing an artifact. |
| `/nudge [agent]` | Send a short reminder to write the expected artifact. |
| `/attention <agent> [off]` | Flag (or unflag) a pane as needing your input — the manual flag stays until you engage the pane or turn it off. council also auto-detects approval prompts (**experimental**): a pane is flagged only when an approval-looking prompt (`[y/N]`, "Do you want to proceed", trust dialogs, …) sits at the bottom of its screen *and* the agent has gone quiet for ~2s; the auto-flag clears itself when output resumes. Disable with `ui.detect_approval_prompts: false`. |

### Runs & resume

| Command | What it does |
|---|---|
| `/runs` | Browse previous runs (timestamp, prompt, artifacts, winner). |
| `/resume [run]` | Reopen an older run. If it was interrupted inside plan/vote/build/review, relaunch that phase, keep existing artifacts, and prompt only the unfinished agents. |

### Misc

| Command | What it does |
|---|---|
| `/setup` | Show pre-launch setup/env status: each setup command's label, PID, lifecycle state, readiness-gate result, and captured output, plus the exported env keys (keys only — values are never shown). Empty unless `env`/`setup` is configured (experimental). |
| `/help` | Open the full command reference in the pager. |
| `/quit` (`/exit`) | Quit (same as `Ctrl+X`). |

---

## CLI subcommands

<!-- BEGIN GENERATED: cli-general -->
```text
council [--agents claude,codex] [--no-local-config]                                  launch the interactive multiplexer
council [--agents claude,codex] ask "<prompt>"                                       launch and broadcast a prompt
council config init [--force] [--interactive]                                        write the default (safe) config
council config wizard                                                                interactive setup
council config add-agent <preset> [--name x] [--role planner,builder,voter,review]   add a known agent CLI to the config
council config schema [--json]                                                       print the configuration reference (Markdown, or JSON Schema)
council doctor [--fix]                                                               check config, commands, repo, run dirs (--fix applies safe fixes)
council trust [--revoke|--show]                                                      trust this repo's .council.yaml
council version                                                                      print build version, commit, and date
```
<!-- END GENERATED: cli-general -->

Orchestration from the shell — each phase opens the live panes and blocks until
you quit it:

<!-- BEGIN GENERATED: cli-orchestration -->
```text
council plan "<issue>" | --file issue.md | --issue 123 [--agents a,b] [--base ref]                         start a run; each agent drafts a plan
council vote [run] [--agents a,b]                                                                          tally ranked votes into a winner
council build [run] [--agents a,b]                                                                         all agents implement the winning plan
council review [run] [--agents a,b]                                                                        gate builds + reviewers pick the best
council adopt [run] [agent] [--dry-run] [--yes]                                                            preview + apply a build's diff
council run "<issue>" | --file issue.md | --issue 123 [--agents a,b] [--base ref]                          plan -> vote -> build
council resume [run] [--agents a,b]                                                                        reopen an older run with fresh agent processes
council status [run]                                                                                       show a run's phase, artifacts, and winners
council cost [run] [--since 30d] [--source ledger|codeburn] | cost prices refresh | cost models [filter]   per-session usage and estimated cost
council report [run] [--post N]                                                                            write report.md (--post N comments on issue #N)
council pr [run] [agent]                                                                                   open a PR from a build branch (via gh)
council scorecard                                                                                          agent performance across runs
council artifacts scan [run] [--all]                                                                       scan run artifacts for likely secrets
council queue add|list|run|clear                                                                           batch issues through council
council stack detect|set <go|node|rust|python>                                                             set review.check_command
council clean [--dry-run] [--yes]                                                                          remove council worktrees + branches
council clean-runs [--keep N] [--dry-run] [--yes]                                                          prune old run artifacts
```
<!-- END GENERATED: cli-orchestration -->

Notes:

- `[run]` is a run timestamp (e.g. `20260605-130000`); omit it for the latest.
- `--agents` restricts to a comma-separated subset.
- `--file` reads the issue from a markdown file; `--issue <n>` fetches a GitHub
  issue body via `gh`.
- `--base <ref>` sets the base ref for the per-agent worktrees (default `HEAD`).
- `council config schema` prints the configuration reference — the same tables as
  [Configuration → Schema reference](configuration.md#schema-reference-generated); `--json` emits a JSON Schema (draft 2020-12) instead.
- Repo-local `.council.yaml` files are only applied once trusted (`council trust`); pass `--no-local-config` to ignore them entirely.
