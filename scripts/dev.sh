#!/bin/bash
# Usage: scripts/dev.sh [visor args...]
#
# Builds visor and visor-weather from this checkout into one folder and runs
# visor from it. `go run ./cmd/visor` builds visor alone, and visor finds its
# bundled Weather Plugin beside its own binary, so Weather would show as an
# unknown type.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${XDG_CACHE_HOME:-$HOME/.cache}/visor-dev"

mkdir -p "$OUT"
go build -C "$ROOT" -o "$OUT/" ./cmd/visor ./cmd/visor-weather
exec "$OUT/visor" "$@"
