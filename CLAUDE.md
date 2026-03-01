# Crit — Development Guide

## What This Is

A single-binary Go CLI tool that opens a browser-based UI for reviewing code changes and markdown files with GitHub PR-style inline commenting. Supports multi-file review with git diff rendering and structured `.crit.json` output for AI coding agents.

## Project Structure

```
crit/
├── main.go              # Entry point: CLI parsing, server setup, graceful shutdown, browser open
├── server.go            # HTTP handlers: REST API (session, file, comments CRUD, finish, share, config)
├── session.go           # Core state: multi-file session, comment storage, .crit.json persistence, SSE
├── git.go               # Git integration: branch detection, changed files, diff parsing
├── diff.go              # LCS-based line diff for inter-round markdown comparison
├── status.go            # Terminal status output formatting
├── server_test.go       # API handler tests (CRUD, validation, path traversal prevention)
├── session_test.go      # Session operations tests (CRUD, concurrent access, file reload, SSE)
├── git_test.go          # Git integration tests (temp repos with real commits)
├── diff_test.go         # LCS diff tests
├── frontend/
│   ├── index.html       # HTML shell — references style.css and app.js
│   ├── style.css        # All CSS (themes, layout, diff rendering, file sections)
│   ├── app.js           # All JS (multi-file state, rendering, comments, SSE, keyboard shortcuts)
│   ├── markdown-it.min.js    # Markdown parser (provides source line mappings via token.map)
│   ├── highlight.min.js      # Syntax highlighter core
│   ├── hljs-*.min.js         # Language packs (js, ts, go, python, elixir, etc.)
│   └── mermaid.min.js        # Mermaid diagram renderer
├── integrations/        # Drop-in config files for AI coding tools
├── e2e/                 # Playwright E2E tests for the frontend
│   ├── playwright.config.ts    # Two projects: git-mode (port 3123) and file-mode (port 3124)
│   ├── setup-fixtures.sh       # Creates temp git repo with fixture files for git mode testing
│   ├── setup-fixtures-filemode.sh  # Creates plain files for file mode testing
│   └── tests/           # Test specs (*.spec.ts = git mode, *.filemode.spec.ts = file mode)
├── go.mod
├── Makefile             # build / build-all (cross-compile) / update-deps / clean / e2e
├── package.json         # Frontend dependency management (markdown-it, highlight.js, mermaid)
├── copy-deps.js         # Copies npm deps to frontend/ for embedding
├── test-plan.md         # Sample file for development testing
├── LICENSE              # MIT
└── README.md
```

## Key Architecture Decisions

1. **All frontend assets embedded** via Go's `embed.FS` — produces a true single binary
2. **No frontend build step** — vanilla JS, no npm/webpack/framework. npm is only for fetching vendor libs.
3. **Multi-file sessions** — `crit` (no args) auto-detects git changes; `crit file1 file2` reviews specific files
4. **Two modes**: "git" mode (auto-detect from git) and "files" mode (explicit file arguments)
5. **markdown-it for parsing** — chosen because it provides `token.map` (source line mappings per block)
6. **Block-level splitting** — lists, code blocks, tables, blockquotes are split into per-item/per-line/per-row blocks so each source line is independently commentable
7. **Diff hunk rendering** — code files show git diffs with dual gutters (old/new line numbers)
8. **Comments reference source line numbers** — stored in structured `.crit.json` with per-file sections
9. **Real-time output** — `.crit.json` written on every comment change (200ms debounce)
10. **GitHub-style gutter interaction** — click-and-drag on line numbers to select ranges
11. **File watching** — git mode polls `git status --porcelain`; files mode polls mtimes; reloads via SSE
12. **Localhost only** — server binds to `127.0.0.1`, no CORS headers needed

## Build & Run

```bash
go build -o crit .                                    # Build
go test ./...                                         # Run all tests
./crit                                                # Git mode (auto-detect changed files)
./crit test-plan.md                                   # Review specific file(s)
./crit --no-open --port 3000 test-plan.md             # Headless on fixed port
./crit --share-url https://crit.live test-plan.md     # Enable Share button
CRIT_SHARE_URL=https://crit.live ./crit test-plan.md  # Same via env var
make build-all                                        # Cross-compile to dist/
```

## Linting

```bash
gofmt -l .                        # Check formatting (should be clean)
golangci-lint run ./...           # Lint (should be clean)
```

## E2E Tests (Playwright)

The `e2e/` directory contains a Playwright test suite that exercises the full frontend in a real browser against a real Crit server.

### Running

```bash
make e2e                                              # Run full suite
cd e2e && npx playwright test tests/comments.spec.ts  # Run one test file
cd e2e && npx playwright test --headed                # Run with visible browser
E2E_DEBUG=1 make e2e                                  # Enable video + trace capture on failure
make e2e-report                                       # View HTML report with screenshots
```

### Architecture

- **Two modes tested**: git mode (auto-detected files from a git repo) and file mode (explicit file args)
- **Two fixture scripts**: `setup-fixtures.sh` creates a temp git repo with a feature branch; `setup-fixtures-filemode.sh` creates plain files without git
- **Two Playwright projects**: `git-mode` (port 3123, runs `*.spec.ts`) and `file-mode` (port 3124, runs `*.filemode.spec.ts`)
- **Real server**: Tests run against the actual compiled `crit` binary — no mocking
- **Video/trace off by default**: Set `E2E_DEBUG=1` to enable video and trace recording on failure (saved to `e2e/test-results/`)
- **CI**: E2E tests run on every push to `main` and on PRs via `.github/workflows/test.yml`. Failed test artifacts are uploaded

### Test organization

| File | Mode | What it covers |
|------|------|---------------|
| `smoke.spec.ts` | git | Basic page load |
| `loading.spec.ts` | git | Branch name, title, file tree, status icons, stats |
| `loading.filemode.spec.ts` | file | Title, no branch, no diff toggle, document view defaults |
| `diff-rendering.spec.ts` | git | Split/unified diffs, hunk headers, spacer expand, mode persistence |
| `markdown.spec.ts` | git | Headings, tables, code blocks, lists, blockquotes, line gutters |
| `comments.spec.ts` | git | Add/edit/delete comments on markdown and diff lines, cross-file |
| `comments.filemode.spec.ts` | file | Comment CRUD on markdown in file mode |
| `keyboard.spec.ts` | git | j/k navigation, c/e/d shortcuts, ?, t, Shift+F, Escape |
| `keyboard.filemode.spec.ts` | file | Same keyboard shortcuts in file mode |
| `theme.spec.ts` | git | Light/dark/system toggle, persistence, file sections, finish review |
| `theme.filemode.spec.ts` | file | Theme, TOC, file sections, finish review in file mode |
| `drag-selection.spec.ts` | git | Gutter drag on markdown and diff (split + unified) |
| `drag-selection.filemode.spec.ts` | file | Gutter drag on markdown in file mode |
| `md-toggle.spec.ts` | git | Document/diff toggle for markdown, cross-view comment persistence |
| `syntax-highlighting.spec.ts` | git | Syntax highlighting in diff code blocks |
| `expanded-comments.spec.ts` | git | Comments on spacer-expanded context lines |
| `draft-autosave.spec.ts` | git | Draft persistence to localStorage, toast notification |
| `file-tree.spec.ts` | git | File tree panel, status icons, active state, comment badges |
| `file-tree.filemode.spec.ts` | file | File tree panel, clicking, comment badges in file mode |
| `round-complete.spec.ts` | git | Multi-round API (finish, round-complete), SSE refresh, UI state |
| `share.spec.ts` | git | Share button visibility, config API defaults |

### Writing new tests

- **Git-mode tests**: name as `*.spec.ts` — runs against the git fixture on port 3123
- **File-mode tests**: name as `*.filemode.spec.ts` — runs against the file fixture on port 3124
- **Comment cleanup**: the server persists comments between tests. Use `clearAllComments(request)` in `beforeEach` to reset state (see `comments.spec.ts` for the pattern)
- **Markdown in git mode**: defaults to diff view. Call `switchToDocumentView()` helper to test document rendering
- **Markdown in file mode**: defaults to document view. No toggle needed

## API Endpoints

Session-scoped:
- `GET  /api/session` — session metadata: mode, branch, baseRef, reviewRound, file list with stats
- `GET  /api/config` — returns `{share_url, hosted_url, delete_token, version, latest_version}`
- `POST /api/finish` — write `.crit.json`, return prompt for agent
- `GET  /api/events` — SSE stream (file-changed, edit-detected, server-shutdown events)
- `POST /api/round-complete` — agent signals all edits are done; triggers new round
- `POST /api/share-url` — persist `{url, delete_token}` to `.crit.json` after upload
- `DELETE /api/share-url` — unpublish: calls crit-web DELETE and clears local persisted URL

File-scoped (use `?path=` query param):
- `GET  /api/file?path=X` — file content + metadata
- `GET  /api/file/diff?path=X` — diff hunks (git diff for code; inter-round diff for markdown)
- `GET  /api/file/comments?path=X` — comments for one file
- `POST /api/file/comments?path=X` — add comment `{start_line, end_line, body}` (10MB body limit)
- `PUT  /api/comment/{id}?path=X` — update comment `{body}` (10MB body limit)
- `DELETE /api/comment/{id}?path=X` — delete comment

Static:
- `GET  /files/<path>` — serve files from repo root (path traversal protected)

## Security

- Server binds to `127.0.0.1` only
- `/files/` endpoint validates paths, blocks `..` traversal, verifies resolved path stays within repo root
- Request body size limited to 10MB for comments, 1MB for share-url via `http.MaxBytesReader`
- HTTP server has `ReadTimeout: 15s`, `IdleTimeout: 60s` (no `WriteTimeout` — SSE needs open connections)
- Comment renderer uses `html: false` to prevent XSS in user comments
- Document renderer uses `html: true` intentionally (reviewing your own local files)

## Frontend Architecture

Frontend is split into three files: `index.html` (HTML shell), `style.css` (all styles), `app.js` (all logic).

### Multi-File State Model

```javascript
let session = {};      // { mode, branch, base_ref, review_round, files: [...] }
let files = [];        // [{ path, status, fileType, content, diffHunks, comments, lineBlocks, ... }]
let activeFilePath;    // which file has the open comment form
let activeForm;        // { filePath, afterBlockIndex, startLine, endLine, editingId }
```

### Source Line Mapping (Markdown Files)

1. Parse markdown with `markdown-it` to get tokens with `token.map` (source line ranges)
2. `buildLineBlocks()` walks the token stream and creates a flat array of commentable blocks
3. Container tokens (lists, tables, blockquotes) are drilled into — each list item, table row, or blockquote child becomes its own block
4. Code blocks (`fence` tokens) are split into per-line blocks with syntax highlighting preserved via `splitHighlightedCode()`
5. Each block gets a gutter entry with its source line number(s)
6. Comments are keyed by `end_line` and displayed after their referenced block

### Diff Hunk Rendering (Code Files)

Code files display as git diffs with:
- Hunk headers (`@@ -27,6 +31,23 @@`)
- Dual-gutter (old line / new line numbers)
- Colored backgrounds for additions/deletions
- Spacers between hunks
- Inline comment support via gutter `+` buttons

### Known Complexities

- **markdown-it token.map quirks**: The last item in a list often claims a trailing blank line. The code trims trailing blank lines from item ranges.
- **Table separator lines** (`|---|---|`): Not represented in tokens, appear as gap lines. Detected via regex and hidden with CSS.
- **Per-row tables**: Each row wrapped in its own `<table>` with `table-layout: fixed` + `<colgroup>` for column alignment.
- **Highlighted code splitting**: `splitHighlightedCode()` tracks open `<span>` tags across lines to properly close/reopen them.

## Theme System

The header has a 3-button theme pill (System / Light / Dark):
- No `data-theme` attribute → system preference via `prefers-color-scheme`
- `data-theme="light"` / `data-theme="dark"` → explicit override
- CSS vars are set in `:root` (dark fallback), `@media (prefers-color-scheme: light) html:not([data-theme])`, `[data-theme="dark"]`, and `[data-theme="light"]` blocks.
- Theme choice persisted to `localStorage` as `crit-theme` (`"system"` | `"light"` | `"dark"`).

## Share Feature

When `--share-url` (or `CRIT_SHARE_URL`) is set:
- The Share button appears in the header.
- Clicking it POSTs the current document + comments to `{share_url}/api/reviews` (crit-web API).
- The response `{url, delete_token}` is persisted to `.crit.json` via `POST /api/share-url`.
- A share-notice banner shows the URL with Copy / Unpublish actions.
- Unpublish calls `DELETE {share_url}/api/reviews?delete_token=...` then clears local state.

## Multi-Round Review

When the agent runs `crit go <PORT>` (or calls `POST /api/round-complete`):
- **Markdown files**: Snapshot content, carry forward unresolved comments, re-read from disk
- **Code files**: Re-run git diff against base ref to get updated hunks
- **File list**: Re-run `ChangedFiles()` to detect new/removed files
- The waiting modal shows a live count of file edits while the agent is working
- Diff toggle for markdown files shows inter-round changes

## Releasing

Releases are fully automated via GitHub Actions (`.github/workflows/release.yml`). To cut a release:

Before tagging, bump the version in `flake.nix`:

```nix
version = "0.x.y";
```

Then commit, tag, and push:

```bash
git add flake.nix && git commit -m "chore: bump Nix flake version to v0.x.y"
git tag v0.x.y && git push origin main v0.x.y
```

Pushing the tag triggers the workflow, which:
1. Runs tests
2. Cross-compiles binaries for darwin/linux (arm64/amd64) with the version injected via ldflags
3. Generates SHA256 checksums
4. Creates a GitHub release with auto-generated notes and all binaries attached
5. Updates the Homebrew tap formula (`tomasz-tomczyk/homebrew-tap`)

The version string lives in `main.go` as `var version = "dev"` and is overridden at build time. There is no version constant to update manually — the tag is the single source of truth.

### Release Notes

After CI creates the release, update it with proper release notes using `gh release edit`. List each change as a bullet point:
- PRs: link to the PR (e.g., `[#4](https://github.com/tomasz-tomczyk/crit/pull/4)`)
- Direct commits: link to the commit with short SHA (e.g., `` [`e283708`](https://github.com/tomasz-tomczyk/crit/commit/<full-sha>) ``)
- Exclude the version bump commit itself
- End with a Full Changelog compare link

To gather changes: `git log v<prev>..v<new> --oneline --no-merges` and `gh pr list --state merged` to match commits to PRs.

Example:

```bash
gh release edit v0.x.y --notes "$(cat <<'EOF'
## What's Changed

- Description of change ([#N](https://github.com/tomasz-tomczyk/crit/pull/N))
- Description of change ([`abcdef0`](https://github.com/tomasz-tomczyk/crit/commit/<full-sha>))

**Full Changelog**: https://github.com/tomasz-tomczyk/crit/compare/v0.x.y-1...v0.x.y
EOF
)"
```

## Output Files

| File | Description |
|------|-------------|
| `.crit.json` | Structured JSON with per-file comments — read by AI agents |
