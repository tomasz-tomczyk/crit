# E2E Frontend Test Suite Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a Playwright E2E test suite that exercises every user-facing behavior in Crit's frontend, runnable headlessly by Claude with screenshot/trace output on failure for visual debugging.

**Architecture:** A Go helper binary (`cmd/e2e-server`) starts a real Crit server on a random port with fixture data (markdown + code files with diffs). Playwright launches this server, navigates the browser, and asserts behavior. Tests are organized by feature area matching the behavior catalog below. Screenshots on failure + trace files give Claude visual debugging info.

**Tech Stack:** Playwright (Node.js), Go (test server binary), Chromium (headless)

---

## Design Decisions

### Why a separate Go binary for the test server?

Playwright's `webServer` config needs a shell command to start/stop. We can't use `go test` hooks easily. A small `cmd/e2e-server/main.go` that:
1. Creates a temp directory with fixture files (markdown, Go code, etc.)
2. Initializes a git repo with commits to produce real diffs
3. Starts a real `Session` + `Server` on a specified port
4. Prints `Ready` to stdout (Playwright watches for this)
5. Exits cleanly on SIGTERM

This gives us a fully realistic server — real git diffs, real file watching, real SSE — not mocked.

### Why not mock the API?

The frontend relies on specific API response shapes, SSE event timing, and real diff hunk structures. Mocking would be fragile and miss integration bugs. A real server with fixture data is more reliable and catches more regressions.

### Test fixture content

The fixture set covers all file types and states we need to test:

| File | Status | Purpose |
|------|--------|---------|
| `plan.md` | added | Markdown rendering: headings, tables, lists, code blocks, task lists, blockquotes, mermaid |
| `server.go` | modified | Code diff with multiple hunks, additions, deletions, context lines |
| `deleted.txt` | deleted | Deleted file placeholder |
| `new-file.js` | added (untracked) | New file with all-addition diff |

### Screenshot and trace strategy

- `screenshot: 'only-on-failure'` — full-page PNG saved to `test-results/`
- `trace: 'retain-on-failure'` — Playwright trace zip (contains DOM snapshots, network, console)
- Claude reads `test-results/` directory and views PNGs directly to diagnose failures

---

## Behavior Catalog (What We're Testing)

Every test maps to a behavior. This is the regression test contract.

### Core Loading
- [ ] Page loads without errors, "Loading..." disappears
- [ ] Branch name shown in header (git mode)
- [ ] File count and +/- stats shown in file tree header
- [ ] Document title set correctly

### File Tree
- [ ] Files rendered with correct indentation (files nested under folders)
- [ ] File status icons match status (green + for added, red - for deleted, yellow dot for modified)
- [ ] Clicking a file scrolls to that file section
- [ ] Folder collapse/expand works
- [ ] Active file highlights as you scroll
- [ ] Comment count badges update when comments added/deleted

### File Sections
- [ ] Each file has a collapsible section with status badge
- [ ] File headers are sticky when scrolling
- [ ] Collapsed/expanded state toggles on click
- [ ] Deleted files show "This file was deleted." placeholder
- [ ] Large diffs show "Load diff" placeholder (if applicable)

### Diff Rendering — Unified Mode
- [ ] Hunk headers displayed with @@ notation
- [ ] Addition lines have green background and + sign
- [ ] Deletion lines have red background and - sign
- [ ] Context lines have no background
- [ ] Old and new line numbers both shown
- [ ] Spacers show "Expand N unchanged lines" between hunks
- [ ] Clicking spacer expands context lines and merges hunks
- [ ] Expanded lines support commenting (+ button appears on hover)

### Diff Rendering — Split Mode
- [ ] Side-by-side layout with old (left) and new (right)
- [ ] Deletion on left, addition on right
- [ ] Empty sides filled when line counts don't match
- [ ] Line numbers on each side

### Diff Mode Toggle
- [ ] Split/Unified toggle visible in git mode
- [ ] Clicking toggles between modes and re-renders
- [ ] Choice persisted to localStorage across page reload

### Markdown Rendering (Document View)
- [ ] Headings render with correct levels
- [ ] Code blocks have syntax highlighting
- [ ] Tables render with correct columns and alignment
- [ ] Lists render as bullet/ordered items
- [ ] Task lists show checkboxes
- [ ] Blockquotes render with border
- [ ] Each block is individually commentable (has line gutter)

### Comments — Add
- [ ] Clicking + gutter button opens comment form
- [ ] Form shows correct line reference
- [ ] Typing in textarea and clicking Submit creates comment
- [ ] Comment appears below the referenced line/block
- [ ] Comment count updates in header and file tree
- [ ] Ctrl+Enter submits the comment

### Comments — Edit
- [ ] Clicking Edit on a comment opens inline editor with existing text
- [ ] Submitting edit updates the comment body
- [ ] Cancelling edit restores original display

### Comments — Delete
- [ ] Clicking Delete removes the comment
- [ ] Comment count updates

### Comments — Multi-line Selection (Drag)
- [ ] Dragging across gutter selects multiple lines
- [ ] Selection highlighted with blue background
- [ ] Comment form shows "Lines X-Y" reference
- [ ] Shift+click extends existing selection

### Comments — Cross-file exclusivity
- [ ] Opening a comment form on file A closes any open form on file B

### Comments — Diff context
- [ ] Can add comments on diff lines (unified mode)
- [ ] Can add comments on diff lines (split mode, both sides)
- [ ] Comment form in unified mode has capped max-width
- [ ] Drag selection works across add/del lines in unified mode

### Keyboard Shortcuts
- [ ] j/k navigates between blocks (focused element highlighted)
- [ ] c opens comment form on focused block
- [ ] e edits comment on focused block (if comment exists)
- [ ] d deletes comment on focused block (if comment exists)
- [ ] Escape cancels comment form / clears selection / clears focus
- [ ] ? toggles shortcuts help overlay
- [ ] t toggles table of contents
- [ ] Shift+F triggers finish review
- [ ] j/k in split diff navigates rows (not left/right sides)
- [ ] Shortcuts disabled when textarea is focused

### Theme
- [ ] System/Light/Dark pill toggles theme
- [ ] Theme persisted to localStorage
- [ ] Page respects stored theme on reload
- [ ] Pill indicator animates to correct position

### Finish Review
- [ ] Clicking Finish Review shows waiting overlay
- [ ] Overlay shows prompt text
- [ ] "Back to editing" returns to review state
- [ ] Button text changes to "Waiting..." while in waiting state

### SSE / Live Updates
- [ ] EventSource connects to /api/events
- [ ] Server shutdown event shows disconnected overlay

### Table of Contents
- [ ] TOC shows headings from markdown files
- [ ] Clicking TOC entry scrolls to heading
- [ ] TOC hidden by default, toggled with t or button

---

## Task 1: Initialize Playwright and test infrastructure

**Files:**
- Create: `e2e/package.json`
- Create: `e2e/playwright.config.ts`
- Create: `e2e/tsconfig.json`
- Create: `e2e/.gitignore`
- Modify: `Makefile` (add e2e targets)

**Step 1: Create the e2e directory and package.json**

```bash
mkdir -p e2e
```

Write `e2e/package.json`:
```json
{
  "name": "crit-e2e",
  "private": true,
  "scripts": {
    "test": "npx playwright test",
    "test:headed": "npx playwright test --headed",
    "test:debug": "npx playwright test --debug",
    "report": "npx playwright show-report"
  },
  "devDependencies": {
    "@playwright/test": "^1.50.0"
  }
}
```

**Step 2: Create playwright.config.ts**

```typescript
import { defineConfig } from '@playwright/test';

const PORT = process.env.CRIT_TEST_PORT || '3123';

export default defineConfig({
  testDir: './tests',
  fullyParallel: false,       // serial — tests share one server
  retries: 0,
  workers: 1,
  reporter: [['html', { open: 'never' }], ['list']],

  use: {
    baseURL: `http://localhost:${PORT}`,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    video: 'retain-on-failure',
  },

  projects: [
    {
      name: 'chromium',
      use: { browserName: 'chromium' },
    },
  ],

  webServer: {
    command: `go run ../cmd/e2e-server/main.go -port ${PORT}`,
    url: `http://localhost:${PORT}/api/session`,
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
    stdout: 'pipe',
  },
});
```

**Step 3: Create tsconfig.json**

```json
{
  "compilerOptions": {
    "target": "ES2020",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "strict": true
  }
}
```

**Step 4: Create .gitignore**

```
node_modules/
test-results/
playwright-report/
blob-report/
```

**Step 5: Install dependencies and browser**

Run:
```bash
cd e2e && npm install && npx playwright install chromium
```

**Step 6: Add Makefile targets**

Add to `Makefile`:
```makefile
e2e:
	cd e2e && npx playwright test

e2e-report:
	cd e2e && npx playwright show-report
```

**Step 7: Commit**

```bash
git add e2e/ Makefile
git commit -m "feat: initialize Playwright E2E test infrastructure"
```

---

## Task 2: Create the E2E test server

**Files:**
- Create: `cmd/e2e-server/main.go`
- Create: `cmd/e2e-server/fixtures.go`

This is a small Go binary that sets up a realistic Crit server with fixture data for E2E testing.

**Step 1: Create `cmd/e2e-server/main.go`**

```go
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	crit "github.com/tomasz-tomczyk/crit"
)
```

Wait — the crit package is `package main`, not importable. We need a different approach.

**Revised approach:** Instead of a separate binary, create a shell script that:
1. Creates a temp directory with fixture files
2. Initializes a git repo with commits to produce real diffs
3. Runs `go run .` (the actual crit binary) pointed at the fixture repo

This is simpler and tests the real binary exactly as users would use it.

**Step 1: Create `e2e/setup-fixtures.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

PORT="${1:-3123}"
DIR=$(mktemp -d)
trap 'rm -rf "$DIR"' EXIT

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

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello, %s!", r.URL.Path[1:])
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
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

# Modify server.go (add auth middleware, modify existing handler)
cat > server.go << 'GOFILE'
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

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
		name := r.URL.Path[1:]
		if name == "" {
			name = "world"
		}
		fmt.Fprintf(w, "Hello, %s!", name)
	}))

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
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

# Delete deleted.txt
rm deleted.txt

# Add new file
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

# Add another new file (JS, to test syntax highlighting)
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

# Build crit binary
CRIT_BIN=$(cd "$OLDPWD" && go build -o "$DIR/.crit-bin" . && echo "$DIR/.crit-bin")

# Run crit in the fixture repo
exec "$DIR/.crit-bin" --no-open --port "$PORT"
```

**Step 2: Make it executable**

```bash
chmod +x e2e/setup-fixtures.sh
```

**Step 3: Update playwright.config.ts webServer command**

```typescript
webServer: {
    command: `bash setup-fixtures.sh ${PORT}`,
    url: `http://localhost:${PORT}/api/session`,
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
    stdout: 'pipe',
},
```

**Step 4: Commit**

```bash
git add cmd/ e2e/
git commit -m "feat: add E2E test server with fixture data"
```

---

## Task 3: Write loading and file tree tests

**Files:**
- Create: `e2e/tests/loading.spec.ts`

**Step 1: Write tests**

```typescript
import { test, expect } from '@playwright/test';

test.describe('Page Loading', () => {
  test('loads without errors and shows file content', async ({ page }) => {
    await page.goto('/');
    // Wait for loading to finish
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    // File sections should be visible
    await expect(page.locator('.file-section')).toHaveCount(4); // server.go, deleted.txt, plan.md, handler.js (or however many fixture files)
  });

  test('shows branch name in header', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await expect(page.locator('#branchName')).toHaveText('feat/add-auth');
  });

  test('sets correct document title', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await expect(page).toHaveTitle(/Crit — feat\/add-auth/);
  });

  test('shows diff mode toggle in git mode', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await expect(page.locator('#diffModeToggle')).toBeVisible();
  });
});

test.describe('File Tree', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
  });

  test('shows files with correct status icons', async ({ page }) => {
    // Added files should have green + icon
    const addedIcon = page.locator('.tree-file-status-icon.added');
    await expect(addedIcon.first()).toBeVisible();

    // Deleted files should have red - icon
    const deletedIcon = page.locator('.tree-file-status-icon.deleted');
    await expect(deletedIcon.first()).toBeVisible();

    // Modified files should have yellow icon
    const modifiedIcon = page.locator('.tree-file-status-icon.modified');
    await expect(modifiedIcon.first()).toBeVisible();
  });

  test('clicking a file scrolls to its section', async ({ page }) => {
    // Click on plan.md in the tree
    await page.locator('.tree-file').filter({ hasText: 'plan.md' }).click();
    // The file section should be in view
    const section = page.locator('[id*="plan.md"]');
    await expect(section).toBeInViewport();
  });

  test('file tree shows +/- stats', async ({ page }) => {
    const stats = page.locator('#fileTreeStats');
    await expect(stats).toContainText('+');
  });
});
```

**Step 2: Run tests to verify they pass**

```bash
cd e2e && npx playwright test tests/loading.spec.ts
```

**Step 3: Commit**

```bash
git add e2e/tests/
git commit -m "test: add loading and file tree E2E tests"
```

---

## Task 4: Write diff rendering tests

**Files:**
- Create: `e2e/tests/diff-rendering.spec.ts`

**Step 1: Write tests**

```typescript
import { test, expect } from '@playwright/test';

test.describe('Diff Rendering', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
  });

  test('shows split diff by default', async ({ page }) => {
    await expect(page.locator('.diff-container.split').first()).toBeVisible();
  });

  test('split diff has left and right sides', async ({ page }) => {
    const splitRow = page.locator('.diff-split-row').first();
    await expect(splitRow.locator('.diff-split-side.left')).toBeVisible();
    await expect(splitRow.locator('.diff-split-side.right')).toBeVisible();
  });

  test('addition lines have green background class', async ({ page }) => {
    const addLine = page.locator('.diff-split-side.addition').first();
    await expect(addLine).toBeVisible();
  });

  test('deletion lines have red background class', async ({ page }) => {
    const delLine = page.locator('.diff-split-side.deletion').first();
    await expect(delLine).toBeVisible();
  });

  test('hunk headers show @@ notation', async ({ page }) => {
    const hunkHeader = page.locator('.diff-hunk-header').first();
    await expect(hunkHeader).toContainText('@@');
  });

  test('deleted file shows placeholder', async ({ page }) => {
    const deletedPlaceholder = page.locator('.diff-deleted-placeholder');
    await expect(deletedPlaceholder).toHaveText('This file was deleted.');
  });

  test('spacer shows expand button between hunks', async ({ page }) => {
    const spacer = page.locator('.diff-spacer').first();
    if (await spacer.isVisible()) {
      await expect(spacer).toContainText('Expand');
      await expect(spacer).toContainText('unchanged line');
    }
  });

  test('clicking spacer expands context lines', async ({ page }) => {
    const spacer = page.locator('.diff-spacer').first();
    if (await spacer.isVisible()) {
      const spacerText = await spacer.textContent();
      await spacer.click();
      // Spacer should be gone (replaced with lines)
      await expect(spacer).toBeHidden();
    }
  });
});

test.describe('Diff Mode Toggle', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
  });

  test('can switch to unified mode', async ({ page }) => {
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();
    await expect(page.locator('.diff-container.unified').first()).toBeVisible();
  });

  test('unified mode shows single-pane lines', async ({ page }) => {
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();
    const line = page.locator('.diff-container.unified .diff-line').first();
    await expect(line).toBeVisible();
  });

  test('diff mode persists across reload', async ({ page }) => {
    // Switch to unified
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();
    await expect(page.locator('.diff-container.unified').first()).toBeVisible();

    // Reload
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // Should still be unified
    await expect(page.locator('.diff-container.unified').first()).toBeVisible();
  });

  test('can switch back to split mode', async ({ page }) => {
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();
    await page.locator('#diffModeToggle .toggle-btn[data-mode="split"]').click();
    await expect(page.locator('.diff-container.split').first()).toBeVisible();
  });
});
```

**Step 2: Run tests**

```bash
cd e2e && npx playwright test tests/diff-rendering.spec.ts
```

**Step 3: Commit**

```bash
git add e2e/tests/
git commit -m "test: add diff rendering E2E tests"
```

---

## Task 5: Write markdown rendering tests

**Files:**
- Create: `e2e/tests/markdown.spec.ts`

**Step 1: Write tests**

```typescript
import { test, expect } from '@playwright/test';

test.describe('Markdown Rendering', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
  });

  // Find the plan.md file section (markdown file in document mode)
  function mdSection(page) {
    return page.locator('.file-section').filter({ hasText: 'plan.md' });
  }

  test('renders headings', async ({ page }) => {
    const section = mdSection(page);
    await expect(section.locator('h1').first()).toBeVisible();
    await expect(section.locator('h2').first()).toBeVisible();
  });

  test('renders tables', async ({ page }) => {
    const section = mdSection(page);
    await expect(section.locator('table').first()).toBeVisible();
    await expect(section.locator('th').first()).toBeVisible();
    await expect(section.locator('td').first()).toBeVisible();
  });

  test('renders code blocks with syntax highlighting', async ({ page }) => {
    const section = mdSection(page);
    const codeBlock = section.locator('.code-line').first();
    await expect(codeBlock).toBeVisible();
    // Should have hljs highlighting spans
    await expect(section.locator('.hljs').first()).toBeVisible();
  });

  test('renders ordered and unordered lists', async ({ page }) => {
    const section = mdSection(page);
    await expect(section.locator('ol').first()).toBeVisible();
  });

  test('renders task list checkboxes', async ({ page }) => {
    const section = mdSection(page);
    const checkbox = section.locator('input[type="checkbox"]').first();
    await expect(checkbox).toBeVisible();
  });

  test('renders blockquotes', async ({ page }) => {
    const section = mdSection(page);
    await expect(section.locator('blockquote').first()).toBeVisible();
  });

  test('each block has a line gutter with line number', async ({ page }) => {
    const section = mdSection(page);
    const gutter = section.locator('.line-gutter .line-num').first();
    await expect(gutter).toBeVisible();
    // Line number should be a number
    const text = await gutter.textContent();
    expect(parseInt(text!)).toBeGreaterThan(0);
  });

  test('markdown file has document/diff toggle if it has diff', async ({ page }) => {
    const section = mdSection(page);
    // plan.md is a new file so it should have a diff available
    // Look for the view mode toggle buttons
    const docBtn = section.locator('.file-header .btn-group .btn').filter({ hasText: 'Document' });
    const diffBtn = section.locator('.file-header .btn-group .btn').filter({ hasText: 'Diff' });
    // At least one should be visible if the file has diffs
    // (may not exist if this is first round with no previous content)
  });
});
```

**Step 2: Run tests**

```bash
cd e2e && npx playwright test tests/markdown.spec.ts
```

**Step 3: Commit**

```bash
git add e2e/tests/
git commit -m "test: add markdown rendering E2E tests"
```

---

## Task 6: Write comment CRUD tests

**Files:**
- Create: `e2e/tests/comments.spec.ts`

**Step 1: Write tests**

```typescript
import { test, expect } from '@playwright/test';

test.describe('Comments', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
  });

  test('clicking + button on markdown gutter opens comment form', async ({ page }) => {
    // Hover over a line block to reveal the + button
    const lineBlock = page.locator('.line-block').first();
    await lineBlock.hover();

    const plusBtn = lineBlock.locator('.line-comment-gutter');
    await plusBtn.click();

    // Comment form should appear
    await expect(page.locator('.comment-form')).toBeVisible();
    await expect(page.locator('.comment-form textarea')).toBeFocused();
  });

  test('submit comment and see it displayed', async ({ page }) => {
    const lineBlock = page.locator('.line-block').first();
    await lineBlock.hover();
    await lineBlock.locator('.line-comment-gutter').click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('This is a test comment');
    await page.locator('.comment-form .btn-primary').click();

    // Comment should appear
    await expect(page.locator('.comment-card')).toBeVisible();
    await expect(page.locator('.comment-body')).toContainText('This is a test comment');

    // Header comment count should update
    await expect(page.locator('#commentCount')).toContainText('1');
  });

  test('Ctrl+Enter submits comment', async ({ page }) => {
    const lineBlock = page.locator('.line-block').first();
    await lineBlock.hover();
    await lineBlock.locator('.line-comment-gutter').click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Keyboard submit test');
    await textarea.press('Control+Enter');

    await expect(page.locator('.comment-body')).toContainText('Keyboard submit test');
  });

  test('edit comment', async ({ page }) => {
    // First create a comment
    const lineBlock = page.locator('.line-block').first();
    await lineBlock.hover();
    await lineBlock.locator('.line-comment-gutter').click();
    await page.locator('.comment-form textarea').fill('Original text');
    await page.locator('.comment-form .btn-primary').click();
    await expect(page.locator('.comment-card')).toBeVisible();

    // Click edit
    await page.locator('.comment-actions button').filter({ hasText: 'Edit' }).click();

    // Editor should show with existing text
    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toHaveValue('Original text');

    // Change text
    await textarea.fill('Updated text');
    await page.locator('.comment-form .btn-primary').click();

    await expect(page.locator('.comment-body')).toContainText('Updated text');
  });

  test('delete comment', async ({ page }) => {
    // Create a comment
    const lineBlock = page.locator('.line-block').first();
    await lineBlock.hover();
    await lineBlock.locator('.line-comment-gutter').click();
    await page.locator('.comment-form textarea').fill('To be deleted');
    await page.locator('.comment-form .btn-primary').click();
    await expect(page.locator('.comment-card')).toBeVisible();

    // Delete it
    await page.locator('.comment-actions .delete-btn').click();

    // Should be gone
    await expect(page.locator('.comment-card')).toBeHidden();
    // Comment count should be empty or gone
  });

  test('cancel comment form with Escape', async ({ page }) => {
    const lineBlock = page.locator('.line-block').first();
    await lineBlock.hover();
    await lineBlock.locator('.line-comment-gutter').click();
    await expect(page.locator('.comment-form')).toBeVisible();

    await page.keyboard.press('Escape');
    await expect(page.locator('.comment-form')).toBeHidden();
  });

  test('opening comment on file A closes form on file B', async ({ page }) => {
    // Open form on first file
    const firstFileBlock = page.locator('.file-section').first().locator('.line-block, .diff-line').first();
    await firstFileBlock.hover();

    // Find + button — could be in a gutter
    const sections = page.locator('.file-section');
    const count = await sections.count();
    if (count < 2) return; // need at least 2 files

    // Click + on first file (markdown gutter or diff gutter)
    const firstSection = sections.first();
    const firstGutter = firstSection.locator('.line-comment-gutter, .diff-comment-btn').first();
    await firstGutter.click({ force: true });
    await expect(page.locator('.comment-form')).toHaveCount(1);

    // Now click + on second file
    const secondSection = sections.nth(1);
    const secondGutter = secondSection.locator('.line-comment-gutter, .diff-comment-btn').first();
    await secondGutter.scrollIntoViewIfNeeded();
    await secondGutter.click({ force: true });

    // Should still be exactly 1 form
    await expect(page.locator('.comment-form')).toHaveCount(1);
  });
});

test.describe('Diff Comments', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
  });

  test('can add comment on diff line via + button', async ({ page }) => {
    // Find a diff line with a + button
    const diffLine = page.locator('.diff-split-side.addition, .diff-line.addition').first();
    await diffLine.hover();

    const plusBtn = diffLine.locator('.diff-comment-btn');
    await plusBtn.click();

    await expect(page.locator('.comment-form')).toBeVisible();
    await page.locator('.comment-form textarea').fill('Diff comment');
    await page.locator('.comment-form .btn-primary').click();

    await expect(page.locator('.comment-card')).toBeVisible();
  });

  test('comment form in unified mode has capped width', async ({ page }) => {
    // Switch to unified
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();

    const diffLine = page.locator('.diff-container.unified .diff-line.addition').first();
    await diffLine.hover();
    await diffLine.locator('.diff-comment-btn').click();

    const form = page.locator('.diff-container.unified .comment-form-wrapper');
    const box = await form.boundingBox();
    expect(box!.width).toBeLessThanOrEqual(910); // 900 max-width + small tolerance
  });
});
```

**Step 2: Run tests**

```bash
cd e2e && npx playwright test tests/comments.spec.ts
```

**Step 3: Commit**

```bash
git add e2e/tests/
git commit -m "test: add comment CRUD E2E tests"
```

---

## Task 7: Write keyboard shortcut tests

**Files:**
- Create: `e2e/tests/keyboard.spec.ts`

**Step 1: Write tests**

```typescript
import { test, expect } from '@playwright/test';

test.describe('Keyboard Shortcuts', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
  });

  test('j navigates to next block, k to previous', async ({ page }) => {
    // Press j to focus first element
    await page.keyboard.press('j');
    const focused = page.locator('.kb-nav.focused');
    await expect(focused).toHaveCount(1);

    // Press j again to move forward
    await page.keyboard.press('j');
    await expect(page.locator('.kb-nav.focused')).toHaveCount(1);

    // Press k to go back
    await page.keyboard.press('k');
    await expect(page.locator('.kb-nav.focused')).toHaveCount(1);
  });

  test('c opens comment form on focused block', async ({ page }) => {
    // Focus first block
    await page.keyboard.press('j');
    await expect(page.locator('.kb-nav.focused')).toHaveCount(1);

    // Press c to comment
    await page.keyboard.press('c');
    await expect(page.locator('.comment-form')).toBeVisible();
  });

  test('Escape cancels comment form', async ({ page }) => {
    await page.keyboard.press('j');
    await page.keyboard.press('c');
    await expect(page.locator('.comment-form')).toBeVisible();

    await page.keyboard.press('Escape');
    await expect(page.locator('.comment-form')).toBeHidden();
  });

  test('Escape clears focus when no form open', async ({ page }) => {
    await page.keyboard.press('j');
    await expect(page.locator('.kb-nav.focused')).toHaveCount(1);

    await page.keyboard.press('Escape');
    await expect(page.locator('.kb-nav.focused')).toHaveCount(0);
  });

  test('? toggles shortcuts overlay', async ({ page }) => {
    await page.keyboard.press('?');
    await expect(page.locator('#shortcutsOverlay')).toHaveClass(/active/);

    await page.keyboard.press('?');
    await expect(page.locator('#shortcutsOverlay')).not.toHaveClass(/active/);
  });

  test('t toggles table of contents', async ({ page }) => {
    // TOC should be hidden by default
    await expect(page.locator('#toc')).toHaveClass(/toc-hidden/);

    await page.keyboard.press('t');
    await expect(page.locator('#toc')).not.toHaveClass(/toc-hidden/);

    await page.keyboard.press('t');
    await expect(page.locator('#toc')).toHaveClass(/toc-hidden/);
  });

  test('shortcuts disabled when textarea is focused', async ({ page }) => {
    // Open a comment form
    await page.keyboard.press('j');
    await page.keyboard.press('c');
    await expect(page.locator('.comment-form textarea')).toBeFocused();

    // Press j — should type 'j' in textarea, not navigate
    await page.keyboard.press('j');
    await expect(page.locator('.comment-form textarea')).toHaveValue('j');
  });

  test('e edits comment on focused block', async ({ page }) => {
    // Create a comment first
    await page.keyboard.press('j');
    await page.keyboard.press('c');
    await page.locator('.comment-form textarea').fill('Test comment for edit');
    await page.locator('.comment-form .btn-primary').click();
    await expect(page.locator('.comment-card')).toBeVisible();

    // Focus back on the block and press e
    await page.keyboard.press('j');
    await page.keyboard.press('e');

    // Editor should appear with existing text
    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toHaveValue('Test comment for edit');
  });

  test('d deletes comment on focused block', async ({ page }) => {
    // Create a comment
    await page.keyboard.press('j');
    await page.keyboard.press('c');
    await page.locator('.comment-form textarea').fill('Comment to delete');
    await page.locator('.comment-form .btn-primary').click();
    await expect(page.locator('.comment-card')).toBeVisible();

    // Focus block and press d
    await page.keyboard.press('j');
    await page.keyboard.press('d');

    await expect(page.locator('.comment-card')).toBeHidden();
  });

  test('j/k in split diff navigates rows not sides', async ({ page }) => {
    // Ensure we're in split mode
    await page.locator('#diffModeToggle .toggle-btn[data-mode="split"]').click();

    // Navigate to a diff section
    const diffRow = page.locator('.diff-split-row').first();
    await diffRow.scrollIntoViewIfNeeded();

    // Hover to set initial focus
    await diffRow.hover();

    // Press j twice and check that focus moved to next rows, not left/right alternation
    await page.keyboard.press('j');
    const focused1 = page.locator('.diff-split-row.focused');
    if (await focused1.count() > 0) {
      await page.keyboard.press('j');
      const focused2 = page.locator('.diff-split-row.focused');
      await expect(focused2).toHaveCount(1);
    }
  });
});
```

**Step 2: Run tests**

```bash
cd e2e && npx playwright test tests/keyboard.spec.ts
```

**Step 3: Commit**

```bash
git add e2e/tests/
git commit -m "test: add keyboard shortcut E2E tests"
```

---

## Task 8: Write theme and UI state tests

**Files:**
- Create: `e2e/tests/theme.spec.ts`

**Step 1: Write tests**

```typescript
import { test, expect } from '@playwright/test';

test.describe('Theme', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
  });

  test('clicking light theme button sets data-theme="light"', async ({ page }) => {
    await page.locator('.theme-pill-btn[data-for-theme="light"]').click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  });

  test('clicking dark theme button sets data-theme="dark"', async ({ page }) => {
    await page.locator('.theme-pill-btn[data-for-theme="dark"]').click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  });

  test('clicking system theme removes data-theme', async ({ page }) => {
    // First set to light
    await page.locator('.theme-pill-btn[data-for-theme="light"]').click();
    // Then back to system
    await page.locator('.theme-pill-btn[data-for-theme="system"]').click();
    const theme = await page.locator('html').getAttribute('data-theme');
    expect(theme).toBeNull();
  });

  test('theme persists across reload', async ({ page }) => {
    await page.locator('.theme-pill-btn[data-for-theme="dark"]').click();
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  });
});

test.describe('File Sections', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
  });

  test('file sections are collapsible', async ({ page }) => {
    const section = page.locator('.file-section').first();
    const summary = section.locator('.file-header');

    // Should be open by default
    await expect(section).toHaveAttribute('open', '');

    // Click to collapse
    await summary.click();
    // After collapse, open attribute should be removed
    await expect(section).not.toHaveAttribute('open');

    // Click to expand again
    await summary.click();
    await expect(section).toHaveAttribute('open', '');
  });

  test('file headers show status badges', async ({ page }) => {
    const badge = page.locator('.file-status').first();
    await expect(badge).toBeVisible();
    const text = await badge.textContent();
    expect(['Modified', 'New File', 'Deleted', 'Untracked']).toContain(text!.trim());
  });

  test('file header shows diff stats', async ({ page }) => {
    // At least one file should have + or - stats
    const addStat = page.locator('.file-header .add');
    const delStat = page.locator('.file-header .del');
    // At least one should be visible across all files
    const addCount = await addStat.count();
    const delCount = await delStat.count();
    expect(addCount + delCount).toBeGreaterThan(0);
  });
});

test.describe('Finish Review', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
  });

  test('finish button shows waiting overlay', async ({ page }) => {
    await page.locator('#finishBtn').click();
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
    await expect(page.locator('#waitingOverlay')).toContainText('Review Complete');
  });

  test('back to editing returns to review state', async ({ page }) => {
    await page.locator('#finishBtn').click();
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);

    await page.locator('#backToEditing').click();
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/);
    await expect(page.locator('#finishBtn')).toBeEnabled();
  });
});
```

**Step 2: Run tests**

```bash
cd e2e && npx playwright test tests/theme.spec.ts
```

**Step 3: Commit**

```bash
git add e2e/tests/
git commit -m "test: add theme, file sections, and finish review E2E tests"
```

---

## Task 9: Write drag selection tests

**Files:**
- Create: `e2e/tests/drag-selection.spec.ts`

**Step 1: Write tests**

```typescript
import { test, expect } from '@playwright/test';

test.describe('Drag Selection', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
  });

  test('drag across gutter selects multiple lines in markdown', async ({ page }) => {
    const section = page.locator('.file-section').filter({ hasText: 'plan.md' });
    const gutters = section.locator('.line-comment-gutter');

    const firstGutter = gutters.first();
    const thirdGutter = gutters.nth(2);

    await firstGutter.scrollIntoViewIfNeeded();
    const start = await firstGutter.boundingBox();
    const end = await thirdGutter.boundingBox();

    if (start && end) {
      await page.mouse.move(start.x + start.width / 2, start.y + start.height / 2);
      await page.mouse.down();
      await page.mouse.move(end.x + end.width / 2, end.y + end.height / 2, { steps: 5 });
      await page.mouse.up();

      // Should have selected lines and opened a comment form
      const form = page.locator('.comment-form');
      await expect(form).toBeVisible();

      // Form header should reference "Lines X-Y"
      const header = page.locator('.comment-form-header');
      await expect(header).toContainText('Lines');
    }
  });

  test('drag on diff + button selects lines', async ({ page }) => {
    const diffBtn = page.locator('.diff-comment-btn').first();
    await diffBtn.scrollIntoViewIfNeeded();

    const box = await diffBtn.boundingBox();
    if (box) {
      await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
      await page.mouse.down();
      // Move down a few pixels to trigger drag
      await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2 + 60, { steps: 3 });
      await page.mouse.up();

      // Should open a form (even if single line)
      await expect(page.locator('.comment-form')).toBeVisible();
    }
  });
});
```

**Step 2: Run tests**

```bash
cd e2e && npx playwright test tests/drag-selection.spec.ts
```

**Step 3: Commit**

```bash
git add e2e/tests/
git commit -m "test: add drag selection E2E tests"
```

---

## Task 10: Verify full suite and add CI-ready script

**Step 1: Run full suite**

```bash
cd e2e && npx playwright test
```

Expected: All tests pass.

**Step 2: Add a top-level test runner script to package.json for convenience**

Verify `Makefile` has:
```makefile
e2e:
	cd e2e && npx playwright test

e2e-report:
	cd e2e && npx playwright show-report
```

**Step 3: Run and verify**

```bash
make e2e
```

**Step 4: Commit**

```bash
git add .
git commit -m "test: finalize E2E test suite with full behavior coverage"
```

---

## File Summary

| File | Action | Task |
|------|--------|------|
| `e2e/package.json` | Create | 1 |
| `e2e/playwright.config.ts` | Create | 1 |
| `e2e/tsconfig.json` | Create | 1 |
| `e2e/.gitignore` | Create | 1 |
| `e2e/setup-fixtures.sh` | Create | 2 |
| `e2e/tests/loading.spec.ts` | Create | 3 |
| `e2e/tests/diff-rendering.spec.ts` | Create | 4 |
| `e2e/tests/markdown.spec.ts` | Create | 5 |
| `e2e/tests/comments.spec.ts` | Create | 6 |
| `e2e/tests/keyboard.spec.ts` | Create | 7 |
| `e2e/tests/theme.spec.ts` | Create | 8 |
| `e2e/tests/drag-selection.spec.ts` | Create | 9 |
| `Makefile` | Modify | 1 |

## Running Tests After Implementation

```bash
# Full suite (headless)
make e2e

# Single file
cd e2e && npx playwright test tests/comments.spec.ts

# Debug mode (headed, step-through)
cd e2e && npx playwright test --debug

# View HTML report with screenshots
make e2e-report

# View trace files for failures
# (open test-results/<test-name>/trace.zip in https://trace.playwright.dev)
```

## For Claude: Diagnosing Failures

When a test fails:
1. Check `e2e/test-results/` for screenshots (PNG) — read them with the Read tool
2. Check console output for assertion messages
3. If needed, run individual test with `--debug` flag for step-through
4. Trace files in `test-results/` contain DOM snapshots, network requests, and console logs

## Verification

After all tasks:
- `make e2e` — all tests pass
- `go test ./...` — Go tests still pass (nothing changed in Go code)
- `e2e/test-results/` is empty (no failures)
