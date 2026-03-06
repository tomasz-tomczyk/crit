# Multi-File Review Refactor

## Context

Crit currently reviews a single markdown file at a time (`crit plan.md`). We want to extend it to review multiple files — both plan markdown files and code changes from git — in a unified GitHub-PR-style UI. This enables reviewing an entire feature branch's changes alongside the plan file, with inline commenting on all of them.

Key changes:
- `crit` with no args auto-detects changed files via git
- `crit file1 file2 ...` reviews specific files
- All files shown in one UI with collapsible sections
- `.md` files render as full document (toggleable to diff); code files render as diffs
- Output moves from `.review.md` to `.crit.json` (structured, multi-file)
- Multi-round review preserved for markdown files

---

## Phase 1: Git Integration Layer

**New file: `git.go`** (~250 lines)

Shell out to `git` CLI for all operations. Functions:

| Function | What it does |
|----------|-------------|
| `IsGitRepo()` | `git rev-parse --is-inside-work-tree` |
| `RepoRoot()` | `git rev-parse --show-toplevel` |
| `DefaultBranch()` | Check remote HEAD, fallback to main/master existence |
| `CurrentBranch()` | `git rev-parse --abbrev-ref HEAD` |
| `IsOnDefaultBranch()` | Compare current branch to default |
| `MergeBase(base)` | `git merge-base HEAD <base>` |
| `ChangedFiles()` | Smart detection (see below) |
| `FileDiffUnified(path, baseRef)` | `git diff <baseRef> -- <path>` parsed into hunks |
| `WorkingTreeFingerprint()` | `git status --porcelain` — returns string to compare for change detection |

**Smart branch detection logic in `ChangedFiles()`:**
- On default branch (main/master): `git diff HEAD` (staged + unstaged) + `git ls-files --others --exclude-standard` (untracked)
- On feature branch: `git diff $(git merge-base HEAD main)` (all commits + working tree vs branch point) + untracked files

**Return type:**
```go
type FileChange struct {
    Path    string // relative to repo root
    Status  string // "added", "modified", "deleted", "renamed"
}
```

**New file: `git_test.go`** — Tests using temp git repos with real commits.

**Diff hunk types (also in `git.go`):**
```go
type DiffHunk struct {
    OldStart, OldCount int
    NewStart, NewCount int
    Header             string // the @@ line text
    Lines              []DiffLine
}
type DiffLine struct {
    Type    string // "context", "add", "del"
    Content string
    OldNum  int    // 0 if add
    NewNum  int    // 0 if del
}
```

---

## Phase 2: Session + Multi-File State Management

**New file: `session.go`** (~400 lines)

Replaces `Document` as the top-level state manager. Contains multiple `FileEntry` structs.

```go
type FileEntry struct {
    Path        string      // relative (e.g., "auth/middleware.go")
    AbsPath     string      // absolute on disk
    Status      string      // "added", "modified", "deleted", "untracked"
    FileType    string      // "markdown" or "code"
    Content     string      // current file content (read from disk)
    DiffHunks   []DiffHunk  // for code files: parsed git diff
    Comments    []Comment   // this file's comments
    nextID      int

    // Multi-round (markdown files only)
    PreviousContent  string
    PreviousComments []Comment
}

type Session struct {
    Files        []*FileEntry
    Branch       string
    BaseRef      string
    RepoRoot     string
    ReviewRound  int

    mu            sync.RWMutex
    subscribers   map[chan SSEEvent]struct{}
    subMu         sync.Mutex
    writeTimer    *time.Timer
    writeGen      int
    sharedURL     string
    deleteToken   string
    status        *Status
    roundComplete chan struct{}
    pendingEdits  int
    lastRoundEdits int
}
```

**Key methods (mirroring Document but multi-file-aware):**
- `NewSessionFromGit()` — calls `ChangedFiles()`, reads content, parses diffs
- `NewSessionFromFiles(paths []string)` — explicit files, determines type by extension
- `FileByPath(path string) *FileEntry`
- `AddComment(filePath string, start, end int, body string) Comment`
- `UpdateComment(filePath, id, body string) (Comment, bool)`
- `DeleteComment(filePath, id string) bool`
- `GetComments(filePath string) []Comment`
- `GetAllComments() map[string][]Comment`
- `WriteFiles()` — writes `.crit.json`
- `SignalRoundComplete()` — reloads markdown files, carries forward
- SSE: `Subscribe()`, `Unsubscribe()`, `notify()`, `Shutdown()`

**File type detection:** `filepath.Ext(path)` — `.md` = markdown, everything else = code.

**New file: `session_test.go`**

### `.crit.json` format

Written to repo root (or cwd for non-git).

```json
{
  "branch": "feat/add-auth",
  "base_ref": "abc123",
  "updated_at": "2026-02-28T12:00:00Z",
  "review_round": 1,
  "share_url": "",
  "delete_token": "",
  "files": {
    "plan.md": {
      "status": "added",
      "file_hash": "sha256:...",
      "comments": [
        {
          "id": "c1",
          "start_line": 5,
          "end_line": 10,
          "body": "Clarify this step",
          "created_at": "...",
          "updated_at": "...",
          "resolved": false
        }
      ]
    },
    "server.go": {
      "status": "modified",
      "file_hash": "sha256:...",
      "comments": [...]
    }
  }
}
```

---

## Phase 3: API Redesign

**Modify: `server.go`**

Change `Server.doc *Document` → `Server.session *Session`.

**New endpoints** (file-scoped via query param `?path=`):

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/session` | Session metadata: branch, baseRef, reviewRound, file list with status/stats/commentCounts |
| GET | `/api/file?path=X` | File content + metadata |
| GET | `/api/file/diff?path=X` | Diff hunks for a file (code: git diff; md: inter-round diff) |
| GET | `/api/file/comments?path=X` | Comments for one file |
| POST | `/api/file/comments?path=X` | Add comment `{start_line, end_line, body}` |
| PUT | `/api/comment/{id}?path=X` | Update comment |
| DELETE | `/api/comment/{id}?path=X` | Delete comment |

**Kept as-is** (session-scoped, not file-scoped):
- `POST /api/finish` — writes `.crit.json`, returns prompt referencing it
- `GET /api/events` — SSE stream (events gain `filepath` field)
- `POST /api/round-complete` — triggers reload of all markdown files
- `GET /api/config` — share URL, version info
- `POST/DELETE /api/share-url` — share management

**Removed endpoints:**
- `GET /api/document` — replaced by `/api/file?path=`
- `GET /api/comments` (unscoped) — replaced by `/api/file/comments?path=`
- `GET /api/stale` — rethink for multi-file (possibly per-file or removed)
- `GET /api/previous-round` — replaced by `/api/file/diff?path=` for markdown files
- `GET /api/diff` — replaced by `/api/file/diff?path=`

**Finish prompt changes to:**
```
Address review comments in .crit.json.
Mark resolved (set "resolved": true, optionally "resolution_note" and "resolution_lines").
When done run: `crit go <port>`
```

**`/files/` static serving:** Change from `doc.FileDir` to `session.RepoRoot` so images/assets from any file's relative paths resolve correctly.

---

## Phase 4: CLI Changes

**Modify: `main.go`**

Update argument parsing:

```go
if flag.NArg() == 0 {
    // No-args: git detection
    if !IsGitRepo() {
        printHelp(); os.Exit(1)
    }
    session = NewSessionFromGit()
} else {
    // Explicit files
    session = NewSessionFromFiles(flag.Args())
}
```

**File watching changes:**

All file types are watched — not just markdown. When the agent edits code files, the git diff changes and must be refreshed.

**Watching approach:**
- Poll `git status --porcelain` + `git diff --stat` every 1-2s to detect any working tree change
- On change detected: increment `session.pendingEdits`, send SSE `edit-detected` event
- Track which files changed (compare porcelain output) for targeted refresh
- **Also detect new files appearing** or files disappearing from the changeset (agent may create/delete files)

**On round-complete (`crit go <port>`):**
- Re-run `ChangedFiles()` to get the full updated file list (may include new files the agent created)
- Re-compute `FileDiffUnified()` for all code files (their git diff has changed)
- Re-read content for all markdown files, carry forward unresolved comments
- Send SSE `file-changed` event with the full refreshed session data
- Frontend re-fetches everything and re-renders

**Why git-level watching (not per-file mtime):**
- A single `git status` call catches all changes across all files efficiently
- Detects new untracked files the agent creates
- Detects deleted files
- Avoids polling N files individually

**Update help text:**
```
Usage:
  crit                          Auto-detect changed files via git
  crit <file> [file...]         Review specific files
  crit go [port]                Signal round-complete
  crit install <agent>          Install integration files
```

---

## Phase 5: Frontend Multi-File UI

**Modify: `frontend/index.html`** (largest change)

Reference design: `frontend/mockup-multi-file.html`

### State model change

```javascript
// Old: single-file globals
let rawContent, fileName, comments, lineBlocks, ...

// New: session + per-file state
let session = {};      // { branch, baseRef, reviewRound }
let files = [];        // [{ path, status, fileType, content, diffHunks, comments, lineBlocks, collapsed, viewMode }]
let activeFilePath = null;  // which file has the open comment form
let activeForm = null;      // { filePath, afterBlockIndex, startLine, endLine, editingId }
```

### Init flow

```javascript
async function init() {
    const [sessionRes, configRes] = await Promise.all([
        fetch('/api/session'), fetch('/api/config')
    ]);
    session = sessionRes;

    // Load all files in parallel
    files = await Promise.all(session.files.map(async f => {
        const [content, comments] = await Promise.all([
            fetch(`/api/file?path=${enc(f.path)}`),
            fetch(`/api/file/comments?path=${enc(f.path)}`)
        ]);
        return { ...f, content, comments, lineBlocks: null, diffHunks: null,
                 collapsed: f.status === 'deleted', viewMode: f.fileType === 'markdown' ? 'document' : 'diff' };
    }));

    // Parse markdown files into line blocks; load diff hunks for code files
    for (const f of files) {
        if (f.fileType === 'markdown') {
            f.lineBlocks = buildLineBlocks(/* reuse existing function, parameterized */);
        } else {
            f.diffHunks = await fetch(`/api/file/diff?path=${enc(f.path)}`);
        }
    }
    renderAll();
}
```

### Layout (matching mockup)

```
header
  ├── "Crit" title
  ├── branch context chip (git branch icon + name)
  ├── total comment count
  ├── theme pill
  └── "Finish Review" button
file-summary bar (sticky below header)
  ├── "N files changed" + total +/- stats
  └── file chips (clickable jump-to anchors, colored dots, comment badges)
file sections (one per file)
  ├── file header (sticky, collapsible)
  │   ├── chevron + file icon + path
  │   ├── status badge (Modified/New File/Deleted)
  │   ├── diff stats (+N -M)
  │   ├── comment count
  │   └── [for .md] document/diff toggle
  └── file body
      ├── [markdown, document mode]: reuse existing line-block renderer
      ├── [markdown, diff mode]: diff hunk renderer (uses inter-round diff)
      └── [code]: diff hunk renderer (uses git diff hunks)
```

### Key refactoring of existing functions

These currently use global state. Parameterize to accept a file context:

| Function | Change |
|----------|--------|
| `buildLineBlocks(tokens, md, content)` | No change needed (already takes content) |
| `renderDocument()` | → `renderFileSection(file)` — takes file entry, renders into its section |
| `handleGutterMouseDown(e)` | Needs `filePath` from DOM `data-filepath` attribute |
| `submitComment()` | POST to `/api/file/comments?path=activeForm.filePath` |
| `createCommentForm()` | Include `filePath` in form state |
| `connectSSE()` | Events include `filepath`; update specific file's state |

### New: Diff hunk renderer for code files

New function `renderDiffHunks(file)` that produces the mockup's diff layout:
- Hunk headers (`@@ -27,6 +31,23 @@`)
- Dual-gutter (old line / new line)
- `+`/`-` signs with colored backgrounds
- Collapsed spacers between hunks ("Expand N unchanged lines")
- Gutter `+` button for commenting (only on "new" side lines)
- Inline comment cards after referenced lines

### New: per-file document/diff toggle for markdown

A small toggle in the file header for `.md` files:
```html
<div class="btn-group">
    <button class="btn btn-sm" onclick="setViewMode(path, 'document')">Document</button>
    <button class="btn btn-sm" onclick="setViewMode(path, 'diff')">Diff</button>
</div>
```
Document mode: existing line-block renderer. Diff mode: same diff hunk renderer used for code files, but with the inter-round LCS diff (or git diff on first round).

### CSS additions

Add the diff-specific styles from `mockup-multi-file.html`:
- `.file-summary`, `.file-chip` — summary bar and jump chips
- `.file-section`, `.file-header`, `.file-body` — collapsible sections
- `.diff-container`, `.diff-line`, `.diff-gutter`, `.diff-hunk-header` — diff rendering
- `.diff-spacer` — collapsed unchanged lines
- `.diff-word-add`, `.diff-word-del` — inline word-level highlights

---

## Phase 6: Multi-Round for Multi-File

### Round-complete flow

1. User clicks "Finish Review" → `.crit.json` written
2. Agent reads `.crit.json`, edits files, marks comments resolved
3. Agent calls `crit go <port>`
4. Backend: `SignalRoundComplete()`:
   - **For markdown files:** Snapshot `Content → PreviousContent`, `Comments → PreviousComments`, re-read from disk, carry forward unresolved
   - **For code files:** Re-run `FileDiffUnified()` against git base — the diff has changed because the agent edited these files. Update `DiffHunks` on each `FileEntry`
   - **For the file list itself:** Re-run `ChangedFiles()` — agent may have created new files or resolved deletions. Add new `FileEntry` structs, remove files no longer in the changeset
   - Load resolved comments from `.crit.json` for all files
5. Send SSE `file-changed` event with updated session (file list + contents)
6. Frontend: re-fetch everything, re-render. Show diff toggles for changed markdown files, updated diff hunks for code files, resolved comments

### File watching (during agent editing)

Poll `git status --porcelain` every 1-2s. On any working tree change:
- Increment `session.pendingEdits`
- SSE `edit-detected` event (frontend shows edit counter in waiting modal)
- Do NOT re-compute diffs yet (expensive, and agent is still working) — save that for round-complete

---

## Phase 7: Integration Updates

**Modify all integration files** to reference `.crit.json` instead of `.review.md`:

- `integrations/claude-code/crit.md` — Update steps 3-4 to read `.crit.json`
- `integrations/claude-code/CLAUDE.md` — Reference `.crit.json`
- `integrations/cursor/crit-command.md`
- `integrations/windsurf/crit.md`
- `integrations/github-copilot/crit.prompt.md`
- `integrations/cline/crit.md`

Also update the finish prompt to explain the `.crit.json` structure so agents know how to parse it.

---

## Phase 8: Cleanup

- Remove `output.go` (no more `.review.md` generation)
- Remove `Document` struct from `document.go` (fully replaced by `Session` + `FileEntry`)
- Keep `diff.go` (LCS logic reused for inter-round markdown diffs)
- Update `CLAUDE.md` documentation
- Delete `mockup-multi-file.html`
- Update tests

---

## Files to create/modify

| File | Action | Phase |
|------|--------|-------|
| `git.go` | Create | 1 |
| `git_test.go` | Create | 1 |
| `session.go` | Create | 2 |
| `session_test.go` | Create | 2 |
| `server.go` | Modify (Session, new API) | 3 |
| `server_test.go` | Modify | 3 |
| `main.go` | Modify (no-args, multi-file) | 4 |
| `frontend/index.html` | Modify (multi-file UI) | 5 |
| `document.go` | Remove (end) | 8 |
| `output.go` | Remove | 8 |
| `integrations/*` | Modify | 7 |

## Verification

After each phase:
- `go test ./...` — all tests pass
- `gofmt -l .` — clean formatting
- Phase 5+: manual browser testing with `crit` on a feature branch with mixed file changes
- Final: `crit` with no args in this repo, verify file detection, diff rendering, commenting, multi-round flow
