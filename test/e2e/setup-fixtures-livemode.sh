#!/usr/bin/env bash
set -euo pipefail

PORT="${1:-3129}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CRIT_SRC="$(cd "$SCRIPT_DIR/../.." && pwd)"
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
e2e_cleanup_install
e2e_cleanup_add "$E2E_TMP" "$BIN_DIR" "$FAKE_HOME"

# 1. Build upstream fixture binary.
(cd "$SCRIPT_DIR/fixtures/livemode-upstream" && go build -o "$UPSTREAM_BIN" .)

# 2. Build (or reuse) crit.
if [ -n "${CRIT_BIN:-}" ] && [ -f "$CRIT_BIN" ]; then
  echo "Using pre-built binary: $CRIT_BIN"
else
  CRIT_BIN="$BIN_DIR/$(e2e_bin_name)"
  (cd "$CRIT_SRC" && go build -o "$CRIT_BIN" ./cmd/crit)
fi

# 3. Launch upstream on a free port; capture origin from its stdout.
e2e_spawn "$UPSTREAM_BIN" --port 0 >"$UPSTREAM_LOG" 2>&1

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

# 4. Isolate HOME (and USERPROFILE on Windows) so config files don't leak.
e2e_export_fake_home "$FAKE_HOME"

# 5. Launch crit _serve in live mode on a fixed port.
#    (Bypassing `crit live` daemon-spawn keeps the process foreground and
#    on a deterministic port — Playwright wants a single child to wait on.)
#    Foreground wait: Playwright / run.sh poll /api/session themselves.
e2e_serve "$CRIT_BIN" _serve --no-open --port "$PORT" --live-origin "$UPSTREAM_ORIGIN"
