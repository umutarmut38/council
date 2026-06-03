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
| `@path/to/file` | Expand the file's contents inline when the message is sent. Type `@` to get a file picker (see [Shortcuts](shortcuts.md)). |
| `/command …` | Run a command (below). Type `/` to see suggestions; `Tab` completes the command word. |

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
| `/adopt [agent]` | `git apply` a build's diff onto your working tree as uncommitted changes — the reviewed winner, or a specific agent's build to override the recommendation. |
| `/finish` | Force-collect the current phase now (use if a pane finished but auto-detect didn't fire). |
| `/status` | Show the active run and phase. |
| `/clean` | Remove council worktrees and branches. |

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
council [--agents claude,codex]                 launch the multiplexer
council [--agents …] ask "<prompt>"             launch and broadcast a prompt
council config init [--force]                   write ~/.council.yaml
council doctor                                  check configured agent commands exist
council version                                 print build version, commit, and date
```

Orchestration from the shell — each phase opens the live panes and blocks until
you quit it:

```text
council plan  "<issue>" | --file issue.md | --issue 123    start a run and plan
council vote  [run]                                        tally ranked votes
council build [run]                                        all agents implement the winner
council run   "<issue>"                                    plan -> vote -> build, chained
council resume [run]                                       reopen an older run
council status [run]                                       show a run's artifacts
council clean                                              remove council worktrees + branches
```

Notes:

- `[run]` is a run timestamp (e.g. `20260605-130000`); omit it for the latest.
- `--agents` restricts to a comma-separated subset.
- `--file` reads the issue from a markdown file; `--issue <n>` fetches a GitHub
  issue body via `gh`.
- The in-chat `/review` and `/adopt` are not (yet) CLI subcommands.
