---
title: Home
nav_order: 1
permalink: /
---

<div class="hero">
  <div class="hero-wordmark">council<span class="cursor">█</span></div>
  <p class="hero-tagline">A terminal workbench for running a council of AI coding
  agents — each in its own live PTY pane, driven through one structured workflow.</p>
  <div class="pipeline" role="list" aria-label="Council workflow">
    <span class="pipeline-step" role="listitem"><b>1</b>plan</span>
    <span class="pipeline-sep" aria-hidden="true">▸</span>
    <span class="pipeline-step" role="listitem"><b>2</b>vote</span>
    <span class="pipeline-sep" aria-hidden="true">▸</span>
    <span class="pipeline-step" role="listitem"><b>3</b>build</span>
    <span class="pipeline-sep" aria-hidden="true">▸</span>
    <span class="pipeline-step" role="listitem"><b>4</b>review</span>
    <span class="pipeline-sep" aria-hidden="true">▸</span>
    <span class="pipeline-step" role="listitem"><b>5</b>adopt</span>
  </div>
  <div class="hero-actions">
    <a href="requirements.html" class="btn btn-primary mr-2">Get started</a>
    <a href="configuration.html" class="btn mr-2">Configuration</a>
    <a href="https://github.com/umutarmut38/council" class="btn">View on GitHub</a>
  </div>
</div>

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
