#!/usr/bin/env bash
# test-plan-daemon.sh — Verify plan-mode daemon reuse across review rounds.
#
# Usage: ./test/test-plan-daemon.sh [port]
#
# Tests the plan mode workflow:
#   1. Pipe content via stdin to "crit plan --name test-plan" on a fixed port
#   2. Verify daemon starts with mode "plan", file shows as "test-plan.md"
#   3. Verify versioned storage (~/.crit/plans/test-plan/v001.md)
#   4. Finish the review (has unresolved comments)
#   5. Pipe revised content — verify daemon is reused (same PID), round 2
#   6. Verify v002.md exists
#   7. Approve (finish with no unresolved comments) — daemon shuts down
#   8. Verify port is freed

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

PORT="${1:-3199}"
BINARY="$ROOT/crit"
SLUG="test-plan"
PLAN_DIR="$HOME/.crit/plans/$SLUG"

if [ ! -f "$BINARY" ]; then
  echo "Binary not found — building..."
  (cd "$ROOT" && go build -o crit .)
fi

# Kill any stale process on our test port
STALE_PID=$(lsof -ti ":$PORT" 2>/dev/null || true)
if [ -n "$STALE_PID" ]; then
  echo "Killing stale process on port $PORT (PID $STALE_PID)..."
  kill $STALE_PID 2>/dev/null || true
  sleep 0.5
fi

# Clean up any previous plan storage for this slug
rm -rf "$PLAN_DIR"

# Set up a temp working directory (plan mode doesn't need git, but we need a cwd)
WORKDIR=$(mktemp -d)
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$WORKDIR"; rm -rf "$PLAN_DIR"' EXIT INT TERM

wait_for_server() {
  local port=$1
  for i in $(seq 1 30); do
    if curl -sf "http://127.0.0.1:$port/api/health" > /dev/null 2>&1; then
      return 0
    fi
    sleep 0.3
  done
  echo "FAIL: server did not start on port $port"
  exit 1
}

wait_for_port_free() {
  local port=$1
  for i in $(seq 1 30); do
    if ! curl -sf "http://127.0.0.1:$port/api/health" > /dev/null 2>&1; then
      return 0
    fi
    sleep 0.3
  done
  echo "FAIL: port $port did not free up"
  exit 1
}

api() {
  curl -sf "$@"
}

PASS=0
FAIL=0

check() {
  local desc="$1"
  local result="$2"
  if [ "$result" = "true" ]; then
    echo "  PASS: $desc"
    PASS=$((PASS + 1))
  else
    echo "  FAIL: $desc"
    FAIL=$((FAIL + 1))
  fi
}

echo ""
echo "=== Phase 1: First plan invocation via stdin ==="
echo ""

# Pipe content to crit plan (runs in background — starts daemon, blocks on review-cycle)
printf "# Plan v1\n\nStep 1\n" | (cd "$WORKDIR" && "$BINARY" plan --name "$SLUG" --no-open --port "$PORT" > /dev/null 2>&1) &
CLIENT1_PID=$!
wait_for_server "$PORT"

# Find the daemon PID — the process LISTENING on the port (not clients connected to it)
DAEMON_PID=$(lsof -ti ":$PORT" -sTCP:LISTEN 2>/dev/null | head -1 || true)
echo "  Daemon running on port $PORT (PID $DAEMON_PID)"

# Check session mode is "plan"
SESSION_MODE=$(api "http://127.0.0.1:$PORT/api/session" | python3 -c "import json,sys; print(json.load(sys.stdin)['mode'])")
check "Session mode is 'plan'" "$([ "$SESSION_MODE" = "plan" ] && echo true || echo false)"

# Check the file list shows test-plan.md (display name, not storage path)
FILE_PATH=$(api "http://127.0.0.1:$PORT/api/session" | python3 -c "
import json, sys
s = json.load(sys.stdin)
print(s['files'][0]['path'])
")
check "File path is '$SLUG.md'" "$([ "$FILE_PATH" = "$SLUG.md" ] && echo true || echo false)"

# Check round 1
ROUND=$(api "http://127.0.0.1:$PORT/api/session" | python3 -c "import json,sys; print(json.load(sys.stdin)['review_round'])")
check "Round is 1" "$([ "$ROUND" = "1" ] && echo true || echo false)"

# Verify versioned storage
check "v001.md exists" "$([ -f "$PLAN_DIR/v001.md" ] && echo true || echo false)"
check "current.md exists" "$([ -f "$PLAN_DIR/current.md" ] && echo true || echo false)"

# Add a comment so finish returns unresolved feedback
ENCODED_PATH=$(python3 -c "import urllib.parse; print(urllib.parse.quote('$FILE_PATH'))")
api -X POST "http://127.0.0.1:$PORT/api/file/comments?path=$ENCODED_PATH" \
  -H 'Content-Type: application/json' \
  -d '{"start_line": 1, "end_line": 1, "body": "Add more detail to the heading"}' > /dev/null

# Finish review (has unresolved comments — returns prompt, daemon stays alive)
FINISH_RESP=$(api -X POST "http://127.0.0.1:$PORT/api/finish")
HAS_PROMPT=$(echo "$FINISH_RESP" | python3 -c "
import json, sys
d = json.load(sys.stdin)
print('true' if d.get('prompt', '') else 'false')
")
check "Finish returns prompt (unresolved comments)" "$HAS_PROMPT"

# The client should have exited after getting finish response
sleep 1
kill $CLIENT1_PID 2>/dev/null || true
wait $CLIENT1_PID 2>/dev/null || true

echo ""
echo "=== Phase 2: Second invocation (daemon reuse) with revised content ==="
echo ""

# Pipe revised content — daemon should be reused
printf "# Plan v2\n\nStep 1\nStep 2\n" | (cd "$WORKDIR" && "$BINARY" plan --name "$SLUG" --no-open --port "$PORT" > /dev/null 2>&1) &
CLIENT2_PID=$!

# Give it a moment to connect to the existing daemon
sleep 2

# Check daemon is still the same PID (reused, not restarted)
check "Same daemon still running on port $PORT" \
  "$(curl -sf "http://127.0.0.1:$PORT/api/health" > /dev/null 2>&1 && echo true || echo false)"

CURRENT_DAEMON_PID=$(lsof -ti ":$PORT" -sTCP:LISTEN 2>/dev/null | head -1 || true)
check "Same daemon PID ($DAEMON_PID)" \
  "$([ "$CURRENT_DAEMON_PID" = "$DAEMON_PID" ] && echo true || echo false)"

# Check round is 2
ROUND2=$(api "http://127.0.0.1:$PORT/api/session" | python3 -c "import json,sys; print(json.load(sys.stdin)['review_round'])")
check "Round is 2" "$([ "$ROUND2" = "2" ] && echo true || echo false)"

# Verify v002.md was created
check "v002.md exists" "$([ -f "$PLAN_DIR/v002.md" ] && echo true || echo false)"

echo ""
echo "=== Phase 3: Approve — daemon should shut down ==="
echo ""

# Delete all comments to simulate "all resolved"
api -X DELETE "http://127.0.0.1:$PORT/api/comments" > /dev/null

# Finish with no unresolved comments = Approve
api -X POST "http://127.0.0.1:$PORT/api/finish" > /dev/null

# Wait for daemon to shut down (client detects approve and sends SIGTERM)
wait_for_port_free "$PORT"

check "Daemon shut down after approve" \
  "$(! curl -sf "http://127.0.0.1:$PORT/api/health" > /dev/null 2>&1 && echo true || echo false)"

check "Daemon process exited" \
  "$(! kill -0 $DAEMON_PID 2>/dev/null && echo true || echo false)"

# Clean up client
kill $CLIENT2_PID 2>/dev/null || true
wait $CLIENT2_PID 2>/dev/null || true

echo ""
echo "==============================="
echo "Results: $PASS passed, $FAIL failed"
echo "==============================="
echo ""

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
