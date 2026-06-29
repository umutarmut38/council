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

for tape in "${tapes[@]}"; do
  echo "==> vhs $tape"
  vhs "$tape"
done
