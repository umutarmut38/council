# Changelog

All notable changes will be documented here.

This project follows semantic versioning once `v0.1.0` is tagged.

## v1.1.0 - 2026-07-09

Provider-independent cost tracking lands, and the HUD is redesigned around it.
council now meters every run locally — an estimated character floor reconciled
against each CLI's own session files — and surfaces it in a redesigned header,
per-pane badges, and a styled `/cost` view with per-agent share bars. Also a
command-registry audit (real flags, `/quit` actually quits) and opt-in freestyle
worktrees.

### Added

- **Provider-independent cost tracking (`usage:`).** A local, provider-agnostic
  per-session cost ledger (`events.jsonl`) with a two-layer model: an estimated
  character floor, reconciled against each CLI's own session files where one
  exists. Native session readers ship for Claude, Codex (`rollout-*.jsonl`), and
  opencode (SQLite, pure-Go `modernc.org/sqlite`); cursor and copilot stay on the
  estimated floor. A bundled LiteLLM pricing snapshot (3944 models) prices runs
  offline, with an owner-only refreshable cache. Model is auto-discovered from an
  agent's session files when `usage.model` is unset. Cost is never silently
  `$0`: metered-but-unpriced shows `$?`. Includes a `council cost` CLI and
  `council cost models [filter]` to find canonical price-table names.
  (#51, #56)
- **Redesigned cost HUD.** The header pins run cost flush-right beside a phase
  stepper line, per-pane badges lead with a liveness glyph (● active / ◌ idle
  age / ✓ clean exit / ✕ failure / ! needs input), and one estimate vocabulary
  runs throughout — `~$` estimated, `$` reported, `$?` unknown. The `/cost` view
  is styled and gains per-agent share bars showing who is burning the budget.
  The composer shows a live `~token` estimate as you type and an accented target
  chip (focus color for a one-agent send). Status is now a transient toast
  (4s TTL) driven by an always-on 1s UI clock. (#53)
- **Freestyle worktrees (opt-in).** Agents can work in isolated worktrees.
  (#51)

### Changed

- **Command-registry audit.** `/quit` and `/exit` now return `tea.Quit` instead
  of killing agents and leaving dead panes; `/help` opens the full reference in
  the pager instead of dumping names to the status line; trailing orchestration
  flags (`plan/run --base/--agents/--file/--issue`, `vote/build/review/resume
  --agents`, `cost --source`, `doctor --fix`, …) are now honored instead of
  silently dropped; and the help/docs use strings list the real flags. Dead code
  (`CLI.RequiresRepo`, unreachable branches) removed. (#55)

### Fixed

- **`/cost` table precision and estimate flagging.** Sub-dime rows now show four
  decimals so small rows reconcile with their total; JPY prints with no minor
  unit; cache reads and writes are split into separate columns (they price at
  0.1× vs 1.25× input) so a row's cost reconciles; and the Cost column carries
  the `~` estimate prefix from token confidence (Copilot mid-run no longer reads
  as final). (#54, #56)
- **Live-usage typing lag.** The 1s reconcile sweep re-parsed every provider
  transcript each second while agents streamed; it's throttled to once per 3s and
  finished transcripts are cached (size+mtime validated), so typing stays
  responsive. (#56)
- **Direct-mode metering.** Direct mode now meters raw typed keystrokes (not the
  wrapped composer prompt) and records an estimate on Enter that seeds provider
  reconciliation, so a direct-only run reports its real cost. (#56)
- **Price-name resolution.** Dotted model names (`claude-haiku-4.5`) resolve to
  their dashed LiteLLM key as a last-resort candidate, and the "price unknown"
  hint now names the remedy (`map it in usage.model_aliases`). (#56)

## v1.0.0 - 2026-06-26

The first stable release. The `.council.yaml` schema and platform support are
settled — macOS, Linux, and Windows (ConPTY) ship prebuilt binaries — and
configs written for 1.0 keep working. Headline work since v0.4.0: full per-phase
roles, a `/refine` consensus round, build comparison and a post-adopt receipt,
mouse support, an integrated editor with a file tree, and configurable color
themes.

### Added

- **Mouse support (`ui.mouse`, default on).** Wheel scrolls the pane under the
  cursor through its history (a `↑N` marker shows when a pane isn't live; new
  output stays anchored while scrolled), and a click focuses a pane. The wheel
  also scrolls the list/pager screens (overview, settings, runs, artifacts,
  compare). In direct mode and the integrated editor, mouse events are forwarded
  to the agent's PTY (as SGR reports with pane-local coordinates) when that
  program has enabled mouse tracking — so `nvim` (`mouse=a`), `less`, and agent
  TUIs receive them; otherwise the wheel falls back to council's own scrollback.
  `Ctrl+W` toggles mouse capture at runtime to restore native text selection;
  set `ui.mouse: false` to start with it off.
- **Configurable editor (`ui.editor`).** Choose the editor used to open files
  from `/artifacts`, `/compare`, and the integrated editor — e.g. `nvim` or
  `code -w`. It takes precedence over `$VISUAL`/`$EDITOR`, falling back to `vim`.
- **Integrated editor with a file tree (`/edit [path]`).** A VSCode-style split:
  a collapsible file-tree sidebar on the left and the configured editor running
  inside council as a PTY pane on the right, reusing the existing agent terminal
  stack. Navigate the tree (`↑/↓`, `→`/`Enter` to expand/open, `←` to collapse),
  jump into the pane with `Tab`, and route keystrokes — including `Esc` — to the
  editor; `F2`/`Ctrl+O` returns to the tree while the editor stays live.
  Selecting another file opens it in the same editor via `ui.editor_open_cmd`
  (default `:e {path}` for vim/nvim; set empty to relaunch per file).
  `/edit <path>` opens a file directly.
- **Editable `/artifacts` browser.** The artifacts browser is now a split — the
  artifact list on the left, the selected file open in the integrated editor on
  the right — so plans, votes, diffs, and reports can be edited in place. `e`
  still opens the external editor; synthetic views (preview/diff/adopt) remain a
  read-only pager. `/compare` gains an `i` action that opens a build's worktree
  (or a file) in the integrated editor.
- **Color themes (`ui.theme`).** Recolor the whole UI chrome — header, footer,
  pane borders, dividers, suggestions, and the diff viewer — with a built-in
  palette (`default`, `nord`, `solarized`, `mono`) or a custom one defined under
  `ui.themes.<name>`. Each role (`title`, `status`, `border`, `focus`, …) is a
  256-color index or hex; unset roles inherit `default`. Per-agent/personality
  `color` still wins for pane borders, and `council doctor`'s color strip renders
  in the active theme. Indexed-256 only, so themes render identically across
  terminals.
- **Full per-phase roles.** `role:` takes one token per phase — `planner`,
  `builder`, `voter`, `review` — so an agent can plan without building or review
  without voting; the legacy `worker`/`reviewer` aliases still expand to the
  phase pairs.
- **`/refine` consensus round.** Every planner revises its plan from the vote
  critiques (or a note you pass), then the council revotes — for converging on a
  single plan before building.
- **`/compare` before review, with live build progress.** Inspect builds — files
  and git-style diffs versus the base or between builds — and watch each
  worktree's build advance live.
- **Full-screen post-adopt receipt.** After `/adopt`, a full-screen receipt
  summarizes exactly what was applied to your working tree.
- **Composer input history.** Arrow up/down walks the prompts you've sent.
- **Richer `/status`.** Shows the active run and phase with the pinned vote
  winner.
- **Confirm-before-interrupt.** `Ctrl+C` in the composer asks before
  interrupting instead of dropping in-progress input.

### Fixed

- **Terminal emulator: charset-designation leak.** Intermediate-byte escape
  sequences such as nvim's frequent `ESC ( B` no longer leak their final byte as
  a literal character into a pane.
- **Terminal emulator: cursor rendering and `Esc` passthrough.** The emulator now
  tracks cursor visibility (DECTCEM `?25h`/`?25l`) and the focused integrated
  editor pane draws a block cursor; `Esc` is forwarded to the PTY so vim/nvim
  leave insert mode.
- **Composer: leaked mouse reports no longer type into the input.** On terminals
  that emit wheel reports council's runtime can't parse as mouse events (or when
  the escape is split across reads), the fragment is dropped instead of being
  typed into the composer as text.
- **Live `/compare` refresh and safe adopt preview.** The compare view refreshes
  as builds change, and `/preview`/`/adopt` never mutate the working tree while
  showing what they would apply.

### Changed

- **`council --help` and `-h` now print the full usage.** Previously only the
  bare word `council help` did; `--help`/`-h` were intercepted by flag parsing
  and exited non-zero. The usage also gained a tagline, flag descriptions, an
  examples block, and a docs footer.

### Docs

- New **Quick Start** guide and a `minimal.yaml` starter config; the interactive
  `council config wizard` is featured as the recommended setup path.

### Quality / CI

- v1.0 cleanup: dropped an unused internal command-runner seam and de-duplicated
  the bounded output writer behind a shared, concurrency-safe `capbuf` package.

## v0.4.0 - 2026-06-17

Agent inheritance and keyboard-driven broadcast targeting, plus config schema
tooling, generated docs, artifact secret scanning, and quality automation.

### Added

- **Agent inheritance (`inherit:`).** An agent can reuse another agent's whole
  definition by name — a preset, a global agent, or another local agent,
  resolved against the fully-merged config — and override only the keys it
  declares, so worker/reviewer variants of the same CLI no longer repeat shared
  `command`/`terminal`/`env` blocks. A field overrides only when set to a
  non-zero value (terminal `resize`/`color` are tri-state); `env` merges per
  key; `enabled` is never inherited; chains are allowed. Resolution runs once on
  the fully-merged agent map, and `council doctor`/validation reports a dangling
  base, a self-reference, or a cycle.
- **Pre-launch `setup` commands and agent `env` (experimental, opt-in).**
  Council can export environment variables to agents (top-level `env` +
  per-agent `agents.<name>.env`) and run commands before any agent launches
  (`setup`: one-shot, or supervised `background` services with an optional
  `wait_for_port` readiness gate, stopped on exit). This is a vendor-agnostic
  primitive — e.g. start a local context-compression proxy and point agents at
  it (see `examples/configs/headroom.yaml`). Because `setup` runs arbitrary
  commands, the feature is **off by default**: enable it with
  `experimental.setup_env: true`, otherwise any `env`/`setup` is ignored and
  `council doctor` warns. `setup` from a repo-local config is additionally
  gated by the trust store; `council doctor` lists the env keys and setup
  commands when enabled.
- **`council config schema` and a shared command registry.** Config and command
  documentation is generated from the source structs and a single command
  registry, so CLI help, TUI command metadata, palette suggestions, and the docs
  stay aligned; `council config schema` prints the reference (with JSON Schema
  output), and a CI job fails on generated-doc drift.
- **Artifact secret scanning.** `council artifacts scan [run] [--all]` flags
  likely secrets in run artifacts, with pre-warnings before `report`/`pr`
  sharing (raw PTY logs are never redacted).
- **`/setup` observability.** Shows each setup command's lifecycle, readiness,
  captured output, and exported env keys.
- **`council-config` skill and a multi-CLI installer** for scaffolding a
  repo-local `.council.yaml` interactively.

### Changed

- **`Ctrl+B` cycles the broadcast target through groups.** It now walks
  all → each group of the active `ui.group_by` (personality or category, in
  configured order) → focused, instead of only toggling all ↔ focused — so
  broadcasting to a personality/category group is reachable from the keyboard
  (previously only via `/target`). With `group_by: none` it stays all ↔ focused.
- **Approval-prompt detection reworked (now marked experimental).** The old
  detector matched substrings anywhere in the output stream and latched
  forever, so codex's greeting "What do you want to work on?" lit up
  "needs input". A pane is now flagged only when a concrete approval
  phrasing is visible in the last lines of its live screen *and* the agent
  has been quiet for ~2s; the auto-flag clears itself when output resumes
  (manual `/attention` flags stick). New `ui.detect_approval_prompts: false`
  disables it.
- **A deeper `council doctor --fix`**: next-action guidance, stack detection,
  and safer, symlink-skipping artifact-permission repair.
- Centralized command execution behind `internal/cmdrun` (context timeouts,
  output caps, structured errors, and a fakeable runner), and added hermetic
  CLI-layer tests for the `cmd/council` command and config/maintenance flows.

### Quality / CI

- New jobs for generated-doc drift, markdown link checking, YAML example
  linting, and shellcheck, plus a coverage summary artifact — alongside the
  existing race detector and Windows cross-build smoke.

## v0.3.0 - 2026-06-11

A large UI/UX and safety release: the TUI becomes an orchestrator HUD with a
phase rail, adaptive layout, a command palette, and an interactive build
inspector, while runs gain a trust model, private-by-default artifacts, and
preview-before-apply adoption.

### Added (UI/UX)

- **`/compare` is a full build inspector**: navigate candidate builds,
  drill into changed files, read per-file git-style colored diffs, open the
  live worktree (or a single file) in `$EDITOR`, and mark one build with `x`
  to diff two implementations directly against each other (via git trees —
  exact rename/mode handling, no noise).
- **`/adopt` opens the full preview**: files, dirty-tree warning, and the
  complete diff, applied with an explicit `y` — the previous status-line-only
  confirm was routinely missed, leaving users believing they had adopted.
- **`$EDITOR` integration**: `e` opens the viewed artifact, diff, or selected
  file in `$VISUAL`/`$EDITOR` (vim by default) from /artifacts, /preview, and
  the adopt preview.
- **Vertical @file picker**: the `@` file suggestions use the same vertical,
  arrow-navigable list as the command palette.
- **Per-agent colors**: `agents.<name>.color` (or the personality color)
  tints the pane border — full strength while focused, a computed darker
  shade otherwise; the pane content stays untouched. Rendering sticks to
  indexed cube colors (never SGR faint, never truecolor sequences), the only
  encoding that proved consistent across emulators; `council doctor` now
  prints a color test strip for diagnosing terminal differences. VS Code
  users need `terminal.integrated.customGlyphs: false` for colored borders
  (its custom-glyph renderer drops colors on box/block glyphs).
- **Smoother rendering**: 120 FPS frame budget and a larger PTY read buffer.
- Esc from synthetic views (/compare, /preview, /clean) returns to the panes;
  only files opened from /artifacts return to the list.
- The command/@file palettes **overlay** the bottom of the panes instead of
  reflowing them, so the agent CLIs no longer jump while a command is typed.
- Fixed panes rendering at the wrong size after visiting overview/settings or
  while a palette was open (PTY sizes no longer track transient footers, and
  every screen exit re-syncs sizes).
- **Command palette**: typing `/` opens a vertical, arrow-navigable list of
  commands (↑/↓ select, Tab/Enter complete), with the commands suggested for
  the current pipeline stage marked ● and sorted first.
- **Rounded pane borders**: panes use the same box-drawing set as the
  composer (no more `+--|` ASCII), and the focused pane title is bold.
- **Resume hardening**: every stage resumes correctly — interrupted `/refine`
  rounds resume with the refine prompt (not a from-scratch plan prompt), and
  post-review runs reopen idle with the HUD pointing at `/compare or /adopt`.
- **Orchestrator HUD**: a phase rail in the header (`Plan 2/2 ✓  Vote 0/2 ●
  …  · Next: /vote`) with artifact counts and the recommended next command,
  visible whenever a run is active.
- **Adaptive grid** (default): 1 pane fills the screen, 2 sit side by side at
  full height, 3-4 use a 2x2; bigger rosters page as before. Adjusting
  rows/cols in `/settings` locks the layout; `ui.adaptive_grid: false`
  disables it.
- **Pane badges** carry phase state: `vote · waiting for VOTE.md`,
  `vote · wrote VOTE.md`, `build · working`, `needs input`.
- **Approval-prompt detection**: panes that print `[y/N]`-style or trust
  prompts get an orange border and a footer recovery hint; `/attention
  <agent> [off]` flags or clears manually.
- **Context-aware footer**: next actions and recovery commands during a run,
  the generic shortcut list otherwise.
- **Overview is a run dashboard**: phase progress plus each agent's role,
  personality, visibility, and artifact state.
- `/preview` now renders the **full diff** in the pager, stages the adopt,
  and accepts `y`/`n` to apply or cancel; `/compare` shows each agent's
  anonymized review letter.
- Settings show a live layout preview; header paths compress `$HOME` to `~`.

### Fixed

- `/compare` and `/adopt` no longer list the anonymized `diff-<letter>`
  reviewer copies as candidate builds.

### Changed (safety)

- **Worktrees are now per-run**: `.council/worktrees/<stamp>/<agent>` instead
  of `.council/worktrees/<agent>`, with branch verification on reuse — a new
  run can no longer build inside a stale checkout, and reviews only consider
  the current run's builds. Worktrees from older releases are removed by
  `council clean`.
- **Repo-local `.council.yaml` requires trust**: council asks before applying
  a new or changed repo config (it can change which commands run) and
  remembers the decision by content hash. New: `council trust [--revoke|--show]`
  and `--no-local-config`. Repo-root discovery now uses
  `git rev-parse --show-toplevel` and handles linked worktrees (`.git` file).
- **Run artifacts are private by default**: `0700` directories / `0600` files
  (`sessions.private: false` restores the old behavior). Optional secret
  redaction for saved transcripts via `sessions.redact: true`.
- **`/adopt` is a two-step preview + confirm**: preflighted with
  `git apply --check --3way`, shows touched files and dirty-tree overlap, then
  `/adopt confirm` applies. CLI parity: `council adopt [run] [agent]
  [--dry-run] [--yes]`.
- **The generated config is safe**: every agent preset ships disabled and
  without auto-approval flags (they moved to commented examples and the
  wizard). `policy.mode: safe|normal|aggressive` sets the risk posture;
  `safe` refuses risky flags outright.
- `@file` expansion is constrained to the working directory (plus a size cap
  and binary detection); `files.allow_absolute` opts out.
- `review.check_command` runs with a timeout (`check_timeout_seconds`, default
  600s) and an output cap (`max_check_output_bytes`, default 1 MiB).
- `/clean`, `council clean`, and the new `council clean-runs` preview what
  they will delete and ask for confirmation.
- Runs save the **effective merged config** (`config.effective.yaml`) and its
  provenance (`config.sources.json`) instead of the raw global file; run IDs
  are collision-proof; agent names are validated (charset + safe-name
  collisions).

### Added (orchestration & CLI)

- `council review` and `council adopt` CLI parity with the in-chat commands.
- Run reports (`report.md`, `/report`, `council report [--post N]`), phase
  timings, and `adopted.json`.
- `/artifacts` in-app browser for plans, votes, diffs, check logs, reviews,
  and transcripts; `/compare` and `/preview` for candidate builds.
- `/judge plan|build` human overrides and the `/refine` consensus round
  (winning planner absorbs reviewer critiques before `/build`).
- `/restart`, `/resend`, and `/nudge` recovery commands.
- `council config wizard`, `council config add-agent <preset>`, and
  `council stack detect|set` for review gates.
- `council scorecard` (agent performance across runs), `council queue`
  (batch issues), and `council pr` (open a PR from the winning branch).
- A much deeper `council doctor`: config validity, trust state, role
  coverage, writable directories, stale worktrees, risky flags, terminal
  settings, and the check command.
- CI: race detector on Linux, plus a required Windows cross-build smoke job;
  releases get build provenance attestations and documented verification
  steps.

## v0.2.0 - 2026-06-07

Native Windows support.

### Added

- Native Windows pseudo-terminals via the ConPTY API
  (`internal/agent/pty_windows.go`). Agents now launch in real pseudo consoles
  on Windows instead of failing to start.
- npm-style `.cmd`/`.bat` agent shims (e.g. `claude`, `codex`) are launched
  through the command interpreter automatically, since `CreateProcess` cannot
  execute batch files directly.
- `internal/agent` tests covering Windows command-line construction and a real
  ConPTY spawn (output capture and exit code).

### Changed

- The agent session is now backed by a platform-neutral `ptyConn` interface:
  `creack/pty` on Unix, ConPTY on Windows. The reader and process waiter run
  concurrently so a session ends cleanly on both a Unix PTY (which reaches EOF)
  and a ConPTY pipe (which does not).
- Upgraded Bubble Tea to v1.3.7, restoring function-key input on the Windows
  console.
- Documentation now lists Windows as supported rather than experimental.

### Fixed

- The `F2` direct-input toggle now works on Windows (Bubble Tea v1.3.4 did not
  map function keys from the Windows console). `Ctrl+O` remains an alias.
- Orchestration worktree handling on Windows: git emits forward-slash paths
  while `filepath` uses backslashes, so worktree detection silently matched
  nothing. Git paths are now normalized to the host's native form, fixing
  worktree reuse, review, and adopt.

### Notes

- ConPTY requires Windows 10 1809 (build 17763) or newer. Windows Terminal is
  recommended; WSL remains a fine alternative.

## v0.1.0 - 2026-06-06

First public release candidate.

### Added

- Multi-pane PTY terminal UI for AI coding-agent CLIs.
- Broadcast, targeted messages, direct input, zoom, paging, overview, settings,
  transcript saving, and file-reference expansion.
- Plan → vote → build → review → adopt orchestration.
- Worker/reviewer roles, behavioral personalities, and target scopes.
- Per-repo config layering via `.council.yaml`.
- Resume support for interrupted plan, vote, build, and review phases.
- Release CI, tagged binary artifacts, and GitHub Pages documentation.

### Notes

- Native macOS and Linux are the primary targets for the first release.
- Windows binaries are built, but PTY behavior should be validated in Windows
  Terminal before declaring first-class support.
