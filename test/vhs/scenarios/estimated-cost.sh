#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/lib.sh"
require_mode "${1:-}"

case "$1" in
  setup)
    setup_case
    make_fake_agent fake-agent
    write_global_config <<EOF
usage:
  enabled: true
  estimator: bytes4
ui:
  initial_prompt_delay_ms: 100
sessions:
  root_dir: "$WORK/.council/runs"
agents:
  fake:
    enabled: true
    command: ["$BIN_DIR/fake-agent"]
    cwd: "$WORK"
    usage:
      model: gpt-5
EOF
    ready
    ;;
  assert)
    events="$(latest_events)"
    assert_file "$events"
    assert_grep '"model":"gpt-5"' "$events"
    assert_grep '"model_source":"config"' "$events"
    assert_grep '"price_model":"gpt-5"' "$events"
    assert_grep '"price_source":"litellm-bundled"' "$events"
    "$COUNCIL_BIN" cost "$(basename "$(latest_run)")" >"$CASE_DIR/cost.out"
    assert_grep 'Usage' "$CASE_DIR/cost.out"
    assert_grep 'gpt-5' "$CASE_DIR/cost.out"
    assert_grep 'litellm-bundled' "$CASE_DIR/cost.out"
    pass
    ;;
esac
