---
title: Workflows
nav_order: 4
---

# Workflows

- [The multiplexer](#the-multiplexer)
- [The council: plan → vote → build → review → adopt](#the-council-plan--vote--build--review--adopt)
- [Personalities, categories, and targeting](#personalities-categories-and-targeting)
- [Pages and overview (many agents)](#pages-and-overview-many-agents)
- [Resuming a run](#resuming-a-run)
- [Per-repo configuration](#per-repo-configuration)
- [Where things are written](#where-things-are-written)

---

## The multiplexer

The simplest use: run several agents at once and talk to them.

```bash
council                      # launch all enabled agents
```

- Type in the composer and `Enter` to **broadcast** to every pane.
- `Tab` / `Shift+Tab` move focus; `Ctrl+F` zooms the focused pane full screen.
- `F2` enters **direct mode** so your keystrokes go straight to the focused
  agent (use its own UI directly), `Esc` returns.
- `@claude do X` sends to one agent; `@spec.md` inlines a file into your message.
- `Ctrl+S` saves transcripts; `Ctrl+X` quits.

## The orchestrator HUD

While a run is active the chrome carries its state, so you never have to
reconstruct the workflow from memory:

- **Phase rail** (third header line):
  `Plan 2/2 ✓  Vote 0/2 ●  Build ○ … · Next: /vote` — artifact counts, the
  active phase, and the recommended next command.
- **Pane badges** show what each agent owes: `vote · waiting for VOTE.md`,
  `vote · wrote VOTE.md`, `build · working`, `needs input`.
- **Blocked panes** turn the border orange and put a recovery hint in the
  footer. Auto-detection is **experimental**: it fires only when an
  approval-looking prompt (`[y/N]`, "Do you want to proceed", trust dialogs)
  is visible at the bottom of the pane and the agent has gone quiet, and it
  clears itself when output resumes. `/attention <agent>` flags one manually
  (and sticks); `F2` direct mode answers the prompt.
- **Footer hints** are phase-aware: next actions during a run, the generic
  shortcut list otherwise.
- **Command palette**: typing `/` lists every command vertically, with the
  ones that make sense at the current stage marked ● and sorted first —
  `↑/↓` to select, `Tab`/`Enter` to complete. After plans land it suggests
  `/vote`; after the vote `/build` and `/refine`; after review `/compare`,
  `/preview`, `/adopt`; and so on.
- **Ctrl+G overview** is a run dashboard: phase progress plus every agent's
  role, personality, and artifact state.

## The council: plan → vote → build → review → adopt

The orchestration turns one issue into competing solutions and converges on one.
Drive it from the composer (or the matching CLI subcommands).

### 1. Plan

```text
/plan add a retry-with-backoff to the HTTP client
/plan @docs/issue.md          # or read the issue from a file
```

Each participating agent writes an implementation plan to
`.council/runs/<ts>/plans/<agent>.md`. Panes show a `✓plan` marker as each file
appears; when all are in, council collects them. (If one finishes without
writing, `/finish` force-collects.)

### 2. Vote

```text
/vote
```

The plans are **anonymized** (`Plan A`, `Plan B`, …) with a randomized,
per-run letter mapping. Each voter ranks them and writes `votes/<agent>.md`
ending in:

```text
RANKING: B > A > C
WINNER: B
```

Votes are tallied by **Borda count**. Each voter only ever ranks the *other*
plans — it cannot vote for its own — so self-bias is structurally prevented. The
winner is written to `votes/result.json`.

### 3. Build (staged) and start

```text
/build         # creates a git worktree + branch per agent and relaunches the
               # panes there, but does NOT send the prompt yet
/start-build   # send the build prompt (each agent implements the winning plan)
```

Splitting build/start lets you adjust each tool (open files, set context) before
kicking everyone off. Build runs in **isolated worktrees**
(`.council/worktrees/<ts>/<agent>` on branch `council/<agent>/<ts>`), so agents never
touch your working tree or each other.

### 4. Review and adopt

```text
/review        # run the check command in each build worktree; drop the ones
               # that changed nothing or failed; agents vote the best diff
/adopt         # git apply --3way the winning diff onto your working tree
```

`/review` gates with `review.check_command` (see
[Configuration](configuration.md)); it's empty by default (language-agnostic —
every changed build goes to the vote). Checks run with a timeout and an output
cap, so a hung test command can't stall the review. One survivor wins
outright; zero means nothing passed. The build diffs include newly-created
files (staging respects `.gitignore`, so build outputs like `node_modules/`
are skipped only if they're ignored).

Before adopting, inspect the field:

```text
/compare           # interactive: inspect each build's files and diffs,
                   # open worktree files in $EDITOR, and diff two builds
                   # against each other (x marks the first one)
/preview [agent]   # exactly what /adopt would change, and whether it applies cleanly
```

`/compare` answers both questions that matter before adoption: *what did
this build change relative to the repo?* (Enter on a build → file list →
per-file git-style diffs, `e` opens the real worktree file in vim/neovim)
and *how do two builds differ from each other?* (`x` on one, Enter on the
other — the diff is computed from the worktrees' git trees, so it is exactly
what `git diff` would say).

`/adopt` is deliberately two-step: the first call preflights the diff
(`git apply --check --3way`), shows the touched files, and warns when your
working tree has uncommitted changes; `/adopt confirm` applies it — still
**uncommitted**, for you to review and commit. `/preview` opens the same
preflight **plus the full diff** in the pager and stages it: press `y` there
to apply, `n` to cancel. **`/adopt <agent>`** stages a different agent's build
to override the recommendation, and `/judge build <agent>` records your own
pick as the winner. The same flow exists in the shell: `council review`,
`council adopt [run] [agent] [--dry-run] [--yes]`.

### Optional: refine before building

```text
/vote
/refine        # the winning planner reads every reviewer's critique and
               # rewrites its plan (risks + test checklist included)
/build
```

`/refine` keeps the original plan as `plans/<agent>.orig.md`. Use it when the
vote surfaced objections worth absorbing before agents burn build tokens.

### Reports

`/report` (or `council report [run]`) writes `report.md` into the run
directory: issue, plans, vote tally and ballots, build checks, review tally,
adoption, and phase timings. `council report --post <issue>` comments it on
the GitHub issue, and `council pr` opens a pull request from the winning
build branch with the report as the body. `council scorecard` aggregates
plan/build wins, check pass rates, and participation across all runs.

### Or run it end-to-end

```bash
council run "add a retry-with-backoff to the HTTP client"   # plan -> vote -> build
```

## Roles: workers and reviewers

Decide which agents **build** the solution and which agents **judge** it, with a
`role` on each agent. Roles are structural — they route agents to phases
automatically, so you don't have to drive targeting by hand.

```yaml
agents:
  codex:   { role: [worker] }              # plans and builds
  cursor:  { role: [worker] }              # plans and builds
  copilot: { role: [reviewer] }            # votes and reviews
  claude:  { role: [worker, reviewer] }    # does everything (also the default)
```

| Phase | Who runs it | What they judge (candidates) |
|---|---|---|
| `plan` | workers | — |
| `vote` | reviewers | every worker's plan |
| `build` | workers | the winning plan |
| `review` | reviewers | every worker's diff |

- Omitting `role` (or `[worker, reviewer]`) means the agent does everything —
  existing configs are unchanged.
- A reviewer never ranks its own artifact, even if it's also a worker.
- This is **structural**; `personality` (below) is **behavioral** and
  independent — an agent can be a `worker` with a `pessimist` personality.

With roles set, the flow is just: `/plan …` → `/vote` → `/build` →
`/start-build` → `/review` → `/adopt`. Routing happens automatically; you don't
need to `/target` between phases unless you want to narrow further.

## Personalities, categories, and targeting

Give agents personas, group the UI by them, inject persona text into prompts,
and **scope each phase** to a subset at runtime.

### Configure (in `~/.council.yaml`)

Personalities are **behavioral dispositions** (how an agent thinks), not job
titles — those are [roles](#roles-workers-and-reviewers).

```yaml
personality_categories:
  constructive: { label: Constructive, order: 10 }
  skeptical: { label: Skeptical, order: 20 }

personalities:
  optimist:
    label: Optimist
    category: constructive
    color: "114"
    prompt_prefix: |
      You see opportunity and move fast. Favor pragmatic, shippable solutions.
  pragmatist:
    label: Pragmatist
    category: constructive
    prompt_prefix: |
      Prefer the smallest, simplest change that fully solves the problem.
  pessimist:
    label: Pessimist
    category: skeptical
    color: "203"
    prompt_prefix: |
      Look for what can go wrong: risks, edge cases, failure modes, missing tests.
  critic:
    label: Critic
    category: skeptical
    prompt_prefix: |
      Scrutinize for bugs, regressions, missing tests, and brittle assumptions.

agents:
  codex:   { role: [worker],   personality: optimist }
  cursor:  { role: [worker],   personality: pragmatist }
  copilot: { role: [reviewer], personality: critic }

ui:
  group_by: category   # none | personality | category
```

A personality's `prompt_prefix` is prepended to prompts sent to that agent —
broadcasts, `@agent`/`/send`, and the orchestration prompts for plan/vote/build/
review. (Direct mode is exempt: it's meant to be raw keystrokes.)

### Target a persona per phase

`/target` scopes **both** messages and orchestration phases. Switch personas as
you move through the flow:

```text
/target category strategy        →  /plan ...     # only the strategy team plans
/target category review          →  /vote         # the review team votes
/target category implementation  →  /build  →  /start-build
/target personality critic       →  /review
```

**Who judges whom:** the scope picks the *judges* (the agents that run a phase),
while the *candidates* are everything produced so far, read from disk. So a
`critic` team can vote on the `architect` team's plans, or review the
`builder` team's diffs. `/target all` restores everyone.

## Pages and overview (many agents)

You can run more agents than fit in one grid.

- All agents keep running; only one **page** of panes is shown.
- `Ctrl+N` / `Ctrl+P` (or `/page next|prev|<n>`) switch pages. Moving focus past
  the visible page changes page automatically.
- Page size is configurable: `ui.page_rows`, `ui.page_cols`. Adjust live in
  `/settings`.
- `Ctrl+G` (or `/overview`) lists every agent with status, page, personality,
  and phase marker. `Enter` jumps to an agent; `Space` shows/hides a personality.
- `ui.group_by` orders panes and the overview by `personality` or `category`.

## Resuming a run

Every stage is resumable: an interrupted plan/vote/review reopens with
prompts only for the agents still missing artifacts; an interrupted build
reopens the worktrees (staged builds wait for `/start-build`); an interrupted
`/refine` resumes with the refine prompt and the preserved `.orig.md` plan;
and a run that already has a review winner reopens idle with the HUD showing
`/compare or /adopt` as the next step.


Resume restores a run's context (issue, plans, votes, build/review results) and
launches **fresh** agent processes — it does not reattach to old PTYs. If the
run was interrupted inside `plan`, `vote`, `build`, or `review`, council reopens
that phase, keeps existing artifacts, and only sends prompts for unfinished
agents. A staged build resumes staged; a build that had already started resumes
by sending the build prompt again without resetting the worktrees.

```text
/runs            # browse runs: timestamp, prompt, artifacts, winner, agents
/resume          # resume the most recent (or pick from the list)
/resume 20260605-130000
```

```bash
council resume [run]
```

Completed runs still reopen as context. Continue manually from completed phase
boundaries: after plan → `/vote`, after vote → `/build`, after build →
`/review`, after review → `/adopt`.

## Per-repo configuration

Drop a `.council.yaml` in a repository and it **layers over** your global
`~/.council.yaml`: keys set locally win, everything else falls through. It's
found by walking up from your working directory to the git root (linked
worktrees, where `.git` is a file, work too). Because a repo config can change
which commands council executes, it is only applied after you **trust** it —
council asks on first use and re-asks when the file changes; `council trust`
manages it explicitly. Useful for per-project agents, `group_by`, or a
project-specific `review.check_command` (`council stack detect` writes that
for you). See [Configuration](configuration.md#per-repo-override).

## Where things are written

```text
.council/
  runs/<timestamp>/
    issue.md                  the task
    report.md                 the run report (/report)
    config.effective.yaml     the merged config the run actually used
    config.sources.json       where that config came from (paths + hashes)
    timings.json              phase start/end, participants, restarts
    adopted.json              which build was applied, when
    plans/<agent>.md          plan phase output (.orig.md kept by /refine)
    votes/<agent>.md          vote ballots
    votes/plan-<letter>.md    anonymized plan copies reviewers read
    votes/result.json|md      tally + winner
    builds/<agent>.diff       each build's diff
    builds/<agent>.check.log  check command output (PASS/FAIL/timeout)
    builds/<agent>.review.md  review ballots
    builds/result.json        review winner
    builds/base.txt           the commit builds branched from
    transcripts/<phase>/…     cleaned pane transcripts
    raw/<phase>/…             raw PTY logs
  worktrees/<stamp>/<agent>/  per-run build checkouts (branch council/<agent>/<stamp>)
```

The run directory is anchored to the directory you launched council from. The
`.council/` directory is git-ignored. Everything is written owner-only
(`sessions.private`), worktrees are scoped per run so a new run can never
build in a stale checkout, and `/artifacts` browses all of it without leaving
the TUI. `council clean-runs --keep 10` prunes old runs.
