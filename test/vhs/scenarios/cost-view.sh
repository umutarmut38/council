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
  stale_price_after_days: 1
  prices:
    stale:
      input_per_million: 2
      output_per_million: 4
      currency: USD
      source: user
      reviewed_at: "2020-01-01"
ui:
  initial_prompt_delay_ms: 100
sessions:
  root_dir: "$WORK/.council/runs"
agents:
  priced:
    enabled: true
    command: ["$BIN_DIR/fake-agent"]
    cwd: "$WORK"
    usage:
      model: local-selector
      price_profile: stale
  unknown:
    enabled: true
    command: ["$BIN_DIR/fake-agent"]
    cwd: "$WORK"
EOF
    ready
    ;;
  assert)
    events="$(latest_events)"
    assert_file "$events"
    assert_grep '"agent":"priced"' "$events"
    assert_grep '"price_profile":"stale"' "$events"
    assert_grep '"agent":"unknown"' "$events"
    "$COUNCIL_BIN" cost "$(basename "$(latest_run)")" >"$CASE_DIR/cost.out"
    assert_grep 'Source' "$CASE_DIR/cost.out"
    assert_grep 'Confidence' "$CASE_DIR/cost.out"
    assert_grep 'stale price' "$CASE_DIR/cost.out"
    assert_grep 'Hints:' "$CASE_DIR/cost.out"
    assert_grep 'usage.model is not configured' "$CASE_DIR/cost.out"
    assert_grep 'price unknown' "$CASE_DIR/cost.out"
    pass
    ;;
esac
