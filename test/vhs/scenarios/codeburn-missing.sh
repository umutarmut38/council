#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/lib.sh"
require_mode "${1:-}"

case "$1" in
  setup)
    setup_case
    write_global_config <<EOF
usage:
  enabled: true
sessions:
  root_dir: "$WORK/.council/runs"
agents: {}
EOF
    ready
    ;;
  assert)
    PATH="$BIN_DIR:/usr/bin:/bin" "$COUNCIL_BIN" cost --source codeburn >"$CASE_DIR/codeburn.out"
    assert_grep 'codeburn is not installed' "$CASE_DIR/codeburn.out"
    assert_grep 'machine-wide totals' "$CASE_DIR/codeburn.out"
    pass
    ;;
esac
