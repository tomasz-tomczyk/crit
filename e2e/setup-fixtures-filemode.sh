#!/usr/bin/env bash
set -euo pipefail

PORT="${1:-3124}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CRIT_SRC="$(cd "$SCRIPT_DIR/.." && pwd)"
DIR=$(mktemp -d)
BIN_DIR=$(mktemp -d)
trap 'rm -rf "$DIR" "$BIN_DIR"' EXIT

cd "$DIR"

# === Create files directly (no git) — this is "file mode" ===

cat > plan.md << 'MDFILE'
# Authentication Plan

## Overview

We're adding API key authentication to the server. This is phase 1 of the auth system.

## Design Decisions

| Decision | Options | Chosen | Rationale |
|----------|---------|--------|-----------|
| Auth method | OAuth, API keys, JWT | API keys | Simplest for M2M |
| Key storage | Env var, database | Database | Supports rotation |
| Header format | Basic, Bearer | Bearer | Industry standard |

## Implementation Steps

1. Add auth middleware
2. Create API key model
3. Add key validation endpoint
4. Write integration tests

### Step 1: Auth Middleware

The middleware checks for a `Bearer` token in the `Authorization` header:

```go
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        key := r.Header.Get("Authorization")
        if !strings.HasPrefix(key, "Bearer ") {
            http.Error(w, "unauthorized", 401)
            return
        }
        next(w, r)
    }
}
```

### Step 2: API Key Model

- [ ] Create migration for `api_keys` table
- [ ] Add CRUD operations
- [x] Define key format: `ck_` prefix + 32 random bytes

## Open Questions

> Should we rate-limit by API key or by IP?
> Leaning toward API key since we want per-tenant limits.

## Timeline

- **Week 1**: Middleware + key model
- **Week 2**: Validation endpoint + tests
- **Week 3**: Dashboard UI for key management
MDFILE

cat > server.go << 'GOFILE'
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

	log.Printf("Server starting on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
GOFILE

cat > handler.js << 'JSFILE'
// Request handler for the notification service
export function handleNotification(req, res) {
  const { userId, message, channel } = req.body;

  if (!userId || !message) {
    return res.status(400).json({ error: 'Missing required fields' });
  }

  const notification = {
    id: crypto.randomUUID(),
    userId,
    message,
    channel: channel || 'email',
    createdAt: new Date().toISOString(),
  };

  queue.push(notification);
  res.status(201).json(notification);
}
JSFILE

# Build crit binary outside the fixture dir (skip if CRIT_BIN is set)
if [ -z "${CRIT_BIN:-}" ]; then
  CRIT_BIN="$BIN_DIR/crit"
  (cd "$CRIT_SRC" && go build -o "$CRIT_BIN" .)
fi

# Run crit in file mode (explicit files, no git detection)
exec "$CRIT_BIN" --no-open --port "$PORT" plan.md server.go handler.js
