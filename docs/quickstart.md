---
title: Quick Start
nav_section: Getting Started
nav_order: 1
---

# Quick Start

Install council, write a config, enable the agents you have, and drive your
first plan → vote → build run. For the full platform matrix and per-agent
notes, see [Requirements](requirements.md).

## 1. Install

Homebrew (macOS and Linux):

```bash
brew install umutarmut38/council/council
```

Or build from source (Go 1.23+):

```bash
git clone https://github.com/umutarmut38/council
cd council
go build -o bin/council ./cmd/council
```

Confirm it runs:

```bash
council version
```

## 2. Create a config

The quickest path is the interactive wizard. It detects the agent CLIs on your
`PATH`, lets you enable each one and pick its role, detects your project's test
command for the build gate, and writes a complete `~/.council.yaml` — with each
agent's terminal quirks (how it submits a prompt, whether it pastes, any delay)
already filled in from a built-in preset:

```bash
council config wizard
```

Prefer to edit by hand? `council config init` writes the same config with
**every agent disabled** and no auto-approval flags. Open `~/.council.yaml` and
flip on the ones you have — the preset quirks are already there, so you only
change `enabled`:

```yaml
agents:
  claude:
    enabled: true        # was false
  codex:
    enabled: true        # was false

review:
  # The build gate, run in each worktree. Set per project, e.g. ["npm", "test"].
  # Use [] to skip it.
  check_command: ["go", "test", "./..."]
```

Writing a config from scratch instead of editing the generated one? Start from
[examples/configs/minimal.yaml](https://github.com/umutarmut38/council/blob/main/examples/configs/minimal.yaml)
— a standalone file must spell out the per-agent `terminal` quirks that the
presets would otherwise provide (see the [agent table](requirements.md#runtime-requirements)).

Already working inside an agent CLI? The
[`council-config` skill](https://github.com/umutarmut38/council/blob/main/skills/README.md)
interviews you about your council and writes a repo-local `.council.yaml`
overlay (git-excluded, never committed). Install it into Claude Code, Codex,
Cursor, Copilot, or OpenCode with `scripts/install-skill.sh`.

> This config lives at `~/.council.yaml` (global). A repository can also carry a
> local `.council.yaml` that layers on top — it must be trusted once with
> `council trust`. See [Configuration](configuration.md).

## 3. Check your setup

```bash
council doctor
```

`doctor` verifies your agent commands are on `PATH`, that git is available, and
that the run directories are writable. Fix anything it flags before launching.

## 4. Launch

```bash
council
```

You're in the multiplexer, with a live pane per enabled agent:

- Type in the bottom composer and press `Enter` to broadcast to every agent.
- `Tab` / `Shift+Tab` move between panes; `F2` types directly into the focused
  agent (`Esc` returns).
- `Ctrl+G` opens the overview; `Ctrl+X` quits.

See [Shortcuts](shortcuts.md) for the full keyboard and mouse map.

## 5. Run the orchestration workflow

Inside a git repository, drive a full run from the composer:

```text
/plan @examples/issues/retry-backoff.md
/vote
/build
/start-build
/review
/adopt
```

Agents draft plans, rank each other's work (self-votes excluded), build the
winner in an isolated git worktree, and you adopt the diff. Everything is
written under `.council/runs/<timestamp>/`. The same flow also runs
non-interactively from the shell:

```bash
council plan "Add retry with backoff to the HTTP client"
council vote
council build
```

## Next steps

- [Workflows](workflows.md) — roles, personalities, targeting, resume, artifacts.
- [Configuration](configuration.md) — the full `.council.yaml` reference.
- [Commands](commands.md) — every composer and CLI command.
- Example configs:
  [minimal](https://github.com/umutarmut38/council/blob/main/examples/configs/minimal.yaml),
  [worker/reviewer](https://github.com/umutarmut38/council/blob/main/examples/configs/worker-reviewer.yaml),
  and a [headroom proxy](https://github.com/umutarmut38/council/blob/main/examples/configs/headroom.yaml).
