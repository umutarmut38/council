# council

> _I subscribed to all of them. Might as well make them earn it._

At some point I had Claude Code, Codex, Copilot, Cursor, and a growing list of
agent CLIs all authenticated on my machine, each with their own opinions, their
own strengths, and their own monthly invoice. I wasn't going to pick a favourite.
I was going to make them **compete**.

`council` is _not_ another SDK wrapper, API integration, or LLM framework. I
didn't want to re-implement what these vendors already ship. They each have a
perfectly good CLI with tool access, context handling, and capabilities I'd never
replicate. So `council` just launches them (real PTY panes, real processes) and
orchestrates a structured **plan → vote → build → review → adopt** workflow on
top. Your agents propose, judge each other's work, build the winner, and you
adopt the diff. Maximum leverage, maximum token waste, zero regret.

![Council terminal grid](docs/assets/council-grid.svg)

## Why

Because one opinionated agent isn't enough. You need _several_ opinionated agents
disagreeing in a terminal multiplexer while you drink coffee.

More seriously:

- Ask several agents the same question and compare answers live.
- Give agents roles: `worker` does the building, `reviewer` judges the code
  (and silently judges you for wasting all those tokens).
- Give agents behavioral personalities: `pragmatist`, `critic`, `pessimist`,
  or whatever chaos you desire.
- Scope messages to one agent, a personality, a category, or broadcast to the
  whole roster.
- Keep build attempts isolated in git worktrees and adopt the winning diff only
  when you're ready.

## Features

- **Live PTY panes**: Claude Code, Codex, Cursor Agent, Copilot CLI, opencode,
  or literally any command that takes text input. No API keys, no SDK bindings,
  just `spawn` and a dream.
- **Broadcast composer** plus `@agent message` and `/send agent message`.
- **Direct input mode** for slash commands and tool-specific settings inside an
  agent's own UI.
- **Paged layouts** because apparently I collect more agents than fit on one
  screen.
- **Overview and settings screens** for navigation, visibility filters, and grid
  sizing.
- **File references** with `@path/to/file` expansion.
- **Configurable terminal delivery** for tools that need paste mode, delayed
  Enter, fixed PTY sizes, or transcript rendering.
- **Plan/vote/build/review/adopt orchestration** — a bureaucratic workflow for
  your AI employees, with an optional `/refine` consensus round and human
  `/judge` overrides.
- **Run reports, timings, and scorecards** — `report.md` per run, `council
  scorecard` across runs, `council pr` to ship the winner.
- **Inspect everything**: `/artifacts` browses plans, votes, diffs, check
  logs, and transcripts in-app; `/compare` and `/preview` before `/adopt`.
- **Resume** from interrupted phases (agents crash; it's fine; so do I), plus
  `/restart`, `/resend`, and `/nudge` for the stragglers.
- **Per-repo config overlays** with `.council.yaml` — gated by a trust store,
  because a repo file that changes which commands run should ask first.
- **Safe defaults**: presets ship disabled with no auto-approval flags,
  artifacts are owner-only, `@file` expansion stays inside the repo, and
  `policy.mode: safe|normal|aggressive` sets the risk posture.
- **Release artifacts** for macOS, Linux, and Windows.

## The Philosophy (if you can call it that)

Every vendor ships a CLI. Each one has unique strengths, tool integrations,
context handling, and opinions. Instead of picking _one_ and living with its
quirks, `council` takes the laziest-smart approach:

1. **Collect** every agent CLI you can get your hands on.
2. **Orchestrate** them into competing proposals.
3. **Let them vote** on each other's work (self-votes excluded, we're not
   savages).
4. **Build** the winner in an isolated worktree.
5. **Adopt** the diff when it passes review, or start over and burn more tokens.

No API wrappers. No LLM SDKs. No RAG pipelines. Just PTYs, opinions, and
someone else's inference bill.

## Install

### Homebrew (macOS and Linux)

```bash
brew install umutarmut38/council/council
```

This taps `umutarmut38/homebrew-council` and installs a prebuilt binary. Upgrade
later with `brew upgrade council`.

### From a release

Download the archive for your platform from the GitHub Releases page, extract
it, and put `council` on your `PATH`.

```bash
council version
council config init
council doctor
```

### From source

```bash
git clone https://github.com/umutarmut38/council
cd council
go build -o bin/council ./cmd/council
./bin/council version
```

Or use the Makefile:

```bash
make test
make build
```

## Requirements

- Go 1.23+ when building from source.
- Git 2.35+ for orchestration, worktrees, review diffs, and adopt.
- A terminal with ANSI color and Unicode support.
- At least one installed and authenticated agent CLI.

| Platform | Support |
|---|---|
| macOS arm64/amd64 | Primary target. |
| Linux arm64/amd64 | Primary target. |
| Windows arm64/amd64 | Supported via native pseudo-terminals (ConPTY). Use Windows Terminal for best results; WSL still works if you prefer it. |

Windows is supported but still maturing: the build is a required CI check while
the full Windows test suite is allowed to fail. See [Windows Support](docs/windows.md)
for the exact stance, and [Requirements](docs/requirements.md) for more detail.

## Quick Start

Create a config:

```bash
council config init
$EDITOR ~/.council.yaml
```

Enable the agents you have installed:

```yaml
agents:
  claude:
    enabled: true
    command: ["claude"]
  codex:
    enabled: true
    command: ["codex"]
```

Check commands and launch:

```bash
council doctor
council
```

Inside the app:

- Type in the bottom composer and press `Enter` to broadcast.
- Press `Tab` / `Shift+Tab` to move between panes.
- Press `F2` to type directly into the focused agent; `Esc` returns.
- Press `Ctrl+G` for overview.
- Press `Ctrl+X` to quit.

## Orchestration Workflow

> _Democracy, but for code. What could go wrong._

![Council workflow](docs/assets/council-workflow.svg)

Run the council flow from the composer:

```text
/plan @examples/issues/retry-backoff.md
/vote
/build
/start-build
/review
/adopt
```

What happens:

| Phase | Output |
|---|---|
| `plan` | Workers write `plans/<agent>.md`. |
| `vote` | Reviewers rank anonymized plans with self-votes excluded. |
| `build` | Workers are relaunched into isolated git worktrees. |
| `review` | Build diffs are checked, anonymized, and ranked. |
| `adopt` | The selected diff is applied to your working tree as uncommitted changes. |

All run data lives under `.council/runs/<timestamp>/`.

## Roles and Personalities

Roles decide **what an agent does**:

```yaml
agents:
  codex-worker:
    enabled: true
    command: ["codex"]
    role: [worker]
  copilot-reviewer:
    enabled: true
    command: ["gh", "copilot"]
    role: [reviewer]
```

Personalities decide **how an agent behaves**:

```yaml
personalities:
  critic:
    label: Critic
    prompt_prefix: |
      Scrutinize for bugs, regressions, missing tests, and brittle assumptions.
```

See [examples/configs/worker-reviewer.yaml](examples/configs/worker-reviewer.yaml)
for a complete starter config.

## Resume

![Resume flow](docs/assets/resume-flow.svg)

If the TUI exits during an orchestration phase:

```text
/resume
```

or:

```bash
council resume
```

`council` reopens the latest run, reads `state.json` and existing artifacts,
relaunches fresh agent processes into the same phase, and prompts only unfinished
agents. Build worktrees are not reset on resume.

## Documentation

The full docs are in `docs/` and can be published with GitHub Pages.

| Page | Contents |
|---|---|
| [Commands](docs/commands.md) | In-chat commands and CLI subcommands. |
| [Shortcuts](docs/shortcuts.md) | Keyboard shortcuts for panes, pages, overview, settings, and run browsing. |
| [Workflows](docs/workflows.md) | Multiplexer use, orchestration, roles, personalities, targeting, resume, artifacts. |
| [Configuration](docs/configuration.md) | YAML reference for agents, terminal quirks, roles, personalities, and local overrides. |
| [Requirements](docs/requirements.md) | Platform support and runtime requirements. |
| [Windows Support](docs/windows.md) | What is supported, experimental, and smoke-tested on Windows; recommended terminal and ConPTY limits. |
| [Architecture](docs/architecture.md) | Package layout, terminal model, orchestration model, trust boundaries. |
| [Troubleshooting](docs/troubleshooting.md) | Prompt delivery, rendering, folder trust, npm examples, worktree cleanup. |
| [Release Guide](docs/release.md) | Tagging, artifacts, release checklist, history cleanup. |

## Examples

- [Worker/reviewer config](examples/configs/worker-reviewer.yaml)
- [Retry/backoff issue](examples/issues/retry-backoff.md)

The example issue can be used directly:

```text
/plan @examples/issues/retry-backoff.md
```

## Configuration Notes

Global config lives at `~/.council.yaml`.

Repositories can add `.council.yaml`; local config is merged over the global
config. This is useful for project-specific agents, check commands, page sizing,
and personalities.

```yaml
review:
  check_command: ["go", "test", "./..."]
ui:
  initial_prompt_delay_ms: 8000
```

## Development

```bash
go test ./...
go vet ./...
go build ./...
```

Release metadata is injected with ldflags:

```bash
go build \
  -ldflags "-X github.com/umutarmut38/council/internal/version.Version=v0.1.0" \
  -o bin/council ./cmd/council
```

## Security

`council` launches local agent CLIs with your user permissions. It does not
sandbox those tools. Build attempts run in git worktrees, and `/adopt`
preflights the diff, shows what it touches, and asks before applying it to
your working tree. Always review changes before committing.

Raw logs and transcripts can contain private prompts, local paths, and secrets
pasted into agents. Run artifacts are written owner-only by default
(`sessions.private`), optional transcript redaction exists
(`sessions.redact`), and repo-local `.council.yaml` files are only applied
once trusted (`council trust`). Still: do not publish `.council/runs/`
without reviewing it.

See [SECURITY.md](SECURITY.md).

## Current Limitations

- Terminal emulation is pragmatic, not a full terminal emulator. Your agents
  will cope.
- Agent CLIs differ in prompt submission behavior; some need terminal config
  tweaks (looking at you, Cursor).
- Windows runs on native pseudo-terminals (ConPTY); full-screen rendering can
  still vary by console, so Windows Terminal is your friend (WSL works too).
- Orchestration requires a git repository. (Where else would your agents fight?)
- Your token bill is not my problem.

## License

MIT. See [LICENSE](LICENSE).

---

_Built because I'm paying for all of them anyway._
