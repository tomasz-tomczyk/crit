# Diff Scope Toggle Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a header toggle (All | Branch | Staged | Unstaged) that controls which git changes are shown — different file lists and different diffs per scope.

**Architecture:** The backend gains a `scope` query parameter on `/api/session` and `/api/file/diff`. New git functions compute per-scope file lists (`git diff --cached --name-status` for staged, `git diff --name-status` for unstaged, `git diff <base>..HEAD --name-status` for branch) and per-scope diffs. The frontend adds a toggle next to the existing split/unified toggle, persists the choice in a cookie, and re-fetches all data when the scope changes. Comments are scope-independent — they live on the file and reference line numbers in the current file content.

**Tech Stack:** Go (backend), vanilla JS (frontend), git CLI

---

## Design Decisions

### Scopes

| Scope | Changed files command | Diff command | Untracked? |
|-------|----------------------|-------------|------------|
| **all** (default) | Feature: `git diff <base> --name-status` + untracked. Default branch: `git diff HEAD --name-status` + untracked | Feature: `git diff <base> -- <path>`. Default: `git diff HEAD -- <path>` | Yes |
| **branch** | `git diff <base>..HEAD --name-status` | `git diff <base>..HEAD -- <path>` | No |
| **staged** | `git diff --cached --name-status` | `git diff --cached -- <path>` | No |
| **unstaged** | `git diff --name-status` + untracked | `git diff -- <path>` (untracked → show-as-new) | Yes |

### Edge cases

- **Default branch:** "Branch" scope is hidden (no commits ahead of HEAD). Toggle shows: All | Staged | Unstaged.
- **Feature branch:** Toggle shows: All | Branch | Staged | Unstaged.
- **Empty scope:** When a scope has zero files, show an empty state message in the files container (e.g., "No staged changes"). File tree shows empty.
- **Untracked files:** Appear in "All" and "Unstaged" scopes. Not in "Branch" or "Staged" (can't stage something that isn't tracked yet — well, you can `git add` an untracked file, but `git diff --cached` handles that as "added").
- **File-mode to git-mode transition (future):** Keep the scope architecture decoupled from session mode so that a future feature can smoothly transition a file-mode session (`crit plan.md`) into git-mode when the user wants to review branch progress. The `scope` parameter is already on the API layer (not tied to session creation), which means a session that starts as file-mode could later expose scope controls if it detects a git repo. No changes needed now, but avoid baking scope logic into `NewSessionFromGit` — keep it in the query-time functions (`ChangedFilesScoped`, `FileDiffScoped`) so any session type can use it.
- **File has changes in multiple scopes:** A file can appear in branch, staged, AND unstaged simultaneously. Each scope shows its own diff for that file.

### Comments

Comments are scope-independent. They reference source line numbers in the current file content. When viewing a scoped diff, comments appear at their line positions if those lines are visible in the current diff hunks. Comments on lines outside the visible hunks are naturally hidden (same as how context-only lines outside hunks aren't shown). No `scope` field on comments — they belong to the file, not the scope.

### Persistence

Scope choice persisted to cookie `crit-diff-scope` (like `crit-diff-mode` for split/unified). Default: `"all"`.

---

## Task 1: Add scoped git functions to `git.go`

**Files:**
- Modify: `crit/git.go`
- Create: `crit/git_test.go` (add new test cases)

**Step 1: Write failing tests for `ChangedFilesScoped`**

Add to `git_test.go`:

```go
func TestChangedFilesStaged(t *testing.T) {
	// Create temp repo, stage a file change, verify changedFilesStaged returns it
	dir := setupTempRepo(t)
	// ... write a file, git add it
	// changedFilesStaged() should return the file
	// changedFilesUnstaged() should NOT return it
}

func TestChangedFilesUnstaged(t *testing.T) {
	// Create temp repo, modify a tracked file WITHOUT staging
	// changedFilesUnstaged() should return it
	// changedFilesStaged() should NOT return it
}

func TestChangedFilesBranch(t *testing.T) {
	// Create temp repo, make commits on a feature branch
	// changedFilesBranch(mergeBase) should return committed files
	// Should NOT include staged/unstaged working tree changes
}

func TestFileDiffScoped(t *testing.T) {
	// Verify FileDiffScoped returns different hunks per scope
}
```

**Step 2: Run tests to verify they fail**

Run: `cd crit && go test -run "TestChangedFiles(Staged|Unstaged|Branch)|TestFileDiffScoped" -v`
Expected: FAIL — functions don't exist yet

**Step 3: Implement scoped git functions**

Add to `git.go`:

```go
// changedFilesStaged returns only staged (cached) changes.
func changedFilesStaged() ([]FileChange, error) {
	cmd := exec.Command("git", "diff", "--cached", "--name-status")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --cached failed: %w", err)
	}
	return parseNameStatus(string(out)), nil
}

// changedFilesUnstaged returns only unstaged working tree changes + untracked files.
func changedFilesUnstaged() ([]FileChange, error) {
	cmd := exec.Command("git", "diff", "--name-status")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff failed: %w", err)
	}
	changes := parseNameStatus(string(out))
	untracked, err := untrackedFiles()
	if err != nil {
		return nil, err
	}
	return dedup(append(changes, untracked...)), nil
}

// changedFilesBranch returns only committed changes on the current branch vs merge base.
// Returns nil if baseRef is empty (on default branch).
func changedFilesBranch(baseRef string) ([]FileChange, error) {
	if baseRef == "" {
		return nil, nil
	}
	cmd := exec.Command("git", "diff", baseRef+"..HEAD", "--name-status")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff branch failed: %w", err)
	}
	return parseNameStatus(string(out)), nil
}

// ChangedFilesScoped returns files changed in a specific scope.
// Valid scopes: "all" (default), "branch", "staged", "unstaged".
func ChangedFilesScoped(scope, baseRef string) ([]FileChange, error) {
	switch scope {
	case "branch":
		return changedFilesBranch(baseRef)
	case "staged":
		return changedFilesStaged()
	case "unstaged":
		return changedFilesUnstaged()
	default:
		return ChangedFiles()
	}
}

// FileDiffScoped returns diff hunks for a file in a specific scope.
func FileDiffScoped(path, scope, baseRef string) ([]DiffHunk, error) {
	var cmd *exec.Cmd
	switch scope {
	case "branch":
		if baseRef == "" {
			return nil, nil
		}
		cmd = exec.Command("git", "diff", baseRef+"..HEAD", "--", path)
	case "staged":
		cmd = exec.Command("git", "diff", "--cached", "--", path)
	case "unstaged":
		cmd = exec.Command("git", "diff", "--", path)
	default:
		return FileDiffUnified(path, baseRef)
	}
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			// git diff exits 1 when there are differences
		} else {
			return nil, fmt.Errorf("git diff scoped failed: %w", err)
		}
	}
	return ParseUnifiedDiff(string(out)), nil
}
```

**Step 4: Run tests to verify they pass**

Run: `cd crit && go test -run "TestChangedFiles(Staged|Unstaged|Branch)|TestFileDiffScoped" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add crit/git.go crit/git_test.go
git commit -m "feat: add scoped git diff functions (branch, staged, unstaged)"
```

---

## Task 2: Add scope parameter to session and diff API endpoints

**Files:**
- Modify: `crit/server.go`
- Modify: `crit/session.go`
- Modify: `crit/server_test.go`

**Step 1: Write failing test for scoped session endpoint**

Add to `server_test.go`:

```go
func TestSessionScopeParam(t *testing.T) {
	// GET /api/session?scope=staged should return different file list
	// than GET /api/session?scope=all
}
```

**Step 2: Run test to verify it fails**

Run: `cd crit && go test -run TestSessionScopeParam -v`
Expected: FAIL

**Step 3: Add `GetSessionInfoScoped` to `session.go`**

```go
// GetSessionInfoScoped returns session info filtered by a diff scope.
// Computes file list and diff stats on-the-fly for the requested scope.
func (s *Session) GetSessionInfoScoped(scope string) SessionInfo {
	if scope == "" || scope == "all" {
		return s.GetSessionInfo()
	}

	s.mu.RLock()
	baseRef := s.BaseRef
	repoRoot := s.RepoRoot
	s.mu.RUnlock()

	changes, err := ChangedFilesScoped(scope, baseRef)
	if err != nil || len(changes) == 0 {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return SessionInfo{
			Mode:        s.Mode,
			Branch:      s.Branch,
			BaseRef:     s.BaseRef,
			ReviewRound: s.ReviewRound,
			Files:       []SessionFileInfo{},
		}
	}

	// Build file info with scoped diffs
	changedPaths := make(map[string]string, len(changes))
	for _, c := range changes {
		changedPaths[c.Path] = c.Status
	}

	s.mu.RLock()
	info := SessionInfo{
		Mode:        s.Mode,
		Branch:      s.Branch,
		BaseRef:     s.BaseRef,
		ReviewRound: s.ReviewRound,
	}

	// Build file list from scoped changes, using session files for comment counts
	commentCounts := make(map[string]int, len(s.Files))
	for _, f := range s.Files {
		commentCounts[f.Path] = len(f.Comments)
	}
	s.mu.RUnlock()

	for _, fc := range changes {
		fi := SessionFileInfo{
			Path:         fc.Path,
			Status:       fc.Status,
			FileType:     detectFileType(fc.Path),
			CommentCount: commentCounts[fc.Path],
		}

		// Compute scoped diff stats
		var hunks []DiffHunk
		if fc.Status == "added" {
			absPath := filepath.Join(repoRoot, fc.Path)
			if data, err := os.ReadFile(absPath); err == nil {
				hunks = FileDiffUnifiedNewFile(string(data))
			}
		} else {
			hunks, _ = FileDiffScoped(fc.Path, scope, baseRef)
		}
		for _, h := range hunks {
			for _, l := range h.Lines {
				switch l.Type {
				case "add":
					fi.Additions++
				case "del":
					fi.Deletions++
				}
			}
		}
		info.Files = append(info.Files, fi)
	}
	return info
}
```

**Step 4: Add `GetFileDiffSnapshotScoped` to `session.go`**

```go
// GetFileDiffSnapshotScoped returns diff data for a specific scope.
func (s *Session) GetFileDiffSnapshotScoped(path, scope string) (map[string]any, bool) {
	if scope == "" || scope == "all" {
		return s.GetFileDiffSnapshot(path)
	}

	s.mu.RLock()
	f := s.fileByPathLocked(path)
	baseRef := s.BaseRef
	s.mu.RUnlock()

	// For non-"all" scopes, compute diff on the fly
	// The file may not be in the session's file list for this scope,
	// but we still try to compute its diff
	var hunks []DiffHunk
	if f != nil && (f.Status == "added" || f.Status == "untracked") && scope == "unstaged" {
		hunks = FileDiffUnifiedNewFile(f.Content)
	} else {
		h, err := FileDiffScoped(path, scope, baseRef)
		if err == nil {
			hunks = h
		}
	}
	if hunks == nil {
		hunks = []DiffHunk{}
	}
	return map[string]any{"hunks": hunks}, true
}
```

**Step 5: Update `handleSession` and `handleFileDiff` in `server.go`**

```go
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	scope := r.URL.Query().Get("scope")
	writeJSON(w, s.session.GetSessionInfoScoped(scope))
}

func (s *Server) handleFileDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path query parameter required", http.StatusBadRequest)
		return
	}
	scope := r.URL.Query().Get("scope")
	snapshot, ok := s.session.GetFileDiffSnapshotScoped(path, scope)
	if !ok {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	writeJSON(w, snapshot)
}
```

**Step 6: Add `available_scopes` to session info**

Add to `SessionInfo`:

```go
type SessionInfo struct {
	Mode            string            `json:"mode"`
	Branch          string            `json:"branch"`
	BaseRef         string            `json:"base_ref"`
	ReviewRound     int               `json:"review_round"`
	AvailableScopes []string          `json:"available_scopes"`
	Files           []SessionFileInfo `json:"files"`
}
```

In `GetSessionInfo()` and `GetSessionInfoScoped()`, populate:

```go
info.AvailableScopes = []string{"all", "staged", "unstaged"}
if s.BaseRef != "" {
	// On feature branch — "branch" scope is meaningful
	info.AvailableScopes = []string{"all", "branch", "staged", "unstaged"}
}
```

**Step 7: Run tests**

Run: `cd crit && go test ./... -v`
Expected: PASS

**Step 8: Commit**

```bash
git add crit/server.go crit/session.go crit/server_test.go
git commit -m "feat: add scope query parameter to session and diff endpoints"
```

---

## Task 3: Add scope toggle UI to frontend

**Files:**
- Modify: `crit/frontend/index.html`
- Modify: `crit/frontend/style.css`
- Modify: `crit/frontend/app.js`

**Step 1: Add toggle HTML to `index.html`**

Add the scope toggle next to the diff mode toggle in the header-right div:

```html
<div class="scope-toggle" id="scopeToggle" style="display:none">
  <button class="toggle-btn active" data-scope="all" title="All changes">All</button>
  <button class="toggle-btn" data-scope="branch" title="Committed changes">Branch</button>
  <button class="toggle-btn" data-scope="staged" title="Staged changes">Staged</button>
  <button class="toggle-btn" data-scope="unstaged" title="Unstaged changes">Unstaged</button>
</div>
```

Place it BEFORE the diffModeToggle div (left of split/unified, since scope is a higher-level filter).

**Step 2: Add scope toggle styles to `style.css`**

The scope toggle reuses the exact same `.toggle-btn` styles as the diff mode toggle. Just needs the container class:

```css
.scope-toggle {
  display: inline-flex;
  gap: 0;
  background: var(--crit-toggle-bg);
  border-radius: 6px;
  padding: 2px;
}
```

This matches `.diff-mode-toggle` exactly — same visual treatment.

**Step 3: Add scope state and toggle handler to `app.js`**

Add state variable near the top (next to `diffMode`):

```javascript
let diffScope = getCookie('crit-diff-scope') || 'all';
```

Add toggle click handler (similar to diffModeToggle handler):

```javascript
document.getElementById('scopeToggle').addEventListener('click', async function(e) {
  var btn = e.target.closest('.toggle-btn');
  if (!btn || btn.classList.contains('active')) return;
  var scope = btn.dataset.scope;
  diffScope = scope;
  setCookie('crit-diff-scope', scope);
  // Update active states
  document.querySelectorAll('#scopeToggle .toggle-btn').forEach(function(b) {
    b.classList.toggle('active', b.dataset.scope === scope);
  });
  // Re-fetch everything for the new scope
  await reloadForScope();
});
```

**Step 4: Add `reloadForScope()` function**

```javascript
async function reloadForScope() {
  // Show loading state
  document.getElementById('filesContainer').innerHTML =
    '<div class="loading" style="padding: 40px; text-align: center; color: var(--fg-muted);">Loading...</div>';

  // Re-fetch session with scope
  const sessionRes = await fetch('/api/session?scope=' + enc(diffScope)).then(r => r.json());
  session = sessionRes;

  if (!session.files || session.files.length === 0) {
    document.getElementById('filesContainer').innerHTML =
      '<div class="loading" style="padding: 40px; text-align: center; color: var(--fg-muted);">No ' + diffScope + ' changes</div>';
    files = [];
    renderFileTree();
    updateCommentCount();
    updateViewedCount();
    return;
  }

  files = await loadAllFileDataScoped(session.files, diffScope);
  files.sort(fileSortComparator);
  restoreViewedState();
  renderFileTree();
  renderAllFiles();
  buildToc();
  updateCommentCount();
  updateViewedCount();
}
```

**Step 5: Add `loadAllFileDataScoped()` function**

Similar to `loadAllFileData` but passes scope to the diff endpoint:

```javascript
async function loadAllFileDataScoped(fileInfos, scope) {
  return Promise.all(fileInfos.map(async (fi) => {
    var diffUrl = '/api/file/diff?path=' + enc(fi.path);
    if (scope && scope !== 'all') {
      diffUrl += '&scope=' + enc(scope);
    }
    const [fileRes, commentsRes, diffRes] = await Promise.all([
      fetch('/api/file?path=' + enc(fi.path)).then(r => r.json()),
      fetch('/api/file/comments?path=' + enc(fi.path)).then(r => r.json()),
      fetch(diffUrl).then(r => r.json()).catch(function() { return { hunks: [] }; }),
    ]);
    // ... same file object construction as loadAllFileData
  }));
}
```

Alternatively (and preferably — DRY), refactor `loadAllFileData` to accept an optional scope parameter:

```javascript
async function loadAllFileData(fileInfos, scope) {
  return Promise.all(fileInfos.map(async (fi) => {
    var diffUrl = '/api/file/diff?path=' + enc(fi.path);
    if (scope && scope !== 'all') {
      diffUrl += '&scope=' + enc(scope);
    }
    // ... rest unchanged
  }));
}
```

Then update all call sites: `loadAllFileData(session.files)` → `loadAllFileData(session.files, diffScope)`.

**Step 6: Show toggle on init and sync with available scopes**

In `init()`, after loading session data:

```javascript
if (session.mode === 'git') {
  var scopeToggle = document.getElementById('scopeToggle');
  scopeToggle.style.display = '';

  // Hide "Branch" button if not on feature branch
  var scopes = session.available_scopes || ['all', 'staged', 'unstaged'];
  scopeToggle.querySelectorAll('.toggle-btn').forEach(function(b) {
    if (scopes.indexOf(b.dataset.scope) === -1) {
      b.style.display = 'none';
    }
  });

  // Sync active state with persisted scope
  // If persisted scope isn't available (e.g., "branch" but now on default), reset to "all"
  if (scopes.indexOf(diffScope) === -1) {
    diffScope = 'all';
    setCookie('crit-diff-scope', 'all');
  }
  scopeToggle.querySelectorAll('.toggle-btn').forEach(function(b) {
    b.classList.toggle('active', b.dataset.scope === diffScope);
  });
}
```

**Step 7: Update init to use scope on initial load**

In `init()`, change the session fetch to include scope:

```javascript
const [sessionRes, configRes] = await Promise.all([
  fetch('/api/session?scope=' + enc(diffScope)).then(r => r.json()),
  fetch('/api/config').then(r => r.json()),
]);
```

And update `loadAllFileData` call:

```javascript
files = await loadAllFileData(session.files || [], diffScope);
```

**Step 8: Commit**

```bash
git add crit/frontend/index.html crit/frontend/style.css crit/frontend/app.js
git commit -m "feat: add diff scope toggle UI (all/branch/staged/unstaged)"
```

---

## Task 4: Handle SSE events with scope awareness

**Files:**
- Modify: `crit/frontend/app.js`

When an SSE `file-changed` event fires (e.g., after round completion or file watching detects changes), the reload should respect the current scope.

**Step 1: Find and update SSE event handlers**

In the SSE `file-changed` handler, the current code re-fetches session data. Update it to pass the scope:

```javascript
// In the SSE handler for 'file-changed':
const sessionRes = await fetch('/api/session?scope=' + enc(diffScope)).then(r => r.json());
// ... and pass scope to loadAllFileData
```

Similarly, `edit-detected` events should not change the scope — they just update the edit counter.

**Step 2: Commit**

```bash
git add crit/frontend/app.js
git commit -m "fix: respect diff scope on SSE file-changed reload"
```

---

## Task 5: E2E tests for scope toggle

**Files:**
- Create: `crit/e2e/tests/scope-toggle.spec.ts`
- Modify: `crit/e2e/setup-fixtures.sh` (if needed — may need staged/unstaged fixtures)

**Step 1: Set up fixtures with staged and unstaged changes**

The existing `setup-fixtures.sh` creates a feature branch with committed changes. Extend it to also:
- Stage a file change (but don't commit) → appears in "staged"
- Modify a tracked file without staging → appears in "unstaged"
- Leave an untracked file → appears in "unstaged"

**Step 2: Write scope toggle tests**

```typescript
// scope-toggle.spec.ts
import { test, expect } from '@playwright/test';

test('scope toggle is visible in git mode', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('#scopeToggle')).toBeVisible();
  await expect(page.locator('#scopeToggle .toggle-btn[data-scope="all"]')).toHaveClass(/active/);
});

test('switching to staged scope shows only staged files', async ({ page }) => {
  await page.goto('/');
  await page.click('#scopeToggle .toggle-btn[data-scope="staged"]');
  // Verify file list updated to show only staged files
  // ... assertions depend on fixture setup
});

test('switching to unstaged scope shows only unstaged files', async ({ page }) => {
  await page.goto('/');
  await page.click('#scopeToggle .toggle-btn[data-scope="unstaged"]');
  // Verify file list updated
});

test('branch scope hidden on default branch', async ({ page }) => {
  // This would need a separate fixture running on main
  // May skip for now or test via API
});

test('scope persists across page reload', async ({ page }) => {
  await page.goto('/');
  await page.click('#scopeToggle .toggle-btn[data-scope="staged"]');
  await page.reload();
  await expect(page.locator('#scopeToggle .toggle-btn[data-scope="staged"]')).toHaveClass(/active/);
});

test('empty scope shows message', async ({ page }) => {
  await page.goto('/');
  // Click a scope that has no files (depends on fixture)
  // Verify empty state message
});
```

**Step 3: Run E2E tests**

Run: `cd crit && make e2e`
Expected: PASS

**Step 4: Commit**

```bash
git add crit/e2e/
git commit -m "test: add E2E tests for diff scope toggle"
```

---

## Summary of files changed

| File | Change |
|------|--------|
| `crit/git.go` | Add `changedFilesStaged`, `changedFilesUnstaged`, `changedFilesBranch`, `ChangedFilesScoped`, `FileDiffScoped` |
| `crit/git_test.go` | Add tests for scoped functions |
| `crit/session.go` | Add `GetSessionInfoScoped`, `GetFileDiffSnapshotScoped`, `AvailableScopes` field |
| `crit/server.go` | Read `scope` query param in `handleSession` and `handleFileDiff` |
| `crit/server_test.go` | Add tests for scoped endpoints |
| `crit/frontend/index.html` | Add scope toggle HTML |
| `crit/frontend/style.css` | Add `.scope-toggle` style (minimal — reuses `.toggle-btn`) |
| `crit/frontend/app.js` | Add `diffScope` state, toggle handler, `reloadForScope()`, scope-aware data loading |
| `crit/e2e/tests/scope-toggle.spec.ts` | New E2E test file |
| `crit/e2e/setup-fixtures.sh` | Add staged/unstaged fixture files |
