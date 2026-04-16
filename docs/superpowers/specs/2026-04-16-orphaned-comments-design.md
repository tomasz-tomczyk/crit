# Orphaned Comments on Removed Files

## Problem

When a file is added on a branch, receives review comments, and is then deleted, `git diff` against the base shows no net change — the file vanishes from `ChangedFiles()`. Comments in the review file for that path have no matching `FileEntry` in the session, so they are silently dropped by `loadCritJSON()` (session.go:1809-1818). The comments become invisible in the UI with no way to resolve or delete them.

This also applies to any file that had comments but is no longer in the session's file list for any reason (e.g., added to ignore patterns after commenting).

## Design

### Backend: Restore orphaned comments as phantom files

**In `Session.loadCritJSON()`** (session.go:1776):

After restoring comments for existing files (the current loop at line 1809), add a second pass that detects orphaned paths — paths present in `cj.Files` that have comments but no matching `FileEntry` in `s.Files`.

For each orphaned path with at least one comment, create a minimal `FileEntry`:

```go
fe := &FileEntry{
    Path:     path,
    Status:   "removed",    // new status distinct from "deleted"
    FileType: detectFileType(path),
    Comments: cf.Comments,
    Orphaned: true,          // new bool field on FileEntry
}
```

- `Status: "removed"` — distinguishes from `"deleted"` (which means "file existed on base, deleted on branch" and has diff hunks). "Removed" means "was in a previous round but no longer in the session."
- `Orphaned: true` — a simple flag for frontend/backend to know this file has no content or diff.
- No content, no diff hunks, no line blocks.
- Append these phantom entries to `s.Files` at the end (after real files).

**In `GetSessionInfo()` / `GetSessionInfoScoped()`:**

The `SessionFileInfo` struct needs a new field:

```go
Orphaned bool `json:"orphaned,omitempty"`
```

This is set when `f.Orphaned` is true. The frontend uses this to render the phantom section differently.

**In `handleFileContent()` and `handleFileDiff()`:**

These already handle missing/empty content gracefully (return empty content or empty hunks). No changes needed — orphaned files will naturally return empty responses.

**Comment CRUD endpoints** (`handleFileComments`, `handleComment`, etc.):

These use `s.fileByPathLocked(path)` which will now find the phantom `FileEntry`. All comment operations (add, edit, delete, resolve, reply) work without changes because they operate on the `FileEntry.Comments` slice, not on file content.

**`saveCritJSON()`:**

Already iterates `s.Files` to build `cj.Files`. Phantom entries will be included automatically, preserving orphaned comments across saves.

### Frontend: Render phantom sections with outdated styling

**In `loadAllFileData()`** (app.js):

When the session response includes files with `orphaned: true`, skip the `/api/file` and `/api/file/diff` fetch calls for those files. Set `file.content = null`, `file.diffHunks = []`.

**In `renderFile()`** (app.js):

For orphaned files:

1. **Section header:** Show the file path with a "Removed" status badge (reuse existing badge styling with a new color — use a CSS variable like `--status-removed`).

2. **File body:** Show a placeholder: "This file is no longer part of the review."

3. **Comments:** All comments (both file-scoped and line-scoped) render in the file-comments container between the header and body, using the existing `createCommentElement` / `createResolvedElement` rendering path. This means resolve/edit/delete all work without any new code.

4. **Line-scoped comments on orphaned files:** Rendered the same as file-level comments but with a line reference label (e.g., "Lines 15-17") shown as text context. The `showLineRef` option in `buildCommentCard` already supports this.

5. **"Outdated" badge:** Each comment card on an orphaned file gets an "Outdated" label, similar to how `carried_forward` comments get a "Previous round" badge. Use a new CSS class `outdated-comment` with a distinct muted/faded style (reduced opacity or a different left-border color via `--outdated-border`).

6. **Not collapsed:** Unlike resolved comments, outdated comments render expanded so they're immediately visible.

7. **Disable "Add file-level comment" button** — no point adding new comments to a removed file.

8. **Disable gutter interactions** — no content means no line gutters to click.

**In the file tree:**

Orphaned files appear at the bottom of the tree with the "Removed" badge and their comment count badge. Clicking navigates to the phantom section.

**In `renderCommentsPanel()`:**

No changes needed — the panel already iterates `files` and renders comments for each. Since orphaned files are now in the `files` array, their comments appear in the panel. Clicking scrolls to the phantom section via `scrollToComment`.

**In the approve flow:**

No changes needed — unresolved comments on orphaned files are unresolved comments like any other. The existing check counts all unresolved comments across all files.

### CSS

New variables in all 4 theme blocks (`theme.css`):

- `--status-removed` — badge color for "Removed" status (a neutral/gray tone)
- `--outdated-border` — left border color for outdated comment cards (muted, e.g., `--color-warning` or a desaturated orange)
- `--outdated-bg` — subtle background tint for outdated comment cards

New classes in `style.css`:

- `.outdated-comment` — applied to comment cards on orphaned files. Adds the `--outdated-border` left border and `--outdated-bg` background.
- `.outdated-badge` — small inline label "Outdated" styled similarly to the existing `.carried-forward-badge`.
- `.status-removed` — badge variant for the file header.

### What doesn't change

- Review file format (`CritJSON`) — orphaned files are already stored there; we're just loading them now instead of dropping them.
- Comment CRUD endpoints — they operate on `FileEntry.Comments`, which phantom entries have.
- `saveCritJSON()` — already iterates all files.
- `renderCommentsPanel()` — already iterates all files.
- Approve/finish flow — already counts all unresolved comments.

### Edge cases

1. **File returns in a later round:** If the file reappears in git changes (e.g., re-added), the normal `loadCritJSON` path matches it by path and restores comments. The phantom entry is not created because the file is no longer orphaned. Comments lose their "outdated" status — they're live again on a real file.

2. **All orphaned comments resolved:** The phantom section still shows (with resolved comments collapsed as usual). The user can collapse the section via the `<details>` toggle. The file remains in the tree with a zero-count badge.

3. **`crit cleanup`:** Cleanup operates on review files by age, not by orphan status. No changes needed.

4. **File mode:** Less likely to hit this (files are explicitly provided), but the same logic applies if a watched file is deleted between rounds.

5. **Multi-round carry-forward (watch.go):** `RefreshFileList()` (watch.go:68) replaces `s.Files` with only the files from `ChangedFiles()`, dropping any phantom entries. Then `carryForwardAllComments()` runs on the new list — but orphaned files aren't in that list, so their comments are lost from memory. They survive only in the review file on disk.

   **Fix:** Extract orphan detection into a helper (e.g., `restoreOrphanedComments()`) called from two places:
   - `loadCritJSON()` — initial session load (already reading the review file)
   - `handleRoundCompleteGit()` — after `RefreshFileList()` + `carryForwardAllComments()`, re-read the review file and restore phantom entries for any paths with comments that aren't in the new `s.Files`

   In `handleRoundCompleteFiles()`, the same helper runs after the file list rebuild.
