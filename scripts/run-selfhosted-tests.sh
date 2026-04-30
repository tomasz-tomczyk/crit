#!/usr/bin/env bash
# run-selfhosted-tests.sh — boot crit-web in selfhosted+OAuth mode on :4001,
# run the TestSelfhosted* integration tests, then tear it down.
#
# Sources OAuth credentials from a sibling crit-web worktree's .envrc.local.
# Looks first at the auth-gate worktree, then the canonical crit-web/ dir.

set -euo pipefail

PORT=4001
HEALTH_URL="http://localhost:${PORT}/health"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PARENT="$(cd "$REPO_ROOT/.." && pwd)"

# Pick crit-web worktree (auth-gate worktree first, fall back to canonical).
WEB_DIR=""
for candidate in \
  "$PARENT/crit-web.fix-review-live-auth-gate" \
  "$PARENT/crit-web"; do
  if [ -d "$candidate" ] && [ -f "$candidate/scripts/start-selfhosted.sh" ]; then
    WEB_DIR="$candidate"
    break
  fi
  if [ -d "$candidate" ] && [ -f "$candidate/.envrc.local" ]; then
    WEB_DIR="$candidate"
  fi
done

if [ -z "$WEB_DIR" ]; then
  echo "ERROR: could not locate a crit-web worktree with .envrc.local under $PARENT" >&2
  exit 1
fi

ENVRC="$WEB_DIR/.envrc.local"
if [ ! -f "$ENVRC" ]; then
  echo "ERROR: $ENVRC not found. Create it with GITHUB_CLIENT_ID/SECRET first." >&2
  exit 1
fi

# Source the envrc into our environment.
set -a
# shellcheck disable=SC1090
. "$ENVRC"
set +a

# Sanity-check the credentials we need.
if [ -z "${GITHUB_CLIENT_ID:-}" ] || [ -z "${GITHUB_CLIENT_SECRET:-}" ]; then
  echo "ERROR: GITHUB_CLIENT_ID / GITHUB_CLIENT_SECRET missing after sourcing $ENVRC" >&2
  exit 1
fi

# Refuse to clobber an existing :4001.
if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "ERROR: port $PORT already in use. Stop the existing process and retry." >&2
  exit 1
fi

START_SCRIPT="$WEB_DIR/scripts/start-selfhosted.sh"
if [ ! -x "$START_SCRIPT" ]; then
  echo "ERROR: $START_SCRIPT not found or not executable" >&2
  exit 1
fi

LOG_FILE="$(mktemp -t crit-selfhost-XXXXXX.log)"
echo "==> booting crit-web (selfhosted, :$PORT) — log: $LOG_FILE"
echo "    using worktree: $WEB_DIR"

# Background the server. We propagate the OAuth env to it.
"$START_SCRIPT" >"$LOG_FILE" 2>&1 &
SERVER_PID=$!

cleanup() {
  echo "==> stopping crit-web (pid $SERVER_PID)"
  if kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    # Children (mix, beam) need a moment.
    sleep 1
    pkill -P "$SERVER_PID" 2>/dev/null || true
    kill -9 "$SERVER_PID" 2>/dev/null || true
  fi
  # Stray beams from `mix phx.server` sometimes outlive their parent.
  pkill -f "beam.smp.*--name.*crit" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# Poll /health until 200 (or timeout).
echo "==> waiting for $HEALTH_URL (timeout 60s)"
deadline=$(( $(date +%s) + 60 ))
while :; do
  if [ "$(date +%s)" -ge "$deadline" ]; then
    echo "ERROR: server did not become healthy within 60s. Last log lines:" >&2
    tail -n 60 "$LOG_FILE" >&2 || true
    exit 1
  fi
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "ERROR: server process exited prematurely. Log:" >&2
    cat "$LOG_FILE" >&2 || true
    exit 1
  fi
  if curl -fsS -o /dev/null -w '%{http_code}' "$HEALTH_URL" 2>/dev/null | grep -q '^200$'; then
    break
  fi
  sleep 1
done
echo "==> server healthy"

# Run the selfhosted integration tests against the booted instance.
cd "$REPO_ROOT"
CRIT_WEB_URL="http://localhost:${PORT}" \
  go test -tags integration -run TestSelfhosted -v -count=1 ./...
