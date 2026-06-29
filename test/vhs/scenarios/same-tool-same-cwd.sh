#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/lib.sh"
require_mode "${1:-}"

run="20260628-same-tool"

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
    usage: { tool: claude }
  claude-b:
    enabled: false
    command: ["fake"]
    cwd: "$WORK"
    usage: { tool: claude }
EOF
    cat >"$WORK/.council/runs/$run/usage/events.jsonl" <<JSONL
{"schema_version":1,"at":"2026-06-28T10:00:00Z","run_id":"$run","agent":"claude-a","phase":"plan","source":"council.prompt","confidence":"estimated","tool":"claude","cwd":"$WORK","model":"unknown","prompt_hash":"a","prompt_preview":"You are Agent A. Build parser carefully.","fingerprint":"You are Agent A.","input_tokens":5,"reconcile_key":"a"}
{"schema_version":1,"at":"2026-06-28T10:00:01Z","run_id":"$run","agent":"claude-b","phase":"plan","source":"council.prompt","confidence":"estimated","tool":"claude","cwd":"$WORK","model":"unknown","prompt_hash":"b","prompt_preview":"You are Agent B. Build user interface carefully.","fingerprint":"You are Agent B.","input_tokens":5,"reconcile_key":"b"}
JSONL
    slug="$(claude_slug "$WORK")"
    mkdir -p "$HOME/.claude/projects/$slug"
    cat >"$HOME/.claude/projects/$slug/a.jsonl" <<JSONL
{"type":"user","cwd":"$WORK","sessionId":"sa","timestamp":"2026-06-28T10:00:02Z","message":{"content":"You are Agent A. Build parser carefully."}}
{"type":"assistant","cwd":"$WORK","sessionId":"sa","timestamp":"2026-06-28T10:00:03Z","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":100,"output_tokens":10}}}
JSONL
    cat >"$HOME/.claude/projects/$slug/b.jsonl" <<JSONL
{"type":"user","cwd":"$WORK","sessionId":"sb","timestamp":"2026-06-28T10:00:02Z","message":{"content":"You are Agent B. Build user interface carefully."}}
{"type":"assistant","cwd":"$WORK","sessionId":"sb","timestamp":"2026-06-28T10:00:03Z","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":200,"output_tokens":20}}}
JSONL
    ready
    ;;
  assert)
    events="$WORK/.council/runs/$run/usage/events.jsonl"
    assert_grep '"agent":"claude-a".*"source":"provider.session"' "$events"
    assert_grep '"agent":"claude-b".*"source":"provider.session"' "$events"
    grep '"source":"provider.session"' "$events" | grep -Eo '"(agent|source|model|replaces)":"[^"]*"'
    pass
    ;;
esac
