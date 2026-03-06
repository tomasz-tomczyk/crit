# E2E Test Suite Improvements — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Improve the E2E test suite's maintainability, reliability, and coverage by extracting shared helpers, adding a bulk comment delete endpoint, eliminating flaky timeouts, enabling parallel execution, and adding multi-file/directory file-mode tests.

**Architecture:** Phase 1 does structural cleanup (server endpoint + helpers extraction + timeout fixes + config). Phase 2 adds a new fixture and test project for multi-file/directory file mode with Go and Elixir code files.

**Tech Stack:** Go (server endpoint + test), TypeScript/Playwright (E2E), Bash (fixture script)

---

### Task 0: Baseline — run full E2E suite and capture timing

**Step 1: Run the full E2E suite and record the time**

Run: `cd e2e && time bash run.sh 2>&1 | tail -20`

Record:
- Total wall-clock time
- Number of tests passed / failed / skipped
- Any existing failures (note what they are)

**Step 2: Fix any existing failures before proceeding**

If any tests fail, fix them first. The structural cleanup must start from a green baseline.

**Step 3: Save the baseline**

Note the timing in the commit message of the next task so we can compare after enabling parallel workers.

---

### Task 1: Add `DELETE /api/comments` endpoint — Go implementation

**Files:**
- Modify: `session.go` (add `ClearAllComments` method)
- Modify: `server.go:35-56` (add handler + route)

**Step 1: Add `ClearAllComments` method to Session**

In `session.go`, add after the existing `DeleteComment` method (~line 416):

```go
// ClearAllComments removes all comments from all files.
func (s *Session) ClearAllComments() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.Files {
		f.Comments = []Comment{}
		f.nextID = 1
	}
}
```

**Step 2: Add handler in server.go**

Add after the `handleCommentByID` method (after the `/api/comment/` handler block):

```go
// handleClearComments deletes all comments across all files.
// DELETE /api/comments
func (s *Server) handleClearComments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.session.ClearAllComments()
	writeJSON(w, map[string]string{"status": "ok"})
}
```

**Step 3: Register the route**

In `NewServer`, add this line after the `/api/comment/` route (line 49):

```go
mux.HandleFunc("/api/comments", s.handleClearComments)
```

**Step 4: Run Go tests to verify nothing broke**

Run: `go test ./...`
Expected: All existing tests pass

**Step 5: Commit**

```
feat: add DELETE /api/comments endpoint for bulk comment cleanup
```

---

### Task 2: Add Go unit test for `DELETE /api/comments`

**Files:**
- Modify: `server_test.go`

**Step 1: Write the test**

Add to `server_test.go`:

```go
func TestDeleteAllComments(t *testing.T) {
	s, session := newTestServer(t)

	// Add comments to the test file
	session.AddComment("test.md", 1, 1, "", "Comment 1")
	session.AddComment("test.md", 2, 2, "", "Comment 2")

	// Verify comments exist
	comments := session.GetComments("test.md")
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}

	// DELETE /api/comments
	req := httptest.NewRequest("DELETE", "/api/comments", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	// Verify all comments are gone
	comments = session.GetComments("test.md")
	if len(comments) != 0 {
		t.Errorf("expected 0 comments after delete, got %d", len(comments))
	}
}

func TestDeleteAllComments_MethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/comments", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}
```

**Step 2: Run tests**

Run: `go test ./... -run TestDeleteAllComments -v`
Expected: Both tests pass

**Step 3: Commit**

```
test: add unit tests for DELETE /api/comments endpoint
```

---

### Task 3: Create shared helpers file

**Files:**
- Create: `e2e/tests/helpers.ts`

**Step 1: Create the helpers file**

```typescript
import { expect, type Page, type APIRequestContext } from '@playwright/test';

/**
 * Clear all comments via bulk DELETE endpoint.
 */
export async function clearAllComments(request: APIRequestContext) {
  await request.delete('/api/comments');
}

/**
 * Navigate to / and wait for the loading spinner to disappear.
 */
export async function loadPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
}

/**
 * Scope selectors to the plan.md file section.
 */
export function mdSection(page: Page) {
  return page.locator('.file-section').filter({ hasText: 'plan.md' });
}

/**
 * Scope selectors to the server.go file section.
 */
export function goSection(page: Page) {
  return page.locator('#file-section-server\\.go');
}

/**
 * Scope selectors to the handler.js file section.
 */
export function jsSection(page: Page) {
  return page.locator('#file-section-handler\\.js');
}

/**
 * Switch plan.md to document view (markdown defaults to diff in git mode).
 */
export async function switchToDocumentView(page: Page) {
  const section = mdSection(page);
  await expect(section).toBeVisible();
  const docBtn = section.locator('.file-header-toggle .toggle-btn[data-mode="document"]');
  await expect(docBtn).toBeVisible();
  await docBtn.click();
  await expect(section.locator('.document-wrapper')).toBeVisible();
}

/**
 * Simulate a click-and-drag between two elements (for gutter range selection).
 */
export async function dragBetween(page: Page, startLocator: any, endLocator: any) {
  const startBox = await startLocator.boundingBox();
  const endBox = await endLocator.boundingBox();
  if (!startBox || !endBox) throw new Error('Could not get bounding boxes for drag');

  await page.mouse.move(startBox.x + startBox.width / 2, startBox.y + startBox.height / 2);
  await page.mouse.down();
  await page.mouse.move(endBox.x + endBox.width / 2, endBox.y + endBox.height / 2, { steps: 5 });
  await page.mouse.up();
}

/**
 * Click the page body to clear any focused element.
 */
export async function clearFocus(page: Page) {
  await page.click('body', { position: { x: 0, y: 0 } });
}

/**
 * Find a non-crit.json file path from the session (for round-complete tests).
 */
export async function getTestFilePath(request: APIRequestContext): Promise<string> {
  const sessionRes = await request.get('/api/session');
  const session = await sessionRes.json();
  const file = session.files.find((f: any) => f.path !== '.crit.json' && f.status !== 'deleted');
  return file?.path || session.files[0].path;
}

/**
 * Wait for a condition to become true by polling, using Playwright's toPass.
 * Replaces waitForTimeout for debounced operations.
 */
export async function waitForLocalStorageDraft(page: Page, expectedCount: number, timeout = 2000) {
  await expect(async () => {
    const count = await page.evaluate(() =>
      Object.keys(localStorage).filter(k => k.startsWith('crit-draft-')).length
    );
    expect(count).toBe(expectedCount);
  }).toPass({ timeout });
}

/**
 * Wait for the session to reach a specific review round (polling).
 */
export async function waitForRound(request: APIRequestContext, expectedRound: number, timeout = 3000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const session = await request.get('/api/session').then(r => r.json());
    if (session.review_round === expectedRound) return;
    await new Promise(r => setTimeout(r, 100));
  }
  // Final assertion for clear error message
  const session = await request.get('/api/session').then(r => r.json());
  expect(session.review_round).toBe(expectedRound);
}
```

**Step 2: Verify the file compiles**

Run: `cd e2e && npx tsc --noEmit tests/helpers.ts 2>&1 || echo "OK - tsc may not be configured for standalone, will verify in test run"`

**Step 3: Commit**

```
refactor: create shared E2E test helpers file
```

---

### Task 4: Update all test files to use shared helpers

**Files:**
- Modify: All 29 test files in `e2e/tests/`

This is a mechanical find-and-replace refactoring — no subagents needed. Process each file inline: add the import, delete the local definition, move on. The repetitive nature makes it fast to do in one pass.

For each test file:

1. Add `import { ... } from './helpers';` at the top with the helpers that file uses
2. Remove the local function definitions that are now imported
3. For `clearAllComments` — all 14 files that define it: the old N+1 implementation is removed, the import uses the new single-API-call version
4. For `round-complete.spec.ts` — the `clearAllComments` had a special `setTimeout(300)` after deleting. With the bulk endpoint, this is no longer needed.

**Important notes per file:**
- `drag-selection.spec.ts` / `drag-selection.filemode.spec.ts`: The local `dragBetween` function has a slightly different signature (it takes locators). Verify the shared version matches. Both have the same mouse-based implementation.
- `comments.spec.ts`: Uses `mdSection`, `goSection`, `jsSection`, `switchToDocumentView`, `clearAllComments`, `loadPage` — all 6 go to import
- `round-complete.spec.ts`: Uses `loadPage`, `clearAllComments`, `getTestFilePath` — replace `clearAllComments` (removes the setTimeout(300) baked in), replace `getTestFilePath`
- `scope-toggle.spec.ts`: Only uses `loadPage`
- Files that define their own `mdSection` differently (e.g., file-mode files that omit `.filter({ hasText: 'plan.md' })`): **DO NOT** import `mdSection` for these — they may need a different selector. Only import what matches exactly.

**Step 1: Update each file mechanically**

For each of the 29 files, replace local helper definitions with imports. Example for `comments.spec.ts`:

```typescript
// BEFORE (lines 1-50):
import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

async function clearAllComments(request: APIRequestContext) { ... }
async function loadPage(page: Page) { ... }
function mdSection(page: Page) { ... }
async function switchToDocumentView(page: Page) { ... }
function goSection(page: Page) { ... }
function jsSection(page: Page) { ... }

// AFTER:
import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, goSection, jsSection, switchToDocumentView } from './helpers';
```

**Step 2: Run the full E2E suite**

Run: `make e2e`
Expected: All tests pass. If any fail, check that the imported helper signature matches what the test expects.

**Step 3: Commit**

```
refactor: use shared helpers across all E2E test files
```

---

### Task 5: Eliminate `waitForTimeout` in draft-autosave.spec.ts

**Files:**
- Modify: `e2e/tests/draft-autosave.spec.ts`

**Step 1: Replace all 5 `waitForTimeout(700)` calls**

Replace each instance of:
```typescript
await page.waitForTimeout(700);
```

With the imported helper:
```typescript
await waitForLocalStorageDraft(page, 1);
```

For the specific tests:

- **'typing in comment form saves draft to localStorage'** (line 62): Replace `waitForTimeout(700)` → `await waitForLocalStorageDraft(page, 1);`
- **'draft is restored on page reload with toast notification'** (line 91): Same replacement
- **'submitting comment clears the draft'** (line 120): Replace first `waitForTimeout(700)` → `await waitForLocalStorageDraft(page, 1);` (waiting for draft to exist before submitting)
- **'cancelling comment clears the draft'** (line 150): Same
- **'pressing Escape clears the draft'** (line 173): Same

Also add `waitForLocalStorageDraft` to the import from `./helpers`.

**Step 2: Run the draft-autosave tests**

Run: `cd e2e && npx playwright test tests/draft-autosave.spec.ts -v`
Expected: All 6 tests pass

**Step 3: Commit**

```
fix(e2e): replace waitForTimeout with polling in draft-autosave tests
```

---

### Task 6: Eliminate `setTimeout` in round-complete.spec.ts

**Files:**
- Modify: `e2e/tests/round-complete.spec.ts`

**Step 1: Replace all 4 `setTimeout(500)` calls in API tests**

The pattern in each API test is:
```typescript
await request.post('/api/round-complete');
await new Promise(r => setTimeout(r, 500));
session = await request.get('/api/session').then(r => r.json());
```

Replace with:
```typescript
await request.post('/api/round-complete');
await waitForRound(request, startRound + 1);
```

For each test that needs this:
- **'POST /api/round-complete increments the round'** (line 87): Replace setTimeout + session check with `waitForRound`
- **'round-complete carries forward unresolved comments'** (line 107): Same pattern
- **'round-complete carries forward resolved comments with resolved fields'** (line 141): Same
- **'file list is preserved after round-complete'** (line 157): Same

Also add `waitForRound` to the import from `./helpers`.

**Also update `clearAllComments` import** — the old local version had `await new Promise(r => setTimeout(r, 300));` baked in. The imported bulk delete version doesn't need this since it's a single synchronous server operation.

**Step 2: Run the round-complete tests**

Run: `cd e2e && npx playwright test tests/round-complete.spec.ts -v`
Expected: All tests pass

**Step 3: Commit**

```
fix(e2e): replace setTimeout with polling in round-complete tests
```

---

### Task 7: Eliminate `waitForTimeout` in scope-toggle.spec.ts

**Files:**
- Modify: `e2e/tests/scope-toggle.spec.ts`

**Step 1: Fix `switchScope` helper (line 8-16)**

Replace:
```typescript
async function switchScope(page: Page, scope: string) {
  const responsePromise = page.waitForResponse(resp =>
    resp.url().includes('/api/session') && resp.status() === 200
  );
  await page.click(`#scopeToggle .toggle-btn[data-scope="${scope}"]`);
  await responsePromise;
  // Wait for rendering to complete
  await page.waitForTimeout(500);
}
```

With:
```typescript
async function switchScope(page: Page, scope: string) {
  const responsePromise = page.waitForResponse(resp =>
    resp.url().includes('/api/session') && resp.status() === 200
  );
  await page.click(`#scopeToggle .toggle-btn[data-scope="${scope}"]`);
  await responsePromise;
  // Wait for rendering to complete
  await expect(page.locator(`#scopeToggle .toggle-btn[data-scope="${scope}"]`)).toHaveClass(/active/);
  await expect(page.locator('.file-section').first()).toBeVisible();
}
```

**Step 2: Fix 'clicking a disabled scope button does nothing' test (line 147)**

Replace:
```typescript
await page.click('#scopeToggle .toggle-btn[data-scope="staged"]', { force: true });
await page.waitForTimeout(300);
```

With:
```typescript
await page.click('#scopeToggle .toggle-btn[data-scope="staged"]', { force: true });
// No need to wait — disabled button should have no effect.
// Just assert the state hasn't changed.
```

**Step 3: Add import of `loadPage` from helpers (and remove local definition)**

**Step 4: Run scope-toggle tests**

Run: `cd e2e && npx playwright test tests/scope-toggle.spec.ts -v`
Expected: All tests pass

**Step 5: Commit**

```
fix(e2e): replace waitForTimeout with DOM waits in scope-toggle tests
```

---

### Task 8: Enable parallel project execution and measure improvement

**Files:**
- Modify: `e2e/playwright.config.ts:13`

**Step 1: Change workers from 1 to 4**

```typescript
// BEFORE:
workers: 1,

// AFTER:
workers: 4,
```

This is safe because:
- Each project runs against a different server on a different port (3123-3127)
- `fullyParallel: false` remains, so tests within each project still run serially
- The `run.sh` script already starts all fixture servers in parallel

**CI compatibility:** GitHub Actions (`ubuntu-latest`) has 2-4 vCPUs. Playwright manages worker scheduling automatically — 4 workers on 2 cores just means some context switching, not failure. The `run.sh` script already starts all fixture servers in parallel before invoking Playwright, so CI needs no changes. The CI workflow (`.github/workflows/test.yml`) runs `bash run.sh` which calls `npx playwright test` — the `workers` setting from config applies automatically.

**Step 2: Run the full suite and compare to baseline**

Run: `cd e2e && time bash run.sh 2>&1 | tail -20`

Compare wall-clock time to the baseline captured in Task 0. Note the improvement in the commit message.

Expected: All tests pass. Suite should run noticeably faster (roughly 2-3x, depending on how evenly tests are distributed across projects).

**Step 3: Commit**

Include baseline vs new timing in the commit message:

```
perf(e2e): enable parallel project execution (workers: 4)

Baseline (workers: 1): Xs
Parallel (workers: 4): Ys
```

---

### Phase 1 complete — checkpoint

Before starting Phase 2 (new tests), verify the improvement:

1. All existing tests still pass
2. Compare `time bash run.sh` to the baseline from Task 0
3. Confirm no `waitForTimeout` / `setTimeout` remains in test files: `grep -rn 'waitForTimeout\|setTimeout' e2e/tests/`
4. Report the numbers to the user before proceeding

---

### Task 9: Create multi-file + directory fixture

**Files:**
- Create: `e2e/setup-fixtures-multifile.sh`

**Step 1: Create the fixture script**

```bash
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

# Run crit in file mode with explicit files AND a directory
exec "$CRIT_BIN" --no-open --port "$PORT" plan.md main.go handler.ex lib/
```

**Step 2: Make it executable**

Run: `chmod +x e2e/setup-fixtures-multifile.sh`

**Step 3: Test the fixture manually**

Run: `cd e2e && bash setup-fixtures-multifile.sh 3127 &` then `curl -s http://localhost:3127/api/session | jq .files[].path`

Expected output should include: `plan.md`, `main.go`, `handler.ex`, `lib/utils.ex`, `lib/config.ex`

Kill the test server after verifying.

**Step 4: Commit**

```
test: add multi-file + directory fixture for E2E tests
```

---

### Task 10: Add multi-file-mode Playwright project

**Files:**
- Modify: `e2e/playwright.config.ts`
- Modify: `e2e/run.sh`

**Step 1: Add port constant and project to playwright.config.ts**

Add after `NOGIT_PORT` line:
```typescript
const MULTI_PORT = process.env.CRIT_TEST_MULTI_PORT || '3127';
```

Add new project to the `projects` array:
```typescript
{
  name: 'multi-file-mode',
  testMatch: /\.multifile\.spec\.ts$/,
  use: {
    browserName: 'chromium',
    baseURL: `http://localhost:${MULTI_PORT}`,
  },
},
```

Add new webServer entry:
```typescript
{
  command: `bash setup-fixtures-multifile.sh ${MULTI_PORT}`,
  url: `http://localhost:${MULTI_PORT}/api/session`,
  reuseExistingServer: true,
  timeout: 30_000,
  stdout: 'pipe',
},
```

Update workers to 5 (one per project):
```typescript
workers: 5,
```

**Step 2: Update run.sh**

Add `MULTI_PORT` variable:
```bash
MULTI_PORT="${CRIT_TEST_MULTI_PORT:-3127}"
```

Add fixture start:
```bash
bash setup-fixtures-multifile.sh "$MULTI_PORT" &
MULTI_PID=$!
```

Update cleanup and wait-for-ready loops to include the new PID and port.

**Step 3: Run the suite to verify config**

Run: `make e2e`
Expected: All existing tests still pass, no errors from the new project (it has no test files yet, so it's a no-op).

**Step 4: Commit**

```
test: add multi-file-mode Playwright project and fixture runner
```

---

### Task 11: Write multi-file mode E2E tests

**Files:**
- Create: `e2e/tests/multifile.multifile.spec.ts`

**Step 1: Write the test file**

```typescript
import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// Helpers scoped to this fixture's files
function planSection(page: any) {
  return page.locator('.file-section').filter({ hasText: 'plan.md' });
}

function goSection(page: any) {
  return page.locator('.file-section').filter({ hasText: 'main.go' });
}

function exSection(page: any) {
  return page.locator('.file-section').filter({ hasText: 'handler.ex' });
}

test.describe('Multi-File Mode — Loading', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('shows all files including directory contents in file tree', async ({ page }) => {
    // Should have 5 files: plan.md, main.go, handler.ex, lib/utils.ex, lib/config.ex
    const treeFiles = page.locator('.tree-file');
    await expect(treeFiles).toHaveCount(5);
  });

  test('file tree shows directory path for nested files', async ({ page }) => {
    // lib/utils.ex and lib/config.ex should appear with their paths
    await expect(page.locator('.tree-file-name', { hasText: 'utils.ex' })).toBeVisible();
    await expect(page.locator('.tree-file-name', { hasText: 'config.ex' })).toBeVisible();
  });

  test('displays all file sections', async ({ page }) => {
    await expect(planSection(page)).toBeVisible();
    await expect(goSection(page)).toBeVisible();
    await expect(exSection(page)).toBeVisible();
    // Nested files too
    await expect(page.locator('.file-section').filter({ hasText: 'utils.ex' })).toBeVisible();
    await expect(page.locator('.file-section').filter({ hasText: 'config.ex' })).toBeVisible();
  });

  test('session mode is "files"', async ({ request }) => {
    const res = await request.get('/api/session');
    const session = await res.json();
    expect(session.mode).toBe('files');
  });

  test('stats show correct file count', async ({ page }) => {
    const stats = page.locator('#fileTreeStats');
    await expect(stats).toContainText('5');
  });
});

test.describe('Multi-File Mode — Code File Rendering', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('Go file renders with syntax-highlighted code', async ({ page }) => {
    const section = goSection(page);
    await expect(section).toBeVisible();
    // Code files in file mode default to document view
    const codeBlock = section.locator('code');
    await expect(codeBlock.first()).toBeVisible();
  });

  test('Elixir file renders with code content', async ({ page }) => {
    const section = exSection(page);
    await expect(section).toBeVisible();
    // Should contain Elixir keywords
    await expect(section).toContainText('defmodule');
    await expect(section).toContainText('def handle_request');
  });

  test('markdown file renders in document view by default', async ({ page }) => {
    const section = planSection(page);
    await expect(section).toBeVisible();
    const docWrapper = section.locator('.document-wrapper');
    await expect(docWrapper).toBeVisible();
    await expect(section).toContainText('Migration Plan');
  });
});

test.describe('Multi-File Mode — Comments on Code Files', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('can add a comment on a Go file line', async ({ page }) => {
    const section = goSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();

    const gutterBtn = section.locator('.line-comment-gutter').first();
    await expect(gutterBtn).toBeVisible();
    await gutterBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Comment on Go code');
    await page.locator('.comment-form .btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Comment on Go code');
  });

  test('can add a comment on an Elixir file line', async ({ page }) => {
    const section = exSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();

    const gutterBtn = section.locator('.line-comment-gutter').first();
    await expect(gutterBtn).toBeVisible();
    await gutterBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Comment on Elixir code');
    await page.locator('.comment-form .btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Comment on Elixir code');
  });

  test('can add a comment on a nested directory file', async ({ page }) => {
    const section = page.locator('.file-section').filter({ hasText: 'utils.ex' });
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();

    const gutterBtn = section.locator('.line-comment-gutter').first();
    await expect(gutterBtn).toBeVisible();
    await gutterBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Comment on nested file');
    await page.locator('.comment-form .btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Comment on nested file');
  });

  test('comment count reflects comments across all files', async ({ page, request }) => {
    // Add comments via API on different files
    await request.post('/api/file/comments?path=main.go', {
      data: { start_line: 1, end_line: 1, body: 'Go comment' },
    });
    await request.post('/api/file/comments?path=handler.ex', {
      data: { start_line: 1, end_line: 1, body: 'Elixir comment' },
    });
    await request.post('/api/file/comments?path=lib/utils.ex', {
      data: { start_line: 1, end_line: 1, body: 'Nested comment' },
    });

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    const countEl = page.locator('#commentCount');
    await expect(countEl).toContainText('3');
  });
});

test.describe('Multi-File Mode — File Tree Interaction', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('clicking a file in the tree scrolls to that section', async ({ page }) => {
    const treeFile = page.locator('.tree-file-name', { hasText: 'config.ex' });
    await treeFile.click();

    const section = page.locator('.file-section').filter({ hasText: 'config.ex' });
    await expect(section).toBeInViewport();
  });

  test('file tree shows comment badges for files with comments', async ({ page, request }) => {
    await request.post('/api/file/comments?path=main.go', {
      data: { start_line: 1, end_line: 1, body: 'Badge test' },
    });

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // The tree file for main.go should have a comment badge
    const goTreeFile = page.locator('.tree-file').filter({ hasText: 'main.go' });
    const badge = goTreeFile.locator('.tree-file-comments');
    await expect(badge).toBeVisible();
    await expect(badge).toContainText('1');
  });
});
```

**Step 2: Run the multi-file tests**

Run: `cd e2e && npx playwright test tests/multifile.multifile.spec.ts -v`
Expected: All tests pass

**Step 3: Commit**

```
test: add multi-file mode E2E tests with Go, Elixir, and directory support
```

---

### Task 12: Final verification — full suite run

**Step 1: Run Go unit tests**

Run: `go test ./...`
Expected: All pass

**Step 2: Run full E2E suite**

Run: `make e2e`
Expected: All tests pass (existing + new multi-file tests)

**Step 3: Verify no waitForTimeout remains**

Run: `grep -rn 'waitForTimeout\|setTimeout' e2e/tests/`
Expected: No results in any test file (only in helpers if needed)

**Step 4: Commit any fixes**

If any tests needed adjustments, commit those fixes.
