---
title: CLI Reference
nav_section: Reference
nav_order: 2
---

# CLI Reference

Every `council` subcommand, with its synopsis, verbs, and flags. This page is
generated from the command registry, so it always matches the tool — running
`council <command> --help` prints the same synopsis and flags for one command.

For the shorter one-line synopsis of every command, see
[Commands → CLI subcommands](commands.md#cli-subcommands). For how the in-chat
composer commands work, see [Commands](commands.md).

Global flags accepted by the bare `council` launch and the orchestration
commands:

- `--agents a,b` — only launch/target these agents (comma-separated).
- `--no-local-config` — ignore the repo-local `.council.yaml`.

`[run]` is a run timestamp (e.g. `20260605-130000`); omit it for the latest run.

---

## General

<!-- BEGIN GENERATED: cli-ref-general -->
### council launch

launch the interactive multiplexer

```text
council [--agents claude,codex] [--no-local-config]
```

The bare `council` command. Opens the interactive multiplexer with every enabled agent in its own live pane. `--agents` narrows the launch to a subset.

- `--agents a,b` — only launch these agents (comma-separated)
- `--no-local-config` — ignore the repo-local .council.yaml

### council ask

launch and broadcast a prompt

```text
council [--agents claude,codex] ask "<prompt>"
```

Launch the multiplexer and immediately broadcast one prompt to every launched agent.

- `--agents a,b` — only launch these agents (comma-separated)
- `--no-local-config` — ignore the repo-local .council.yaml

### council config init

write the default (safe) config

```text
council config init [--force] [--interactive]
```

- `--force` — overwrite an existing config
- `--interactive` — run the setup wizard instead (alias: -i)

### council config wizard

interactive setup

```text
council config wizard
```

Detect installed agent CLIs, pick which to enable and their roles, optionally opt into auto-approval, detect the project stack for the review gate, and write ~/.council.yaml.

### council config add-agent

add a known agent CLI to the config

```text
council config add-agent <preset> [--name x] [--role planner,builder,voter,review]
```

- `--name x` — agent name in the config (defaults to the preset name)
- `--role planner,builder,voter,review` — comma-separated roles (review = review-only; empty = all phases)

### council config schema

print the configuration reference (Markdown, or JSON Schema)

```text
council config schema [--json]
```

- `--json` — emit a JSON Schema (draft 2020-12) instead of Markdown

### council doctor

check config, commands, repo, run dirs (--fix applies safe fixes)

```text
council doctor [--fix]
```

- `--fix` — apply safe, local, reversible fixes (default is read-only)
- `--no-local-config` — ignore the repo-local .council.yaml

### council trust

trust this repo's .council.yaml

```text
council trust [--revoke|--show]
```

- `--revoke` — revoke trust for this repo's .council.yaml
- `--show` — print the trust status without changing it

### council version

print build version, commit, and date

```text
council version
```
<!-- END GENERATED: cli-ref-general -->

---

## Orchestration

Each orchestration command is repo-scoped — it only works inside a git
repository. The plan/vote/build/review phases open the live panes and block
until you quit them.

<!-- BEGIN GENERATED: cli-ref-orchestration -->
### council plan

start a run; each agent drafts a plan

```text
council plan "<issue>" | --file issue.md | --issue 123 [--agents a,b] [--base ref]
```

- `--file issue.md` — read the issue from a markdown file
- `--issue 123` — fetch the issue from GitHub by number (via gh)
- `--agents a,b` — comma-separated agent names
- `--base ref` — base ref for worktrees (default HEAD)
- `--no-local-config` — ignore the repo-local .council.yaml

### council vote

tally ranked votes into a winner

```text
council vote [run] [--agents a,b]
```

- `--agents a,b` — comma-separated agent names
- `--no-local-config` — ignore the repo-local .council.yaml

### council build

all agents implement the winning plan

```text
council build [run] [--agents a,b]
```

- `--agents a,b` — comma-separated agent names
- `--no-local-config` — ignore the repo-local .council.yaml

### council review

gate builds + reviewers pick the best

```text
council review [run] [--agents a,b]
```

- `--agents a,b` — comma-separated agent names
- `--no-local-config` — ignore the repo-local .council.yaml

### council adopt

preview + apply a build's diff

```text
council adopt [run] [agent] [--dry-run] [--yes]
```

- `--dry-run` — show what would be applied without touching the tree
- `--yes` — skip the confirmation prompt
- `--no-local-config` — ignore the repo-local .council.yaml

### council run

plan -> vote -> build

```text
council run "<issue>" | --file issue.md | --issue 123 [--agents a,b] [--base ref]
```

- `--file issue.md` — read the issue from a markdown file
- `--issue 123` — fetch the issue from GitHub by number (via gh)
- `--agents a,b` — comma-separated agent names
- `--base ref` — base ref for worktrees (default HEAD)
- `--no-local-config` — ignore the repo-local .council.yaml

### council resume

reopen an older run with fresh agent processes

```text
council resume [run] [--agents a,b]
```

- `--agents a,b` — comma-separated agent names
- `--no-local-config` — ignore the repo-local .council.yaml

### council status

show a run's phase, artifacts, and winners

```text
council status [run]
```

- `--no-local-config` — ignore the repo-local .council.yaml

### council cost

per-session usage and estimated cost

```text
council cost [run] [--since 30d] [--source ledger|codeburn] | cost prices refresh | cost models [filter]
```

```text
Verbs:
  cost [run]            usage/cost for one run (default: latest)
  cost prices refresh   refresh the LiteLLM price cache
  cost models [filter]  list price-table model names + aliases
```

- `--since 30d` — aggregate across runs newer than this (e.g. 30d, 7d)
- `--source ledger|codeburn` — ledger, or codeburn for machine-wide cross-tool totals
- `--no-local-config` — ignore the repo-local .council.yaml

### council report

write report.md (--post N comments on issue #N)

```text
council report [run] [--post N]
```

- `--post N` — post the report as a comment on GitHub issue #N (via gh)
- `--no-local-config` — ignore the repo-local .council.yaml

### council pr

open a PR from a build branch (via gh)

```text
council pr [run] [agent]
```

- `--no-local-config` — ignore the repo-local .council.yaml

### council scorecard

agent performance across runs

```text
council scorecard
```

- `--no-local-config` — ignore the repo-local .council.yaml

### council artifacts

scan run artifacts for likely secrets

```text
council artifacts scan [run] [--all]
```

```text
Verbs:
  artifacts scan [run]  scan one run (latest by default) for likely secrets
```

- `--all` — scan every run under the sessions root, not just one
- `--no-local-config` — ignore the repo-local .council.yaml

### council queue

batch issues through council

```text
council queue add|list|run|clear
```

```text
Verbs:
  queue add [--issue N | --file task.md | "<text>"]  append a task
  queue list                                        show queued tasks
  queue run                                         run each task as a full `council run`
  queue clear                                       empty the queue
```

- `--issue N` — (queue add) GitHub issue number
- `--file task.md` — (queue add) issue file

### council stack

set review.check_command

```text
council stack detect|set <go|node|rust|python>
```

```text
Verbs:
  stack detect                     detect the project stack and set the review gate
  stack set <go|node|rust|python>  set the review gate for a named stack
```

### council clean

remove council worktrees + branches

```text
council clean [--dry-run] [--yes]
```

- `--dry-run` — list what would be removed
- `--yes` — skip the confirmation prompt
- `--no-local-config` — ignore the repo-local .council.yaml

### council clean-runs

prune old run artifacts

```text
council clean-runs [--keep N] [--dry-run] [--yes]
```

- `--keep N` — number of most recent runs to keep (default 10)
- `--dry-run` — list what would be removed
- `--yes` — skip the confirmation prompt
- `--no-local-config` — ignore the repo-local .council.yaml
<!-- END GENERATED: cli-ref-orchestration -->
