# Scripts

## e2e-share.sh

End-to-end integration tests for the crit CLI to crit-web share flow. Tests the full round-trip: sharing reviews, fetching web comments, re-sharing without duplicates, and unpublishing.

### Prerequisites

- A local crit-web checkout as a sibling directory (`../crit-web` relative to the crit repo root, or set `CRIT_WEB_DIR`)
- PostgreSQL running locally (the script creates a `crit_e2e` database)
- mise (for Go and Elixir toolchain management)

### Usage

```bash
# Full run: build crit, start crit-web on :4001, run tests, tear down
make e2e-share
# or directly:
./scripts/e2e-share.sh

# Start crit-web for manual testing (Ctrl+C to stop)
./scripts/e2e-share.sh --serve

# Run tests against an already-running crit-web
./scripts/e2e-share.sh --skip-web

# Run a specific test
./scripts/e2e-share.sh -run TestShareSyncFullLifecycle
```

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `CRIT_WEB_PORT` | `4001` | Port for the test crit-web instance |
| `CRIT_WEB_DIR` | `../crit-web` | Path to crit-web checkout |
| `DB_NAME` | `crit_e2e` | PostgreSQL database name (separate from dev) |

### What the script does

1. Builds crit via `make build`
2. Creates/resets the `crit_e2e` database
3. Starts crit-web with `SELFHOSTED=true` on the test port (no OAuth)
4. Waits for `GET /health` to return 200
5. Runs all `TestShareSync*` integration tests with `CRIT_AUTH_TOKEN=""` to bypass any local auth config
6. Tears down crit-web on exit

### Test file: `share_integration_test.go`

Build tag: `//go:build integration` — these tests are excluded from `go test ./...` and only run via `go test -tags integration`.

Tests use `--output <dir>` to write `.crit.json` to a temp directory (not `~/.crit/reviews/`), and `--share-url` to point at the local crit-web instance.

Every test logs its review URL (`t.Logf("  -> Review: ...")`) so you can open them in a browser for visual inspection. Reviews persist on crit-web after tests run (except the unpublish test).

#### Test cases

| Test | What it covers |
|---|---|
| `TestShareSyncIntegration` | Original: share, seed web comment, re-share, verify content + export |
| `TestShareSyncNoComments` | Share with zero comments, verify document on web |
| `TestShareSyncLineComments` | Line-scoped comments: body, position, scope verified on web |
| `TestShareSyncFileComment` | File-scoped comment: body, scope, file_path verified on web |
| `TestShareSyncReviewLevelComments` | Review-level comments shared (tests #297 fix for CLI path) |
| `TestShareSyncMixedCommentTypes` | All 3 scopes together, each verified on web |
| `TestShareSyncResolvedExcluded` | Resolved comments filtered out of share payload |
| `TestShareSyncReshareNoDuplicates` | Re-share preserves comments without duplication |
| `TestShareSyncReshareNoChanges` | No-op when content unchanged (round stays same) |
| `TestShareSyncFetchWebComments` | Web-authored comments pulled into local .crit.json |
| `TestShareSyncFetchWebCommentsNoDuplicates` | Repeated syncs don't duplicate web comments |
| `TestShareSyncMultipleFiles` | Multi-file share with per-file comment association |
| `TestShareSyncMultipleRounds` | Round progression across 3 share cycles, content verified |
| `TestShareSyncCommentWithReplies` | Threaded replies included in share |
| `TestShareSyncUnpublish` | Full unpublish: web deletion + local state cleared |
| `TestShareSyncExport` | Export endpoint returns .crit.json-compatible shape |
| `TestShareSyncFetchReviewLevelWebComment` | Review-level web comments merged into local ReviewComments |
| `TestShareSyncFullLifecycle` | Complete round-trip: local comments with threads, share, web comments added, fetch, re-share (preserved), fetch again (no duplicates) |

#### Adding new tests

- Name test functions `TestShareSync*` so they're picked up by the `-run TestShareSync` filter
- Use the helpers: `critShareCmd`, `critUnpublishCmd`, `writeTestCritJSON`, `readCritJSON`, `commentsFromAPI`, `documentFromAPI`, `seedComment`, `seedCommentAt`, `seedReviewComment`, `logReview`, `extractToken`
- Always call `logReview(t, output)` after sharing so the URL is visible in test output
- Use `writeTestCritJSON` (not `writeCritJSON` — that name conflicts with `github.go`)
