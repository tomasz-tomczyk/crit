#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CRIT_SRC="$(cd "$SCRIPT_DIR/.." && pwd)"
GIT_PORT="${CRIT_TEST_PORT:-3123}"
FILE_PORT="${CRIT_TEST_FILE_PORT:-3124}"

# Build crit once
BIN_DIR=$(mktemp -d)
trap 'rm -rf "$BIN_DIR"' EXIT
export CRIT_BIN="$BIN_DIR/crit"
(cd "$CRIT_SRC" && go build -o "$CRIT_BIN" .)

# Start both fixture servers in parallel
cd "$SCRIPT_DIR"
bash setup-fixtures.sh "$GIT_PORT" &
GIT_PID=$!
bash setup-fixtures-filemode.sh "$FILE_PORT" &
FILE_PID=$!

cleanup() {
  kill "$GIT_PID" "$FILE_PID" 2>/dev/null || true
  wait "$GIT_PID" "$FILE_PID" 2>/dev/null || true
  rm -rf "$BIN_DIR"
}
trap cleanup EXIT

# Wait for servers to be ready
for port in "$GIT_PORT" "$FILE_PORT"; do
  while ! curl -sf "http://localhost:$port/api/session" >/dev/null 2>&1; do
    sleep 0.1
  done
done

# Run tests
npx playwright test "$@"
