#!/usr/bin/env bash
set -Eeuo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"
GO_BIN=${GO_BIN:-go}
"$GO_BIN" mod download
mkdir -p bin .run
"$GO_BIN" build -trimpath -o bin/prices .
# POSTGRES_* are read by the application; the existing DB is never cleared.
if [[ -n ${POSTGRES_PASSWORD:-} ]]; then
  ./bin/prices --prepare
elif [[ ${GITHUB_ACTIONS:-false} == true ]]; then
  # The unchanged template passes DB credentials only to the next run step.
  echo "Build ready; schema initialization will run with the application."
else
  echo "Set POSTGRES_PASSWORD to initialize the database." >&2
  exit 2
fi
