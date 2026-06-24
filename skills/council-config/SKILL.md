---
name: council-config
description: Interactively scaffold a project-local .council.yaml for the council multi-agent coding-agent TUI. Use when a user wants to create, customize, or scaffold a .council.yaml; pick council members/agents and their launch commands; set roles, personalities, or categories; or tune the plan/vote/build/review orchestration, review gate, and UI for a repo. Writes the file at the git root and git-excludes it via .git/info/exclude (never .gitignore), so it is never committed.
---

# council-config

Design a repo-local `.council.yaml` for [`council`](https://github.com/umutarmut38/council)
by interviewing the user about how each council member should behave, then write
the file and make sure git ignores it locally and permanently.

`.council.yaml` is an **overlay**: council loads `~/.council.yaml` first, then
deep-merges the repo-local `.council.yaml` on top (searched from the current
directory up to the git root). Top-level sections (`ui`, `sessions`, `review`,
`files`, `policy`, `personalities`, …) and each entry under `agents`,
`personalities`, and `personality_categories` are merged field-by-field, so the
local file only needs the keys it wants to change per repo — it does not have to
redeclare a whole agent.

> Read [reference/schema.md](reference/schema.md) for every field, default, and
> the known per-agent terminal quirks before writing. It is generated from the
> repo's `internal/config` structs and is the source of truth for this skill.

## Hard rules (do not violate)

1. **Never add `.council.yaml` to `.gitignore`.** `.gitignore` is a committed
   file; putting it there leaks the file's existence and is itself a commit.
2. **Never commit `.council.yaml`.** Do not `git add` it. If it is already
   tracked, tell the user and offer `git rm --cached .council.yaml`.
3. Exclude it **locally** via `.git/info/exclude` (untracked, never committed).
   This is the only correct mechanism for this requirement.
4. **Invent nothing.** Every key you write must exist in
   [reference/schema.md](reference/schema.md). When unsure, leave it out — the
   file is an overlay and council fills defaults.

## Built-in alternatives (mention, don't replace)

council ships its own config tooling; this skill complements it by tailoring a
repo-local overlay through a conversation:

- `council config init [--force]` — write the default global `~/.council.yaml`
  (every agent preset disabled, no auto-approval flags).
- `council config wizard` — interactive global setup.
- `council config add-agent <preset>` — add a known agent CLI (`claude`,
  `codex`, `cursor`, `copilot`, `opencode`) to the config.
- `council stack detect` — write `review.check_command` for the detected stack.
- `council config schema` / `council doctor` — print the reference and validate
  the resolved config, commands on PATH, repo, and run dirs.

## Workflow

### 1. Locate the repo

Run `git rev-parse --show-toplevel`. Write `.council.yaml` at that root (so
council's upward search finds it from any subdirectory). If it is not a git
repo, tell the user `.git/info/exclude` is unavailable and ask whether to
proceed (the file just won't be git-excluded, since `.gitignore` is off-limits).

If a `.council.yaml` already exists at the root, read it and offer to edit or
extend it rather than overwrite.

### 2. Interview the user about the council

Ask how the council should be designed (use `AskUserQuestion` when running
inside Claude Code; otherwise ask in plain text). Don't dump the whole schema —
ask in this order, one focused question (or small batch) at a time. Make a
sensible recommendation the first option in each question; the user can always
override. Skip fields the user clearly doesn't care about — defaults are fine
and the file is an overlay.

**a. Which members?** Which agent CLIs join this council. Each member is an entry
under `agents.<name>`; the name labels the pane, branch, and artifacts (use
`[A-Za-z0-9_-]+`, e.g. `codex-worker`). Capture the launch `command` as an argv
list. Current preset commands (from `internal/config/presets.go`):

| Agent | `command` | Install hint |
|-------|-----------|--------------|
| claude | `["claude"]` | `npm i -g @anthropic-ai/claude-code` |
| codex | `["codex"]` | `npm i -g @openai/codex` |
| cursor | `["cursor-agent"]` | `curl https://cursor.com/install -fsS \| bash` |
| copilot | `["copilot"]` | `npm i -g @github/copilot` |
| opencode | `["opencode"]` | `npm i -g opencode-ai` |

> The same CLI can appear multiple times under different names (e.g.
> `codex-worker` and `codex-reviewer`) with different roles, models, or
> personalities. To avoid repeating the shared `command`/`terminal`/`env`,
> give one member an `inherit: <other-agent>` key: it copies that agent's whole
> definition (a preset, a global agent, or another local agent) and overrides
> the keys you set. A field overrides only when you set a non-zero value, so an
> inherited non-zero scalar or orchestration flag can't be reset to its zero
> value (e.g. you can't add `exclude_build: false` to undo a base's `true`); only
> `terminal.resize`/`terminal.color` are tri-state. `enabled` is never inherited
> — declare it per member — and `inherit` chains are allowed. Example:
>
> ```yaml
> agents:
>   codex-worker:
>     enabled: true
>     command: ["codex", "--model", "gpt-5.4-mini"]
>     role: [planner, builder]
>     terminal: { send_mode: paste, before_send_sequence: ctrl+u }
>   codex-reviewer:
>     inherit: codex-worker   # same command + terminal
>     enabled: true
>     role: [voter, review]   # only the role differs
> ```

**b. Role per member.** Roles select phases, one token each: `planner` (plan),
`builder` (build), `voter` (vote), `review` (review). Omit `role` for all four
phases. Legacy aliases still work: `worker` = `planner`+`builder`, and a bare
`reviewer` = `voter`+`review` (so for a **review-only** agent use `review`, not
`reviewer`). A useful council covers every phase with at least one member;
self-judging is always prevented automatically.

**c. Personality per member.** Optional. A `personality` injects a
`prompt_prefix` into that agent's prompts and drives UI grouping. Define them
under `personalities.<name>` (`label`, optional `category`, `color`, `order`,
`prompt_prefix`) and group them under `personality_categories.<name>` if the
user wants grouped layouts. Ask whether to reuse a starter set (optimist,
pragmatist, pessimist, critic) or invent new ones. Personalities are orthogonal
to roles.

**d. Orchestration / auto-approval.** Phases run in live interactive panes, so an
unattended build/plan/vote needs the agent to auto-approve its own edits.
Auto-approval flags are **never** set by default and bypass each tool's
permission prompts — set them only with the user's consent, per phase, via
`orchestration.{plan,vote,build}_command`. Known opt-in commands:

| Agent | Auto-approval command |
|-------|-----------------------|
| claude | `["claude", "--dangerously-skip-permissions"]` |
| codex | `["codex", "--full-auto"]` |
| cursor | `["cursor-agent", "--force"]` |
| copilot | `["copilot", "--allow-all-tools"]` |

Copilot ships `orchestration.exclude_build: true` by default (less suited to
parallel build worktrees). Ask whether any member should be excluded from a
phase (`exclude`, `exclude_plan`, `exclude_vote`, `exclude_build`). Note that
`policy.mode: safe` refuses to run when an enabled agent carries such flags.

**e. Terminal delivery.** Most agents work with defaults. Only set `terminal`
keys for a member known to need them (see the quirk table in the reference):

- codex → `send_mode: paste`, `before_send_sequence: ctrl+u`, `submit_sequence: cr`.
- claude / cursor / copilot → `send_mode: type`, `submit_sequence: cr`,
  `submit_delay_ms: 250` (lands Enter as its own write so the prompt posts).
- opencode → typed defaults, no delay.

**f. UI + review.** Ask about `ui.layout` (always `grid`), `ui.adaptive_grid`
(default `true`; sizes the grid to the visible panes), `ui.page_rows`/`page_cols`
(bound the page for large rosters), `ui.group_by` (`none` / `personality` /
`category`), and `ui.initial_prompt_delay_ms` (raise to ~`8000` when running many
agents). Set `review.check_command` to the project's build/test gate run in each
build worktree before voting, e.g. `["go", "test", "./..."]`, `["npm", "test"]`,
`["cargo", "build"]` — empty means no gate (`council stack detect` can fill it).

**g. Optional extras.** Only if the user asks: `sessions` (`root_dir`,
`private`, `redact`), `policy.mode` (`safe` / `normal` / `aggressive`), `files`
(`allow_absolute`, `max_bytes`), and the **experimental** `env` + `setup`
hooks (require `experimental.setup_env: true`, and from a repo-local file are
subject to the trust gate below).

### 3. Write `.council.yaml`

Write only the sections the user customized (it overlays the global config).
Validate against [reference/schema.md](reference/schema.md): correct key names,
`command` / `role` / `check_command` as YAML lists, `prompt_prefix` as block
scalars (`|`), and `color` as a quoted string (`"212"` or `"#ff5f87"`). Add a
top comment noting the file is git-excluded locally and must not be committed.

### 4. Exclude from git (never via .gitignore)

After writing, at the repo root:

1. Confirm it isn't already tracked: `git ls-files --error-unmatch .council.yaml`.
   If it **is** tracked, warn the user and offer `git rm --cached .council.yaml`.
2. Ensure `.council.yaml` and `.council.yml` are in `.git/info/exclude`, adding
   them only if missing (don't duplicate lines). Do **not** modify `.gitignore`.
3. Verify: `git status --porcelain .council.yaml` should print nothing
   (untracked + excluded). Report the result to the user.

### 5. Confirm

Tell the user the path written, the members configured, and that it's excluded
locally via `.git/info/exclude` (not `.gitignore`, not committed). Then:

- A repo-local config changes which commands council runs, so it is **trusted on
  first use**: council prompts before applying it (and whenever its content
  changes). Mention `council trust` to approve it non-interactively, and
  `--no-local-config` to ignore it for one invocation.
- Suggest `council doctor` to verify the agent CLIs resolve on PATH and to audit
  any auto-approval flags.
