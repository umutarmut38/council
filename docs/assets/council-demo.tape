# council — plan → vote → build → review → adopt demo (live agents)
#
# Regenerate with:  vhs docs/assets/council-demo.tape   (from the council repo root)
# Produces:         docs/assets/council-demo.mp4
#
# What it shows:
#   A full council run driven against the self-contained playground in poc/.
#   The tape cd's into poc/ first, so council loads poc/.council.yaml and
#   @TASK.md resolves to poc/TASK.md — the "Local-First Dependency Kanban"
#   task. poc enables two agents, claude and codex (each worker + reviewer),
#   so the run drives /plan -> /vote -> /build -> /start-build -> /review ->
#   /adopt end to end.
#
# Self-pacing (why there are almost no fixed Sleeps):
#   This tape does NOT guess per-phase durations. It uses VHS `Wait+Screen` to
#   block until a stable marker that council actually prints on screen for each
#   phase — the status line text such as "collected N plan(s)", "winner: …",
#   "ready in worktrees", "best build: …", and "uncommitted". Each Wait carries
#   a generous @timeout that is only a SAFETY CEILING, not the expected time; a
#   slow live run can legitimately take minutes (build most of all). Tune the
#   ceilings up if a real run on your machine trips them.
#
#   One step has no marker to wait on: the build work itself. council emits no
#   "build complete" status — build completion is user-judged (the HUD only
#   hints "Next: /review when done") and the agent CLIs stay interactive rather
#   than exiting, so there is no stable screen anchor. That single step falls
#   back to a commented, APPROXIMATE Sleep you must tune to your hardware/models.
#
# Prerequisites:
#   - vhs installed                (https://github.com/charmbracelet/vhs)
#   - council on your PATH         (go build -o bin/council ./cmd/council)
#   - claude and codex CLIs installed and authenticated (poc's enabled agents)
#   - poc/.council.yaml trusted    (run `council trust` once inside poc/)
#   - Run from the council repo root; the tape cd's into poc/ itself.
#   Optional: poc's headroom proxy only starts if poc sets
#   experimental.setup_env: true (off by default, so agents run directly).

Output docs/assets/council-demo.mp4

# Fail fast if council is not installed rather than recording an error.
Require council

Set Shell "bash"
Set FontSize 16
Set Width 1920
Set Height 1080
Set Padding 24
Set Theme "Dracula"
Set TypingSpeed 60ms
Set PlaybackSpeed 1.0

# --- Enter the poc/ playground ---------------------------------------------
# Run from the council repo root; cd so council picks up poc/.council.yaml and
# @TASK.md resolves to poc/TASK.md. The repo root has NO .council.yaml or
# TASK.md, so skipping this cd makes council load no config and /plan @TASK.md
# reference a nonexistent file — the demo only works from inside poc/.
Type "cd poc"
Sleep 500ms
Enter
Sleep 1s

# --- Launch the council TUI -------------------------------------------------
Type "council"
Enter
# Block until the TUI has painted its header rail (always shows "Agents: …").
# Doubles as a fail-fast: if council errored on startup, the marker never shows
# and the recording stops at the ceiling instead of typing into a dead shell.
# Ceiling also covers — if poc enables experimental.setup_env — the headroom
# proxy's wait_for_port gate.
Wait+Screen@1m /Agents:/
# "Agents:" paints the instant the TUI draws, BEFORE the claude and codex panes
# have booted their CLIs and the composer has settled. Give both agent CLIs time
# to come up so the /plan command below lands in a ready composer. (This is
# distinct from poc's ui.initial_prompt_delay_ms: 8000, which council enforces
# itself after /plan relaunches the panes, before the first prompt reaches them.)
Sleep 10s

# --- Plan: workers draft proposals from poc/TASK.md ------------------------
# "@TASK.md" opens council's @file picker. The TRAILING SPACE after it dismisses
# that picker so the single Enter below SUBMITS the command. Without the space,
# the first Enter is swallowed to ACCEPT the file suggestion (see
# internal/tui/keys.go acceptFileSuggestion) and the command is never sent —
# that was the original recording failure (/plan left sitting in the composer).
# Keep the trailing space.
Type "/plan @TASK.md "
Sleep 1s
Enter
# poc sets ui.initial_prompt_delay_ms: 8000, so council waits 8s after relaunching
# the panes before the plan prompt reaches each agent; both then draft a full
# plan. Block on the plan-complete status, which reads "collected N plan(s) —
# type /vote". Ceiling only — real drafting is usually tens of seconds to a
# couple of minutes.
Wait+Screen@5m /collected .+ plan/

# --- Vote: reviewers rank the anonymized plans (self-votes excluded) -------
Type "/vote"
Sleep 1s
Enter
# Block until the tally announces the winner ("winner: <agent> (Plan X) — type
# /build"). The agent name sits between "winner: " and "(Plan", so the regex
# escapes the literal "(" — a bare " Plan" would never match the real text.
# Ceiling only — a vote is a short model round-trip per reviewer.
Wait+Screen@4m /winner: .+ \(Plan/

# --- Build: stage one git worktree per worker, then send the prompt --------
Type "/build"
Sleep 1s
Enter
# /build relaunches the panes inside fresh worktrees but does NOT prompt yet.
# Block until it reports the staged state ("build ready in worktrees …").
Wait+Screen@3m /ready in worktrees/

Type "/start-build"
Sleep 1s
Enter
# Confirm the staged prompt was dispatched ("build prompt sent — agents working").
Wait+Screen@1m /build prompt sent/

# FALLBACK Sleep — no on-screen marker exists for build completion. council does
# not emit a "build done" status (completion is user-judged; the HUD only hints
# "Next: /review when done"), and the agent CLIs stay interactive instead of
# exiting, so there is nothing stable to Wait on. This is the long pole of the
# whole recording. APPROXIMATE — tune to your machine/agents/models, as building
# a complete vanilla HTML/CSS/JS app can take several minutes.
Sleep 4m

# --- Review: gate builds with poc's check_command, then rank the diffs ------
Type "/review"
Sleep 1s
Enter
# /review runs poc's review.check_command in each worktree, drops failures, then
# reviewers vote the best surviving diff. Block until the winner is announced.
# Matches the normal multi-survivor tally ("best build: …") and the
# single-survivor shortcut ("only <agent> passed …"). Ceiling only.
Wait+Screen@4m /best build:|only .+ passed/

# --- Adopt: preview the winning diff, then apply it ------------------------
Type "/adopt"
Sleep 1s
Enter
# policy.mode is "normal", so /adopt opens a full-screen preview and waits for a
# keypress. Block until the preview's apply prompt is on screen.
Wait+Screen@2m /Apply this diff/
Sleep 1s
# Apply the diff to the working tree (y = apply, n = cancel).
Type "y"
# Block until council confirms the changes landed ("… (N files, uncommitted) …").
Wait+Screen@1m /uncommitted/

# --- Quit -------------------------------------------------------------------
Sleep 3s
Ctrl+X
Sleep 2s
