# Orphaned Comments Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface comments on files that have been removed from the review (e.g., added then deleted on a branch) as "orphaned" phantom file sections with outdated styling and full CRUD support.

**Architecture:** Backend detects orphaned paths in the review file (paths with comments but no matching session file), creates minimal phantom `FileEntry` structs with `Orphaned: true`. Frontend renders these as collapsed-open sections with an "Outdated" badge on each comment and a "Removed" status badge on the file header. All existing comment CRUD works unchanged since phantom entries are real `FileEntry` objects.

**Tech Stack:** Go backend, vanilla JS frontend, CSS custom properties for theming.

**Spec:** `docs/superpowers/specs/2026-04-16-orphaned-comments-design.md`

---

### Task 1: Backend — Add `Orphaned` field and orphan detection helper

**Files:**
- Modify: `session.go:83-108` (FileEntry struct)
- Modify: `session.go:2002-2010` (SessionFileInfo struct)
- Modify: `session.go:1776-1827` (loadCritJSON method)
- Modify: `session.go:2013-2059` (GetSessionInfo method)
- Test: `session_test.go`

- [ ] **Step 1: Write failing test for orphan detection in loadCritJSON**

Add a test that creates a session with one file, writes a review file with comments on two paths (one matching, one orphaned), calls `loadCritJSON()`, and asserts the orphaned path appears as a new `FileEntry` with `Orphaned: true`.

```go
func TestLoadCritJSON_OrphanedComments(t *testing.T) {
	dir := initTestRepo(t)
	branch := "main"

	// Create session with just one file
	writeFile(t, dir, "existing.md", "# Hello")
	s := &Session{
		Mode:     "git",
		Branch:   branch,
		RepoRoot: dir,
		Files: []*FileEntry{
			{Path: "existing.md", AbsPath: filepath.Join(dir, "existing.md"), Status: "modified", FileType: "markdown"},
		},
		ReviewRound: 1,
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	// Write a review file with comments on both existing and orphaned paths
	critPath := s.critJSONPath()
	cj := CritJSON{
		Branch:      branch,
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"existing.md": {
				Status: "modified",
				Comments: []Comment{
					{ID: "c_exist1", Body: "comment on existing", Scope: "line", StartLine: 1, EndLine: 1},
				},
			},
			"removed.go": {
				Status: "added",
				Comments: []Comment{
					{ID: "c_orphan1", Body: "file-level comment", Scope: "file"},
					{ID: "c_orphan2", Body: "line comment on removed file", Scope: "line", StartLine: 5, EndLine: 10},
				},
			},
		},
	}
	data, err := json.Marshal(cj)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(critPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(critPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	s.loadCritJSON()

	// Should now have 2 files
	if len(s.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(s.Files))
	}

	// Find the orphaned file
	var orphaned *FileEntry
	for _, f := range s.Files {
		if f.Path == "removed.go" {
			orphaned = f
			break
		}
	}
	if orphaned == nil {
		t.Fatal("orphaned file not found in session")
	}
	if !orphaned.Orphaned {
		t.Error("expected Orphaned=true")
	}
	if orphaned.Status != "removed" {
		t.Errorf("expected status 'removed', got %q", orphaned.Status)
	}
	if orphaned.FileType != "code" {
		t.Errorf("expected file type 'code', got %q", orphaned.FileType)
	}
	if len(orphaned.Comments) != 2 {
		t.Errorf("expected 2 comments, got %d", len(orphaned.Comments))
	}

	// Existing file should still have its comment
	var existing *FileEntry
	for _, f := range s.Files {
		if f.Path == "existing.md" {
			existing = f
			break
		}
	}
	if len(existing.Comments) != 1 {
		t.Errorf("expected 1 comment on existing file, got %d", len(existing.Comments))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd .worktrees/orphaned-comments && go test -run TestLoadCritJSON_OrphanedComments -v`
Expected: FAIL — `FileEntry` has no `Orphaned` field yet.

- [ ] **Step 3: Add `Orphaned` field to `FileEntry` and `SessionFileInfo`**

In `session.go`, add the `Orphaned` field to `FileEntry` (after the `Lazy` field block around line 101):

```go
// Orphaned: file has comments in the review file but is no longer in the session's
// file list (e.g., added on branch then deleted). No content or diff available.
Orphaned bool `json:"-"`
```

In `session.go`, add the `Orphaned` field to `SessionFileInfo` (around line 2009):

```go
Orphaned bool `json:"orphaned,omitempty"`
```

- [ ] **Step 4: Implement orphan detection in `loadCritJSON`**

In `session.go`, after the existing comment-restore loop (line 1818), add orphan detection. Build a set of known paths, then iterate `cj.Files` to find orphaned paths:

```go
// Detect orphaned paths: files in the review file with comments but not in the session.
knownPaths := make(map[string]bool, len(s.Files))
for _, f := range s.Files {
	knownPaths[f.Path] = true
}
for path, cf := range cj.Files {
	if knownPaths[path] || len(cf.Comments) == 0 {
		continue
	}
	fe := &FileEntry{
		Path:     path,
		Status:   "removed",
		FileType: detectFileType(path),
		Comments: cf.Comments,
		Orphaned: true,
	}
	for i := range fe.Comments {
		if fe.Comments[i].Scope == "" {
			fe.Comments[i].Scope = "line"
		}
	}
	s.Files = append(s.Files, fe)
}
```

- [ ] **Step 5: Propagate `Orphaned` in `GetSessionInfo`**

In `session.go` `GetSessionInfo()`, inside the file loop (around line 2033-2057), set `fi.Orphaned`:

```go
fi := SessionFileInfo{
	Path:         f.Path,
	Status:       f.Status,
	FileType:     f.FileType,
	CommentCount: len(f.Comments),
	Lazy:         f.Lazy,
	Orphaned:     f.Orphaned,
}
```

Do the same in `GetSessionInfoScoped()` — search for where it builds `SessionFileInfo` and add `Orphaned: f.Orphaned`.

- [ ] **Step 6: Run test to verify it passes**

Run: `cd .worktrees/orphaned-comments && go test -run TestLoadCritJSON_OrphanedComments -v`
Expected: PASS

- [ ] **Step 7: Write test for orphaned path with no comments (should NOT create phantom)**

```go
func TestLoadCritJSON_OrphanedNoComments(t *testing.T) {
	dir := initTestRepo(t)

	s := &Session{
		Mode:     "git",
		Branch:   "main",
		RepoRoot: dir,
		Files: []*FileEntry{
			{Path: "existing.md", AbsPath: filepath.Join(dir, "existing.md"), Status: "modified", FileType: "markdown"},
		},
		ReviewRound: 1,
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	critPath := s.critJSONPath()
	cj := CritJSON{
		Branch:      "main",
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"existing.md": {Status: "modified"},
			"removed.go":  {Status: "added", Comments: []Comment{}}, // no comments
		},
	}
	data, _ := json.Marshal(cj)
	os.MkdirAll(filepath.Dir(critPath), 0o755)
	os.WriteFile(critPath, data, 0o644)

	s.loadCritJSON()

	if len(s.Files) != 1 {
		t.Fatalf("expected 1 file (no phantom for empty comments), got %d", len(s.Files))
	}
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `cd .worktrees/orphaned-comments && go test -run TestLoadCritJSON_OrphanedNoComments -v`
Expected: PASS (the code already skips paths with 0 comments)

- [ ] **Step 9: Write test for `GetSessionInfo` including orphaned field**

```go
func TestGetSessionInfo_OrphanedField(t *testing.T) {
	s := &Session{
		Mode:   "git",
		Branch: "main",
		Files: []*FileEntry{
			{Path: "real.go", Status: "modified", FileType: "code"},
			{Path: "gone.go", Status: "removed", FileType: "code", Orphaned: true,
				Comments: []Comment{{ID: "c1", Body: "orphaned", Scope: "file"}}},
		},
		ReviewRound: 1,
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	info := s.GetSessionInfo()
	if len(info.Files) != 2 {
		t.Fatalf("expected 2 files in session info, got %d", len(info.Files))
	}

	var orphanedInfo *SessionFileInfo
	for i := range info.Files {
		if info.Files[i].Path == "gone.go" {
			orphanedInfo = &info.Files[i]
			break
		}
	}
	if orphanedInfo == nil {
		t.Fatal("orphaned file not in session info")
	}
	if !orphanedInfo.Orphaned {
		t.Error("expected Orphaned=true in session info")
	}
	if orphanedInfo.Status != "removed" {
		t.Errorf("expected status 'removed', got %q", orphanedInfo.Status)
	}
	if orphanedInfo.CommentCount != 1 {
		t.Errorf("expected comment count 1, got %d", orphanedInfo.CommentCount)
	}
}
```

- [ ] **Step 10: Run test to verify it passes**

Run: `cd .worktrees/orphaned-comments && go test -run TestGetSessionInfo_OrphanedField -v`
Expected: PASS

- [ ] **Step 11: Run full test suite**

Run: `cd .worktrees/orphaned-comments && go test ./... -v 2>&1 | tail -20`
Expected: All tests PASS

- [ ] **Step 12: Commit**

```bash
cd .worktrees/orphaned-comments
git add session.go session_test.go
git commit -m "feat: detect orphaned comments and create phantom file entries"
```

---

### Task 2: Backend — Restore orphaned comments during round-complete

**Files:**
- Modify: `watch.go:333-353` (handleRoundCompleteGit)
- Modify: `watch.go:358-378` (handleRoundCompleteFiles)
- Modify: `session.go` (extract helper)
- Test: `watch_test.go`

- [ ] **Step 1: Write failing test for orphan restoration after RefreshFileList**

This test simulates the round-complete flow: a file has comments in the review file but disappears from `ChangedFiles()` after `RefreshFileList()`. The orphan detection should restore it.

```go
func TestHandleRoundComplete_RestoresOrphanedComments(t *testing.T) {
	dir := initTestRepo(t)

	// Create a file on the branch so it shows up initially
	writeFile(t, dir, "temp.go", "package main")
	runGit(t, dir, "add", "temp.go")
	runGit(t, dir, "commit", "-m", "add temp")

	s := &Session{
		Mode:     "git",
		Branch:   "main",
		RepoRoot: dir,
		Files: []*FileEntry{
			{Path: "temp.go", AbsPath: filepath.Join(dir, "temp.go"), Status: "added", FileType: "code",
				Comments: []Comment{{ID: "c_temp1", Body: "this will be orphaned", Scope: "file"}}},
		},
		ReviewRound: 1,
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	// Save the review file with comments
	s.save()

	// Delete the file and commit — now git diff shows no net change
	runGit(t, dir, "rm", "temp.go")
	runGit(t, dir, "commit", "-m", "remove temp")

	// RefreshFileList will drop temp.go from s.Files
	s.RefreshFileList()

	// Verify temp.go is gone
	found := false
	for _, f := range s.Files {
		if f.Path == "temp.go" {
			found = true
		}
	}
	if found {
		t.Fatal("expected temp.go to be gone after RefreshFileList")
	}

	// Now restore orphaned comments
	s.restoreOrphanedComments()

	// temp.go should be back as orphaned
	var orphaned *FileEntry
	for _, f := range s.Files {
		if f.Path == "temp.go" {
			orphaned = f
			break
		}
	}
	if orphaned == nil {
		t.Fatal("orphaned file not restored after round-complete")
	}
	if !orphaned.Orphaned {
		t.Error("expected Orphaned=true")
	}
	if len(orphaned.Comments) != 1 {
		t.Errorf("expected 1 comment, got %d", len(orphaned.Comments))
	}
}
```

Note: This test may need adjustment to match the actual `save()` method name. Check the codebase — the method that writes the review file is likely `saveCritJSON()` or similar. The test helper `initTestRepo` sets up the `~/.crit/reviews/` directory structure.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd .worktrees/orphaned-comments && go test -run TestHandleRoundComplete_RestoresOrphanedComments -v`
Expected: FAIL — `restoreOrphanedComments` method doesn't exist yet.

- [ ] **Step 3: Extract `restoreOrphanedComments` helper**

Extract the orphan detection logic from `loadCritJSON` into a reusable method on `*Session`. This method reads the review file, finds paths with comments not in `s.Files`, and appends phantom entries. It must be called with `s.mu` NOT held (it acquires the lock internally).

In `session.go`, add:

```go
// restoreOrphanedComments reads the review file and creates phantom FileEntry
// objects for any paths that have comments but aren't in s.Files.
// Safe to call multiple times — existing orphaned entries are skipped.
func (s *Session) restoreOrphanedComments() {
	data, err := os.ReadFile(s.critJSONPath())
	if err != nil {
		return
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	knownPaths := make(map[string]bool, len(s.Files))
	for _, f := range s.Files {
		knownPaths[f.Path] = true
	}
	for path, cf := range cj.Files {
		if knownPaths[path] || len(cf.Comments) == 0 {
			continue
		}
		fe := &FileEntry{
			Path:     path,
			Status:   "removed",
			FileType: detectFileType(path),
			Comments: cf.Comments,
			Orphaned: true,
		}
		for i := range fe.Comments {
			if fe.Comments[i].Scope == "" {
				fe.Comments[i].Scope = "line"
			}
		}
		s.Files = append(s.Files, fe)
	}
}
```

Then refactor `loadCritJSON` to call this helper instead of duplicating the logic. Since `loadCritJSON` is called during init (before any concurrent access), you can either:
- Call `restoreOrphanedComments()` at the end of `loadCritJSON` (but it re-reads the file — acceptable for init)
- Or inline the orphan loop in `loadCritJSON` and have `restoreOrphanedComments` be the standalone version

The pragmatic approach: keep the inline version in `loadCritJSON` (it already has the parsed `cj`), and have `restoreOrphanedComments` as the standalone version for round-complete. Both use the same logic but `loadCritJSON` avoids re-reading the file.

- [ ] **Step 4: Call `restoreOrphanedComments` in `handleRoundCompleteGit`**

In `watch.go` `handleRoundCompleteGit()` (line 333), add the call after `carryForwardAllComments()` and before `ReviewRound++`:

```go
func (s *Session) handleRoundCompleteGit() {
	s.mu.RLock()
	edits := s.lastRoundEdits
	s.mu.RUnlock()

	s.loadResolvedComments()
	s.RefreshFileList()

	s.mu.Lock()
	s.rereadFileContents(false)
	s.carryForwardAllComments()
	s.mu.Unlock()

	// Restore phantom entries for files that disappeared but have comments in the review file
	s.restoreOrphanedComments()

	s.mu.Lock()
	s.ReviewRound++
	s.mu.Unlock()

	s.RefreshDiffs()
	s.finishRoundComplete(edits)
}
```

Note: `restoreOrphanedComments` acquires its own lock, so it must be called outside the `s.mu.Lock()` block. This means splitting the existing `s.mu.Lock()` block that covers `rereadFileContents`, `carryForwardAllComments`, and `ReviewRound++` into two parts. Verify that this split is safe — `rereadFileContents` and `carryForwardAllComments` must complete before `restoreOrphanedComments` reads the file list, which they will since this runs sequentially on the watcher goroutine.

- [ ] **Step 5: Call `restoreOrphanedComments` in `handleRoundCompleteFiles`**

In `watch.go` `handleRoundCompleteFiles()` (line 358), add the call in the same position — after carry-forward, before `ReviewRound++`:

```go
func (s *Session) handleRoundCompleteFiles() {
	s.mu.RLock()
	edits := s.lastRoundEdits
	s.mu.RUnlock()

	s.loadResolvedComments()
	s.carryForwardComments()

	s.mu.Lock()
	s.carryForwardAllComments()
	s.mu.Unlock()

	// Restore phantom entries for files that disappeared but have comments
	s.restoreOrphanedComments()

	s.mu.Lock()
	s.rereadFileContents(true)
	s.ReviewRound++
	s.mu.Unlock()

	s.finishRoundComplete(edits)
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `cd .worktrees/orphaned-comments && go test -run TestHandleRoundComplete_RestoresOrphanedComments -v`
Expected: PASS

- [ ] **Step 7: Run full test suite**

Run: `cd .worktrees/orphaned-comments && go test ./... 2>&1 | tail -5`
Expected: All tests PASS

- [ ] **Step 8: Commit**

```bash
cd .worktrees/orphaned-comments
git add session.go watch.go watch_test.go
git commit -m "feat: restore orphaned comments after round-complete file refresh"
```

---

### Task 3: Frontend — Skip fetches for orphaned files and render phantom sections

**Files:**
- Modify: `frontend/app.js` (loadAllFileData, loadSingleFile, renderFile)

- [ ] **Step 1: Handle orphaned files in `loadAllFileData`**

In `frontend/app.js` `loadAllFileData()` (line 235), add orphaned file handling alongside the lazy file path. Orphaned files get placeholders like lazy files but with `orphaned: true`:

In the `if (!hasLazy)` early return (line 239-241), change it to also check for orphaned files. The simplest approach: handle orphaned files in `loadSingleFile` by returning a placeholder when `fi.orphaned` is true.

In `loadSingleFile()` (line 284), add an early return at the top:

```javascript
async function loadSingleFile(fi, scope) {
    // Orphaned files have no content or diff on the server — skip fetches
    if (fi.orphaned) {
      return {
        path: fi.path,
        status: fi.status,
        fileType: fi.file_type,
        content: '',
        previousContent: '',
        comments: Array.isArray(fi._prefetchedComments) ? fi._prefetchedComments : [],
        diffHunks: [],
        lineBlocks: null,
        previousLineBlocks: null,
        tocItems: [],
        collapsed: false, // open by default so orphaned comments are visible
        viewMode: 'document',
        additions: 0,
        deletions: 0,
        lazy: false,
        orphaned: true,
      };
    }
    // ... existing code
```

Wait — orphaned files still need their comments fetched from the server. The `/api/file/comments?path=X` endpoint will work because the backend has a phantom `FileEntry`. So we should fetch comments but skip content and diff:

```javascript
async function loadSingleFile(fi, scope) {
    // Orphaned files have no content or diff — only fetch comments
    if (fi.orphaned) {
      const comments = await fetch('/api/file/comments?path=' + enc(fi.path))
        .then(function(r) { return r.ok ? r.json() : []; })
        .catch(function() { return []; });
      return {
        path: fi.path,
        status: fi.status,
        fileType: fi.file_type,
        content: '',
        previousContent: '',
        comments: Array.isArray(comments) ? comments : [],
        diffHunks: [],
        lineBlocks: null,
        previousLineBlocks: null,
        tocItems: [],
        collapsed: false,
        viewMode: 'document',
        additions: 0,
        deletions: 0,
        lazy: false,
        orphaned: true,
      };
    }
    // ... rest of existing loadSingleFile
```

Also set `orphaned: false` in the normal return path (line 298-313) by adding `orphaned: false` to the object literal.

- [ ] **Step 2: Render orphaned file sections**

In `renderFile()`, the section body rendering (around line 1799) already has a branch for deleted files with no diff hunks. Add an orphaned branch before it:

```javascript
if (file.orphaned) {
  // Orphaned files: show placeholder, render all comments in file-comments container
  const placeholder = document.createElement('div');
  placeholder.className = 'diff-deleted-placeholder orphaned-placeholder';
  placeholder.textContent = 'This file is no longer part of the review.';
  body.appendChild(placeholder);
} else if (file.status === 'deleted' && (!file.diffHunks || file.diffHunks.length === 0)) {
```

- [ ] **Step 3: Render all comments (including line-scoped) in file-comments container for orphaned files**

The existing code at line 1774-1790 only renders file-scoped comments in the `file-comments` container. For orphaned files, ALL comments should render there (since there are no line blocks to anchor line comments to).

Modify the file-comments rendering block (line 1774):

```javascript
// File-level comments container (between header and file body)
const isOrphaned = file.orphaned;
const fileComments = isOrphaned
  ? file.comments // ALL comments for orphaned files
  : file.comments.filter(function(c) { return c.scope === 'file'; });
const fileForm = getFormsForFile(file.path).find(function(f) { return f.scope === 'file'; });
if (fileComments.length > 0 || (fileForm && !isOrphaned)) {
```

For orphaned files, each comment card should include the "Outdated" badge. Add the `outdated` class to comment elements:

```javascript
for (let ci = 0; ci < fileComments.length; ci++) {
  const comment = fileComments[ci];
  let el;
  if (comment.resolved) {
    el = createResolvedElement(comment, file.path);
  } else {
    el = createCommentElement(comment, file.path);
  }
  if (isOrphaned) {
    el.classList.add('outdated-comment');
    // Add "Outdated" badge to the comment header
    const badge = document.createElement('span');
    badge.className = 'outdated-badge';
    badge.textContent = 'Outdated';
    const headerLeft = el.querySelector('.comment-header-left');
    if (headerLeft) headerLeft.appendChild(badge);
  }
  fileCommentsContainer.appendChild(el);
}
// Only show the add-comment form for non-orphaned files
if (fileForm && !isOrphaned) {
  fileCommentsContainer.appendChild(createFileCommentForm(fileForm));
}
```

- [ ] **Step 4: Hide "Add file-level comment" button for orphaned files**

In the file-comment button creation (line 1746-1756), wrap it in a condition:

```javascript
// File comment button — not for orphaned files (no point adding comments to removed files)
if (!file.orphaned) {
  const fileCommentBtn = document.createElement('button');
  // ... existing code
  header.appendChild(fileCommentBtn);
}
```

- [ ] **Step 5: Add "Removed" badge label for orphaned files**

In the badge label logic (line 1684-1686), add a case for "removed":

```javascript
if (file.status === 'untracked') badgeLabel = 'New';
if (file.status === 'added') badgeLabel = 'New File';
if (file.status === 'removed') badgeLabel = 'Removed';
```

Also show the badge even in file mode for orphaned files — modify the `showBadge` condition (line 1683):

```javascript
const showBadge = session.mode === 'git' || file.orphaned;
```

- [ ] **Step 6: Add "removed" status icon to file tree**

In `fileStatusIcon()` (line 1226-1247), add a case for "removed" status. Use a neutral/gray icon — a document with an "x" badge:

```javascript
if (status === 'removed') {
  return '<svg class="tree-file-status-icon removed" viewBox="0 0 16 16">' + doc +
    '<rect x="8" y="8" width="7" height="7" rx="1.5" fill="var(--fg-dimmed)"/>' +
    '<path d="M10 10.5l3 3m0-3l-3 3" stroke="var(--bg-secondary)" stroke-width="1.2" fill="none"/></svg>';
}
```

Add this before the `// renamed or other` fallback (line 1245).

- [ ] **Step 7: Propagate `orphaned` flag through SSE reload**

When SSE triggers a session reload, the new session data flows through `loadAllFileData` again. Since `fi.orphaned` comes from the session API response and `loadSingleFile` handles it, this should work automatically. Verify by checking `reloadSessionAndFiles()` or equivalent — ensure it passes through `fi.orphaned`.

Search for where session reload builds the file info list and confirm it preserves the `orphaned` field. The session API response already includes `orphaned: true` from the backend changes in Task 1.

- [ ] **Step 8: Commit**

```bash
cd .worktrees/orphaned-comments
git add frontend/app.js
git commit -m "feat: render orphaned file sections with outdated comments"
```

---

### Task 4: CSS — Theme variables and orphaned comment styling

**Files:**
- Modify: `frontend/theme.css` (all 4 theme blocks)
- Modify: `frontend/style.css`

- [ ] **Step 1: Add CSS variables to all 4 theme blocks**

In `frontend/theme.css`, add these variables to each of the 4 theme blocks. The 4 blocks are:
1. `:root` (dark default) — around line 1-70
2. `@media (prefers-color-scheme: light) html:not([data-theme])` — around line 100-160
3. `[data-theme="dark"]` — around line 180-240
4. `[data-theme="light"]` — around line 260-320

Add near the existing `--badge-*` variables in each block:

**Dark theme blocks (`:root` and `[data-theme="dark"]`):**
```css
--badge-removed-bg: rgba(169, 177, 214, 0.12);
--badge-removed-color: var(--fg-dimmed);
--badge-removed-border: color-mix(in srgb, var(--fg-dimmed) 20%, transparent);
--outdated-border: var(--yellow);
--outdated-bg: rgba(224, 175, 104, 0.06);
```

**Light theme blocks (`prefers-color-scheme: light` and `[data-theme="light"]`):**
```css
--badge-removed-bg: rgba(100, 100, 100, 0.08);
--badge-removed-color: var(--fg-dimmed);
--badge-removed-border: color-mix(in srgb, var(--fg-dimmed) 15%, transparent);
--outdated-border: var(--yellow);
--outdated-bg: rgba(154, 103, 0, 0.04);
```

- [ ] **Step 2: Add CSS rules for orphaned comment styling**

In `frontend/style.css`, add after the existing `.file-header-badge.deleted` rule (around line 3029):

```css
.file-header-badge.removed { background: var(--badge-removed-bg); color: var(--badge-removed-color); border: 1px solid var(--badge-removed-border); }
```

Add the outdated comment styling (near the existing comment card styles):

```css
/* Orphaned/outdated comments */
.outdated-comment .comment-card {
  border-left: 3px solid var(--outdated-border);
  background: var(--outdated-bg);
}

.outdated-badge {
  font-size: 10px;
  font-weight: 600;
  padding: 1px 5px;
  border-radius: 3px;
  background: var(--outdated-bg);
  color: var(--outdated-border);
  border: 1px solid color-mix(in srgb, var(--outdated-border) 30%, transparent);
}

.orphaned-placeholder {
  font-style: italic;
  color: var(--fg-dimmed);
  padding: 12px 16px;
}
```

- [ ] **Step 3: Run the CSS variable checker**

Run: `cd .worktrees/orphaned-comments && bash check-css-vars.sh 2>&1 || true`

If the checker exists, verify no undefined CSS variable warnings. If it doesn't exist, manually verify the new variables are defined in all 4 blocks.

- [ ] **Step 4: Commit**

```bash
cd .worktrees/orphaned-comments
git add frontend/theme.css frontend/style.css
git commit -m "feat: add orphaned comment and removed file badge styling"
```

---

### Task 5: E2E test — Orphaned comments on removed files

**Files:**
- Create: `e2e/tests/orphaned-comments.spec.ts`
- May modify: `e2e/setup-fixtures.sh` (if fixture changes needed)

- [ ] **Step 1: Write E2E test for orphaned comments**

Create `e2e/tests/orphaned-comments.spec.ts` (git-mode test, runs on port 3123).

The test needs to:
1. Add a file-level comment on a file via the API
2. Use `crit comment` CLI or API to add comments to a path
3. Then manipulate git state so the file disappears from the diff
4. Reload and verify the orphaned section appears

Since the E2E fixture creates a feature branch with specific files, the simplest approach is to use the comment API to add comments to a non-existent path. The backend will need to handle this — but actually, the backend requires the path to exist in the session to add comments. So we need a different approach.

**Alternative approach:** Use the `crit comment` CLI (which writes directly to the review file) to add a comment for a path that isn't in the session. Then reload and verify the phantom section appears.

```typescript
import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

test.describe('Orphaned comments', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('shows phantom section for file with orphaned comments', async ({ page, request }) => {
    await loadPage(page);

    // Use crit comment CLI to add a comment for a path not in the session
    // This writes directly to the review file, bypassing the session check
    const { exec } = require('child_process');
    // Actually, in Playwright we use the API. Let's write directly to the review file
    // or use the CLI via shell.

    // Better: use the crit comment CLI which writes to the review file
    // The E2E test runs against the fixture at a known port
    // We can exec crit comment in the fixture directory

    // For now, let's test via the API by first adding a comment to an existing file,
    // then verifying the comments panel shows it.
    // The full orphaned flow requires git manipulation which is complex in E2E.

    // Simpler test: verify the frontend handles orphaned files from the session API.
    // We can mock this by injecting a comment into the review file directly.
  });
});
```

**Pragmatic approach for E2E:** The cleanest way to test this is:

1. Use `crit comment` CLI (which writes to the review file without needing the server) to add a comment for a non-existent path
2. The server picks up the review file change via its file watcher
3. Verify the phantom section appears

```typescript
import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';
import { execSync } from 'child_process';
import * as path from 'path';
import * as fs from 'fs';

const FIXTURE_DIR = path.resolve(__dirname, '..', 'fixtures', 'git-mode');

test.describe('Orphaned comments', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('renders phantom section for orphaned file comments', async ({ page, request }) => {
    await loadPage(page);

    // Write a comment for a non-existent path directly to the review file
    // using crit comment CLI in the fixture directory
    execSync(
      'crit comment "nonexistent.go:1" "This comment is orphaned" --author "Test"',
      { cwd: FIXTURE_DIR, timeout: 5000 }
    );

    // Wait for SSE to pick up the change and re-render
    // The orphaned file section should appear
    await expect(async () => {
      const section = page.locator('[id^="file-section-nonexistent.go"]');
      await expect(section).toBeVisible();
    }).toPass({ timeout: 10000 });

    // Verify the "Removed" badge
    const badge = page.locator('.file-header-badge.removed');
    await expect(badge).toBeVisible();
    await expect(badge).toHaveText('Removed');

    // Verify the comment is visible and styled as outdated
    const comment = page.locator('.outdated-comment');
    await expect(comment).toBeVisible();

    // Verify the placeholder text
    const placeholder = page.locator('.orphaned-placeholder');
    await expect(placeholder).toHaveText('This file is no longer part of the review.');

    // Verify the comment can be resolved from inline
    const resolveBtn = page.locator('.outdated-comment .resolve-btn');
    await resolveBtn.click();

    // After resolving, the comment should show as resolved
    await expect(page.locator('.outdated-comment .resolved-card')).toBeVisible();
  });

  test('orphaned comments appear in comments panel', async ({ page, request }) => {
    await loadPage(page);

    execSync(
      'crit comment "orphan-file.md:5-10" "Check this range" --author "Test"',
      { cwd: FIXTURE_DIR, timeout: 5000 }
    );

    // Wait for the comment to appear
    await expect(async () => {
      const badge = page.locator('.header-comments-count');
      await expect(badge).toBeVisible();
    }).toPass({ timeout: 10000 });

    // Open the comments panel
    await page.locator('.header-comments-btn').click();

    // Verify the orphaned file group appears in the panel
    const panelGroup = page.locator('.comments-panel-file-name', { hasText: 'orphan-file.md' });
    await expect(panelGroup).toBeVisible();

    // Verify clicking scrolls to the phantom section
    const panelCard = page.locator('.panel-comment-block').first();
    await panelCard.click();

    const section = page.locator('[id^="file-section-orphan-file.md"]');
    await expect(section).toBeVisible();
  });

  test('no add-comment button on orphaned files', async ({ page, request }) => {
    await loadPage(page);

    execSync(
      'crit comment "gone.tsx:1" "orphaned comment" --author "Test"',
      { cwd: FIXTURE_DIR, timeout: 5000 }
    );

    await expect(async () => {
      const section = page.locator('[id^="file-section-gone.tsx"]');
      await expect(section).toBeVisible();
    }).toPass({ timeout: 10000 });

    // The file-comment button should NOT be present on orphaned sections
    const section = page.locator('[id^="file-section-gone.tsx"]');
    await expect(section.locator('.file-comment-btn')).toHaveCount(0);
  });
});
```

Note: The test assumes `crit comment` for a non-existent path triggers the orphan detection. This requires the SSE `comments-changed` event to cause a session re-read that includes orphan detection. If the current SSE flow doesn't re-run orphan detection, the test will fail and we'll need to ensure the comment-changed SSE handler in the backend calls `restoreOrphanedComments()`.

Verify the fixture directory path — check `e2e/playwright.config.ts` for where the git-mode fixture directory is. It's likely `e2e/fixtures/git-mode` or similar.

- [ ] **Step 2: Verify fixture directory path**

Run: `cd .worktrees/orphaned-comments && grep -n 'fixture\|FIXTURE\|cwd' e2e/playwright.config.ts | head -20`

Update the `FIXTURE_DIR` path in the test if needed.

- [ ] **Step 3: Run the E2E test**

Run: `cd .worktrees/orphaned-comments && make e2e` or specifically:
`cd .worktrees/orphaned-comments/e2e && npx playwright test tests/orphaned-comments.spec.ts --project=git-mode`

If the test fails, debug and fix. Common issues:
- SSE doesn't trigger orphan re-detection (need backend change)
- `crit comment` CLI path isn't in PATH (use absolute path to built binary)
- Fixture directory path is wrong
- CSS class names don't match

- [ ] **Step 4: Commit**

```bash
cd .worktrees/orphaned-comments
git add e2e/tests/orphaned-comments.spec.ts
git commit -m "test: add E2E tests for orphaned comments on removed files"
```

---

### Task 6: Verify `crit comment` CLI writes to orphaned paths correctly

**Files:**
- Possibly modify: `session.go` or `github.go` (if `crit comment` doesn't handle non-session paths)
- Test: `session_test.go` or `github_test.go`

- [ ] **Step 1: Verify `crit comment` behavior for non-session paths**

`crit comment` writes directly to the review file (it's a headless CLI command). Check whether it validates the path against the session or just writes to the file. Read the `crit comment` implementation.

Run: `cd .worktrees/orphaned-comments && grep -n 'func.*runComment\|"comment"' main.go | head -10`

Read the comment subcommand handler to understand if it creates the file entry in the review file when the path doesn't exist in the session.

- [ ] **Step 2: If needed, ensure `crit comment` can write to any path**

If `crit comment` validates against the session's file list and rejects non-session paths, we need to either:
- Remove that validation (since the CLI is headless and trusted)
- Or accept that orphaned comments only come from round-to-round transitions, not from CLI

This step may be a no-op if `crit comment` already writes to arbitrary paths.

- [ ] **Step 3: Run full test suite**

Run: `cd .worktrees/orphaned-comments && go test ./... 2>&1 | tail -5`
Expected: All tests PASS

- [ ] **Step 4: Commit if changes were needed**

```bash
cd .worktrees/orphaned-comments
git add -A
git commit -m "fix: allow crit comment CLI to write to non-session paths"
```

---

### Task 7: Final integration verification

- [ ] **Step 1: Build and run manual smoke test**

```bash
cd .worktrees/orphaned-comments
go build -o crit .
```

Manual test scenario:
1. Create a test repo with a feature branch
2. Add a file, commit
3. Run `./crit`, add a file-level comment on that file, finish review
4. Delete the file, commit
5. Run `./crit` again
6. Verify the deleted file appears as a phantom section with "Removed" badge
7. Verify the comment is styled as outdated and can be resolved

- [ ] **Step 2: Run full Go test suite**

Run: `cd .worktrees/orphaned-comments && go test ./... -v 2>&1 | tail -20`

- [ ] **Step 3: Run linter**

Run: `cd .worktrees/orphaned-comments && gofmt -l . && golangci-lint run ./...`

- [ ] **Step 4: Run E2E tests**

Run: `cd .worktrees/orphaned-comments && make e2e`

- [ ] **Step 5: Commit any final fixes**

If any issues found, fix and commit individually.
