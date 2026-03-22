#!/usr/bin/env bash
# test-daemon-reuse.sh — Verify daemon reuse across review rounds and mode transitions.
#
# Usage: ./test/test-daemon-reuse.sh [port]
#
# Reproduces the workflow from GitHub issues #184 and #185:
#   1. Start crit on a specific file (plan.md) with a fixed port
#   2. Add comments, finish the review
#   3. Verify the prompt tells the agent to run "crit <file>" (not bare "crit")
#   4. Edit the file, re-run "crit <file>" on the same port
#   5. Confirm the daemon is reused (round 2, not a new server)
#   6. Approve (finish with no unresolved comments)
#   7. Confirm the daemon shuts down and the port is freed
#   8. Start bare "crit" in git mode on the same port
#   9. Confirm git mode loads successfully
#
# This catches:
#   - #185: prompt saying "crit" instead of "crit <file>"
#   - #184: daemon not shutting down on approve, blocking the port

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

PORT="${1:-3198}"
BINARY="$ROOT/crit"

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

# Set up a temp git repo so we have both file-mode and git-mode available
WORKDIR=$(mktemp -d)
trap 'kill $(jobs -p) 2>/dev/null; rm -rf "$WORKDIR"' EXIT INT TERM

git -C "$WORKDIR" init -q
git -C "$WORKDIR" config user.email "test@test.com"
git -C "$WORKDIR" config user.name "Test"

cat > "$WORKDIR/plan.md" << 'EOF'
# Notification System Plan

## Overview

Build a notification pipeline that supports email, SMS, and push notifications.

## Requirements

1. Multi-channel delivery (email, SMS, push)
2. Template system for message formatting
3. Rate limiting per user
4. Delivery status tracking
5. Retry with exponential backoff

## Architecture

The system uses a message queue (SQS) to decouple producers from consumers.
Each channel has its own consumer worker pool.

## Database Schema

- `notifications` table: id, user_id, channel, template_id, status, metadata
- `templates` table: id, name, subject, body, channel
- `delivery_log` table: id, notification_id, attempt, status, error
EOF

git -C "$WORKDIR" add -A
git -C "$WORKDIR" commit -q -m "initial plan"

# Create a feature branch so git mode has something to diff later
git -C "$WORKDIR" checkout -q -b feature/notifications

PLAN_FILE="plan.md"

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
echo "=== Phase 1: File-mode review with crit $PLAN_FILE ==="
echo ""

# Start crit with plan.md
(cd "$WORKDIR" && "$BINARY" _serve --no-open --port "$PORT" "$PLAN_FILE") &
DAEMON_PID=$!
wait_for_server "$PORT"
echo "  Daemon started (PID $DAEMON_PID, port $PORT)"

# Get the file path as the server sees it
FILE_PATH=$(api "http://127.0.0.1:$PORT/api/session" | python3 -c "
import json, sys
s = json.load(sys.stdin)
print(s['files'][0]['path'])
")
ENCODED_PATH=$(python3 -c "import urllib.parse; print(urllib.parse.quote('$FILE_PATH'))")

# Check session mode
SESSION_MODE=$(api "http://127.0.0.1:$PORT/api/session" | python3 -c "import json,sys; print(json.load(sys.stdin)['mode'])")
check "Session is file mode" "$([ "$SESSION_MODE" = "files" ] && echo true || echo false)"

# Check round 1
ROUND=$(api "http://127.0.0.1:$PORT/api/session" | python3 -c "import json,sys; print(json.load(sys.stdin)['review_round'])")
check "Round is 1" "$([ "$ROUND" = "1" ] && echo true || echo false)"

# Add 2 comments
api -X POST "http://127.0.0.1:$PORT/api/file/comments?path=$ENCODED_PATH" \
  -H 'Content-Type: application/json' \
  -d '{"start_line": 7, "end_line": 7, "body": "Should we add webhook support as a channel too?"}' > /dev/null

api -X POST "http://127.0.0.1:$PORT/api/file/comments?path=$ENCODED_PATH" \
  -H 'Content-Type: application/json' \
  -d '{"start_line": 19, "end_line": 19, "body": "metadata column needs a size constraint — suggest 64KB CHECK."}' > /dev/null

# Finish review (has unresolved comments)
FINISH_RESP=$(api -X POST "http://127.0.0.1:$PORT/api/finish")
PROMPT=$(echo "$FINISH_RESP" | python3 -c "import json,sys; print(json.load(sys.stdin)['prompt'])")

# Check that prompt includes file args (issue #185)
check "Prompt says 'crit $PLAN_FILE' (not bare 'crit')" \
  "$(echo "$PROMPT" | grep -q "crit $PLAN_FILE" && echo true || echo false)"

echo ""
echo "=== Phase 2: Agent edits file, re-runs crit $PLAN_FILE (same port) ==="
echo ""

# Simulate agent editing the file
cat > "$WORKDIR/plan.md" << 'EOF'
# Notification System Plan

## Overview

Build a notification pipeline that supports email, SMS, push, and webhook notifications.

## Requirements

1. Multi-channel delivery (email, SMS, push, webhooks)
2. Template system for message formatting
3. Rate limiting per user
4. Delivery status tracking
5. Retry with exponential backoff

## Architecture

The system uses a message queue (SQS) to decouple producers from consumers.
Each channel has its own consumer worker pool.

## Database Schema

- `notifications` table: id, user_id, channel, template_id, status, metadata JSONB (max 64KB, CHECK constraint)
- `templates` table: id, name, subject, body, channel
- `delivery_log` table: id, notification_id, attempt, status, error
EOF

sleep 1.5  # let file watcher detect the change

# Extract the reinvoke command from the prompt (the "When done run: `...`" part).
# On main this will be bare "crit"; with the fix it will be "crit plan.md".
REINVOKE_CMD=$(echo "$PROMPT" | python3 -c "
import sys, re
m = re.search(r'When done run: \x60(.+?)\x60', sys.stdin.read())
print(m.group(1) if m else 'crit')
")
echo "  Prompt says to run: $REINVOKE_CMD"

# Run exactly what the prompt told us, with the same fixed port.
# With the bug (main): bare "crit" computes a different session key → starts a NEW daemon → port conflict.
# With the fix: "crit plan.md" finds the existing daemon → blocks on review-cycle (correct).
REINVOKE_ARGS="${REINVOKE_CMD#crit}"
REINVOKE_LOG="$WORKDIR/reinvoke.log"
(cd "$WORKDIR" && $BINARY --no-open --port "$PORT" $REINVOKE_ARGS > "$REINVOKE_LOG" 2>&1) &
REINVOKE_PID=$!

# Give it a moment to either crash (port conflict) or connect (success)
sleep 3

# Check if it crashed with port conflict
if ! kill -0 $REINVOKE_PID 2>/dev/null; then
  # Process exited — check if it was a port conflict
  REINVOKE_OUTPUT=$(cat "$REINVOKE_LOG" 2>/dev/null || true)
  if echo "$REINVOKE_OUTPUT" | grep -q "address already in use"; then
    check "Reinvoke did not hit port conflict" "false"
  else
    check "Reinvoke did not hit port conflict" "true"
  fi
else
  # Process still running = connected to daemon and blocking on review-cycle (correct behavior)
  check "Reinvoke did not hit port conflict" "true"
  kill $REINVOKE_PID 2>/dev/null || true
  wait $REINVOKE_PID 2>/dev/null || true
fi

# Check daemon is still on the same port (reused, not a new one)
check "Same daemon still running on port $PORT" \
  "$(curl -sf "http://127.0.0.1:$PORT/api/health" > /dev/null 2>&1 && echo true || echo false)"

# Check PID is still the same
check "Same daemon PID ($DAEMON_PID)" \
  "$(kill -0 $DAEMON_PID 2>/dev/null && echo true || echo false)"

echo ""
echo "=== Phase 3: Approve (no unresolved comments) — daemon should shut down ==="
echo ""

# Delete all comments to simulate "all resolved"
api -X DELETE "http://127.0.0.1:$PORT/api/comments" > /dev/null

# Finish with no unresolved comments = Approve
api -X POST "http://127.0.0.1:$PORT/api/finish" > /dev/null

# Wait for daemon to shut down (issue #184 fix)
wait_for_port_free "$PORT"

check "Daemon shut down after approve" \
  "$(! curl -sf "http://127.0.0.1:$PORT/api/health" > /dev/null 2>&1 && echo true || echo false)"

check "Daemon process exited" \
  "$(! kill -0 $DAEMON_PID 2>/dev/null && echo true || echo false)"

echo ""
echo "=== Phase 4: Start bare 'crit' in git mode on same port ==="
echo ""

# Make a change on the feature branch so git mode has something to show
cat >> "$WORKDIR/plan.md" << 'EOF'

## Monitoring

- Datadog dashboards for queue depth, delivery latency, and failure rates
- PagerDuty alerts for sustained queue backlog (> 1000 messages for 5 min)
EOF

git -C "$WORKDIR" add -A
git -C "$WORKDIR" commit -q -m "add monitoring section"

# Start bare crit (git mode) on the same port — should work since daemon shut down
(cd "$WORKDIR" && "$BINARY" _serve --no-open --port "$PORT") &
GIT_DAEMON_PID=$!
wait_for_server "$PORT"
echo "  Git-mode daemon started (PID $GIT_DAEMON_PID, port $PORT)"

# Check it's git mode
GIT_MODE=$(api "http://127.0.0.1:$PORT/api/session" | python3 -c "import json,sys; print(json.load(sys.stdin)['mode'])")
check "Session is git mode" "$([ "$GIT_MODE" = "git" ] && echo true || echo false)"

# Check it has files from the git diff
FILE_COUNT=$(api "http://127.0.0.1:$PORT/api/session" | python3 -c "import json,sys; print(len(json.load(sys.stdin)['files']))")
check "Git mode found changed files" "$([ "$FILE_COUNT" -gt 0 ] && echo true || echo false)"

# Clean up git daemon
kill $GIT_DAEMON_PID 2>/dev/null || true
wait $GIT_DAEMON_PID 2>/dev/null || true

echo ""
echo "==============================="
echo "Results: $PASS passed, $FAIL failed"
echo "==============================="
echo ""

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
