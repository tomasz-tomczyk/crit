#!/usr/bin/env bash
# End-to-end integration test runner for crit ↔ crit-web share flow.
# Builds crit, starts a local crit-web, runs share integration tests, tears down.
#
# Usage:
#   ./scripts/e2e-share.sh              # run all share integration tests
#   ./scripts/e2e-share.sh -run TestFoo # pass extra args to go test
#   ./scripts/e2e-share.sh --skip-web   # assume crit-web already running
#   ./scripts/e2e-share.sh --serve     # start crit-web and keep it running (no tests)
set -euo pipefail

CRIT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
WEB_DIR="${CRIT_WEB_DIR:-$(cd "$CRIT_DIR/../crit-web" && pwd)}"

# Activate mise so go/mix/etc are available
if command -v /opt/homebrew/bin/mise >/dev/null 2>&1; then
  eval "$(/opt/homebrew/bin/mise env -s bash -C "$CRIT_DIR" 2>/dev/null)" || true
  eval "$(/opt/homebrew/bin/mise env -s bash -C "$WEB_DIR" 2>/dev/null)" || true
fi
WEB_PORT="${CRIT_WEB_PORT:-4001}"
WEB_URL="http://localhost:$WEB_PORT"
WEB_PID=""
SKIP_WEB=false
SERVE_ONLY=false
GO_TEST_ARGS=()

# Parse our flags vs go test args
for arg in "$@"; do
  case "$arg" in
    --skip-web) SKIP_WEB=true ;;
    --serve) SERVE_ONLY=true ;;
    *) GO_TEST_ARGS+=("$arg") ;;
  esac
done

cleanup() {
  if [ -n "$WEB_PID" ]; then
    echo "→ Stopping crit-web (pid $WEB_PID)..."
    kill "$WEB_PID" 2>/dev/null || true
    wait "$WEB_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

# 1. Build crit
echo "→ Building crit..."
make -C "$CRIT_DIR" build -j

# 2. Start crit-web (unless --skip-web)
if [ "$SKIP_WEB" = false ]; then
  # Check if something is already on the port
  if lsof -ti tcp:"$WEB_PORT" >/dev/null 2>&1; then
    echo "✗ Port $WEB_PORT already in use. Use --skip-web if crit-web is already running."
    exit 1
  fi

  echo "→ Starting crit-web on :$WEB_PORT..."
  cd "$WEB_DIR"
  # Use a separate DB so we don't trash the dev database
  export DB_NAME="${DB_NAME:-crit_e2e}"
  # Reset DB to clean state
  MIX_ENV=dev mix ecto.reset --quiet 2>/dev/null || mix ecto.setup --quiet
  # Start Phoenix in background on the test port
  PORT=$WEB_PORT SELFHOSTED=true ADMIN_PASSWORD=test mix phx.server &
  WEB_PID=$!
  cd "$CRIT_DIR"

  # Wait for health
  echo "→ Waiting for crit-web..."
  for i in $(seq 1 30); do
    if curl -sf "$WEB_URL/health" >/dev/null 2>&1; then
      echo "→ crit-web ready"
      break
    fi
    if ! kill -0 "$WEB_PID" 2>/dev/null; then
      echo "✗ crit-web process died"
      exit 1
    fi
    if [ "$i" -eq 30 ]; then
      echo "✗ crit-web failed to start within 30s"
      exit 1
    fi
    sleep 1
  done
fi

# 3. Serve-only mode: keep running for manual testing
if [ "$SERVE_ONLY" = true ]; then
  echo "✓ crit-web running at $WEB_URL (Ctrl+C to stop)"
  echo "  crit binary: $CRIT_DIR/crit"
  echo "  Usage: CRIT_SHARE_URL=$WEB_URL CRIT_AUTH_TOKEN='' $CRIT_DIR/crit share -o /tmp/test plan.md"
  wait "$WEB_PID"
  exit 0
fi

# 3. Run integration tests
echo "→ Running share integration tests..."
cd "$CRIT_DIR"
CRIT_SHARE_URL="$WEB_URL" \
CRIT_WEB_URL="$WEB_URL" \
CRIT_BINARY="$CRIT_DIR/crit" \
CRIT_AUTH_TOKEN="" \
  go test -tags integration -run TestShareSync -v -count=1 "${GO_TEST_ARGS[@]}" ./...

echo "✓ All share integration tests passed"
