#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCENARIO="${SCENARIO:-$(basename "$0" .sh)}"
CASE_DIR="$ROOT/.tmp/vhs/$SCENARIO"
HOME="$CASE_DIR/home"
XDG_CONFIG_HOME="$CASE_DIR/xdg-config"
XDG_DATA_HOME="$CASE_DIR/xdg-data"
WORK="$CASE_DIR/work"
BIN_DIR="$CASE_DIR/bin"
COUNCIL_BIN="${COUNCIL_BIN:-$ROOT/bin/council}"
ENV_FILE="$CASE_DIR/env"

export HOME XDG_CONFIG_HOME XDG_DATA_HOME
export PATH="$BIN_DIR:$PATH"

setup_case() {
  rm -rf "$CASE_DIR"
  mkdir -p "$HOME" "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$WORK" "$BIN_DIR"
  if [[ ! -x "$COUNCIL_BIN" ]]; then
    (cd "$ROOT" && go build -o bin/council ./cmd/council)
    COUNCIL_BIN="$ROOT/bin/council"
  fi
  write_env_file
}

write_env_file() {
  {
    printf 'export HOME=%q\n' "$HOME"
    printf 'export XDG_CONFIG_HOME=%q\n' "$XDG_CONFIG_HOME"
    printf 'export XDG_DATA_HOME=%q\n' "$XDG_DATA_HOME"
    printf 'export ROOT=%q\n' "$ROOT"
    printf 'export WORK=%q\n' "$WORK"
    printf 'export CASE_DIR=%q\n' "$CASE_DIR"
    printf 'export COUNCIL_BIN=%q\n' "$COUNCIL_BIN"
    printf 'export PATH=%q:"$PATH"\n' "$BIN_DIR"
  } >"$ENV_FILE"
}

ready() {
  printf 'READY %s\n' "$SCENARIO"
}

write_global_config() {
  cat >"$HOME/.council.yaml"
}

make_fake_agent() {
  local name="${1:-fake-agent}"
  cat >"$BIN_DIR/$name" <<'SH'
#!/usr/bin/env sh
printf 'fake-agent ready\n'
while IFS= read -r line; do
  printf 'fake-agent received: %s\n' "$line"
done
SH
  chmod +x "$BIN_DIR/$name"
}

latest_run() {
  find "$WORK/.council/runs" -mindepth 1 -maxdepth 1 -type d | sort | tail -n 1
}

latest_events() {
  local run
  run="$(latest_run)"
  printf '%s/usage/events.jsonl\n' "$run"
}

assert_file() {
  [[ -f "$1" ]] || { echo "missing file: $1" >&2; exit 1; }
}

assert_grep() {
  local pattern="$1" file="$2"
  grep -qE "$pattern" "$file" || {
    echo "missing pattern $pattern in $file" >&2
    sed -n '1,160p' "$file" >&2 || true
    exit 1
  }
}

assert_not_grep() {
  local pattern="$1" file="$2"
  if grep -qE "$pattern" "$file"; then
    echo "unexpected pattern $pattern in $file" >&2
    sed -n '1,160p' "$file" >&2 || true
    exit 1
  fi
}

pass() {
  printf 'PASS %s\n' "$SCENARIO"
}

claude_slug() {
  printf '%s' "$1" | tr '/.' '--'
}

require_mode() {
  case "${1:-}" in
    setup | assert) ;;
    *)
      echo "usage: $0 setup|assert" >&2
      exit 2
      ;;
  esac
}
