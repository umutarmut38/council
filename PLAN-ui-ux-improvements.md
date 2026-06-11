# council UI/UX Improvement Plan

This plan is based on the June 2026 UI screenshots from a real PoC run. The
current interface already has a useful terminal cockpit feel: the panes are
clear, focused borders work, and the bottom composer is discoverable. The main
gap is not the pane rendering itself; it is the orchestration layer around the
panes.

The user should always know:

- What phase the run is in.
- Which agents are expected to produce which artifacts.
- Which agents are done, blocked, missing output, or waiting for approval.
- What command is safe and useful to run next.
- What will happen before a risky action such as adopt or clean.

## Core UX Theme

Make `council` feel less like "terminal panes plus memory" and more like an
orchestrator HUD around real agent CLIs.

The panes should remain the default workspace, but the surrounding UI should
carry run state, progress, next actions, and recovery options.

## Findings From Screenshots

### 1. Too much orchestration state is hidden in prose

The header/status line currently says useful things such as:

```text
collected 2 plan(s) — type /vote
vote prompt sent — agents working
```

That is helpful, but users have to read, remember, and infer the workflow state.
The UI should represent phase progress structurally.

Recommended improvement:

```text
Plan 2/2 ✓  Vote 0/2 ●  Build ○  Review ○  Adopt ○
Next: /vote
```

This phase rail should be visible in the header or second status line whenever
an orchestration run is active.

### 2. The grid wastes space when only two panes are active

In the plan/vote/build screenshots with two active agents, panes occupy only the
top half of the terminal and leave a large empty area below.

Recommended improvement:

- When 1 pane is active, use a full-screen single pane.
- When 2 panes are active, default to a full-height 1x2 layout.
- When 3-4 panes are active, use the current 2x2 grid.
- Respect explicit user settings if the user locks rows/columns.

This should make worker-only and reviewer-only phases feel much better.

### 3. Pane titles need richer phase state

Current titles mostly show process state:

```text
codex-worker [running]
copilot-reviewer [running]
```

But `running` only means the child process is alive. It does not tell the user
whether the agent wrote its artifact, missed the prompt, is blocked on approval,
or is expected to do anything in the current phase.

Recommended title states:

```text
codex-worker     planning ✓ PLAN.md
copilot-worker   planning ✓ PLAN.md
codex-reviewer   voting   waiting for VOTE.md
copilot-worker   build    needs input
```

Useful state labels:

- `starting`
- `running`
- `waiting for PLAN.md`
- `waiting for VOTE.md`
- `waiting for REVIEW.md`
- `wrote PLAN.md`
- `wrote VOTE.md`
- `wrote REVIEW.md`
- `needs input`
- `exited`
- `failed`

### 4. Approval prompts are too easy to miss

In the build screenshot, Copilot is blocked on a command approval prompt, but
the council footer still shows a normal composer prompt. The user has to notice
the agent's own UI inside the pane.

Recommended improvements:

- Detect common approval prompt text where feasible.
- Add a manual `/attention <agent>` command if automatic detection is unreliable.
- Highlight blocked panes in the overview and header.
- Show a footer hint when a pane likely needs direct interaction:

```text
copilot-worker may need input · F2 direct · /nudge copilot-worker · /restart copilot-worker
```

This matters because agent CLIs often block on permissions, trust prompts, or
tool confirmations.

### 5. Build agents should be told not to commit

The build screenshot shows Copilot trying to run `git add` and `git commit`.
For council worktrees, agents should edit files but not commit. Council captures
diffs from worktrees for review/adopt.

Recommended prompt change:

Add explicit build prompt language:

```text
Do not commit changes. Do not create branches. Leave your implementation as
uncommitted working tree changes; council will capture the diff.
```

This is a UX issue because a prompt-created commit triggers extra approval UI
and confuses what council will adopt.

### 6. Overview is clean but too sparse

The overview screenshot uses a full screen to show only two reviewer rows. It is
readable, but it does not yet function as a run dashboard.

Recommended overview/dashboard content:

```text
Run 20260610-104257
Phase: Vote
Next: wait for 2 vote files, then tally

Progress
Plan    2/2 complete
Vote    0/2 waiting
Build   not started
Review  not started
Adopt   not ready

Agents
codex-reviewer     reviewer · Correctness · running · missing VOTE.md
copilot-reviewer   reviewer · Usability    · running · missing VOTE.md
```

This lets the user recover orientation without leaving the TUI.

### 7. Settings is too sparse for the space it uses

The settings screen is simple, which is good, but it could carry more useful
layout information.

Recommended additions:

- Adaptive grid: on/off.
- Rows/columns lock: auto/manual.
- Current phase participant count.
- Layout preview text:

```text
Current layout: 2 agents -> 1 row x 2 cols, full height
```

### 8. Footer hints are too static

The footer often shows generic shortcuts:

```text
Enter send | Ctrl+G overview | F2 direct | Ctrl+B target | ...
```

During orchestration, the footer should prioritize context-specific commands.

Examples:

```text
Plan complete: 2/2 · Next: /vote · Useful: /status /artifacts
```

```text
Vote in progress: 0/2 · Useful: /finish /resend <agent> /restart <agent>
```

```text
Build in progress · F2 direct if an agent needs approval · Next: /review when done
```

```text
Best build: codex-worker · Next: /compare or /adopt
```

The generic shortcut list can still appear when no run is active.

## Recommended UI Features

### Phase Rail

Persistent run progress indicator.

Example:

```text
Plan 2/2 ✓  Vote 1/2 ●  Build ○  Review ○  Adopt ○
```

Details:

- Show counts where artifacts are expected.
- Use a current-phase marker.
- Use compact labels so it fits in the header.
- Include the next recommended command.

### Run Dashboard

Enhance `/status` or add a dedicated `/run` screen.

Should show:

- Run ID and task preview.
- Current phase.
- Next action.
- Workers and reviewers.
- Expected artifacts.
- Missing artifacts.
- Build check status.
- Review/adopt readiness.

### Artifact Browser

Add `/artifacts` for read-only inspection of run outputs.

Sections:

- Plans.
- Votes.
- Result summaries.
- Build diffs.
- Check logs.
- Reviews.
- Report.
- Transcripts.

Even a simple list with file previews would improve trust substantially.

### Compare Screen

Add `/compare` for build candidates.

Example:

```text
Agent            Changed  Check  Review  Notes
codex-worker     yes      PASS   5       best structure
copilot-worker   yes      FAIL   -       missing persistence
```

The compare screen should make `/adopt <agent>` decisions easier.

### Adopt Preview

Before applying changes, show a preview.

Example:

```text
Winning build: codex-worker
Changed files:
  index.html
  styles.css
  app.js

Check: PASS
Review score: 5

Apply this diff to your working tree? y/N
```

Adoption is the highest-trust moment in the workflow, so it deserves an explicit
preview unless `policy.mode: aggressive` is configured.

### Attention and Recovery Commands

Add commands for common messy-agent situations:

```text
/resend <agent>
/restart <agent>
/nudge <agent>
/attention <agent>
```

Behavior:

- `/resend` sends the current phase prompt again.
- `/restart` relaunches one agent in the current phase context.
- `/nudge` asks the agent to write the expected artifact.
- `/attention` manually marks a pane as needing user input.

### Adaptive Layout

Default layout based on active pane count:

- 1 agent: 1x1 full height.
- 2 agents: 1x2 full height.
- 3-4 agents: 2x2.
- More agents: paged grid.

This should be the default, with settings to override.

### Better Pane Badges

Pane headers should include both process and phase artifact state.

Examples:

```text
codex-worker [build · working]
copilot-worker [build · needs input]
codex-reviewer [vote · missing VOTE.md]
copilot-reviewer [vote · wrote VOTE.md]
```

### Prompt Guardrails for Build

Update build prompts to reduce UI friction:

- Tell agents not to commit.
- Tell agents not to create branches.
- Tell agents to leave edits as working tree changes.
- Remind agents that council will capture the diff.

## Visual Direction

Keep the interface dense, operational, and terminal-native. Avoid marketing-like
screens or decorative visuals. The right feel is a focused cockpit.

Recommended visual changes:

- Dim unfocused borders more strongly.
- Keep the focused border visible, but consider reducing the hot-pink intensity.
- Compress full paths in the header; show full paths in `/status`.
- Replace long agent lists with counts/roles when space is tight.
- Use the footer for next actions before generic shortcuts.
- Make warnings and blocked states visually distinct.

## Suggested Implementation Order

1. Add adaptive grid for 1-2 active panes.
2. Add phase rail and next-action hint.
3. Add richer pane state badges.
4. Make footer hints context-aware.
5. Update build prompt to say "do not commit".
6. Turn overview into a lightweight run dashboard.
7. Add `/resend`, `/restart`, `/nudge`, and `/attention`.
8. Add `/artifacts`.
9. Add `/compare`.
10. Add adopt preview and confirmation.

The first five items are small but high leverage. They would make the current
TUI feel much more guided without changing the core architecture.
