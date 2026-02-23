#!/usr/bin/env bash
# test-diff.sh — Simulate a multi-round diff view with resolved comments.
#
# Usage: ./test/test-diff.sh [port]
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
      "start_line": 20,
      "end_line": 20,
      "body": "Redis Streams will lose the queue on restart if AOF isn't enabled. Worth checking before we commit. We're already on AWS — SQS gives us durable delivery without needing to think about Redis persistence config.",
      "created_at": "2026-01-01T10:00:00Z",
      "updated_at": "2026-01-01T10:00:00Z"
    },
    {
      "id": "2",
      "start_line": 61,
      "end_line": 62,
      "body": "Even on the internal network we should have some protection on this endpoint. A buggy upstream service could spam /send and flood user inboxes with no rate limiting in place. At minimum a shared secret header, and rate limiting per caller should be in the MVP checklist.",
      "created_at": "2026-01-01T10:01:00Z",
      "updated_at": "2026-01-01T10:01:00Z"
    },
    {
      "id": "3",
      "start_line": 121,
      "end_line": 121,
      "body": "2 hours is a long tail for webhook consumers. If my endpoint is down I'd want a failure signal faster so I can investigate. Most webhook systems cap at 30-60 minutes. Recommend dropping this to 30 minutes max.",
      "created_at": "2026-01-01T10:02:00Z",
      "updated_at": "2026-01-01T10:02:00Z"
    },
    {
      "id": "4",
      "start_line": 158,
      "end_line": 159,
      "body": "This is blocking the migration. metadata JSONB is currently unbounded — someone will try to store a 10MB blob in it. We need a cap in the schema before migrations run. Suggest 64KB and enforce with a CHECK constraint.",
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
      "start_line": 20,
      "end_line": 20,
      "body": "Redis Streams will lose the queue on restart if AOF isn't enabled. Worth checking before we commit. We're already on AWS — SQS gives us durable delivery without needing to think about Redis persistence config.",
      "created_at": "2026-01-01T10:00:00Z",
      "updated_at": "2026-01-01T10:00:00Z",
      "resolved": true,
      "resolution_note": "Switched to SQS. Durability is handled by AWS, no AOF config needed, and we're already paying for it.",
      "resolution_lines": [20]
    },
    {
      "id": "2",
      "start_line": 61,
      "end_line": 62,
      "body": "Even on the internal network we should have some protection on this endpoint. A buggy upstream service could spam /send and flood user inboxes with no rate limiting in place. At minimum a shared secret header, and rate limiting per caller should be in the MVP checklist.",
      "created_at": "2026-01-01T10:01:00Z",
      "updated_at": "2026-01-01T10:01:00Z",
      "resolved": true,
      "resolution_note": "Added X-Internal-Token requirement to the endpoint description and a rate limiting checklist item.",
      "resolution_lines": [62, 140]
    },
    {
      "id": "3",
      "start_line": 121,
      "end_line": 121,
      "body": "2 hours is a long tail for webhook consumers. If my endpoint is down I'd want a failure signal faster so I can investigate. Most webhook systems cap at 30-60 minutes. Recommend dropping this to 30 minutes max.",
      "created_at": "2026-01-01T10:02:00Z",
      "updated_at": "2026-01-01T10:02:00Z",
      "resolved": true,
      "resolution_note": "Capped at 30 minutes. Both attempts 4 and 5 now use the same interval.",
      "resolution_lines": [122]
    },
    {
      "id": "4",
      "start_line": 158,
      "end_line": 159,
      "body": "This is blocking the migration. metadata JSONB is currently unbounded — someone will try to store a 10MB blob in it. We need a cap in the schema before migrations run. Suggest 64KB and enforce with a CHECK constraint.",
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
echo "Comment #4 (metadata size cap) is intentionally left unresolved."
echo ""
echo "Press Enter to stop the server."
read -r
