#!/usr/bin/env bash
# test-diff.sh — Simulate a multi-round diff view with resolved comments.
#
# Usage: ./test-diff.sh [port]
#
# What this does:
#   1. Resets test-plan-copy.md to v1 and seeds it with review comments
#   2. Starts crit on that file
#   3. Waits for you to press Enter (browse the comments first)
#   4. Swaps in test-plan-v2.md to simulate agent edits
#   5. Marks some comments as resolved in the JSON
#   6. Signals round-complete so the diff + resolved comments appear

set -e

# Always run from the repo root regardless of where the script is called from
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT"

PORT="${1:-3001}"
BINARY="./crit"
FILE="test/test-plan-copy.md"
COMMENTS_FILE="test/.test-plan-copy.md.comments.json"

if [ ! -f "$BINARY" ]; then
  echo "Binary not found — building..."
  go build -o crit .
fi

# Reset the copy to v1
cp test-plan.md "$FILE"

# Seed initial comments (not yet resolved)
cat > "$COMMENTS_FILE" <<'EOF'
{
  "file": "test-plan-copy.md",
  "file_hash": "",
  "updated_at": "2026-01-01T00:00:00Z",
  "comments": [
    {
      "id": "1",
      "start_line": 5,
      "end_line": 6,
      "body": "SAML is mentioned here but I thought we agreed to drop it. Confirm with product before this ships.",
      "created_at": "2026-01-01T10:00:00Z",
      "updated_at": "2026-01-01T10:00:00Z"
    },
    {
      "id": "2",
      "start_line": 71,
      "end_line": 71,
      "body": "5 attempts per minute is too restrictive — mobile users on flaky connections retry fast. Consider 10-20.",
      "created_at": "2026-01-01T10:01:00Z",
      "updated_at": "2026-01-01T10:01:00Z"
    },
    {
      "id": "3",
      "start_line": 97,
      "end_line": 98,
      "body": "Login was merged last week and refresh is done too — these should be checked off.",
      "created_at": "2026-01-01T10:02:00Z",
      "updated_at": "2026-01-01T10:02:00Z"
    },
    {
      "id": "4",
      "start_line": 135,
      "end_line": 135,
      "body": "We need a decision on session TTL before writing the migration. This is blocking.",
      "created_at": "2026-01-01T10:03:00Z",
      "updated_at": "2026-01-01T10:03:00Z"
    }
  ]
}
EOF

echo "Starting crit on $FILE (port $PORT)..."
"$BINARY" --port "$PORT" "$FILE" &
CRIT_PID=$!

cleanup() {
  kill "$CRIT_PID" 2>/dev/null || true
  wait "$CRIT_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# Wait for the server to be ready
echo -n "Waiting for server..."
for i in $(seq 1 20); do
  if curl -sf "http://127.0.0.1:$PORT/api/document" > /dev/null 2>&1; then
    echo " ready."
    break
  fi
  sleep 0.5
  echo -n "."
done

echo ""
echo "Crit is running with 4 seeded comments. Browse them in the browser,"
echo "then press Enter to simulate the agent editing the file."
read -r

echo "Swapping in v2 content..."
cp test/test-plan-v2.md "$FILE"

# Give the file watcher one tick to detect the change (polls every 1s).
# ReloadFile() will snapshot PreviousComments from in-memory Comments here.
sleep 1.5

# Now overwrite the JSON with resolved versions.
# loadResolvedComments() will read this on round-complete and set PreviousComments.
cat > "$COMMENTS_FILE" <<'EOF'
{
  "file": "test-plan-copy.md",
  "file_hash": "",
  "updated_at": "2026-01-01T01:00:00Z",
  "comments": [
    {
      "id": "1",
      "start_line": 5,
      "end_line": 6,
      "body": "SAML is mentioned here but I thought we agreed to drop it. Confirm with product before this ships.",
      "created_at": "2026-01-01T10:00:00Z",
      "updated_at": "2026-01-01T10:00:00Z",
      "resolved": true,
      "resolution_note": "Confirmed with product — SAML dropped from scope entirely, moved to Phase 3.",
      "resolution_lines": [6]
    },
    {
      "id": "2",
      "start_line": 71,
      "end_line": 71,
      "body": "5 attempts per minute is too restrictive — mobile users on flaky connections retry fast. Consider 10-20.",
      "created_at": "2026-01-01T10:01:00Z",
      "updated_at": "2026-01-01T10:01:00Z",
      "resolved": true,
      "resolution_note": "Bumped to 10 per minute.",
      "resolution_lines": [73]
    },
    {
      "id": "3",
      "start_line": 97,
      "end_line": 98,
      "body": "Login was merged last week and refresh is done too — these should be checked off.",
      "created_at": "2026-01-01T10:02:00Z",
      "updated_at": "2026-01-01T10:02:00Z",
      "resolved": true,
      "resolution_note": "Marked login and token refresh as complete in the checklist.",
      "resolution_lines": [101, 102]
    },
    {
      "id": "4",
      "start_line": 135,
      "end_line": 135,
      "body": "We need a decision on session TTL before writing the migration. This is blocking.",
      "created_at": "2026-01-01T10:03:00Z",
      "updated_at": "2026-01-01T10:03:00Z"
    }
  ]
}
EOF

echo "Signalling round-complete..."
curl -s -X POST "http://127.0.0.1:$PORT/api/round-complete" > /dev/null

echo ""
echo "Done — check the browser for the diff view with resolved comments."
echo "Comment #4 (session TTL) is intentionally left unresolved."
echo ""
echo "Press Enter to stop the server."
read -r
