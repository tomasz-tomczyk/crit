#!/usr/bin/env bash
# test-diff.sh — Simulate a multi-round diff view with resolved comments and threaded replies.
#
# Usage: ./test/test-diff.sh [port]
#
# What this does:
#   1. Resets test-plan-copy.md to v1 and starts crit on that file
#   2. Seeds 4 review comments + 4 threaded replies via the API
#   3. Waits for you to press Enter (browse the comments + replies first)
#   4. Swaps in test-plan-v2.md to simulate agent edits
#   5. Marks some comments as resolved in the review file
#   6. Signals round-complete so the diff + resolved comments appear
#
# Threading coverage:
#   - Comment #2 gets 2 agent replies, then is resolved → test expanded resolved with replies
#   - Comment #4 gets 2 replies (agent + reviewer), stays unresolved → test active threading

set -e

# Always run from the repo root regardless of where the script is called from
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT"

PORT="${1:-3001}"
BINARY="./crit"
FILE="test/test-plan-copy.md"

if [ ! -f "$BINARY" ]; then
  echo "Binary not found — building..."
  go build -o crit .
fi

# Kill any stale processes on test ports
for port in "$PORT" "$((PORT + 1))"; do
  lsof -ti tcp:"$port" 2>/dev/null | xargs kill -9 2>/dev/null || true
done

# Reset the copy to v1 and remove any stale .crit.json
cp test/notification-plan.md "$FILE"
rm -f .crit.json

echo "Starting crit on $FILE (port $PORT)..."
"$BINARY" --port "$PORT" --no-open "$FILE" &
CRIT_PID=$!

WORD_DIFF_PORT=$((PORT + 1))
WORD_DIFF_DIR=$(mktemp -d)

# Create a git repo with a Go file, then modify it to produce paired del/add lines
git -C "$WORD_DIFF_DIR" init -q
cat > "$WORD_DIFF_DIR/main.go" << 'GOEOF'
package main

import (
	"fmt"
	"net/http"
)

func healthHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "ok")
}

func main() {
	http.HandleFunc("/health", healthHandler)
	fmt.Println("Server starting on :8080")
	http.ListenAndServe(":8080", nil)
}
GOEOF

# Create an Elixir file to test adjacent word-diff merging and whitespace-only filtering
cat > "$WORD_DIFF_DIR/accounts.ex" << 'EXEOF'
defmodule MyApp.Accounts do
  def reset_password(token) do
    case verify_reset_password_token(token) do
      {:ok, provider} ->
        case Accounts.update_provider_password(provider, %{
               "password" => password,
               "password_confirmation" => password_confirmation
             }) do
          {:ok, _} -> :ok
          {:error, changeset} -> {:error, changeset}
        end
      {:error, reason} ->
        {:error, reason}
    end
  end

  def provider_password_change(provider, params) do
    provider
    |> cast(params, [:password, :password_confirmation])
    |> validate_required([:password, :password_confirmation])
    |> validate_length(:password, min: 4)
    |> validate_confirmation(:password)
    |> hash_password()
  end
end
EXEOF

# Scheduler file — exercises unified-diff gutter drag starting from a deletion line.
# v1 → v2 produces three deletion/addition pairs separated by context lines; drag
# the + gutter from the first deletion to the last to verify the selection spans
# context lines (not collapsed to deletions only).
cat > "$WORD_DIFF_DIR/scheduler.ex" << 'EXEOF'
defmodule Vetspire.DistributedWorker.Scheduler do
  use GenServer

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def init(opts) do
    {:ok, %{dynamic_supervisor: opts[:sup], owned: %{}}}
  end

  def handle_call({:schedule, task_id, child_spec}, _from, state) do
    owned = state.owned

    case DynamicSupervisor.start_child(state.dynamic_supervisor, child_spec) do
      {:ok, pid} ->
        Map.put(owned, task_id, {pid, Process.monitor(pid)})

      {:ok, pid, _info} ->
        Map.put(owned, task_id, {pid, Process.monitor(pid)})

      {:error, {:already_started, pid}} ->
        Map.put(owned, task_id, {pid, Process.monitor(pid)})
    end
  end
end
EXEOF

git -C "$WORD_DIFF_DIR" add -A && git -C "$WORD_DIFF_DIR" commit -q -m "initial"

# Modify the file to produce good word-level diff pairs
cat > "$WORD_DIFF_DIR/main.go" << 'GOEOF'
package main

import (
	"log"
	"net/http"
	"os"
)

func healthHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, `{"status":"ok"}`)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	http.HandleFunc("/health", healthHandler)
	log.Printf("Server starting on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
GOEOF

# Modify the Elixir file — tests both adjacent word merging and whitespace-only filtering
cat > "$WORD_DIFF_DIR/accounts.ex" << 'EXEOF'
defmodule MyApp.Accounts do
  def reset_password(token) do
    case verify_reset_password_token(token) do
      {:ok, provider} ->
        require_complex? =
          provider.org_id
          |> Entities.org_preferences(["org.complex_passwords"])
          |> Entities.is_pref_enabled?("org.complex_passwords")
      {:error, reason} ->
        {:error, reason}
    end
  end

  def provider_password_change(provider, params, opts \\ []) do
      changeset =
        provider
        |> cast(params, [:password, :password_confirmation])
        |> validate_required([:password, :password_confirmation])
        |> validate_length(:password, min: 8)
        |> validate_confirmation(:password)
  end
end
EXEOF

# Modify scheduler.ex — Map.put → track/3, producing three paired del/add blocks
# separated by context lines. This is the target for the unified-diff gutter drag test.
cat > "$WORD_DIFF_DIR/scheduler.ex" << 'EXEOF'
defmodule Vetspire.DistributedWorker.Scheduler do
  use GenServer

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def init(opts) do
    {:ok, %{dynamic_supervisor: opts[:sup], owned: %{}}}
  end

  def handle_call({:schedule, task_id, child_spec}, _from, state) do
    owned = state.owned

    case DynamicSupervisor.start_child(state.dynamic_supervisor, child_spec) do
      {:ok, pid} ->
        track(owned, task_id, pid)

      {:ok, pid, _info} ->
        track(owned, task_id, pid)

      {:error, {:already_started, pid}} ->
        track(owned, task_id, pid)
    end
  end
end
EXEOF

echo ""
echo "Starting git-mode crit for word-level diff on port $WORD_DIFF_PORT..."
(cd "$WORD_DIFF_DIR" && "$ROOT/$BINARY" --port "$WORD_DIFF_PORT" --no-open) &
WORD_DIFF_PID=$!

cleanup() {
  kill "$CRIT_PID" 2>/dev/null || true
  kill "$WORD_DIFF_PID" 2>/dev/null || true
  wait "$CRIT_PID" 2>/dev/null || true
  wait "$WORD_DIFF_PID" 2>/dev/null || true
  rm -f .crit.json
  rm -rf "$WORD_DIFF_DIR"
}
trap cleanup EXIT INT TERM

# Wait for servers to be ready (poll until /api/session returns 200, not 503)
for port_to_wait in "$PORT" "$WORD_DIFF_PORT"; do
  for i in $(seq 1 40); do
    if curl -sf "http://127.0.0.1:$port_to_wait/api/session" > /dev/null 2>&1; then
      break
    fi
    sleep 0.5
  done
done

# Clear any leftover comments from previous runs (the daemon persists
# reviews to ~/.crit/reviews/ — re-running without this accumulates dupes)
curl -sf -X DELETE "http://127.0.0.1:$PORT/api/comments" > /dev/null

# Determine the file path as the server sees it
FILE_PATH=$(curl -sf "http://127.0.0.1:$PORT/api/session" | python3 -c "
import json, sys
s = json.load(sys.stdin)
for f in s['files']:
    if f['path'] != '.crit.json':
        print(f['path'])
        break
")
ENCODED_PATH=$(python3 -c "import urllib.parse; print(urllib.parse.quote('$FILE_PATH'))")

# Seed 5 comments via the API — capture IDs for threading replies
C1=$(curl -sf -X POST "http://127.0.0.1:$PORT/api/file/comments?path=$ENCODED_PATH" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 20, "end_line": 20,
    "body": "Redis Streams will lose the queue on restart if AOF isn'\''t enabled. Worth checking before we commit. We'\''re already on AWS — SQS gives us durable delivery without needing to think about Redis persistence config."
  }' | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")

C2=$(curl -sf -X POST "http://127.0.0.1:$PORT/api/file/comments?path=$ENCODED_PATH" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 61, "end_line": 62,
    "body": "Even on the internal network we should have some protection on this endpoint. A buggy upstream service could spam `/send` and flood user inboxes with no rate limiting in place.\n\nAt minimum the MVP checklist should include:\n\n- A shared secret header (e.g. `X-Internal-Token`)\n- Rate limiting per caller\n\n**These are not optional** — a single misconfigured upstream can take down the notification pipeline."
  }' | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")

C3=$(curl -sf -X POST "http://127.0.0.1:$PORT/api/file/comments?path=$ENCODED_PATH" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 121, "end_line": 121,
    "body": "2 hours is a long tail for webhook consumers. If my endpoint is down I'\''d want a failure signal faster so I can investigate. Most webhook systems cap at 30-60 minutes. Recommend dropping this to 30 minutes max."
  }' | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")

C4=$(curl -sf -X POST "http://127.0.0.1:$PORT/api/file/comments?path=$ENCODED_PATH" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 158, "end_line": 159,
    "body": "This is blocking the migration. metadata JSONB is currently unbounded — someone will try to store a 10MB blob in it. We need a cap in the schema before migrations run. Suggest 64KB and enforce with a CHECK constraint."
  }' | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")

# Comment on the Code Standards heading — replicates the screenshot scenario
# where comments + deletion markers interrupt formatted markdown sections
C5=$(curl -sf -X POST "http://127.0.0.1:$PORT/api/file/comments?path=$ENCODED_PATH" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 162, "end_line": 162,
    "body": "These standards are good but we should split them into a separate doc once we'\''re past MVP. Having them inline in the plan adds noise for anyone skimming the implementation steps."
  }' | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")

# Seed replies on comments to exercise threading (use captured IDs)
curl -sf -X POST "http://127.0.0.1:$PORT/api/comment/$C2/replies?path=$ENCODED_PATH" \
  -H 'Content-Type: application/json' \
  -d '{
    "body": "Agreed on the shared secret. I'\''ll add `X-Internal-Token` validation to the middleware before the endpoint goes live.",
    "author": "agent"
  }' > /dev/null

curl -sf -X POST "http://127.0.0.1:$PORT/api/comment/$C2/replies?path=$ENCODED_PATH" \
  -H 'Content-Type: application/json' \
  -d '{
    "body": "Rate limiting is done — added a per-caller sliding window (100 req/min). The token header is enforced in middleware now too.\n\nSee the updated endpoint spec at line 62.",
    "author": "agent"
  }' > /dev/null

curl -sf -X POST "http://127.0.0.1:$PORT/api/comment/$C4/replies?path=$ENCODED_PATH" \
  -H 'Content-Type: application/json' \
  -d '{
    "body": "64KB sounds right. I'\''ll add a CHECK constraint in the migration. Should we also add an application-level validation in the changeset?",
    "author": "agent"
  }' > /dev/null

curl -sf -X POST "http://127.0.0.1:$PORT/api/comment/$C4/replies?path=$ENCODED_PATH" \
  -H 'Content-Type: application/json' \
  -d '{
    "body": "Yes — belt and suspenders. CHECK constraint in Postgres + `validate_length(:metadata_json, max: 65536)` in the changeset. The DB constraint is the safety net if someone bypasses the app layer.",
    "author": "reviewer"
  }' > /dev/null

# Finish the review to write the review file
REVIEW_FILE=$(curl -sf -X POST "http://127.0.0.1:$PORT/api/finish" | python3 -c "import json, sys; print(json.load(sys.stdin)['review_file'])")

echo ""
echo "Crit is running at http://127.0.0.1:$PORT with 5 comments + 4 replies."
echo "Browse them in the browser, then press Enter to simulate the agent editing the file."
read -r

echo "Swapping in v2 content..."
cp test/test-plan-v2.md "$FILE"

# Give the file watcher one tick to detect the change (polls every 1s).
sleep 1.5

# Mark 3 of 4 comments as resolved in the review file (comment #4 stays open)
python3 - "$REVIEW_FILE" <<'PYEOF'
import json, sys
path = sys.argv[1]
with open(path) as f:
    cj = json.load(f)
for fk in cj['files']:
    comments = cj['files'][fk]['comments']
    if len(comments) >= 3:
        comments[0]['resolved'] = True
        comments[0]['resolution_note'] = "Switched to SQS. Durability is handled by AWS, no AOF config needed, and we're already paying for it."
        comments[0]['resolution_lines'] = [20]
        comments[1]['resolved'] = True
        comments[1]['resolution_note'] = 'Added X-Internal-Token requirement to the endpoint description and a rate limiting checklist item.'
        comments[1]['resolution_lines'] = [62, 140]
        comments[2]['resolved'] = True
        comments[2]['resolution_note'] = 'Capped at 30 minutes. Both attempts 4 and 5 now use the same interval.'
        comments[2]['resolution_lines'] = [122]
with open(path, 'w') as f:
    json.dump(cj, f, indent=2)
PYEOF

echo "Signalling round-complete..."
curl -sf -X POST "http://127.0.0.1:$PORT/api/round-complete" > /dev/null

# --- Orphaned comments on the word-diff (git-mode) instance ---
# Wait for the word-diff server to be ready
for i in $(seq 1 20); do
  if curl -sf "http://127.0.0.1:$WORD_DIFF_PORT/api/session" > /dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

# Create a temporary file, commit it so it shows up in the diff
cat > "$WORD_DIFF_DIR/helpers.go" << 'GOEOF'
package main

// FormatBytes returns a human-readable byte size string.
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
GOEOF
git -C "$WORD_DIFF_DIR" add helpers.go && git -C "$WORD_DIFF_DIR" commit -q -m "add helpers"

# Signal round-complete so crit picks up the new file
curl -sf -X POST "http://127.0.0.1:$WORD_DIFF_PORT/api/round-complete" > /dev/null
sleep 1

# Add comments on the helpers file: one file-level, one line-scoped
curl -sf -X POST "http://127.0.0.1:$WORD_DIFF_PORT/api/file/comments?path=helpers.go" \
  -H 'Content-Type: application/json' \
  -d '{"body": "Do we really need a custom byte formatter? There are stdlib options.", "scope": "file"}' > /dev/null

curl -sf -X POST "http://127.0.0.1:$WORD_DIFF_PORT/api/file/comments?path=helpers.go" \
  -H 'Content-Type: application/json' \
  -d '{"start_line": 5, "end_line": 8, "body": "This will overflow for values above exabyte range. Use math.Log instead of the loop."}' > /dev/null

# Finish to persist comments to the review file
curl -sf -X POST "http://127.0.0.1:$WORD_DIFF_PORT/api/finish" > /dev/null

# Now delete the file and amend the commit so there is no net diff
git -C "$WORD_DIFF_DIR" rm -q helpers.go && git -C "$WORD_DIFF_DIR" commit -q -m "remove helpers"

# Signal round-complete — helpers.go disappears from git diff, comments become orphaned
curl -sf -X POST "http://127.0.0.1:$WORD_DIFF_PORT/api/round-complete" > /dev/null

echo ""
echo "Three views running:"
echo "  1. Markdown diff (inter-round):  http://127.0.0.1:$PORT"
echo "  2. Code diff (word-level):       http://127.0.0.1:$WORD_DIFF_PORT"
echo ""
echo "Instance 1: diff view with resolved comments + threaded replies + deletion markers."
echo "            Comment #2 (resolved): 2 agent replies — visible when expanded."
echo "            Comment #4 (unresolved): 2 replies (agent + reviewer) — visible inline."
echo "            Comment #5 (on Code Standards heading): tests formatting near deletion markers."
echo "            Scroll to bottom: deletion markers interrupt the markdown code fence."
echo "Instance 2: word-level diff + orphaned comments on helpers.go"
echo "            helpers.go was added then deleted — should appear as a phantom"
echo "            section with 'Removed' badge, 2 outdated comments (1 file-level,"
echo "            1 line-scoped), and full resolve/edit/delete support."
echo ""
echo "Press Enter to stop both servers."
read -r
