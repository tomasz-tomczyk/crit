#!/usr/bin/env bash
set -euo pipefail

PORT="${1:-3123}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CRIT_SRC="$(cd "$SCRIPT_DIR/.." && pwd)"
DIR=$(mktemp -d)
BIN_DIR=$(mktemp -d)
trap 'rm -rf "$DIR" "$BIN_DIR"' EXIT

cd "$DIR"
git init -q
git config user.email "test@test.com"
git config user.name "Test"

# === Initial commit: files that will be "modified" or "deleted" ===

cat > server.go << 'GOFILE'
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
GOFILE

cat > deleted.txt << 'EOF'
This file will be deleted.
It has some content that used to matter.
But now it's gone.
EOF

cat > utils.go << 'GOFILE'
package main

import "strings"

func Capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
GOFILE

git add -A
git commit -q -m "initial commit"

# === Feature branch: modifications ===

git checkout -q -b feat/add-auth

# Modify server.go significantly to produce multi-hunk diff
# Hunk 1: change imports. Hunk 2: add authMiddleware. Hunk 3: modify main (wraps handler, changes startup).
# The unchanged helper functions and /version, /ready handlers create gaps between hunks.
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
GOFILE

# Delete deleted.txt
rm deleted.txt

# Add plan.md with comprehensive markdown
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

# Add handler.js (new file, all-addition diff)
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

git add -A
git commit -q -m "feat: add auth middleware and plan"

# Build crit binary outside the repo (skip if CRIT_BIN is set)
if [ -z "${CRIT_BIN:-}" ]; then
  CRIT_BIN="$BIN_DIR/crit"
  (cd "$CRIT_SRC" && go build -o "$CRIT_BIN" .)
fi

# Run crit in the fixture repo
exec "$CRIT_BIN" --no-open --port "$PORT"
