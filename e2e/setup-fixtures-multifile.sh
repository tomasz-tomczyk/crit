#!/usr/bin/env bash
set -euo pipefail

PORT="${1:-3127}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CRIT_SRC="$(cd "$SCRIPT_DIR/.." && pwd)"
DIR=$(realpath "$(mktemp -d)")
BIN_DIR=$(mktemp -d)
trap 'rm -rf "$DIR" "$BIN_DIR"' EXIT

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

git add -A && git commit -q -m "initial commit"

# Build crit binary outside the fixture dir (skip if CRIT_BIN is set)
if [ -z "${CRIT_BIN:-}" ]; then
  CRIT_BIN="$BIN_DIR/crit"
  (cd "$CRIT_SRC" && go build -o "$CRIT_BIN" .)
fi

# Isolate from user's ~/.crit.config.json
export HOME="$DIR"

# Run crit in file mode with explicit files AND a directory
# --share-url enables the Share button so E2E tests can exercise the share payload
exec "$CRIT_BIN" --no-open --quiet --port "$PORT" --share-url "http://localhost:19999" plan.md main.go handler.ex lib/
