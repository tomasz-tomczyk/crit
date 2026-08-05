#!/usr/bin/env bash
# Fixture for the share-transport Playwright project: a crit daemon in file mode
# pointed at a stub crit-web (fixtures/sharetransport-crit-web) so the browser
# Share flow — share, pull comments, re-share, unpublish — can be driven for
# real without a Postgres-backed crit-web.
set -euo pipefail

PORT="${1:-3132}"
STUB_PORT="${2:-3133}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CRIT_SRC="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

DIR=$(e2e_native_tempdir)
BIN_DIR=$(e2e_native_tempdir)
FAKE_HOME=$(e2e_native_tempdir)

if [ "$E2E_IS_WINDOWS" -eq 1 ]; then
  STUB_BIN="$BIN_DIR/stub-crit-web.exe"
else
  STUB_BIN="$BIN_DIR/stub-crit-web"
fi
STUB_LOG="$BIN_DIR/stub-crit-web.log"
STUB_PID=""
CRIT_PID=""

cleanup() {
  if [ -n "$CRIT_PID" ]; then kill "$CRIT_PID" 2>/dev/null || true; fi
  if [ -n "$STUB_PID" ]; then kill "$STUB_PID" 2>/dev/null || true; fi
  rm -rf "$DIR" "$BIN_DIR" "$FAKE_HOME"
}
trap cleanup EXIT INT TERM

cd "$DIR"

git init -q
git config user.email "test@test.com"
git config user.name "Test"

cat > plan.md << 'MDFILE'
# Share Transport Plan

## Overview

Exercise the browser share round trip against a stub crit-web.

## Steps

1. Share
2. Pull comments
3. Re-share
4. Unpublish
MDFILE

cat > main.go << 'GOFILE'
package main

import "fmt"

func main() {
	fmt.Println("share transport fixture")
}

func helper(n int) int {
	return n * 2
}
GOFILE

git add -A && git commit -q -m "initial commit"

# 1. Build the stub crit-web and launch it on a fixed port.
(cd "$SCRIPT_DIR/fixtures/sharetransport-crit-web" && go build -o "$STUB_BIN" .)
"$STUB_BIN" --port "$STUB_PORT" >"$STUB_LOG" 2>&1 &
STUB_PID=$!

STUB_ORIGIN=""
for _ in $(seq 1 50); do
  if grep -q '^listening on ' "$STUB_LOG"; then
    STUB_ORIGIN=$(awk '/^listening on / {print $3; exit}' "$STUB_LOG")
    break
  fi
  sleep 0.1
done
if [ -z "$STUB_ORIGIN" ]; then
  echo "stub crit-web failed to start" >&2
  cat "$STUB_LOG" >&2
  exit 1
fi
echo "stub crit-web listening at $STUB_ORIGIN" >&2

# 2. Build (or reuse) crit.
if [ -n "${CRIT_BIN:-}" ] && [ -f "$CRIT_BIN" ]; then
  echo "Using pre-built binary: $CRIT_BIN"
else
  CRIT_BIN="$BIN_DIR/$(e2e_bin_name)"
  (cd "$CRIT_SRC" && go build -o "$CRIT_BIN" ./cmd/crit)
fi

# 3. Isolate from the user's ~/.crit.config.json. A non-default --share-url also
#    means no share-consent prompt, so the Share button works unattended.
e2e_export_fake_home "$FAKE_HOME"

"$CRIT_BIN" _serve --no-open --port "$PORT" --share-url "$STUB_ORIGIN" plan.md main.go &
CRIT_PID=$!

for _ in $(seq 1 100); do
  if curl -sf "http://localhost:$PORT/api/session" >/dev/null 2>&1; then
    break
  fi
  sleep 0.2
done

# Foreground wait — Playwright treats this as the long-running webServer.
wait "$CRIT_PID"
