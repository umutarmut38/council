#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/lib.sh"
require_mode "${1:-}"

case "$1" in
  setup)
    setup_case
    make_fake_agent claude
    make_fake_agent codex
    write_global_config <<EOF
usage:
  enabled: true
  estimator: bytes4
ui:
  initial_prompt_delay_ms: 100
sessions:
  root_dir: "$WORK/.council/runs"
agents:
  claude:
    enabled: true
    command: ["$BIN_DIR/claude", "--model", "haiku"]
    cwd: "$WORK"
  codex:
    enabled: true
    command: ["$BIN_DIR/codex", "--model", "gpt-5.4-mini"]
    cwd: "$WORK"
EOF
    ready
    ;;
  assert)
    events="$(latest_events)"
    assert_file "$events"
    assert_grep '"tool":"unknown"' "$events"
    assert_grep '"model":"unknown"' "$events"
    assert_not_grep '"tool":"claude"' "$events"
    assert_not_grep '"tool":"codex"' "$events"
    assert_not_grep '"model":"haiku"' "$events"
    assert_not_grep '"model":"gpt-5.4-mini"' "$events"
    (cd "$WORK" && "$COUNCIL_BIN" cost "$(basename "$(latest_run)")") >"$CASE_DIR/cost.out"
    assert_grep 'usage.tool is not configured' "$CASE_DIR/cost.out"
    assert_grep 'usage.model is not configured' "$CASE_DIR/cost.out"
    pass
    ;;
esac
