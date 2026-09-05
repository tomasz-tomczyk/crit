#!/usr/bin/env bash
# Fixture for the share-transport Playwright project: a crit daemon in file mode
# pointed at stub crit-web backends (fixtures/sharetransport-crit-web) so the
# browser Share flow — share, pull comments, re-share, unpublish, and
# multi-target routing — can be driven for real without a Postgres-backed
# crit-web.
set -euo pipefail

PORT="${1:-3132}"
STUB_PORT="${2:-3133}"
STUB2_PORT="${3:-3135}"
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
STUB2_LOG="$BIN_DIR/stub-crit-web-2.log"
STUB_PID=""
STUB2_PID=""
CRIT_PID=""

cleanup() {
  if [ -n "$CRIT_PID" ]; then kill "$CRIT_PID" 2>/dev/null || true; fi
  if [ -n "$STUB_PID" ]; then kill "$STUB_PID" 2>/dev/null || true; fi
  if [ -n "$STUB2_PID" ]; then kill "$STUB2_PID" 2>/dev/null || true; fi
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

wait_for_stub() {
  local log="$1"
  local origin=""
  for _ in $(seq 1 50); do
    if grep -q '^listening on ' "$log"; then
      origin=$(awk '/^listening on / {print $3; exit}' "$log")
      break
    fi
    sleep 0.1
  done
  if [ -z "$origin" ]; then
    echo "stub crit-web failed to start" >&2
    cat "$log" >&2
    exit 1
  fi
  printf '%s' "$origin"
}

# 1. Build the stub crit-web and launch two instances on distinct origins.
(cd "$SCRIPT_DIR/fixtures/sharetransport-crit-web" && go build -o "$STUB_BIN" .)
"$STUB_BIN" --port "$STUB_PORT" >"$STUB_LOG" 2>&1 &
STUB_PID=$!
STUB_ORIGIN=$(wait_for_stub "$STUB_LOG")
echo "stub crit-web A listening at $STUB_ORIGIN" >&2
"$STUB_BIN" --port "$STUB2_PORT" >"$STUB2_LOG" 2>&1 &
STUB2_PID=$!
STUB2_ORIGIN=$(wait_for_stub "$STUB2_LOG")
echo "stub crit-web B listening at $STUB2_ORIGIN" >&2

# 2. Build (or reuse) crit.
if [ -n "${CRIT_BIN:-}" ] && [ -f "$CRIT_BIN" ]; then
  echo "Using pre-built binary: $CRIT_BIN"
else
  CRIT_BIN="$BIN_DIR/$(e2e_bin_name)"
  (cd "$CRIT_SRC" && go build -o "$CRIT_BIN" ./cmd/crit)
fi

# 3. Isolate from the user's ~/.crit.config.json and configure two live stubs
#    plus the public crit.md destination (consent-only; tests never share to it).
#    Stub A is the CLI default; the browser still shows the destination picker
#    whenever more than one target is configured.
e2e_export_fake_home "$FAKE_HOME"
cat > "$FAKE_HOME/.crit.config.json" <<EOF
{
  "share_targets": [
    {"name": "Stub A", "url": "$STUB_ORIGIN", "default": true},
    {"name": "Stub B", "url": "$STUB2_ORIGIN"},
    {"name": "crit.md", "url": "https://crit.md"}
  ]
}
EOF

STATE_DIR="$(e2e_state_dir)"
mkdir -p "$STATE_DIR"
cat > "$STATE_DIR/sharetransport.env" <<EOF
STUB_ORIGIN=$STUB_ORIGIN
STUB2_ORIGIN=$STUB2_ORIGIN
CRIT_HOME=$FAKE_HOME
EOF

# No --share-url: process override would collapse share_targets to one target.
"$CRIT_BIN" _serve --no-open --port "$PORT" plan.md main.go &
CRIT_PID=$!

for _ in $(seq 1 100); do
  if curl -sf "http://localhost:$PORT/api/session" >/dev/null 2>&1; then
    break
  fi
  sleep 0.2
done

# Foreground wait — Playwright treats this as the long-running webServer.
wait "$CRIT_PID"
