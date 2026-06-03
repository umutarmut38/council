# Architecture

`council` is intentionally small: one command, a handful of internal packages,
and file-backed orchestration state.

## Packages

| Package | Responsibility |
|---|---|
| `cmd/council` | CLI parsing, config loading, TUI launch, shell orchestration commands. |
| `internal/agent` | Starts agent processes under PTYs, writes raw logs, resizes and terminates sessions. |
| `internal/config` | YAML schema, defaults, local config merging, roles, personalities, terminal quirks. |
| `internal/session` | Run-scoped transcript/log store for non-orchestration sessions. |
| `internal/tui` | Bubble Tea model, pane rendering, input modes, file suggestions, settings, runs, and in-chat orchestration commands. |
| `internal/orchestrate` | Run directories, prompts, git worktrees, voting, review, adopt, resume state. |
| `internal/version` | Build metadata injected by release builds. |

## Terminal Model

Each agent process runs in a real PTY. Output is read in chunks, appended to raw
logs, and fed into a lightweight terminal emulator for pane rendering.

Prompt delivery is intentionally configurable:

- `send_mode: type` sends raw text.
- `send_mode: paste` wraps text in bracketed paste.
- `submit_sequence` chooses the key used to submit.
- `submit_delay_ms` can send Enter as a separate delayed write.

This keeps agent-specific behavior in config instead of code branches keyed on
agent names.

## Orchestration Model

Runs live under `.council/runs/<timestamp>/`.

1. `plan` asks workers to write plans to `plans/<agent>.md`.
2. `vote` writes anonymized plan copies and asks reviewers to rank them.
3. `build` creates one git worktree per worker and stages the build prompt.
4. `review` captures diffs, runs optional checks, anonymizes surviving diffs,
   and asks reviewers to rank implementations.
5. `adopt` applies the selected diff to the user's working tree as uncommitted
   changes.

State is file-backed so the TUI can be killed and resumed:

- `state.json` records the active phase and whether prompts were sent.
- `votes/plan-assignments.json` persists Plan A/B/C mappings.
- `builds/review-assignments.json` persists Diff A/B/C mappings.

## Trust Boundaries

`council` does not sandbox agent CLIs. It isolates build implementations with
git worktrees, but each configured agent command has the same OS permissions as
the user running it. Use conservative agent flags in untrusted repositories.

## Design Rules

- Prefer config knobs over hard-coded agent names.
- Keep orchestration artifacts readable Markdown/JSON files.
- Never require all phases to use the same set of agents.
- Preserve interrupted worktrees on resume.
- Leave adopted changes uncommitted for user review.
