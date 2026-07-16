#!/usr/bin/env bash
# tui-reconcile-badge: proves the live pane badge uses the cheap estimated floor
# until an explicit /cost scan upgrades it to the reported total. Uses a fake
# agent + a local fixture Claude session stamped now (so it lands inside the
# reconcile window). No real CLI, no network.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
require_mode "${1:-}"

case "$1" in
  setup)
    setup_case
    make_fake_agent claude-agent
    write_global_config <<EOF
usage:
  enabled: true
  estimator: bytes4
ui:
  initial_prompt_delay_ms: 100
sessions:
  root_dir: "$WORK/.council/runs"
personalities:
  architect:
    label: The Architect
    prompt_prefix: "You are The Architect."
agents:
  planner:
    enabled: true
    command: ["$BIN_DIR/claude-agent"]
    cwd: "$WORK"
    personality: architect
    usage:
      tool: claude
      model: claude-haiku-4-5
EOF
    ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    slug="$(claude_slug "$WORK")"
    mkdir -p "$HOME/.claude/projects/$slug"
    cat >"$HOME/.claude/projects/$slug/s1.jsonl" <<JSONL
{"type":"user","cwd":"$WORK","sessionId":"s1","timestamp":"$ts","message":{"content":"You are The Architect. Build it."}}
{"type":"assistant","cwd":"$WORK","sessionId":"s1","timestamp":"$ts","message":{"model":"claude-haiku-4-5","usage":{"input_tokens":200,"output_tokens":8000}}}
JSONL
    ready
    ;;
  assert)
    events="$(latest_events)"
    assert_file "$events"
    assert_grep '"source":"provider.session"' "$events"
    assert_grep '"model":"claude-haiku-4-5"' "$events"
    pass
    ;;
esac
