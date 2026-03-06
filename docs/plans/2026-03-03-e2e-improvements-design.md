# E2E Test Suite Improvements

## Problem

The E2E suite (250+ tests, 27 files) has significant structural issues: ~500 lines of duplicated helpers across files, 9+ hardcoded `waitForTimeout` calls creating flakiness, single-worker serial execution, and no coverage for multi-file/directory file mode with code files.

## Phase 1: Structural Cleanup

### 1. Extract shared helpers into `e2e/tests/helpers.ts`

All copy-pasted helpers consolidated into one import. Functions:

- `clearAllComments(request)` — single `DELETE /api/comments` call
- `loadPage(page)` — navigate + wait for loading hidden
- `mdSection(page)`, `goSection(page)`, `jsSection(page)` — file section locators
- `switchToDocumentView(page)` — click toggle + wait
- `dragBetween(page, start, end)` — mousedown/move/up
- `clearFocus(page)` — click body

Update all 27 test files to import. ~500 lines removed.

### 2. Add `DELETE /api/comments` endpoint

- `ClearAllComments()` on Session: iterate files, set `Comments = []Comment{}`
- Handler in server.go, registered as `DELETE /api/comments`
- Unit test in server_test.go
- Replaces the N+1 API call pattern in `clearAllComments` helper

### 3. Eliminate waitForTimeout calls

9 instances across 3 files:

| File | Count | Current | Replacement |
|---|---|---|---|
| draft-autosave.spec.ts | 5 | `waitForTimeout(700)` | `toPass()` polling localStorage |
| round-complete.spec.ts | 4 | `setTimeout(500)` | Poll session API for round increment |
| scope-toggle.spec.ts | 2 | `waitForTimeout(500)`, `waitForTimeout(300)` | Wait for active class + section visible |

### 4. Enable parallel project execution

Change `workers: 1` → `workers: 4`. The 4 projects use different ports (3123-3126) and can run concurrently. Keep `fullyParallel: false` within each project since tests share server state.

## Phase 2: New Coverage

### 5. Multi-file + directory file-mode fixture

New fixture `setup-fixtures-multifile.sh`:

- `plan.md` — markdown file
- `main.go` — Go source
- `handler.ex` — Elixir source
- `lib/` subdirectory with another file

New Playwright project `multi-file-mode` on port 3127. Crit invoked with explicit file + directory args.

Tests (`*.multifile.spec.ts`):

- File tree rendering with mixed types
- Code files render in document view
- Commenting on code file lines
- Directory contents visible
- Stats reflect all files

## Not included

- data-testid migration — CSS classes are domain-semantic, low ROI
- Keyboard loop optimization — loops test real navigation behavior
- Theme test deduplication — parameterized shared functions add complexity
- no-git-mode cleanup — intentionally re-runs file-mode tests in non-git context
