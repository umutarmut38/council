#!/usr/bin/env bash
# reconciliation-codex: a codex pane (usage.tool: codex) whose launch command
# embeds --model gpt-5.4-mini reconciles from a fixture codex rollout. The model
# is taken from the provider session (gpt-5.4-mini), NOT the command flag, and
# prices via the bundled LiteLLM table. Reconciliation is idempotent.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
require_mode "${1:-}"

run="20260628-reconcile-codex"

case "$1" in
  setup)
    setup_case
    mkdir -p "$WORK/.council/runs/$run/usage"
    write_global_config <<EOF
usage:
  enabled: true
sessions:
  root_dir: "$WORK/.council/runs"
agents:
  codex-a:
    enabled: false
    command: ["codex", "--model", "gpt-5.4-mini"]
    cwd: "$WORK"
    usage:
      tool: codex
EOF
    cat >"$WORK/.council/runs/$run/usage/events.jsonl" <<JSONL
{"schema_version":1,"at":"2026-06-28T10:00:00Z","run_id":"$run","agent":"codex-a","phase":"plan","source":"council.prompt","confidence":"estimated","tool":"codex","tool_source":"config","tool_confidence":"exact","model":"unknown","model_source":"unknown","model_confidence":"unknown","estimator":"bytes4","cwd":"$WORK","prompt_hash":"p1","prompt_preview":"You are The Minimalist. Less is more. Build it.","fingerprint":"You are The Minimalist. Less is more.","input_tokens":5,"reconcile_key":"estimate-codex"}
JSONL
    # The codex reader prunes date dirs older than ~a week, so the fixture must
    # live under today's date (file-content timestamps are independent of it).
    day="$(date -u +%Y/%m/%d)"
    mkdir -p "$HOME/.codex/sessions/$day"
    cat >"$HOME/.codex/sessions/$day/rollout-2026-06-28T10-00-00-cx1.jsonl" <<JSONL
{"type":"session_meta","timestamp":"2026-06-28T10:00:01Z","payload":{"cwd":"$WORK","session_id":"cx1"}}
{"type":"turn_context","payload":{"type":"turn_context","model":"gpt-5.4-mini"}}
{"type":"event_msg","payload":{"type":"user_message","message":"You are The Minimalist. Less is more. Build it."}}
{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":120,"output_tokens":30,"cached_input_tokens":5}}}}
JSONL
    ready
    ;;
  assert)
    events="$WORK/.council/runs/$run/usage/events.jsonl"
    assert_grep '"source":"provider.session"' "$events"
    assert_grep '"model":"gpt-5.4-mini"' "$events"
    assert_not_grep '"model":"gpt-5.5"' "$events"
    count1="$(grep -c '"source":"provider.session"' "$events")"
    "$COUNCIL_BIN" cost "$run" >"$CASE_DIR/cost2.out"
    count2="$(grep -c '"source":"provider.session"' "$events")"
    [[ "$count1" = "1" && "$count2" = "1" ]] || { echo "provider events not idempotent: $count1 -> $count2" >&2; exit 1; }
    assert_grep 'gpt-5.4-mini' "$CASE_DIR/cost2.out"
    assert_grep 'litellm-bundled' "$CASE_DIR/cost2.out"
    pass
    ;;
esac
