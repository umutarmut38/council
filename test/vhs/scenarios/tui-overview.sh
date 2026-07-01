#!/usr/bin/env bash
# tui-overview: the "looks like council" shot — the live multi-pane grid with
# two priced agents, a colored personality header rail, the cost in the header
# ("Run $… est") and per-pane border badges, then the /cost breakdown.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
require_mode "${1:-}"

case "$1" in
  setup)
    setup_case
    make_fake_agent alpha-agent
    make_fake_agent beta-agent
    write_global_config <<EOF
usage:
  enabled: true
  estimator: bytes4
ui:
  layout: grid
  group_by: personality
  initial_prompt_delay_ms: 100
sessions:
  root_dir: "$WORK/.council/runs"
personalities:
  architect:
    label: The Architect
    color: "69"
    prompt_prefix: "You are The Architect. Favor clean interfaces."
  minimalist:
    label: The Minimalist
    color: "114"
    prompt_prefix: "You are The Minimalist. Less is more."
agents:
  alpha:
    enabled: true
    command: ["$BIN_DIR/alpha-agent"]
    cwd: "$WORK"
    personality: architect
    usage:
      model: gpt-5
  beta:
    enabled: true
    command: ["$BIN_DIR/beta-agent"]
    cwd: "$WORK"
    personality: minimalist
    usage:
      model: claude-sonnet-4-6
EOF
    ready
    ;;
  assert)
    events="$(latest_events)"
    assert_file "$events"
    assert_grep '"agent":"alpha"' "$events"
    assert_grep '"agent":"beta"' "$events"
    assert_grep '"model":"gpt-5"' "$events"
    assert_grep '"model":"claude-sonnet-4-6"' "$events"
    assert_grep '"price_source":"litellm-bundled"' "$events"
    pass
    ;;
esac
