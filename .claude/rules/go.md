---
paths:
  - "*.go"
  - "**/*.go"
---

# Go Code Rules (semantic — linters handle syntax/style)

## Struct & State Integrity
- When building structs for serialization or cross-boundary transfer, verify ALL source fields are mapped. Missing fields cause silent data loss.
- For boolean config fields, always use presence tracking to distinguish "not set" from "set to false". Go's zero-value for bool is `false`, which is ambiguous.
- When clearing/resetting state from a struct, enumerate ALL fields. Review comments, file comments, and metadata must all be cleared.
- When storing error values in shared state (atomic.Value, channels), copy the value first: `e := err; store(&e)`.

## Code Organization
- This is `package main`. Do NOT export functions/types unless required by tests in `_test.go` files.
- Never create wrapper functions that just call another function with the same signature.
- Route all review file writes through `saveCritJSON()`. Never write directly.
- Before implementing logic inline, search for existing helper functions that do the same thing.
- After replacing a function with a new implementation, delete the old function AND its tests in the same PR.

## Git Operations
- When determining file status, always diff against baseRef consistently. Don't mix `git status --porcelain` (working tree) with `git diff --name-status` (branch diff).

## Daemon/Server
- When checking if a service is alive, validate the response body, not just HTTP status.
- Error paths in CLI subcommands must exit with non-zero status (`os.Exit(1)`).

## Testing
- Use table-driven tests instead of separate test functions for parameter variations.
- When adding a new API endpoint, add tests for: happy path, error cases, not-found, method-not-allowed.
