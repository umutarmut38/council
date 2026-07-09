---
title: Configuration
nav_section: Reference
nav_order: 1
has_children: true
---

# Configuration

council reads `~/.council.yaml`. Create it with `council config init`. A repo may
also carry a local `.council.yaml` that layers on top (see
[Per-repo override](#per-repo-override)).

```yaml
agents: { … }                 # the agents and how to drive them
ui: { … }                     # layout, paging, grouping, timing, themes
sessions: { root_dir: … }     # where runs are written, privacy, redaction
review: { check_command: … }  # the build gate (+ timeout and output caps)
worktrees: { freestyle: … }   # optional: per-pane git worktrees
usage: { enabled: … }         # optional: local cost/usage ledger
env: { … }                    # optional: exported env (experimental)
setup: [ … ]                  # optional: pre-launch commands (experimental)
files: { … }                  # optional: @file expansion limits
policy: { mode: … }           # optional: safe | normal | aggressive
personality_categories: { … } # optional: persona groups
personalities: { … }          # optional: behavioral personas
```

The generated default config ships every agent preset **disabled** and without
auto-approval flags — enable what you use, or run `council config wizard`.

> **Prefer a guided setup for a repo?** The
> [`council-config` Agent Skill](../skills/README.md) interviews you and writes a
> repo-local `.council.yaml` overlay (git-excluded via `.git/info/exclude`). It
> installs into Claude Code, Codex CLI, Cursor, GitHub Copilot, and OpenCode via
> `scripts/install-skill.sh`.

## Sections

Each section has its own page:

- **[Agents](config-agents.md)** — the agents, roles, terminal/send behavior, and per-phase orchestration.
- **[UI & themes](config-ui.md)** — layout, paging, grouping, timing, and color themes.
- **[Sessions & review](config-sessions.md)** — where runs are written, privacy/redaction, and the build gate.
- **[Worktrees](config-worktrees.md)** — opt-in per-pane git worktrees for freestyle panes.
- **[Usage & cost](config-usage.md)** — the local cost ledger, `/cost`, and pricing.
- **[Files, environment & policy](config-files-policy.md)** — `@file` limits, `env`/`setup`, and the automation policy mode.
- **[Personalities](config-personalities.md)** — behavioral personas and categories.
- **[Schema reference](config-schema.md)** — the generated field-by-field tables (authoritative).

Everything below applies across all sections: how a repo-local config layers over
your global one, and the trust gate that protects it.

---

## Per-repo override

A repository can carry its own `.council.yaml` (or `.council.yml`) that
**layers over** the global `~/.council.yaml`. council searches from your current
directory up to the git repo root.

The merge is **deep**: top-level sections (`ui`, `sessions`, `review`,
`personalities`, …) and each agent are merged field-by-field, so a local file
only needs the keys it changes:

```yaml
# .council.yaml at a repo root — a Node project that only wants two agents
agents:
  codex: { enabled: false }
review:
  check_command: ["npm", "test"]
ui:
  initial_prompt_delay_ms: 5000
```

When a local file is applied, council prints `Using repo config <path>`.

### Trust

A repo-local config can change **which commands council executes** (including
auto-approval flags), so it is only applied once you trust it:

- The first time council sees a repo's `.council.yaml` (or whenever its
  content changes), it asks before applying it and remembers your answer by
  content hash.
- `council trust` trusts the current repo's config explicitly (useful in
  scripts); `council trust --show` audits it; `council trust --revoke` forgets it.
- `--no-local-config` ignores repo-local config for one invocation.
- The trust store lives under your user config directory
  (`council/trust.json`). `council stack` writes are auto-trusted — you just
  authored them.
