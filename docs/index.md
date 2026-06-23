---
title: Home
nav_order: 1
permalink: /
---

# council
{: .fs-9 }

A terminal workbench for running multiple AI coding agents at once. Each agent
gets a live PTY pane; broadcast or direct prompts to them, and orchestrate a
**plan → vote → build → review → adopt** workflow.
{: .fs-6 .fw-300 }

[Get started](requirements.md){: .btn .btn-primary .mr-2 }
[Configuration](configuration.md){: .btn .mr-2 }
[View on GitHub](https://github.com/umutarmut38/council){: .btn }

---

## Start here

- [Requirements](requirements.md) — supported platforms, build and runtime
  requirements, and agent-specific notes.
- [Terminal Demo](demo.md) — a reproducible VHS recording of the full
  plan → vote → build → review → adopt workflow.
- [Workflows](workflows.md) — multiplexer usage, orchestration, roles,
  personalities, targeting, resume, and artifact locations.
- [Commands](commands.md) — in-chat composer commands and CLI subcommands.
- [Keyboard Shortcuts](shortcuts.md) — keys for panes, pages, direct input,
  overview, settings, run browsing, and the integrated editor.
- [Configuration](configuration.md) — full `.council.yaml` reference: agents,
  terminal quirks, orchestration roles, personalities, themes, and overrides.
- [Architecture](architecture.md) — package layout, terminal model,
  orchestration model, and trust boundaries.
- [Troubleshooting](troubleshooting.md) — prompt delivery, rendering, folder
  trust, resume, and worktree cleanup.
- [Windows Support](windows.md) — what is supported, experimental, and
  smoke-tested, plus ConPTY limitations.
- [Release Guide](release.md) — release checklist, artifacts, and tagging.

---

## Workflow

![council workflow](assets/council-workflow.svg)

```text
/plan @examples/issues/retry-backoff.md
/vote
/build
/start-build
/review
/adopt
```

## Resume

![Resume flow](assets/resume-flow.svg)

Runs are stored under `.council/runs/<timestamp>/`. If the TUI is interrupted
inside `plan`, `vote`, `build`, or `review`, `/resume` relaunches fresh agent
processes into the same phase, keeps existing artifacts, and prompts only the
unfinished agents.

## Requirements at a glance

- Go 1.23+ to build from source.
- Git 2.35+ for worktrees and patch adoption.
- A terminal with Unicode and ANSI color support.
- One or more configured agent CLIs — Claude Code, OpenAI Codex, Cursor Agent,
  GitHub Copilot CLI, or opencode.

See [Requirements](requirements.md) for the full matrix, and the repository
[README](https://github.com/umutarmut38/council#readme) for installation and
release artifacts.
