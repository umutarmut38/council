# council Improvement Plan

This document captures the project audit findings and feature ideas from the
June 2026 review. The project is in good shape overall: it is compact, has a
clear philosophy, has useful documentation, and `go test ./...` plus
`go vet ./...` both passed locally during review.

The strongest opportunity is to make `council` feel less like a collection of
agent panes and more like a safe, inspectable, repeatable multi-agent decision
system.

## Guiding Principles

- Preserve the core philosophy: launch real vendor CLIs in real PTYs instead of
  wrapping model APIs.
- Prefer guardrails, observability, and recovery over heavyweight framework
  abstractions.
- Keep orchestration artifacts human-readable Markdown/JSON.
- Make risky automation explicit, especially when local config, auto-approval
  flags, generated diffs, or logs containing secrets are involved.
- Improve trust before adding too much automation.

## Priority 0: Correctness and Safety

### Fix stale worktree reuse

Worktrees are stored at `.council/worktrees/<agent>`, while branches include the
run stamp as `council/<agent>/<stamp>`. `Manager.Add` reuses an existing
worktree path without verifying that it belongs to the current run/branch.

Risk:

- A new run can accidentally build in an old run's worktree.
- Review/adopt may compare or apply the wrong implementation.
- Resume behavior can become confusing when stale worktrees exist.

Relevant code:

- `internal/orchestrate/worktree.go`: `pathFor`, `Add`, `Reset`, `Remove`

Proposed fixes:

- Prefer stamp-specific paths such as `.council/worktrees/<stamp>/<agent>`.
- Or, before reuse, verify the existing worktree branch matches
  `council/<agent>/<stamp>`.
- If it does not match, fail with a clear message or remove/recreate only after
  explicit cleanup.
- Add tests for fresh runs, resume runs, stale worktrees, and branch mismatch.

### Add trust controls for repo-local config

Repo-local `.council.yaml` can override commands and phase commands. That is
powerful, but it also means opening an untrusted repo can alter what gets
executed, including risky auto-approval flags.

Risk:

- A malicious repository can ship a `.council.yaml` that runs unexpected
  commands.
- The current docs warn users, but the application applies local config
  automatically.
- `FindLocalConfig` stops at a `.git` directory, which may miss git worktrees
  where `.git` is a file.

Relevant code:

- `internal/config/config.go`: `FindLocalConfig`, `ApplyLocal`
- `cmd/council/main.go`: config load and local overlay

Proposed fixes:

- Add `--no-local-config`.
- Add a trust store for repo-local config, keyed by repo root and config hash.
- Show a first-run prompt or warning when a repo-local config changes commands.
- Use `git rev-parse --show-toplevel` for repo root discovery rather than only
  walking until a `.git` directory.
- Add `council doctor` output showing whether local config was applied.

### Harden artifact privacy

Raw logs, transcripts, prompts, copied config, plans, votes, diffs, and check
logs may contain secrets, private paths, or pasted credentials. Many files are
currently written with `0644`, and directories with `0755`.

Risk:

- Other local users may read sensitive run artifacts.
- Users may accidentally publish `.council/runs`.

Relevant code:

- `internal/session/store.go`
- `internal/orchestrate/run.go`
- `internal/orchestrate/review.go`

Proposed fixes:

- Use `0700` for run/log directories.
- Use `0600` for logs, transcripts, prompts, copied configs, and run artifacts.
- Add a config option such as `sessions.private: true`, defaulting to true.
- Add optional redaction for common secret patterns in saved transcripts.
- Add `council clean-runs` or `council redact` for artifact lifecycle hygiene.

### Make `/adopt` safer

`/adopt` applies a selected diff directly to the working tree with
`git apply --3way`.

Risk:

- Users may apply a diff into an already-dirty working tree without a clear
  preview.
- Failed or partial patch behavior can be confusing.
- Agent-generated diffs deserve one last inspection step.

Relevant code:

- `internal/orchestrate/review.go`: `Adopt`
- `internal/tui/model.go`: `cmdAdopt`

Proposed fixes:

- Run `git apply --check --3way` before applying.
- Detect a dirty working tree and warn before adopting.
- Add `/adopt --dry-run` or `/preview <agent>`.
- Add a TUI confirmation step for adoption.
- After adoption, show changed files and next suggested commands.

### Rework default generated config

The default config enables multiple agents and includes risky phase commands
such as permission bypass or broad tool approval flags.

Risk:

- First-time users may run dangerous defaults before understanding the trust
  model.
- Defaults normalize aggressive orchestration behavior.

Relevant code:

- `internal/config/config.go`: `Default`
- `cmd/council/main.go`: `config init`

Proposed fixes:

- Generate disabled agent presets by default.
- Move risky flags into commented examples or named policy profiles.
- Add `council config init --interactive`.
- Add profiles such as `safe`, `normal`, and `aggressive`.
- Make `doctor` warn when phase commands include known risky flags.

## Priority 1: Usability and Workflow

### Expand `council doctor`

Current `doctor` mostly checks whether configured binaries exist.

Better checks:

- Global and local config validity.
- Whether local config was applied.
- Missing commands for enabled agents.
- Role coverage: at least one worker for plan/build and one reviewer for
  vote/review.
- Git repository availability for orchestration.
- Writable `.council/runs` and `.council/worktrees`.
- Stale or mismatched worktrees.
- `review.check_command` presence and executable availability.
- Risky command flags.
- Terminal setting sanity, including `send_mode`, `submit_sequence`, and
  `submit_delay_ms`.

### Add CLI parity for review and adopt

The in-chat UI supports `/review` and `/adopt`, but CLI orchestration does not
yet expose matching subcommands.

Proposed commands:

```text
council review [run]
council adopt [run] [agent] [--dry-run]
```

Benefits:

- Supports scripted or non-interactive workflows.
- Makes run recovery easier outside the TUI.
- Allows CI-like experimentation with build checks and report generation.

### Improve `status`

Current status is useful but shallow.

Add:

- Active phase.
- Participants for each phase.
- Missing plan/vote/review artifacts.
- Winning plan and build winner.
- Build check pass/fail/changed status.
- Adoptable builds.
- Paths to plans, votes, diffs, check logs, and reports.

### Add a config wizard

YAML is powerful but has a steep first-run curve.

Feature:

```text
council config wizard
council config init --interactive
```

Wizard flow:

- Detect installed agent CLIs.
- Ask which agents to enable.
- Ask worker/reviewer roles.
- Choose conservative terminal settings.
- Detect the project stack and set `review.check_command`.
- Explain risky approval flags before enabling them.
- Write global or repo-local config.

### Add agent presets

Feature:

```text
council config add-agent codex
council config add-agent claude --role worker
council config add-agent copilot --role reviewer
```

Benefits:

- Reduces YAML editing.
- Encodes known terminal quirks in one place.
- Gives users a safe path for adding common tools.

### Add stack presets

Feature:

```text
council stack detect
council stack set go
council stack set node
council stack set rust
```

Potential presets:

- Go: `["go", "test", "./..."]`
- Node: `["npm", "test"]` or package-script detection
- Rust: `["cargo", "test"]`
- Python: `["pytest"]` when configured

Benefits:

- Improves review quality.
- Helps new users get meaningful gates quickly.
- Makes per-repo setup less fiddly.

### Add restart, resend, and nudge commands

Agents can miss prompts, hang, or exit.

Feature:

```text
/restart codex
/resend codex
/nudge reviewer-team
```

Expected behavior:

- `/restart` terminates and relaunches one pane using the current phase
  configuration.
- `/resend` resends the current phase prompt to a specific agent.
- `/nudge` sends a short reminder to produce the expected artifact.

Benefits:

- Reduces manual recovery work.
- Makes long orchestration sessions less brittle.

### Fix docs/config mismatch around layout

The example config uses `layout: paged-grid`, while the docs list `layout:
grid`, and code primarily uses `page_rows`/`page_cols`.

Relevant files:

- `examples/configs/worker-reviewer.yaml`
- `docs/configuration.md`
- `internal/config/config.go`
- `internal/tui/model.go`

Proposed fixes:

- Either document `paged-grid` as accepted, or change the example to `grid`.
- If `layout` is currently unused, either remove it from examples or implement
  explicit layout modes.
- Add validation/warnings for unknown layout values.

## Priority 2: Code Quality and Maintainability

### Split the TUI model

`internal/tui/model.go` carries command handling, rendering, file suggestions,
orchestration commands, transcript handling, terminal emulation, and key
mapping.

Proposed split:

- `model.go`: state and high-level update loop
- `commands.go`: composer commands
- `orchestration.go`: `/plan`, `/vote`, `/build`, `/review`, `/adopt`, resume
- `render.go`: header, footer, panes, overview, settings, runs
- `terminal.go`: terminal emulator and escape handling
- `files.go`: `@file` suggestions and expansion helpers
- `keys.go`: key mapping and direct mode

Benefits:

- Easier review.
- Smaller test targets.
- Lower risk when changing terminal behavior.

### Validate agent names

Agent names feed:

- Artifact names.
- Branch names.
- Worktree paths.
- TUI labels.

Risk:

- Names like `a/b` and `a_b` can collide after safe-name conversion.
- Branch names may become invalid or surprising.

Proposed fixes:

- Validate configured names against a conservative pattern such as
  `[A-Za-z0-9_-]+`.
- Reject duplicates after safe-name normalization.
- Add tests for collisions.

### Make run IDs collision-proof

Runs use second-resolution timestamps.

Risk:

- Two runs started in the same second can collide.

Proposed fixes:

- Include milliseconds or nanoseconds.
- Or append a short random suffix.
- Create run dirs with collision retry.
- Add a test that starts multiple runs rapidly.

### Save the effective merged config per run

Interactive launch currently saves the raw global config bytes even after local
config is applied.

Risk:

- A run's saved config may not match the actual behavior.
- Debugging old runs becomes harder.

Proposed fixes:

- Marshal and save the normalized, effective config.
- Also record paths/hashes for global and local config inputs.
- Consider saving `config.effective.yaml` and `config.sources.json`.

### Improve error handling around best-effort commands

Some git operations intentionally ignore errors, such as staging changes before
diff capture.

Proposed improvements:

- Log ignored errors into check logs or run metadata.
- Surface warnings in `/review` status.
- Keep best-effort behavior where it is useful, but make it inspectable.

## Priority 3: Security Hardening

### Constrain `@file` expansion

`@file` expansion can read any readable absolute path or relative path.

Risk:

- Users can accidentally paste sensitive local files into agent prompts.
- A copied task can include surprising absolute file refs.

Relevant code:

- `internal/orchestrate/issue.go`: `ExpandFileRefs`
- `internal/tui/model.go`: file suggestion and send path expansion

Proposed fixes:

- Default to repo-root-relative paths only.
- Require an explicit config/flag for absolute path expansion.
- Add maximum file size limits.
- Detect and skip binary files.
- Warn when expanding files outside the repo.

### Add timeout and output limits for review checks

`review.check_command` runs without timeout or output caps.

Risk:

- A hung test command can block review indefinitely.
- Large output can produce huge logs.

Relevant code:

- `internal/orchestrate/review.go`: `runInDir`

Proposed fixes:

- Add `review.check_timeout`.
- Add `review.max_check_output_bytes`.
- Show timeout status in check logs and run reports.

### Make `/clean` safer

`/clean` removes worktrees and branches.

Proposed fixes:

- Add preview mode listing what will be removed.
- Require confirmation in the TUI.
- Provide a narrower cleanup command for a specific run or agent.

### Add policy profiles

Feature:

```yaml
policy:
  mode: safe # safe | normal | aggressive
```

Possible behavior:

- `safe`: no auto-approval phase commands, no local config without trust, no
  absolute file refs, confirm adopt/clean.
- `normal`: current interactive defaults with key warnings.
- `aggressive`: explicit opt-in for fast autonomous runs.

## Priority 4: Test and CI Improvements

### Add targeted tests

Recommended tests:

- Stale worktree reuse fails or creates a stamp-specific path.
- Git worktree `.git` file does not break local config discovery.
- Safe-name collision rejection.
- Dirty working tree behavior before adopt.
- `git apply --check` failure handling.
- Run ID collision resistance.
- `@file` refuses absolute/out-of-repo paths when configured.
- `review.check_command` timeout behavior.
- Config wizard/preset generation once those features exist.

### Add race testing

Add `go test -race ./...` on Linux CI at minimum.

Rationale:

- The app uses goroutines around PTY reads, delayed submits, TUI messages, and
  session state.
- Race checks are especially valuable for terminal/process orchestration.

### Revisit Windows CI behavior

Windows CI is allowed to fail, while release artifacts include Windows binaries.

Options:

- Keep Windows experimental but make this explicit in release notes.
- Add a smaller Windows smoke test that must pass.
- Move native Windows support toward first-class once PTY behavior is proven.

### Add release supply-chain polish

Current releases include checksums.

Potential additions:

- Signed checksums.
- SBOM.
- Provenance attestation.
- Documented verification steps in release notes.

## New Feature Roadmap

### Run report

Generate a readable report after each orchestration run.

Output:

```text
.council/runs/<timestamp>/report.md
```

Contents:

- Issue summary.
- Participants and roles.
- Plans produced.
- Vote tally and ballots.
- Winning plan.
- Build check results.
- Review tally.
- Winning implementation.
- Adopted diff, if any.
- Key paths to artifacts.

Why it matters:

- Makes runs easier to inspect and share.
- Gives users confidence in the decision process.
- Creates a natural artifact for GitHub comments or PR descriptions later.

### TUI artifact browser

Add an in-app browser for run artifacts.

Potential command:

```text
/artifacts
```

Views:

- Plans.
- Votes.
- Result summaries.
- Diffs.
- Check logs.
- Reviews.
- Transcripts.
- Raw logs, perhaps hidden behind an advanced toggle.

Why it matters:

- Current artifacts are useful but filesystem-first.
- Users should be able to inspect evidence without leaving the TUI.

### Implementation comparison view

Before adoption, show candidate builds side by side or in a ranked list.

Data to show:

- Agent.
- Changed files.
- Check pass/fail.
- Review score.
- Winner/recommendation marker.
- Path to diff and check log.

Potential commands:

```text
/compare
/preview codex-worker
```

Why it matters:

- Adoption becomes more deliberate.
- Users can override the winner with better context.

### Human judge mode

Let the user vote or override using structured commands.

Potential commands:

```text
/judge plan B
/judge build codex-worker
/vote-human B > A > C
```

Why it matters:

- Keeps the human in the loop.
- Helps when reviewers disagree or produce low-quality votes.
- Makes the process feel collaborative rather than fully delegated.

### Consensus/refinement round

After `/vote`, feed reviewers' objections back into the winning planner or all
workers before building.

Potential command:

```text
/refine
```

Output:

- A final implementation brief.
- A risk list.
- A test checklist.

Why it matters:

- The winning plan can absorb useful critique before build starts.
- This turns the council from a one-pass contest into a stronger deliberation
  loop.

### Batch issue queue

Run council across multiple tasks.

Potential commands:

```text
council queue add --issue 123
council queue add --file task.md
council queue run
```

Why it matters:

- Useful once orchestration is reliable.
- Supports maintainers triaging several small issues.

### GitHub integration

Possible capabilities:

- Fetch issues using `gh`.
- Post run reports as issue comments.
- Open a PR from the winning implementation branch.
- Attach plan/review summaries to PR descriptions.
- Request human review after adoption.

Why it matters:

- Connects the council workflow to where project decisions already happen.
- Makes the report feature immediately useful.

### Cost and time tracking

Exact token tracking is hard across vendor CLIs, but useful approximations are
still possible.

Track:

- Phase start/end time.
- Agent runtime.
- Number of restarts/resends.
- Check command duration.
- Which agents completed, failed, or missed artifacts.

Why it matters:

- Helps users tune their council.
- Supports scorecards and future optimization.

### Agent scorecards

Track agent performance across runs.

Metrics:

- Plan wins.
- Build wins.
- Passing build rate.
- Missing artifact rate.
- Review alignment with final winner.
- Average runtime.

Why it matters:

- Helps users decide which agents are worth running.
- Makes the tool's "let them compete" philosophy measurable.

### Replay mode

Reopen a run and replay transcript/artifact progress.

Potential command:

```text
/replay <run>
```

Why it matters:

- Useful for debugging orchestration problems.
- Makes demos and postmortems much easier.

## Suggested Implementation Sequence

1. Fix stale worktree reuse.
2. Add local config trust controls and improve repo-root discovery.
3. Make run artifacts private by default.
4. Add safer adopt flow with dry-run/check/dirty-tree preflight.
5. Expand `doctor`.
6. Generate run reports.
7. Add TUI artifact browser.
8. Add implementation comparison and preview.
9. Add config wizard, agent presets, and stack presets.
10. Add human judge mode and refinement round.
11. Add GitHub integration and scorecards.

This order improves trust and debuggability first, then builds toward a richer
product experience.
