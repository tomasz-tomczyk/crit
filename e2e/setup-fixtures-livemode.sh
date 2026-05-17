#!/usr/bin/env bash
set -euo pipefail

PORT="${1:-3129}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CRIT_SRC="$(cd "$SCRIPT_DIR/.." && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

E2E_TMP=$(e2e_native_tempdir)
BIN_DIR=$(e2e_native_tempdir)
FAKE_HOME=$(e2e_native_tempdir)

if [ "$E2E_IS_WINDOWS" -eq 1 ]; then
  UPSTREAM_BIN="$E2E_TMP/upstream.exe"
else
  UPSTREAM_BIN="$E2E_TMP/upstream"
fi
UPSTREAM_LOG="$E2E_TMP/upstream.log"
UPSTREAM_PID=""
CRIT_PID=""

cleanup() {
  if [ -n "$CRIT_PID" ]; then kill "$CRIT_PID" 2>/dev/null || true; fi
  if [ -n "$UPSTREAM_PID" ]; then kill "$UPSTREAM_PID" 2>/dev/null || true; fi
  rm -rf "$E2E_TMP" "$BIN_DIR" "$FAKE_HOME"
}
trap cleanup EXIT INT TERM

# 1. Build upstream fixture binary.
(cd "$CRIT_SRC/e2e/fixtures/livemode-upstream" && go build -o "$UPSTREAM_BIN" .)

# 2. Build (or reuse) crit.
if [ -n "${CRIT_BIN:-}" ] && [ -f "$CRIT_BIN" ]; then
  echo "Using pre-built binary: $CRIT_BIN"
else
  CRIT_BIN="$BIN_DIR/$(e2e_bin_name)"
  (cd "$CRIT_SRC" && go build -o "$CRIT_BIN" .)
fi

# 3. Launch upstream on a free port; capture origin from its stdout.
"$UPSTREAM_BIN" --port 0 >"$UPSTREAM_LOG" 2>&1 &
UPSTREAM_PID=$!

UPSTREAM_ORIGIN=""
for _ in $(seq 1 50); do
  if grep -q '^listening on ' "$UPSTREAM_LOG"; then
    UPSTREAM_ORIGIN=$(awk '/^listening on / {print $3; exit}' "$UPSTREAM_LOG")
    break
  fi
  sleep 0.1
done
if [ -z "$UPSTREAM_ORIGIN" ]; then
  echo "upstream fixture failed to start" >&2
  cat "$UPSTREAM_LOG" >&2
  exit 1
fi
echo "upstream listening at $UPSTREAM_ORIGIN" >&2

# 4. Isolate HOME so config files don't leak.
export HOME="$FAKE_HOME"

# 5. Launch crit _serve in live mode on a fixed port.
#    (Bypassing `crit live` daemon-spawn keeps the process foreground and
#    on a deterministic port — Playwright wants a single child to wait on.)
"$CRIT_BIN" _serve --no-open --port "$PORT" --live-origin "$UPSTREAM_ORIGIN" &
CRIT_PID=$!

# 6. Block until /api/session answers.
for _ in $(seq 1 100); do
  if curl -sf "http://127.0.0.1:$PORT/api/session" >/dev/null 2>&1; then
    break
  fi
  sleep 0.2
done

# Foreground wait — Playwright treats this as the long-running webServer.
wait "$CRIT_PID"
