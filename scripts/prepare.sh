#!/usr/bin/env bash
set -Eeuo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"
GO_BIN=${GO_BIN:-go}
"$GO_BIN" mod download
mkdir -p bin .run
"$GO_BIN" build -trimpath -o bin/prices .
# POSTGRES_* are read by the application; the existing DB is never cleared.
./bin/prices --prepare
