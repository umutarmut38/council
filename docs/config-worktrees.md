---
title: Worktrees
parent: Configuration
nav_section: Reference
nav_order: 4
---

# `worktrees`

Opt-in per-pane isolation for **freestyle** panes — the agents you chat with
directly, outside the orchestrated workflow. Off by default. Orchestration is
never affected: `/plan`, `/build`, and `/review` always use their own
run-stamped worktrees under `.council/worktrees/<stamp>/`.

```yaml
worktrees:
  freestyle: true          # each freestyle pane gets its own worktree
  seed:                    # extra files copied in, on top of the built-in allowlist
    - .env.example
    - config/*.yaml
```

With `freestyle: true`, each freestyle pane runs in its own **persistent,
repo-local** git worktree at `.council/workspaces/<agent>`, created on first use
as a detached checkout (no `council/<agent>` branch spam). This buys two things:

- **Per-pane cost.** Same-tool panes get distinct working directories, so
  provider-session [reconciliation](config-usage.md#attribution-and-pricing) can
  attribute usage to each pane instead of collapsing several into one combined
  row.
- **File isolation.** Panes editing the same files no longer stomp each other.

Worktrees are **reused across sessions and never auto-reset**, so a pane keeps its
work between runs. The pane border shows a staleness marker — `⟳` when the
worktree is behind the repo HEAD, `*` when it has uncommitted changes.
[`/refresh`](commands.md) resets a worktree to HEAD and re-seeds it (it refuses
when the worktree is dirty unless you append `force`); [`/clean`](commands.md)
lists and removes them. Staleness is probed on demand, never on a timer.

`seed` copies additional files/globs (relative to the repo root) into each
worktree when it is created, on top of the built-in instruction-file allowlist:
`CLAUDE.md`, `AGENTS.md`, `GEMINI.md`, `QWEN.md`, `.cursorrules`,
`.github/copilot-instructions.md`, and `.mcp.json`. Use it for git-ignored files
an agent still needs (a local `.env`, machine config). Nothing is seeded that you
don't list, beyond that allowlist.

Some tools can't run in a clean detached checkout because they rely on the live
working tree (an installed `node_modules`, build output, uncommitted state). Keep
those in council's launch directory with a per-agent override:

```yaml
agents:
  copilot:
    worktree: false        # stay in the launch directory even when freestyle is on
```

Freestyle worktrees are a no-op outside a git repository.
