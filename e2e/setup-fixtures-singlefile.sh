#!/usr/bin/env bash
set -euo pipefail

PORT="${1:-3125}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CRIT_SRC="$(cd "$SCRIPT_DIR/.." && pwd)"
DIR=$(mktemp -d)
BIN_DIR=$(mktemp -d)
trap 'rm -rf "$DIR" "$BIN_DIR"' EXIT

cd "$DIR"

# Init a git repo (people almost always run crit inside one)
git init -q
git config user.email "test@test.com"
git config user.name "Test"

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

git add -A && git commit -q -m "initial commit"

# Build crit binary outside the fixture dir (skip if CRIT_BIN is set)
if [ -z "${CRIT_BIN:-}" ]; then
  CRIT_BIN="$BIN_DIR/crit"
  (cd "$CRIT_SRC" && go build -o "$CRIT_BIN" .)
fi

# Run crit in single-file mode
exec "$CRIT_BIN" --no-open --quiet --port "$PORT" plan.md
