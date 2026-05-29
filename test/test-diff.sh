#!/usr/bin/env bash
# test-diff.sh — Simulate multi-round reviews with comments, threading, and carry-forward.
#
# Usage: ./test/test-diff.sh [port]
#
# Starts 4 server instances:
#   1. Markdown diff (port):     resolved comments + threaded replies + deletion markers
#   2. Code diff (port+1):       word-level diff + orphaned comments on removed files
#   3. Carry-forward file-mode (port+2): comment positioning across content changes
#   4. Carry-forward git-mode (port+3):  same carry-forward test in git context
#
# Flow:
#   1. Starts all servers, seeds comments on instances 1, 3, and 4
#   2. Waits for Enter (browse the comments first)
#   3. Swaps content to v2 on instances 1, 3, 4; resolves some comments on instance 1
#   4. Signals round-complete on all instances
#   5. Instance 2 gets orphaned-file comments seeded post-round
#   6. Shows expected carry-forward results, waits for Enter to stop

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
for port in "$PORT" "$((PORT + 1))" "$((PORT + 2))" "$((PORT + 3))" "$((PORT + 4))" "$((PORT + 5))"; do
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

# server.go v1 — large file with helpers in the middle to produce spacer gaps in the diff.
# Changes happen in imports (top) and main() (bottom); the middle helpers stay unchanged,
# creating folded spacers between the hunks.
cat > "$WORD_DIFF_DIR/server.go" << 'GOEOF'
package main

import (
	"fmt"
	"net/http"
)

// respondJSON writes a JSON response with the given status code.
func respondJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprint(w, body)
}

// logRequest logs the incoming request method and path.
func logRequest(r *http.Request) {
	fmt.Printf("%s %s\n", r.Method, r.URL.Path)
}

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		logRequest(r)
		fmt.Fprintf(w, "Hello, %s!", r.URL.Path[1:])
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		logRequest(r)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	http.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		logRequest(r)
		respondJSON(w, http.StatusOK, `{"version":"1.0.0"}`)
	})

	http.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		logRequest(r)
		respondJSON(w, http.StatusOK, `{"ready":true}`)
	})

	fmt.Println("Server starting on :8080")
	http.ListenAndServe(":8080", nil)
}
GOEOF

git -C "$WORD_DIFF_DIR" add -A && git -C "$WORD_DIFF_DIR" commit -q -m "initial"

# Modify server.go — imports + authMiddleware + main changes produce 3 hunks
# with spacer gaps over the unchanged respondJSON/logRequest and /version/ready handlers
cat > "$WORD_DIFF_DIR/server.go" << 'GOEOF'
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

// respondJSON writes a JSON response with the given status code.
func respondJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprint(w, body)
}

// logRequest logs the incoming request method and path.
func logRequest(r *http.Request) {
	fmt.Printf("%s %s\n", r.Method, r.URL.Path)
}

// authMiddleware checks for a valid API key in the Authorization header.
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Authorization")
		if !strings.HasPrefix(key, "Bearer ") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		logRequest(r)
		name := r.URL.Path[1:]
		if name == "" {
			name = "world"
		}
		fmt.Fprintf(w, "Hello, %s!", name)
	}))

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		logRequest(r)
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	http.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		logRequest(r)
		respondJSON(w, http.StatusOK, `{"version":"1.0.0"}`)
	})

	http.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		logRequest(r)
		respondJSON(w, http.StatusOK, `{"ready":true}`)
	})

	log.Printf("Server starting on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
GOEOF
# server.go v2 is left as an uncommitted working-tree change (like the other files)
# so crit shows it in the diff view with spacer gaps between hunks.

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

# --- Carry-forward: file-mode test ---
CF_FILE_PORT=$((PORT + 2))
CF_FILE="test/carry-forward-copy.md"
cp test/carry-forward-v1.md "$CF_FILE"

echo "Starting file-mode carry-forward test on port $CF_FILE_PORT..."
"$BINARY" --port "$CF_FILE_PORT" --no-open "$CF_FILE" &
CF_FILE_PID=$!

# --- Carry-forward: git-mode test ---
CF_GIT_PORT=$((PORT + 3))
CF_GIT_DIR=$(mktemp -d)

# Base: minimal stub on main
git -C "$CF_GIT_DIR" init -q
cat > "$CF_GIT_DIR/plan.md" << 'MDEOF'
# Database Migration Plan

## Overview

Placeholder for the migration plan.
MDEOF
git -C "$CF_GIT_DIR" add -A && git -C "$CF_GIT_DIR" commit -q -m "initial stub"

# Feature branch: full v1 content
git -C "$CF_GIT_DIR" checkout -q -b feature/migration
cp test/carry-forward-v1.md "$CF_GIT_DIR/plan.md"
git -C "$CF_GIT_DIR" add -A && git -C "$CF_GIT_DIR" commit -q -m "add full migration plan"

echo "Starting git-mode carry-forward test on port $CF_GIT_PORT..."
(cd "$CF_GIT_DIR" && "$ROOT/$BINARY" --port "$CF_GIT_PORT" --no-open) &
CF_GIT_PID=$!

# --- Instance 5: Range mode (--range A..B against a stacked git fixture) ---
# Verifies SHA-pinned diff, on-disk head_sha + diff_scope=layer stamping, and
# focus-picker round-trip (range -> working tree -> range preserves comments).
# Fixture: main → feat-a → feat-b → feat-c (three real branches stacked on
# each other) so the picker has actual layers to surface in the popover.
RANGE_PORT=$((PORT + 4))
RANGE_DIR=$(mktemp -d)

git -C "$RANGE_DIR" init -q -b main
git -C "$RANGE_DIR" -c user.email=t@t -c user.name=t commit --allow-empty -q -m "main seed"
RANGE_MAIN_SHA=$(git -C "$RANGE_DIR" rev-parse HEAD)

git -C "$RANGE_DIR" checkout -q -b feat-a
echo "alpha" > "$RANGE_DIR/a.txt"
git -C "$RANGE_DIR" add a.txt
git -C "$RANGE_DIR" -c user.email=t@t -c user.name=t commit -q -m "A: add a.txt"
RANGE_A_SHA=$(git -C "$RANGE_DIR" rev-parse HEAD)

git -C "$RANGE_DIR" checkout -q -b feat-b
echo "beta" > "$RANGE_DIR/b.txt"
git -C "$RANGE_DIR" add b.txt
git -C "$RANGE_DIR" -c user.email=t@t -c user.name=t commit -q -m "B: add b.txt"
RANGE_B_SHA=$(git -C "$RANGE_DIR" rev-parse HEAD)

git -C "$RANGE_DIR" checkout -q -b feat-c
echo "gamma" > "$RANGE_DIR/c.txt"
git -C "$RANGE_DIR" add c.txt
git -C "$RANGE_DIR" -c user.email=t@t -c user.name=t commit -q -m "C: add c.txt"

echo "Starting range-mode crit on port $RANGE_PORT (--range A..B)..."
(cd "$RANGE_DIR" && "$ROOT/$BINARY" --port "$RANGE_PORT" --no-open --range "$RANGE_A_SHA..$RANGE_B_SHA") &
RANGE_PID=$!

# --- Instance 6: Stacked PR layer/full-stack toggle ---
# Synthesizes stacked metadata via /api/focus so the diff-scope toggle UI
# becomes visible without a real gh PR. Seeds per-scope comments to verify
# lossless toggling and exercises the runPush gate-1 refusal. Fixture: same
# three-branch stack as Instance 5 so the popover shows real layers.
TOGGLE_PORT=$((PORT + 5))
TOGGLE_DIR=$(mktemp -d)

git -C "$TOGGLE_DIR" init -q -b main
git -C "$TOGGLE_DIR" -c user.email=t@t -c user.name=t commit --allow-empty -q -m "main seed"
TOGGLE_MAIN_SHA=$(git -C "$TOGGLE_DIR" rev-parse HEAD)

git -C "$TOGGLE_DIR" checkout -q -b feat-a
echo "alpha" > "$TOGGLE_DIR/a.txt"
git -C "$TOGGLE_DIR" add a.txt
git -C "$TOGGLE_DIR" -c user.email=t@t -c user.name=t commit -q -m "A: add a.txt"
TOGGLE_A_SHA=$(git -C "$TOGGLE_DIR" rev-parse HEAD)

git -C "$TOGGLE_DIR" checkout -q -b feat-b
echo "beta" > "$TOGGLE_DIR/b.txt"
git -C "$TOGGLE_DIR" add b.txt
git -C "$TOGGLE_DIR" -c user.email=t@t -c user.name=t commit -q -m "B: add b.txt"
TOGGLE_B_SHA=$(git -C "$TOGGLE_DIR" rev-parse HEAD)

echo "Starting stacked-toggle crit on port $TOGGLE_PORT (--range A..B)..."
(cd "$TOGGLE_DIR" && "$ROOT/$BINARY" --port "$TOGGLE_PORT" --no-open --range "$TOGGLE_A_SHA..$TOGGLE_B_SHA") &
TOGGLE_PID=$!

cleanup() {
  kill "$CRIT_PID" 2>/dev/null || true
  kill "$WORD_DIFF_PID" 2>/dev/null || true
  kill "$CF_FILE_PID" 2>/dev/null || true
  kill "$CF_GIT_PID" 2>/dev/null || true
  kill "$RANGE_PID" 2>/dev/null || true
  kill "$TOGGLE_PID" 2>/dev/null || true
  wait "$CRIT_PID" 2>/dev/null || true
  wait "$WORD_DIFF_PID" 2>/dev/null || true
  wait "$CF_FILE_PID" 2>/dev/null || true
  wait "$CF_GIT_PID" 2>/dev/null || true
  wait "$RANGE_PID" 2>/dev/null || true
  wait "$TOGGLE_PID" 2>/dev/null || true
  rm -f .crit.json
  rm -f "$CF_FILE"
  rm -rf "$WORD_DIFF_DIR"
  rm -rf "$CF_GIT_DIR"
  rm -rf "$RANGE_DIR"
  rm -rf "$TOGGLE_DIR"
}
trap cleanup EXIT INT TERM

# Wait for servers to be ready (poll until /api/session returns 200, not 503)
for port_to_wait in "$PORT" "$WORD_DIFF_PORT" "$CF_FILE_PORT" "$CF_GIT_PORT" "$RANGE_PORT" "$TOGGLE_PORT"; do
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

# Seed a comment that references other comments by ID — exercises comment-ref linking
curl -sf -X POST "http://127.0.0.1:$PORT/api/file/comments?path=$ENCODED_PATH" \
  -H 'Content-Type: application/json' \
  -d "{
    \"start_line\": 40, \"end_line\": 40,
    \"body\": \"Same durability concern as \`$C1\` — if we switch to SQS we resolve both issues. Also ties into the rate-limiting work from $C2.\"
  }" > /dev/null

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

# Seed a couple of review-level (general) comments to exercise the
# Review Conversation section at the top of the document.
curl -sf -X POST "http://127.0.0.1:$PORT/api/comments" \
  -H 'Content-Type: application/json' \
  -d '{
    "body": "Overall this plan is solid but I think the scope might be too broad for a single round — could we split the SQS migration and the rate-limiting work into separate deliverables?",
    "author": "reviewer"
  }' > /dev/null

curl -sf -X POST "http://127.0.0.1:$PORT/api/comments" \
  -H 'Content-Type: application/json' \
  -d '{
    "body": "Have we looped in the on-call team about the maintenance window? They'\''ll need to know about the table swap timing.",
    "author": "reviewer"
  }' > /dev/null

# Finish the review to write the review file
REVIEW_FILE=$(curl -sf -X POST "http://127.0.0.1:$PORT/api/finish" | python3 -c "import json, sys; print(json.load(sys.stdin)['review_file'])")

# --- Seed GitHub-synced comments (issue #370) ---
# The POST API has no `github_id` field (synced comments normally arrive via
# `crit pull`), so we inject them directly into the review file. The daemon's
# file watcher (mergeExternalCritJSON, ~1s tick) appends brand-new comments
# wholesale — preserving GitHubID — and appends synced replies onto existing
# comments, so both render with the `.github-badge` pill the PR adds. This
# demonstrates the badge on (a) a fully-synced comment header, (b) a synced
# reply inside that thread, and (c) a synced reply mixed into a native thread.
python3 - "$REVIEW_FILE" <<'PYEOF'
import json, sys
path = sys.argv[1]
with open(path) as f:
    cj = json.load(f)

# The single reviewed file (everything except the .crit.json control file).
fk = next(k for k in cj["files"] if k != ".crit.json")
comments = cj["files"][fk]["comments"]

# (a) + (b): a fully GitHub-synced comment carrying a synced reply.
comments.append({
    "id": "c_gh_demo1",
    "start_line": 8,
    "end_line": 8,
    "side": "",
    "body": "Are webhook deliveries going to be signed? If we're POSTing to "
            "user-controlled URLs the receiver needs a way to verify the payload "
            "came from us — an HMAC signature header is the usual approach.\n\n"
            "_Imported from the GitHub PR review._",
    "author": "octocat",
    "scope": "line",
    "created_at": "2024-01-15T10:00:00Z",
    "updated_at": "2024-01-15T10:00:00Z",
    "github_id": 2147483001,
    "replies": [
        {
            "id": "rp_gh_demo1",
            "body": "Good point — we'll add an `X-Signature` HMAC header in "
                    "Phase 1. Tracked in the security checklist below.",
            "author": "maintainer",
            "created_at": "2024-01-15T10:05:00Z",
            "github_id": 2147483002,
        }
    ],
})

# (c): a synced reply mixed into the first native comment's thread.
if comments and comments[0].get("id") != "c_gh_demo1":
    comments[0].setdefault("replies", []).append({
        "id": "rp_gh_demo2",
        "body": "GitHub reviewer here — +1 on SQS. We already hit the "
                "Redis-restart data loss in staging last quarter.",
        "author": "octocat",
        "created_at": "2024-01-15T10:10:00Z",
        "github_id": 2147483003,
    })

with open(path, "w") as f:
    json.dump(cj, f, indent=2)
PYEOF

# Give the file watcher a tick to merge the injected synced comments into the
# running session before the script moves on.
sleep 1.2

# --- Seed carry-forward comments (file-mode) ---
curl -sf -X DELETE "http://127.0.0.1:$CF_FILE_PORT/api/comments" > /dev/null

CF_FILE_PATH=$(curl -sf "http://127.0.0.1:$CF_FILE_PORT/api/session" | python3 -c "
import json, sys
s = json.load(sys.stdin)
for f in s['files']:
    if f['path'] != '.crit.json':
        print(f['path'])
        break
")
CF_FILE_ENCODED=$(python3 -c "import urllib.parse; print(urllib.parse.quote('$CF_FILE_PATH'))")

# C1: Lines 31-32 — sessions table description. Should shift to 39-40 in v2.
curl -sf -X POST "http://127.0.0.1:$CF_FILE_PORT/api/file/comments?path=$CF_FILE_ENCODED" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 31, "end_line": 32,
    "body": "The description says \"complete rewrite\" but the SQL below looks like a straightforward new table. Is there a data migration from the old sessions table? That'\''s the risky part."
  }' > /dev/null

# C2: Line 67 — Backfill step. Should shift to 76 in v2.
curl -sf -X POST "http://127.0.0.1:$CF_FILE_PORT/api/file/comments?path=$CF_FILE_ENCODED" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 67, "end_line": 67,
    "body": "Backfilling from feature_flags is fragile — what if the flag was never set for some users? We need a default value strategy for missing entries."
  }' > /dev/null

# C3: Lines 75-79 — Rollback plan. This section is REMOVED in v2 → should be outdated.
curl -sf -X POST "http://127.0.0.1:$CF_FILE_PORT/api/file/comments?path=$CF_FILE_ENCODED" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 75, "end_line": 79,
    "body": "Step 3 says \"restore from backup if data corruption detected\" but how do we detect corruption? We need a verification query that runs post-swap to confirm data integrity before declaring success."
  }' > /dev/null

# C4: Line 85 — Performance section. Content is REWRITTEN in v2 → should be drifted.
curl -sf -X POST "http://127.0.0.1:$CF_FILE_PORT/api/file/comments?path=$CF_FILE_ENCODED" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 85, "end_line": 85,
    "body": "\"Will grow rapidly\" is too vague. How many rows per month? We need concrete numbers to size the partitions and set retention policies correctly."
  }' > /dev/null

# C5: Line 103 — Risks. Should shift to 112 in v2.
curl -sf -X POST "http://127.0.0.1:$CF_FILE_PORT/api/file/comments?path=$CF_FILE_ENCODED" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 103, "end_line": 103,
    "body": "Have we actually measured the lock duration for a table swap on our dataset size? The difference between \"brief\" and \"30 seconds\" matters a lot for a Saturday maintenance window."
  }' > /dev/null

# Finish to persist
curl -sf -X POST "http://127.0.0.1:$CF_FILE_PORT/api/finish" > /dev/null

# --- Seed carry-forward comments (git-mode) — same lines, same content ---
CF_GIT_PATH="plan.md"
CF_GIT_ENCODED=$(python3 -c "import urllib.parse; print(urllib.parse.quote('$CF_GIT_PATH'))")

curl -sf -X POST "http://127.0.0.1:$CF_GIT_PORT/api/file/comments?path=$CF_GIT_ENCODED" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 31, "end_line": 32,
    "body": "[git-mode] Sessions table: same comment as file-mode. Should shift to 39-40."
  }' > /dev/null

curl -sf -X POST "http://127.0.0.1:$CF_GIT_PORT/api/file/comments?path=$CF_GIT_ENCODED" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 67, "end_line": 67,
    "body": "[git-mode] Backfill step: should shift to 76."
  }' > /dev/null

curl -sf -X POST "http://127.0.0.1:$CF_GIT_PORT/api/file/comments?path=$CF_GIT_ENCODED" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 75, "end_line": 79,
    "body": "[git-mode] Rollback plan: this section is REMOVED in v2 — should be outdated."
  }' > /dev/null

curl -sf -X POST "http://127.0.0.1:$CF_GIT_PORT/api/file/comments?path=$CF_GIT_ENCODED" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 85, "end_line": 85,
    "body": "[git-mode] Performance: content is REWRITTEN in v2 — should be drifted."
  }' > /dev/null

curl -sf -X POST "http://127.0.0.1:$CF_GIT_PORT/api/file/comments?path=$CF_GIT_ENCODED" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 103, "end_line": 103,
    "body": "[git-mode] Risks: should shift to 112."
  }' > /dev/null

# Seed review-level comments on the git-mode carry-forward instance —
# exercises the Review Conversation section in git mode.
curl -sf -X POST "http://127.0.0.1:$CF_GIT_PORT/api/comments" \
  -H 'Content-Type: application/json' \
  -d '{
    "body": "[git-mode] General concern: the migration plan keeps pointing at \"Postgres\" without specifying which version. Some of the partitioning syntax assumes 13+.",
    "author": "reviewer"
  }' > /dev/null

# Pre-mark one as resolved by toggling via the resolve endpoint.
RC_RESOLVED=$(curl -sf -X POST "http://127.0.0.1:$CF_GIT_PORT/api/comments" \
  -H 'Content-Type: application/json' \
  -d '{
    "body": "[git-mode] Should we capture rollback steps as a runbook in addition to the plan? Easy to miss them under time pressure.",
    "author": "reviewer"
  }' | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")

curl -sf -X PUT "http://127.0.0.1:$CF_GIT_PORT/api/review-comment/$RC_RESOLVED/resolve" \
  -H 'Content-Type: application/json' \
  -d '{"resolved": true}' > /dev/null

curl -sf -X POST "http://127.0.0.1:$CF_GIT_PORT/api/finish" > /dev/null

# --- Folded-line comment on the code diff instance (#317) ---
# server.go has spacer gaps between hunks. Line 59 (respondJSON call in /version
# handler) falls in the gap between the /health hunk and the startup-code hunk.
# The fix auto-expands that spacer so the comment appears at its correct position.
curl -sf -X DELETE "http://127.0.0.1:$WORD_DIFF_PORT/api/comments" > /dev/null

FOLDED_C=$(curl -sf -X POST "http://127.0.0.1:$WORD_DIFF_PORT/api/file/comments?path=server.go" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 59, "end_line": 59,
    "body": "Should we version this via a build-time variable instead of hardcoding `1.0.0`? We already inject the version in main.go via ldflags."
  }' | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")

curl -sf -X POST "http://127.0.0.1:$WORD_DIFF_PORT/api/comment/$FOLDED_C/replies?path=server.go" \
  -H 'Content-Type: application/json' \
  -d '{
    "body": "Good catch — I'\''ll wire it up to the same `version` var. The `/version` endpoint will return the real build version instead of a hardcoded string.",
    "author": "agent"
  }' > /dev/null

# --- Instance 5: seed two layer-scope comments on b.txt + verify stamping ---
curl -sf -X DELETE "http://127.0.0.1:$RANGE_PORT/api/comments" > /dev/null

curl -sf -X POST "http://127.0.0.1:$RANGE_PORT/api/file/comments?path=b.txt" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 1, "end_line": 1, "side": "RIGHT",
    "body": "First range-mode comment — expect head_sha + diff_scope=layer on disk.",
    "author": "tester"
  }' > /dev/null

curl -sf -X POST "http://127.0.0.1:$RANGE_PORT/api/file/comments?path=b.txt" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 1, "end_line": 1, "side": "RIGHT",
    "body": "Second range-mode comment — same scope, different content.",
    "author": "tester"
  }' > /dev/null

# Verify on-disk stamping. /api/finish flushes the debounced writer so the
# review file is guaranteed present and includes our seeded comments.
RANGE_REVIEW_PATH=$(curl -sf -X POST "http://127.0.0.1:$RANGE_PORT/api/finish" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("review_file",""))')
if [ -n "$RANGE_REVIEW_PATH" ] && [ -f "$RANGE_REVIEW_PATH" ]; then
  if python3 - "$RANGE_REVIEW_PATH" "$RANGE_B_SHA" <<'PYEOF'
import json, sys
path, want_sha = sys.argv[1], sys.argv[2]
with open(path) as f:
    cj = json.load(f)
ok = True
for fp, cf in cj.get("files", {}).items():
    for c in cf.get("comments", []):
        if c.get("head_sha") != want_sha:
            print(f"FAIL: {fp} comment head_sha={c.get('head_sha')!r} want {want_sha!r}", file=sys.stderr)
            ok = False
        if c.get("diff_scope") != "layer":
            print(f"FAIL: {fp} comment diff_scope={c.get('diff_scope')!r} want 'layer'", file=sys.stderr)
            ok = False
if not ok:
    sys.exit(1)
print("Range comments are stamped with head_sha and diff_scope=layer")
PYEOF
  then
    echo "[Instance 5] PASS: on-disk stamping (head_sha + diff_scope=layer)"
  else
    echo "[Instance 5] FAIL: on-disk stamping check failed"
  fi
else
  echo "[Instance 5] WARN: review file not found at \"$RANGE_REVIEW_PATH\" — stamping check skipped"
fi

# --- Instance 6: synthesize stacked focus + per-scope comments + push-gate ---
curl -sf -X DELETE "http://127.0.0.1:$TOGGLE_PORT/api/comments" > /dev/null

# Promote the range to a "stacked PR" by setting is_stacked + default_sha.
# This is the only way the layer/full-stack toggle UI becomes visible without
# a real PR.
curl -sf -X POST "http://127.0.0.1:$TOGGLE_PORT/api/focus" \
  -H 'Content-Type: application/json' \
  -d "{\"kind\":\"range\",\"base_sha\":\"$TOGGLE_A_SHA\",\"head_sha\":\"$TOGGLE_B_SHA\",\"default_sha\":\"$TOGGLE_MAIN_SHA\",\"diff_scope\":\"layer\",\"is_stacked\":true}" > /dev/null

# Seed a layer-scope comment.
curl -sf -X POST "http://127.0.0.1:$TOGGLE_PORT/api/file/comments?path=b.txt" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 1, "end_line": 1, "side": "RIGHT",
    "body": "LAYER ONLY — visible only in layer scope.",
    "author": "tester"
  }' > /dev/null

# Flip to full-stack and seed a full-stack-scope comment.
curl -sf -X POST "http://127.0.0.1:$TOGGLE_PORT/api/focus" \
  -H 'Content-Type: application/json' \
  -d "{\"kind\":\"range\",\"base_sha\":\"$TOGGLE_A_SHA\",\"head_sha\":\"$TOGGLE_B_SHA\",\"default_sha\":\"$TOGGLE_MAIN_SHA\",\"diff_scope\":\"full_stack\",\"is_stacked\":true}" > /dev/null

curl -sf -X POST "http://127.0.0.1:$TOGGLE_PORT/api/file/comments?path=a.txt" \
  -H 'Content-Type: application/json' \
  -d '{
    "start_line": 1, "end_line": 1, "side": "RIGHT",
    "body": "FULL_STACK ONLY — visible only in full-stack scope.",
    "author": "tester"
  }' > /dev/null

# Push-gate assertion: while ActiveDiffScope=full_stack on disk, `crit push`
# must refuse with the gate-1 message. Wait for the 200ms scheduleWrite to
# flush ActiveDiffScope to disk.
sleep 1
PUSH_OUT=$(cd "$TOGGLE_DIR" && "$ROOT/$BINARY" push --dry-run 999 2>&1 || true)
if echo "$PUSH_OUT" | grep -q "Switch to Layer diff before posting a platform review"; then
  echo "[Instance 6] PASS: crit push refuses from full_stack scope (gate 1)"
elif echo "$PUSH_OUT" | grep -qE "gh CLI not found|gh is not authenticated"; then
  echo "[Instance 6] SKIP: crit push gate not exercised (gh unavailable). Output:"
  echo "$PUSH_OUT" | sed 's/^/    /'
else
  echo "[Instance 6] FAIL: crit push did NOT refuse from full_stack scope. Output:"
  echo "$PUSH_OUT" | sed 's/^/    /'
fi

# Flip back to layer for the reviewer to inspect (matches the "layer is the
# safe default for browsing" UX).
curl -sf -X POST "http://127.0.0.1:$TOGGLE_PORT/api/focus" \
  -H 'Content-Type: application/json' \
  -d "{\"kind\":\"range\",\"base_sha\":\"$TOGGLE_A_SHA\",\"head_sha\":\"$TOGGLE_B_SHA\",\"default_sha\":\"$TOGGLE_MAIN_SHA\",\"diff_scope\":\"layer\",\"is_stacked\":true}" > /dev/null

echo ""
echo "Servers running:"
echo "  1. Markdown diff:                 http://127.0.0.1:$PORT"
echo "  2. Code diff (word-level):        http://127.0.0.1:$WORD_DIFF_PORT"
echo "  3. Carry-forward (file-mode):     http://127.0.0.1:$CF_FILE_PORT"
echo "  4. Carry-forward (git-mode):      http://127.0.0.1:$CF_GIT_PORT"
echo "  5. Range mode (--range A..B):     http://127.0.0.1:$RANGE_PORT"
echo "  6. Stacked PR (layer/full-stack): http://127.0.0.1:$TOGGLE_PORT"
echo ""
echo "Instance 1 — GitHub-synced comment badge (#370):"
echo "  The overview paragraph (line 8) has a comment authored by 'octocat' that"
echo "  was synced from a GitHub PR — it shows a 'GitHub' pill in the header, and"
echo "  its reply does too. The first comment (Redis/SQS durability) also has a"
echo "  synced reply mixed in with the native replies. Native comments stay"
echo "  unbadged, so you can tell synced from native at a glance."
echo ""
echo "Instance 2 — folded-line comment (#317):"
echo "  server.go has a comment on line 59 (/version handler), which is in a"
echo "  spacer gap between the /health and startup-code hunks. The spacer should"
echo "  auto-expand so the comment + agent reply are visible inline."
echo "  The first spacer (respondJSON/logRequest) stays folded — no comments there."
echo "  Open the All Comments panel (Shift+C) and click the comment to scroll to it."
echo ""
echo "Carry-forward comments placed on v1 content (instances 3 & 4):"
echo "  C1 (lines 31-32): sessions table description"
echo "  C2 (line 67):     backfill migration step"
echo "  C3 (lines 75-79): rollback plan (REMOVED in v2)"
echo "  C4 (line 85):     performance section (REWRITTEN in v2)"
echo "  C5 (line 103):    risk about lock duration"
echo ""
echo "Instance 5 — Range mode (issue #300):"
echo "  - File list shows ONLY b.txt (B's diff, not c.txt from the C commit)"
echo "  - Both seeded comments are visible on b.txt with badge count = 2"
echo "  - Header label reads <short(A)>..<short(B)> ($(echo "$RANGE_A_SHA" | cut -c1-7)..$(echo "$RANGE_B_SHA" | cut -c1-7))"
echo "  - Open focus picker → Working tree + local stack entries appear"
echo "  - Click 'Working tree' → file list shows the working-tree diff;"
echo "    the seeded range comments DISAPPEAR (different scope)"
echo "  - Switch back to range → comments REAPPEAR (lossless toggling)"
echo ""
echo "Instance 6 — Stacked toggle (synthesized via /api/focus):"
echo "  - Diff-scope toggle popover visible (because is_stacked=true)"
echo "  - In layer scope: only 'LAYER ONLY' comment visible; file list = b.txt"
echo "  - Toggle to full-stack: only 'FULL_STACK ONLY' visible; file list"
echo "    expands to include a.txt as well"
echo "  - Both comments persist on disk regardless of toggle"
echo "  - The push-gate check above ran while in full_stack scope and asserted"
echo "    that 'crit push --dry-run 999' refuses with the gate-1 message"
echo ""
echo "Press Enter to simulate agent edits (swap v2 content + round-complete on instances 1-4)."
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

# --- Carry-forward: swap to v2 and round-complete ---
echo "Swapping carry-forward content to v2..."
cp test/carry-forward-v2.md "$CF_FILE"
cp test/carry-forward-v2.md "$CF_GIT_DIR/plan.md"
sleep 1.5

curl -sf -X POST "http://127.0.0.1:$CF_FILE_PORT/api/round-complete" > /dev/null
curl -sf -X POST "http://127.0.0.1:$CF_GIT_PORT/api/round-complete" > /dev/null

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
echo "Six views running:"
echo "  1. Markdown diff (inter-round):   http://127.0.0.1:$PORT"
echo "  2. Code diff (word-level):        http://127.0.0.1:$WORD_DIFF_PORT"
echo "  3. Carry-forward (file-mode):     http://127.0.0.1:$CF_FILE_PORT"
echo "  4. Carry-forward (git-mode):      http://127.0.0.1:$CF_GIT_PORT"
echo "  5. Range mode (--range A..B):     http://127.0.0.1:$RANGE_PORT"
echo "  6. Stacked PR (layer/full-stack): http://127.0.0.1:$TOGGLE_PORT"
echo ""
echo "Instance 1: diff view with resolved comments + threaded replies + deletion markers."
echo "            Comment #2 (resolved): 2 agent replies — visible when expanded."
echo "            Comment #4 (unresolved): 2 replies (agent + reviewer) — visible inline."
echo "            Comment #5 (on Code Standards heading): tests formatting near deletion markers."
echo "            GitHub-synced (#370): the 'octocat' comment on the overview carries a"
echo "            'GitHub' badge (header + reply); the durability comment has a synced reply."
echo "            Scroll to bottom: deletion markers interrupt the markdown code fence."
echo "Instance 2: word-level diff + folded-line comment (#317) + orphaned comments"
echo "            server.go: comment on line 59 should be at its correct position"
echo "            (spacer auto-expanded), NOT in an outdated section."
echo "            helpers.go was added then deleted — should appear as a phantom"
echo "            section with 'Removed' badge, 2 outdated comments (1 file-level,"
echo "            1 line-scoped), and full resolve/edit/delete support."
echo "Instances 3+4: carry-forward comment positioning after v1 → v2 content change."
echo "            Expected results (switch to Document view on instance 3):"
echo "              C1 (v1:31-32 → v2:39-40): 'sessions table' — should follow content down"
echo "              C2 (v1:67    → v2:76):     'Backfill'        — should follow content down"
echo "              C3 (v1:75-79):             Rollback plan     — REMOVED, should be outdated"
echo "              C4 (v1:85):                'grow rapidly'    — REWRITTEN, should be drifted"
echo "              C5 (v1:103   → v2:112):    'Large table'     — should follow content down"
echo "Instance 5: range mode (issue #300) — focus is pinned to A..B, not modified by"
echo "            this round. File list is still just b.txt. Use the focus picker"
echo "            to switch to working tree and back; range comments stay tagged with"
echo "            head_sha = $(echo "$RANGE_B_SHA" | cut -c1-7)."
echo "Instance 6: stacked toggle — the layer/full-stack diff-scope popover should"
echo "            still be visible. Toggle: layer shows LAYER ONLY on b.txt;"
echo "            full-stack shows FULL_STACK ONLY on a.txt. The push-gate"
echo "            verdict was printed during setup."
echo ""
echo "Press Enter to stop all servers."
read -r
