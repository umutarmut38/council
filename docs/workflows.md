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
(`.council/worktrees/<agent>` on branch `council/<agent>/<ts>`), so agents never
touch your working tree or each other.

### 4. Review and adopt

```text
/review        # run the check command in each build worktree; drop the ones
               # that changed nothing or failed; agents vote the best diff
/adopt         # git apply --3way the winning diff onto your working tree
```

`/review` gates with `review.check_command` (see
[Configuration](configuration.md)); it's empty by default (language-agnostic —
every changed build goes to the vote). One survivor wins outright; zero means
nothing passed. The build diffs include newly-created files (staging respects
`.gitignore`, so build outputs like `node_modules/` are skipped only if they're
ignored). `/adopt` leaves the winning change **uncommitted** for you to review
and commit; **`/adopt <agent>`** applies a different agent's build to override
the recommendation (the review status line lists the available builds).

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
found by walking up from your working directory to the git root. Useful for
per-project agents, `group_by`, or a project-specific `review.check_command`.
See [Configuration](configuration.md#per-repo-override).

## Where things are written

```text
.council/
  runs/<timestamp>/
    issue.md                  the task
    plans/<agent>.md          plan phase output
    votes/<agent>.md          vote ballots
    votes/plan-<letter>.md    anonymized plan copies reviewers read
    votes/result.json|md      tally + winner
    builds/<agent>.diff       each build's diff
    builds/<agent>.review.md  review ballots
    builds/result.json        review winner
    builds/base.txt           the commit builds branched from
    transcripts/<phase>/…     cleaned pane transcripts
    raw/<phase>/…             raw PTY logs
  worktrees/<agent>/          per-agent build checkout (branch council/<agent>/<ts>)
```

The run directory is anchored to the directory you launched council from. The
`.council/` directory is git-ignored.
