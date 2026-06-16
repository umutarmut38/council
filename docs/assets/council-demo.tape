# council - retry/backoff orchestration demo
#
# Regenerate with:  vhs docs/assets/council-demo.tape
# Produces:         docs/assets/council-demo.gif
#
# Prerequisites:
#   - vhs installed                (https://github.com/charmbracelet/vhs)
#   - council on your PATH         (go build -o bin/council ./cmd/council)
#   - At least two agents enabled in ~/.council.yaml, with both worker and
#     reviewer roles present (see examples/configs/worker-reviewer.yaml).
#   - Run from the council repo root so @examples/... resolves and orchestration
#     has a git repository to work in.
#
# This drives the documented plan -> vote -> build -> review -> adopt flow using
# the bundled example issue at examples/issues/retry-backoff.md. The Sleep
# durations leave room for live agents to respond; tune them to your setup.

Output docs/assets/council-demo.gif

# Fail fast if council is not installed rather than recording an error.
Require council

Set Shell "bash"
Set FontSize 16
Set Width 1280
Set Height 720
Set Padding 24
Set Theme "Dracula"
Set TypingSpeed 60ms
Set PlaybackSpeed 1.0

# --- Launch the council TUI -------------------------------------------------
Type "council"
Enter
Sleep 4s

# --- Plan: workers draft proposals from the example issue -------------------
Type "/plan @examples/issues/retry-backoff.md"
Sleep 1s
Enter
Sleep 12s

# --- Vote: reviewers rank the anonymized plans (self-votes excluded) --------
Type "/vote"
Sleep 1s
Enter
Sleep 10s

# --- Build: relaunch the winning plan into isolated git worktrees -----------
Type "/build"
Sleep 1s
Enter
Sleep 5s
Type "/start-build"
Sleep 1s
Enter
Sleep 14s

# --- Review: check and rank the build diffs ---------------------------------
Type "/review"
Sleep 1s
Enter
Sleep 10s

# --- Adopt: apply the winning diff to the working tree ----------------------
Type "/adopt"
Sleep 1s
Enter
Sleep 6s

# --- Quit -------------------------------------------------------------------
Ctrl+X
Sleep 2s
