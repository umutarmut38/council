#!/usr/bin/env bash
# freestyle-worktrees: the opt-in per-pane worktree feature (worktrees.freestyle).
# Two same-tool (claude) freestyle panes each launch in their OWN persistent,
# repo-local worktree (.council/workspaces/<agent>) — distinct cwds — so their
# provider-session cost reconciles PER PANE (two separate reported rows) instead
# of one combined row. Each fake also dirties its worktree, so the stale marker
# and /refresh flow can be exercised in the tape.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
require_mode "${1:-}"

# mk_claude writes a fake claude that (a) records a provider-session usage line at
# slug($PWD) — its worktree cwd, since council chdirs each pane there — and (b)
# dirties README.md so the worktree reads as drifted for the stale/refresh demo.
mk_claude() {
  local name="$1" in_tok="$2" out_tok="$3"
  cat >"$BIN_DIR/$name" <<SH
#!/usr/bin/env bash
printf 'claude ready\n'
slug="\$(pwd | tr '/.' '--')"
dir="\$HOME/.claude/projects/\$slug"
mkdir -p "\$dir"
ts="\$(date -u +%Y-%m-%dT%H:%M:%SZ)"
while IFS= read -r line; do
  printf 'claude received: %s\n' "\$line"
  printf '{"type":"assistant","cwd":"%s","sessionId":"s","timestamp":"%s","message":{"model":"claude-haiku-4-5","usage":{"input_tokens":%s,"output_tokens":%s}}}\n' "\$(pwd)" "\$ts" "$in_tok" "$out_tok" >>"\$dir/s.jsonl"
  printf 'dirty %s\n' "\$line" >>README.md
done
SH
  chmod +x "$BIN_DIR/$name"
}

case "$1" in
  setup)
    setup_case
    # A git repo in $WORK so detached worktrees can be created.
    git -C "$WORK" init -b main >/dev/null 2>&1
    printf 'hello\n' >"$WORK/README.md"
    GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t git -C "$WORK" add -A >/dev/null 2>&1
    GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t git -C "$WORK" commit -m init >/dev/null 2>&1

    mk_claude claude-a-agent 100 1000
    mk_claude claude-b-agent 200 2000
    write_global_config <<EOF
usage:
  enabled: true
  estimator: bytes4
worktrees:
  freestyle: true
ui:
  initial_prompt_delay_ms: 100
sessions:
  root_dir: "$WORK/.council/runs"
agents:
  claude-a:
    enabled: true
    command: ["$BIN_DIR/claude-a-agent"]
    cwd: "$WORK"
    usage: { tool: claude, model: claude-haiku-4-5 }
  claude-b:
    enabled: true
    command: ["$BIN_DIR/claude-b-agent"]
    cwd: "$WORK"
    usage: { tool: claude, model: claude-haiku-4-5 }
EOF
    ready
    ;;
  assert)
    ws="$WORK/.council/workspaces"
    [[ -d "$ws/claude-a" && -d "$ws/claude-b" ]] || { echo "expected two distinct freestyle worktrees under $ws" >&2; ls -la "$ws" >&2 2>/dev/null || true; exit 1; }
    events="$(latest_events)"
    assert_file "$events"
    # Reconcile idempotently in case the live tick hadn't fired at capture time.
    "$COUNCIL_BIN" cost "$(basename "$(latest_run)")" >"$CASE_DIR/cost.out" 2>&1 || true
    # Distinct worktree cwds → PER-PANE reported rows (100 vs 200), not combined.
    assert_grep '"agent":"claude-a".*"source":"provider.session".*"input_tokens":100' "$events"
    assert_grep '"agent":"claude-b".*"source":"provider.session".*"input_tokens":200' "$events"
    assert_not_grep '"agent":"claude",.*"source":"provider.session"' "$events"
    assert_grep "\"cwd\":\"$ws/claude-a\"" "$events"
    assert_grep "\"cwd\":\"$ws/claude-b\"" "$events"
    pass
    ;;
esac
