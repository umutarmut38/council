#!/usr/bin/env bash
set -euo pipefail

if ! command -v vhs >/dev/null 2>&1; then
  echo "vhs is required for integration/e2e tests: https://github.com/charmbracelet/vhs" >&2
  exit 127
fi

if [[ "$#" -gt 0 ]]; then
  tapes=("$@")
else
  tapes=(test/vhs/*.tape)
fi

mkdir -p .tmp/vhs
go build -o bin/council ./cmd/council
export COUNCIL_BIN="$PWD/bin/council"
export PATH="$PWD/bin:$PATH"

for tape in "${tapes[@]}"; do
  scenario="$(basename "$tape" .tape)"
  scenario_script="test/vhs/scenarios/$scenario.sh"
  if [[ -f "$scenario_script" && "$scenario" != "no-command-scanning" ]]; then
    bash "$scenario_script" setup
    # shellcheck disable=SC1090
    source ".tmp/vhs/$scenario/env"
  fi
  echo "==> vhs $tape"
  vhs "$tape"
  if [[ -f "$scenario_script" ]]; then
    bash "$scenario_script" assert
  fi
done
