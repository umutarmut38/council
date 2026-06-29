#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/lib.sh"
require_mode "${1:-}"

run="20260628-reconcile"

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
  claude-a:
    enabled: false
    command: ["fake"]
    cwd: "$WORK"
    usage:
      tool: claude
EOF
    cat >"$WORK/.council/runs/$run/usage/events.jsonl" <<JSONL
{"schema_version":1,"at":"2026-06-28T10:00:00Z","run_id":"$run","agent":"claude-a","phase":"plan","source":"council.prompt","confidence":"estimated","tool":"claude","tool_source":"config","tool_confidence":"exact","model":"unknown","model_source":"unknown","model_confidence":"unknown","estimator":"bytes4","cwd":"$WORK","prompt_hash":"p1","prompt_preview":"You are The Architect. Think in systems. Do it.","fingerprint":"You are The Architect. Think in systems.","input_tokens":5,"reconcile_key":"estimate-a"}
JSONL
    slug="$(claude_slug "$WORK")"
    mkdir -p "$HOME/.claude/projects/$slug"
    cat >"$HOME/.claude/projects/$slug/s1.jsonl" <<JSONL
{"type":"user","cwd":"$WORK","sessionId":"s1","timestamp":"2026-06-28T10:00:01Z","message":{"content":"You are The Architect. Think in systems. Do it."}}
{"type":"assistant","cwd":"$WORK","sessionId":"s1","timestamp":"2026-06-28T10:00:02Z","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":100,"output_tokens":40,"cache_read_input_tokens":7}}}
JSONL
    ready
    ;;
  assert)
    events="$WORK/.council/runs/$run/usage/events.jsonl"
    assert_grep '"source":"provider.session"' "$events"
    assert_grep '"model":"claude-sonnet-4-6"' "$events"
    count1="$(grep -c '"source":"provider.session"' "$events")"
    "$COUNCIL_BIN" cost "$run" >"$CASE_DIR/cost2.out"
    count2="$(grep -c '"source":"provider.session"' "$events")"
    [[ "$count1" = "1" && "$count2" = "1" ]] || { echo "provider events not idempotent: $count1 -> $count2" >&2; exit 1; }
    grep -m1 '"source":"provider.session"' "$events" | grep -Eo '"(agent|source|model|replaces)":"[^"]*"'
    printf 'provider session events: %s -> %s\n' "$count1" "$count2"
    pass
    ;;
esac
