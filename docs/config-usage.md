---
title: Usage & cost
parent: Configuration
nav_section: Reference
nav_order: 5
---

# `usage`

A local, provider-agnostic cost ledger. Off by default; everything it records
stays under `.council/`, and it never needs a provider account, API key, or
billing API. Turn it on to see a run-cost total in the header, a per-pane cost in
each pane border, and the `/cost` breakdown:

```yaml
usage:
  enabled: true
agents:
  codex:
    enabled: true
    usage:
      tool: codex          # provider-session reader: claude|codex|copilot|cursor|opencode
      model: gpt-5-codex   # model for estimated pricing until a report supplies one
```

## How cost is calculated

council only knows what it can observe locally, so cost is built in two layers,
and every number carries a **confidence**:

1. **Estimated floor.** Council prices each prompt immediately with a local
  estimator (`bytes4` — roughly 4 bytes per token — or `runes4`). Input is
  measured from the prompt the model actually sees (personality prefix + your
  text), *not* the terminal control bytes on the wire. Output is the transcript
  delta, flushed at the next prompt, `/save`, or pane termination. This lower
  bound is shown as **estimated**.
2. **Reported totals.** When you request `/cost`, or after the TUI exits, council
  reads each declared `usage.tool` CLI's *own* session files (Claude Code,
  Codex, Copilot, opencode; cursor-agent records no token counts) and reconciles
  the real numbers over the estimate, shown as **reported**. Cached/reused
  context is kept in its own column and priced at the cheaper cache-read rate
  rather than double-charged as fresh input.

council never guesses an agent's tool or model from its `command` — set
`usage.tool` and `usage.model` explicitly, or the row stays an estimate. An
unknown `usage.tool` is rejected when the config loads.

Confidence shows as a prefix in the header, the pane badges, and the `/cost`
share bars, so it reads at a glance:

| Shown | Meaning |
|---|---|
| `~$0.02` | **estimated** — council's local character estimate |
| `$0.02` | **reported** — real token counts from the CLI's session files |
| `$?` | **unknown** — usage recorded, but no price could be resolved |

An unknown paid model stays `$?`; council never silently reports it as `$0`. Real
sub-cent costs show extra decimals (`~$0.0047`) instead of rounding to `$0.00`.

## Reading `/cost`

`/cost` in the TUI (or `council cost [run]` from the CLI) shows a per-session
table and a share bar of where the run's spend went:

```text
Agent          Phase    Tool   Model         Input  Cache  Output  Cost     Source           Confidence
codex-builder  session  codex  gpt-5.4-mini  8.8k   4.5k   36      $0.01    litellm-bundled  exact
codex-planner  session  codex  gpt-5.4-mini  5.3k   8.1k   36      $0.0047  litellm-bundled  exact
Total                                         14.1k  12.6k  72      $0.0147

Share:
codex-builder  ███████████░░░░░░░░░  68%  $0.01
codex-planner  ██████░░░░░░░░░░░░░░░  32%  $0.0047
```

**Input** is *fresh* input only; **Cache** is the reused context, priced at the
cache-read rate. That split is why two panes that sent nearly the same context can
cost different amounts — above, the planner sent less fresh input and hit cache
more, so it is cheaper despite the near-identical total. **Source** and
**Confidence** describe the *price* (where the rate came from and whether it was
an exact table match), not the token counts. The header carries the run total
(right-pinned), and while you type, the composer shows a live `~N tok` estimate of
what the send will cost before it fans out to every agent.

## Attribution and pricing

**Reconciliation** matches an estimate to a reported session by `(tool, cwd)`, and
credits only sessions whose activity overlaps the run. Two same-tool panes sharing
one directory can't be told apart, so they report as a single combined row; give
them distinct directories with [`worktrees.freestyle`](config-worktrees.md) for a
per-pane breakdown. A few CLIs (notably Copilot) write their totals only when the
process exits, so their input stays estimated while the pane is running. After the
pane exits, request `/cost` to see its reported total, or quit council and let the
final reconciliation update persisted history automatically.

**Pricing** resolves a model in order: a user `usage.prices` profile → a user or
built-in `usage.model_aliases` → a fresh LiteLLM cache → the bundled LiteLLM
snapshot → unknown. Refresh the cache with `council cost prices refresh` (the only
step that touches the network). A `usage.prices` profile sets input/output
per-million rates; cache-write/read rates derive from the input rate unless you
set them, so a cache-heavy session is never priced at `$0`. A profile older than
`stale_price_after_days` raises a warning.

View costs with `/cost` in the TUI, or `council cost [run]` / `council cost --since
30d` from the CLI. `council cost --source codeburn` relays machine-wide totals
from the optional `codeburn` CLI (`npm i -g codeburn`) for tools council doesn't
launch itself. See the [CLI Reference](cli.md#council-cost) for the full `council
cost` verbs and flags.

The generated field tables for `usage`, `usage.prices.<name>`, and
`agents.<name>.usage` are in the [Schema reference](config-schema.md#usage).
