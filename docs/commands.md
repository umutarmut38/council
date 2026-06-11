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
| `/vote` | In-scope agents rank the (anonymized) plans; a winner is tallied. |
| `/build` | **Stage** the build: create a git worktree per agent and relaunch the panes there — but do not send the prompt yet. |
| `/start-build` | Send the build prompt staged by `/build`. |
| `/review` | Run the check command in each build worktree, drop failures, then in-scope agents vote the best diff. |
| `/refine` | Consensus round: the winning planner reads the reviewers' critiques and rewrites its plan before `/build`. |
| `/compare` | **Interactive build inspector.** ↑/↓ selects a build (files touched, check result, review points, live/cleaned worktree, ★ winner). `Enter` drills into its changed files vs the run base; `Enter` on a file shows its git-style colored diff; `e` opens the worktree file (or the whole worktree from the build list) in `$EDITOR`. Press `x` to mark a build, then `Enter` on another to diff the **two implementations against each other** (computed natively via git trees). `d` shows the full diff vs base. Esc unwinds: diff → files → builds → panes. |
| `/preview [agent]` | Show exactly what `/adopt` would change: files, dirty-tree overlap, the `git apply --check` result, **and the full diff**. A clean preview is staged — press `y` in the viewer to apply, `n` to cancel, `e` to open the diff in `$EDITOR`. |
| `/adopt [agent]` | Opens the same full-screen preview and waits for `y` to apply the diff as uncommitted changes (`n` cancels). Name an agent to override the reviewed winner. `policy.mode: aggressive` applies immediately without the preview. |
| `/judge plan <agent\|letter>` | Record a human-picked plan winner (override or stand in for the vote). |
| `/judge build <agent>` | Record a human-picked build winner. |
| `/finish` | Force-collect the current phase now (use if a pane finished but auto-detect didn't fire). |
| `/status` | Show the active run and phase. |
| `/report` | Write `report.md` for the run and open it in the viewer. |
| `/artifacts` | Browse the run's plans, votes, diffs, check logs, reviews, and transcripts in-app. `Enter` views in the pager; `e` opens the file in `$VISUAL`/`$EDITOR` (vim by default). |
| `/clean` | Two-step removal: first call previews the worktrees/branches; `/clean confirm` removes them. |

### Recovery

| Command | What it does |
|---|---|
| `/restart <agent>` | Terminate and relaunch one pane with its current phase command. |
| `/resend [agent]` | Resend the current phase prompt — to one agent, or to everyone still missing an artifact. |
| `/nudge [agent]` | Send a short reminder to write the expected artifact. |
| `/attention <agent> [off]` | Flag (or unflag) a pane as needing your input. council also auto-detects common approval prompts (`[y/N]`, "Do you want to…", trust prompts) and highlights the pane + footer. |

### Runs & resume

| Command | What it does |
|---|---|
| `/runs` | Browse previous runs (timestamp, prompt, artifacts, winner). |
| `/resume [run]` | Reopen an older run. If it was interrupted inside plan/vote/build/review, relaunch that phase, keep existing artifacts, and prompt only the unfinished agents. |

### Misc

| Command | What it does |
|---|---|
| `/help` | List commands in the status line. |
| `/quit` (`/exit`) | Quit (same as `Ctrl+X`). |

---

## CLI subcommands

```text
council [--agents claude,codex] [--no-local-config]   launch the multiplexer
council [--agents …] ask "<prompt>"             launch and broadcast a prompt
council config init [--force]                   write ~/.council.yaml (safe defaults, agents disabled)
council config wizard                           interactive setup: detect CLIs, roles, stack, policy
council config add-agent <preset> [--role …]    add a known agent CLI (claude, codex, cursor, copilot, opencode)
council doctor                                  check config, commands, roles, git, run dirs, risky flags
council trust [--revoke|--show]                 trust (or audit) this repo's .council.yaml
council version                                 print build version, commit, and date
```

Orchestration from the shell — each phase opens the live panes and blocks until
you quit it:

```text
council plan  "<issue>" | --file issue.md | --issue 123    start a run and plan
council vote  [run]                                        tally ranked votes
council build [run]                                        all agents implement the winner
council review [run]                                       gate builds, reviewers pick the best
council adopt [run] [agent] [--dry-run] [--yes]            preview + apply a build's diff
council run   "<issue>"                                    plan -> vote -> build, chained
council resume [run]                                       reopen an older run
council status [run]                                       phase, artifacts, winners, check results
council report [run] [--post N]                            write report.md (--post comments on issue N via gh)
council pr [run] [agent]                                   push the build branch and open a PR via gh
council scorecard                                          agent performance across all runs
council queue add|list|run|clear                           batch several issues through council
council stack detect|set <go|node|rust|python>             set review.check_command in .council.yaml
council clean [--dry-run] [--yes]                          remove council worktrees + branches
council clean-runs [--keep N] [--dry-run] [--yes]          prune old run artifact directories
```

Notes:

- `[run]` is a run timestamp (e.g. `20260605-130000`); omit it for the latest.
- `--agents` restricts to a comma-separated subset.
- `--file` reads the issue from a markdown file; `--issue <n>` fetches a GitHub
  issue body via `gh`.
- Repo-local `.council.yaml` files are only applied once trusted (`council trust`); pass `--no-local-config` to ignore them entirely.
