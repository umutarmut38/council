#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/lib.sh"
require_mode "${1:-}"

run="20260628-cli-cost"

case "$1" in
  setup)
    setup_case
    mkdir -p "$WORK/.council/runs/$run/usage"
    write_global_config <<EOF
usage:
  enabled: true
  prices:
    local-free:
      input_per_million: 0
      output_per_million: 0
      currency: USD
      source: user
      reviewed_at: "2026-06-28"
sessions:
  root_dir: "$WORK/.council/runs"
agents:
  local:
    enabled: false
    command: ["fake"]
    usage:
      model: local-model
      price_profile: local-free
EOF
    cat >"$WORK/.council/runs/$run/usage/events.jsonl" <<'JSONL'
{"schema_version":1,"at":"2026-06-28T10:00:00Z","run_id":"20260628-cli-cost","agent":"local","phase":"session","source":"council.prompt","confidence":"estimated","tool":"unknown","model":"local-model","price_profile":"local-free","estimator":"bytes4","input_tokens":1000,"output_tokens":500,"reconcile_key":"local"}
JSONL
    ready
    ;;
  assert)
    "$COUNCIL_BIN" cost "$run" >"$CASE_DIR/cost.out"
    assert_grep 'Usage' "$CASE_DIR/cost.out"
    assert_grep 'local-free' "$CASE_DIR/cost.out"
    assert_grep '\$0.00' "$CASE_DIR/cost.out"
    pass
    ;;
esac
