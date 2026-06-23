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

<div class="cards">
  <a class="card" href="requirements.html">
    <span class="card-title">Requirements</span>
    <span class="card-desc">Supported platforms, build and runtime needs, and per-agent notes.</span>
  </a>
  <a class="card" href="demo.html">
    <span class="card-title">Terminal Demo</span>
    <span class="card-desc">A reproducible recording of the full plan → vote → build → review → adopt run.</span>
  </a>
  <a class="card" href="workflows.html">
    <span class="card-title">Workflows</span>
    <span class="card-desc">Orchestration, roles, personalities, targeting, resume, and artifacts.</span>
  </a>
  <a class="card" href="commands.html">
    <span class="card-title">Commands</span>
    <span class="card-desc">In-chat composer commands and the <code>council</code> CLI subcommands.</span>
  </a>
  <a class="card" href="configuration.html">
    <span class="card-title">Configuration</span>
    <span class="card-desc">The full <code>.council.yaml</code> reference — agents, quirks, roles, themes.</span>
  </a>
  <a class="card" href="architecture.html">
    <span class="card-title">Architecture</span>
    <span class="card-desc">Package layout, the terminal and orchestration models, trust boundaries.</span>
  </a>
</div>

The sidebar lists everything else — keyboard shortcuts, troubleshooting,
Windows support, and the release guide.

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
