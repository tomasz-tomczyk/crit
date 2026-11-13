#!/usr/bin/env bash
set -euo pipefail

PORT="${1:-3127}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CRIT_SRC="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
DIR=$(e2e_native_tempdir)
BIN_DIR=$(mktemp -d)
e2e_cleanup_install
e2e_cleanup_add "$DIR" "$BIN_DIR"

cd "$DIR"

# Init a git repo (file mode inside a git repo, mirrors real usage)
git init -q
git config user.email "test@test.com"
git config user.name "Test"

# === Markdown file ===
cat > plan.md << 'MDFILE'
# Migration Plan

## Overview

Migrating the database from PostgreSQL to CockroachDB.

## Steps

1. Audit current schema
2. Test compatibility
3. Run migration scripts
4. Validate data integrity

## Notes

> CockroachDB is wire-compatible with PostgreSQL but has some differences.

```sql
SELECT * FROM users WHERE created_at > NOW() - INTERVAL '30 days';
```
MDFILE

# === Go file ===
cat > main.go << 'GOFILE'
package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello, %s!", r.URL.Path[1:])
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	fmt.Printf("Listening on :%s\n", port)
	http.ListenAndServe(":"+port, nil)
}
GOFILE

# === Elixir file ===
cat > handler.ex << 'EXFILE'
defmodule MyApp.Handler do
  @moduledoc """
  HTTP request handler for the notification service.
  """

  alias MyApp.Notifications

  def handle_request(%{method: "POST", path: "/notify"} = conn) do
    case Jason.decode(conn.body) do
      {:ok, %{"user_id" => user_id, "message" => message}} ->
        notification = Notifications.create(user_id, message)
        send_json(conn, 201, notification)

      {:error, _reason} ->
        send_json(conn, 400, %{error: "Invalid JSON"})
    end
  end

  def handle_request(%{method: "GET", path: "/health"} = conn) do
    send_json(conn, 200, %{status: "ok"})
  end

  def handle_request(conn) do
    send_json(conn, 404, %{error: "Not found"})
  end

  defp send_json(conn, status, body) do
    conn
    |> put_resp_content_type("application/json")
    |> send_resp(status, Jason.encode!(body))
  end
end
EXFILE

# === Subdirectory with files ===
mkdir -p lib

cat > lib/utils.ex << 'EXFILE'
defmodule MyApp.Utils do
  @moduledoc "Utility functions."

  def capitalize(s) when is_binary(s) do
    String.capitalize(s)
  end

  def truncate(s, max_length) when is_binary(s) and is_integer(max_length) do
    if String.length(s) > max_length do
      String.slice(s, 0, max_length) <> "..."
    else
      s
    end
  end
end
EXFILE

cat > lib/config.ex << 'EXFILE'
defmodule MyApp.Config do
  @moduledoc "Application configuration helpers."

  def get_env(key, default \\ nil) do
    System.get_env(key) || default
  end

  def port do
    get_env("PORT", "4000") |> String.to_integer()
  end

  def environment do
    get_env("MIX_ENV", "dev")
  end
end
EXFILE

# Mermaid file (.mmd, mermaid.js.org's recommended extension): rendered by the
# built-in pluggable renderer with a Diagram / Source toggle.
cat > flow.mmd << 'MMDFILE'
flowchart TD
  Start[Receive request] --> Auth{Authorized?}
  Auth -->|yes| Handle[Handle request]
  Auth -->|no| Reject[Reject]
MMDFILE

# Sequence diagram labels are SVG <text>, not HTML in a foreignObject.
cat > seq.mmd << 'MMDFILE'
sequenceDiagram
  Client->>Server: Send request
  Server-->>Client: Reply
MMDFILE

# Excalidraw scene (.excalidraw): rendered by the built-in Excalidraw renderer
# with a Diagram / Source toggle; labels are text elements bound to shapes, in
# Excalifont, a freedraw stroke, a sticky note and an ER-style arrow.
cat > flow.excalidraw << 'EXCALIDRAWFILE'
{
  "type": "excalidraw",
  "version": 2,
  "source": "https://excalidraw.com",
  "elements": [
    {
      "id": "gw",
      "type": "rectangle",
      "x": 0,
      "y": 0,
      "width": 200,
      "height": 80,
      "strokeColor": "#1971c2",
      "backgroundColor": "#a5d8ff",
      "roundness": { "type": 3 },
      "seed": 101
    },
    {
      "id": "gw-label",
      "type": "text",
      "x": 10,
      "y": 27.5,
      "width": 180,
      "height": 25,
      "text": "API gateway",
      "fontSize": 20,
      "textAlign": "center",
      "containerId": "gw",
      "seed": 102
    },
    {
      "id": "call",
      "type": "arrow",
      "x": 200,
      "y": 40,
      "points": [[0, 0], [120, 0]],
      "endArrowhead": "arrow",
      "seed": 103
    },
    {
      "id": "db",
      "type": "ellipse",
      "x": 320,
      "y": 0,
      "width": 160,
      "height": 80,
      "strokeColor": "#2f9e44",
      "backgroundColor": "#b2f2bb",
      "seed": 104
    },
    {
      "id": "db-label",
      "type": "text",
      "x": 330,
      "y": 27.5,
      "width": 140,
      "height": 25,
      "text": "Orders DB",
      "fontSize": 20,
      "textAlign": "center",
      "containerId": "db",
      "seed": 105
    },
    {
      "id": "scribble",
      "type": "freedraw",
      "x": 40,
      "y": 120,
      "strokeColor": "#e03131",
      "points": [[0, 0], [20, 10], [40, 4], [60, 18], [80, 6], [100, 14]],
      "pressures": [],
      "simulatePressure": true,
      "seed": 106
    },
    {
      "id": "note",
      "type": "stickynote",
      "x": 520,
      "y": 120,
      "width": 160,
      "height": 120,
      "baseHeight": 120,
      "roughness": 1,
      "roundness": { "type": 3 },
      "created": 1788800000000,
      "seed": 107
    },
    {
      "id": "note-label",
      "type": "text",
      "x": 536,
      "y": 155,
      "width": 128,
      "height": 25,
      "text": "Cache reads?",
      "fontSize": 20,
      "textAlign": "center",
      "containerId": "note",
      "seed": 108
    },
    {
      "id": "relation",
      "type": "arrow",
      "x": 400,
      "y": 80,
      "points": [[0, 0], [0, 60], [12, 60]],
      "startArrowhead": "cardinality_one",
      "endArrowhead": "cardinality_zero_or_many",
      "seed": 109
    }
  ],
  "appState": { "viewBackgroundColor": "#ffffff" },
  "files": {}
}
EXCALIDRAWFILE

git add -A && git commit -q -m "initial commit"

# Build crit binary outside the fixture dir (skip if CRIT_BIN is set)
if [ -z "${CRIT_BIN:-}" ]; then
  CRIT_BIN="$BIN_DIR/$(e2e_bin_name)"
  (cd "$CRIT_SRC" && go build -o "$CRIT_BIN" ./cmd/crit)
fi

# Isolate from user's ~/.crit.config.json (and USERPROFILE on Windows).
e2e_export_fake_home "$DIR"

# Run crit in file mode with explicit files AND a directory
# --share-url enables the Share button so E2E tests can exercise the share payload
e2e_serve "$CRIT_BIN" _serve --no-open --port "$PORT" --share-url "http://localhost:19999" plan.md main.go handler.ex flow.mmd seq.mmd flow.excalidraw lib/
