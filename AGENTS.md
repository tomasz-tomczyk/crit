# Crit — Development Guide

## What This Is

A single-binary Go CLI tool that opens a browser-based UI for reviewing code changes and markdown files with GitHub PR-style inline commenting. Supports multi-file review with git diff rendering and structured `.crit.json` output for AI coding agents.

## Project Structure

```
crit/
├── main.go              # Entry point: subcommand dispatcher + individual runX() functions
├── server.go            # HTTP handlers: REST API (session, file, comments CRUD, finish, share, config)
├── session.go           # Core state: multi-file session, comment storage, .crit.json persistence, SSE
├── watch.go             # File/git watching, round-complete handlers, comment carry-forward
├── git.go               # Git integration: branch detection, changed files, diff parsing
├── github.go            # GitHub PR sync: fetch/post PR comments, crit comment CLI, .crit.json I/O
├── config.go            # Config file loading: ~/.crit.config.json + .crit.config.json merge, ignore patterns
├── diff.go              # LCS-based line diff for inter-round markdown comparison
├── status.go            # Terminal status output formatting
├── main_test.go         # Subcommand argument parsing tests
├── testutil_test.go     # Shared test helpers (initTestRepo, runGit, writeFile, flushWrites)
├── *_test.go            # Tests for all Go files above
├── frontend/
│   ├── index.html       # HTML shell — references style.css, theme.css, and app.js
│   ├── app.js           # All JS (multi-file state, rendering, comments, SSE, keyboard shortcuts)
│   ├── style.css        # Layout, diff rendering, file sections, components
│   ├── theme.css        # Color themes (light/dark/system CSS variables)
│   ├── markdown-it.min.js    # Markdown parser (provides source line mappings via token.map)
│   ├── highlight.min.js      # Syntax highlighter core
│   ├── hljs-*.min.js         # Language packs (js, ts, go, python, elixir, etc.)
│   └── mermaid.min.js        # Mermaid diagram renderer
├── integrations/        # Drop-in config files for AI coding tools (claude-code, cursor, aider, etc.)
├── e2e/                 # Playwright E2E tests for the frontend
│   ├── playwright.config.ts         # Five projects: git-mode, file-mode, single-file, no-git, multi-file
│   ├── setup-fixtures.sh            # Git repo with feature branch
│   ├── setup-fixtures-filemode.sh   # Plain files without git
│   ├── setup-fixtures-singlefile.sh # Single markdown file
│   ├── setup-fixtures-multifile.sh  # Multiple code + markdown files
│   ├── setup-fixtures-nogit.sh      # File mode without git (no-git)
│   └── tests/           # Test specs (naming convention determines which project runs them)
├── go.mod
├── Makefile             # build / build-all (cross-compile) / update-deps / clean / e2e
├── package.json         # Frontend dependency management (markdown-it, highlight.js, mermaid)
├── copy-deps.js         # Copies npm deps to frontend/ for embedding
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
13. **Two-level config** — `~/.crit.config.json` (global) merged with `.crit.config.json` (project), CLI flags override both
14. **GitHub PR sync** — `crit pull` / `crit push` bridge between `.crit.json` and GitHub PR review comments via `gh` CLI
15. **Headless CLI comment** — `crit comment` writes directly to `.crit.json` without starting the server; SSE notifies any running server
16. **Comment threading** — comments support nested replies and a `resolved` boolean. Agents reply with `crit comment --reply-to <id> --resolve`. The `.crit.json` schema nests replies inside each comment's `replies` array.
17. **Commit selection** — in git mode, a sidebar lists individual commits. Selecting one scopes the file list and diffs to that commit only.

## Build & Run

```bash
go build -o crit .                                    # Build
go test ./...                                         # Run all tests
./crit                                                # Git mode (auto-detect changed files)
./crit test-plan.md                                   # Review specific file(s)
./crit --no-open --port 3000 test-plan.md             # Headless on fixed port
make build-all                                        # Cross-compile to dist/
```

## CLI Subcommands

```bash
crit                          # Review git changes (starts daemon, blocks for feedback)
crit <file|dir> [...]         # Review specific files or directories
crit stop                     # Stop the daemon for current directory
crit stop --all               # Stop all daemons for current directory
crit pull [pr-number]         # Fetch GitHub PR comments into .crit.json
crit push [--dry-run] [pr]    # Post .crit.json comments as a GitHub PR review
crit comment <path>:<line[-end]> <body>         # Add a comment to .crit.json (no server needed)
crit comment --reply-to <id> [--resolve] <body> # Reply to a comment (optionally mark resolved)
crit comment --json [--author <name>]           # Bulk add comments from stdin JSON
crit share <file> [file...]   # Share files to crit-web, print URL
crit unpublish                # Remove shared review from crit-web
crit config                   # Print resolved configuration (merged global + project)
crit config --generate        # Print a starter .crit.config.json template
crit install <agent>          # Install integration config for an AI tool
crit help                     # Show help
```

## Config System

Two-level JSON config files, merged (project overrides global):

- **Global**: `~/.crit.config.json` — user-wide defaults
- **Project**: `.crit.config.json` in repo root — per-project overrides

Config keys: `port`, `no_open`, `share_url`, `quiet`, `output`, `author`, `base_branch`, `ignore_patterns`.

- `base_branch` overrides auto-detected default branch (used as diff base in git mode, and by `crit pull`/`crit push`/`crit comment`)
- `author` falls back to `git config user.name` if not set
- `ignore_patterns` are unioned (both global and project patterns apply)
- Pattern types: `*.ext` (extension), `dir/` (directory prefix), `exact.file` (filename), `path/*.ext` (glob)
- CLI flags override config file values

## GitHub PR Integration

Requires `gh` CLI installed and authenticated.

- `crit pull` fetches PR review comments (RIGHT-side only) and merges them into `.crit.json`, deduplicating by author+lines+body
- `crit push` reads `.crit.json` and posts unresolved comments as a GitHub PR review
- `crit push --dry-run` shows what would be posted without actually creating the review
- PR number auto-detected from current branch, or pass explicitly: `crit pull 42`

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

- **Five Playwright projects**, each with its own fixture script and port:
  - `git-mode` (port 3123) — `setup-fixtures.sh` — git repo with feature branch. Runs `*.spec.ts` (excludes other suffixes)
  - `file-mode` (port 3124) — `setup-fixtures-filemode.sh` — plain files, no git. Runs `*.filemode.spec.ts`
  - `single-file-mode` (port 3125) — `setup-fixtures-singlefile.sh` — single markdown file. Runs `*.singlefile.spec.ts`
  - `no-git-mode` (port 3126) — `setup-fixtures-nogit.sh` — file mode without git. Runs `*.nogit.spec.ts`
  - `multi-file-mode` (port 3127) — `setup-fixtures-multifile.sh` — multiple code + markdown files. Runs `*.multifile.spec.ts`
- **Real server**: Tests run against the actual compiled `crit` binary — no mocking
- **Video/trace off by default**: Set `E2E_DEBUG=1` to enable video and trace recording on failure (saved to `e2e/test-results/`)
- **CI**: E2E tests run on every push to `main` and on PRs via `.github/workflows/test.yml`. Failed test artifacts are uploaded

### Test organization

| File | Mode | What it covers |
| --- | --- | --- |
| `loading.spec.ts` | git | Branch name, title, file tree, status icons, stats |
| `loading.filemode.spec.ts` | file | Title, no branch, no diff toggle, document view defaults |
| `diff-rendering.spec.ts` | git | Split/unified diffs, hunk headers, spacer expand, mode persistence |
| `markdown.spec.ts` | git | Headings, tables, code blocks, lists, blockquotes, line gutters |
| `comments.spec.ts` | git | Add/edit/delete comments on markdown and diff lines, cross-file |
| `comments.filemode.spec.ts` | file | Comment CRUD on markdown in file mode |
| `comments-panel.spec.ts` | git | View all comments panel |
| `comment-count-badge.spec.ts` | git | Comment count badge in header |
| `comment-range-highlight.spec.ts` | git | Highlighted line ranges for comments |
| `multi-form.spec.ts` | git | Multiple comment forms open simultaneously |
| `cli-comment.spec.ts` | git | `crit comment` CLI writes synced to running server via SSE |
| `templates.spec.ts` | git | Comment template chips |
| `keyboard.spec.ts` | git | j/k navigation, c/e/d shortcuts, ?, t, Shift+F, Escape |
| `keyboard.filemode.spec.ts` | file | Same keyboard shortcuts in file mode |
| `theme.spec.ts` | git | Light/dark/system toggle, persistence, file sections, finish review |
| `theme.filemode.spec.ts` | file | Theme, TOC, file sections, finish review in file mode |
| `drag-selection.spec.ts` | git | Gutter drag on markdown and diff (split + unified) |
| `drag-selection.filemode.spec.ts` | file | Gutter drag on markdown in file mode |
| `md-toggle.spec.ts` | git | Document/diff toggle for markdown, cross-view comment persistence |
| `rendered-diff.filemode.spec.ts` | file | Rendered markdown diff view in file mode |
| `syntax-highlighting.spec.ts` | git | Syntax highlighting in diff code blocks |
| `expanded-comments.spec.ts` | git | Comments on spacer-expanded context lines |
| `draft-autosave.spec.ts` | git | Draft persistence to localStorage, toast notification |
| `file-tree.spec.ts` | git | File tree panel, status icons, active state, comment badges |
| `file-tree.filemode.spec.ts` | file | File tree panel, clicking, comment badges in file mode |
| `scope-toggle.spec.ts` | git | Diff scope toggle (all/branch/staged/unstaged) |
| `round-complete.spec.ts` | git | Multi-round API (finish, round-complete), SSE refresh, UI state |
| `round-complete.filemode.spec.ts` | file | Multi-round API + frontend in file mode |
| `share.spec.ts` | git | Share button visibility, config API defaults |
| `share.filemode.spec.ts` | file | Share in file mode |
| `share.multifile.spec.ts` | multi | Share in multi-file mode |
| `viewed.spec.ts` | git | Viewed state persistence across round transitions |
| `change-nav.filemode.spec.ts` | file | File change navigation |
| `select-to-comment.spec.ts` | git | Text selection to comment |
| `word-diff.spec.ts` | git | Word-level diff rendering |
| `suggestion-diff.spec.ts` | git | Suggestion diff display |
| `old-side-suggest.spec.ts` | git | Suggestions on old-side (deletion) lines |
| `toc.singlefile.spec.ts` | single | Table of contents |
| `toc-scrollspy.singlefile.spec.ts` | single | TOC scroll-spy highlighting |
| `multifile.multifile.spec.ts` | multi | Loading, code rendering, comments on Go/Elixir, directory files |
| `threading.spec.ts` | git | Comment threading: replies, resolve/unresolve, collapse |
| `commit-selection.spec.ts` | git | Commit selection sidebar, per-commit file list and diffs |
| `file-picker.spec.ts` | git | @-triggered file picker autocomplete in comment forms |
| `file-picker.filemode.spec.ts` | file | @-triggered file picker in file mode |
| `toc-refresh.singlefile.spec.ts` | single | TOC refresh when file content changes |
| `nogit.nogit.spec.ts` | no-git | Git-absence invariants: no branch, no diff toggle, session mode |

### Writing new tests

- **Git-mode tests**: name as `*.spec.ts` — runs against the git fixture on port 3123
- **File-mode tests**: name as `*.filemode.spec.ts` — runs against the file fixture on port 3124
- **Single-file tests**: name as `*.singlefile.spec.ts` — runs against single-file fixture on port 3125
- **No-git tests**: name as `*.nogit.spec.ts` — runs against the no-git fixture on port 3126
- **Multi-file tests**: name as `*.multifile.spec.ts` — runs against the multi-file fixture on port 3127
- **Comment cleanup**: the server persists comments between tests. Use `clearAllComments(request)` in `beforeEach` to reset state — this calls `DELETE /api/comments` (bulk endpoint)
- **Shared helpers**: import from `./helpers` — provides `clearAllComments`, `loadPage`, `mdSection`, `goSection`, `jsSection`, `switchToDocumentView`, `dragBetween`, `clearFocus`, `addComment`, `getMdPath`
- **Markdown in git mode**: defaults to diff view. Call `switchToDocumentView()` helper to test document rendering
- **Markdown in file mode**: defaults to document view. No toggle needed

### E2E best practices

- **Never use `waitForTimeout` or `setTimeout`** for waiting on state. Use Playwright auto-retrying assertions (`toPass()`, `toHaveClass()`, `toBeVisible()`, etc.) instead. The only exception is a sleep interval inside a polling loop where you're already retrying.
- **Never use `.count()` followed by `expect(count).toBe(N)`** — this is a snapshot that doesn't retry. Use `await expect(locator).toHaveCount(N)` or wrap in `toPass()` for range checks.
- **Always import from `./helpers`** — don't redefine `loadPage`, `mdSection`, etc. locally. If a test needs a fixture-specific helper, define it in that file but use `Page` types, not `any`.
- **Use `clearAllComments(request)` in `beforeEach`** — the server persists state across tests. Always clean up.
- **Parallel execution**: projects run in parallel via shell (each in its own `npx playwright test --project=X` process). Tests within a project run sequentially (`workers: 1`) because they share server state. Don't add `workers > 1` to `playwright.config.ts`.
- **Test naming convention**: `*.spec.ts` (git-mode), `*.filemode.spec.ts` (file-mode), `*.singlefile.spec.ts` (single-file), `*.nogit.spec.ts` (no-git-mode), `*.multifile.spec.ts` (multi-file). The git-mode regex explicitly excludes all other patterns.
- **CSS selectors**: check existing tests for the correct class names before writing assertions. The codebase uses specific names like `.tree-comment-badge` (not `.tree-file-comments`).
- **Scroll before interact**: if an element might be below the viewport (especially in file-mode with multiple files), call `scrollIntoViewIfNeeded()` before hover/click/drag.

## API Endpoints

Session-scoped:

- `GET  /api/session` — session metadata: mode, branch, baseRef, reviewRound, file list with stats
- `GET  /api/config` — returns `{share_url, hosted_url, delete_token, version, latest_version}`
- `POST /api/finish` — write `.crit.json`, return prompt for agent
- `GET  /api/events` — SSE stream (file-changed, edit-detected, server-shutdown events)
- `GET  /api/wait-for-event` — long-poll that blocks until finish, returns event JSON (used by `crit` in daemon mode)
- `POST /api/round-complete` — agent signals all edits are done; triggers new round
- `POST /api/share-url` — persist `{url, delete_token}` to `.crit.json` after upload
- `DELETE /api/share-url` — unpublish: calls crit-web DELETE and clears local persisted URL
- `GET  /api/commits` — list commits between base ref and HEAD (git mode only)
- `DELETE /api/comments` — bulk delete all comments across all files (used by E2E test cleanup)

File-scoped (use `?path=` query param):

- `GET  /api/file?path=X` — file content + metadata
- `GET  /api/file/diff?path=X` — diff hunks (git diff for code; inter-round diff for markdown)
- `GET  /api/file/comments?path=X` — comments for one file
- `POST /api/file/comments?path=X` — add comment `{start_line, end_line, body}` (10MB body limit)
- `PUT  /api/comment/{id}?path=X` — update comment `{body}` (10MB body limit)
- `DELETE /api/comment/{id}?path=X` — delete comment
- `POST   /api/comment/{id}/replies?path=X` — add reply `{body, author}`
- `PUT    /api/comment/{id}/replies/{rid}?path=X` — edit reply `{body}`
- `DELETE /api/comment/{id}/replies/{rid}?path=X` — delete reply
- `POST   /api/comment/{id}/resolve?path=X` — set resolved state `{resolved: bool}`

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

Frontend is split into four files: `index.html` (HTML shell), `app.js` (all logic), `style.css` (layout/components), and `theme.css` (color theme variables).

### Multi-File State Model

```javascript
let session = {}; // { mode, branch, base_ref, review_round, files: [...] }
let files = []; // [{ path, status, fileType, content, diffHunks, comments, lineBlocks, ... }]
let activeForms = []; // multiple comment forms can be open simultaneously
// each: { filePath, afterBlockIndex, startLine, endLine, editingId }
```

### Source Line Mapping (Markdown Files)

1. Parse markdown with `markdown-it` to get tokens with `token.map` (source line ranges)
2. `buildLineBlocks()` dispatches to per-token-type handlers: `handleFenceToken`, `handleListToken`, `handleTableToken`, `handleBlockquoteToken`
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

Sharing is opt-in. When `--share-url` (or `CRIT_SHARE_URL` env var, or `share_url` in config file) is set:

- The Share button appears in the header.
- Clicking it POSTs the current document + comments to `{share_url}/api/reviews` (crit-web API).
- The response `{url, delete_token}` is persisted to `.crit.json` via `POST /api/share-url`.
- A share-notice banner shows the URL with Copy / Unpublish actions.
- Unpublish calls `DELETE {share_url}/api/reviews?delete_token=...` then clears local state.

## Multi-Round Review

When the agent runs `crit` (or calls `POST /api/round-complete`):

- **Markdown files**: Snapshot content, carry forward unresolved comments, re-read from disk
- **Code files**: Re-run git diff against base ref to get updated hunks
- **File list**: Re-run `ChangedFiles()` to detect new/removed files
- The waiting modal shows a live count of file edits while the agent is working
- Diff toggle for markdown files shows inter-round changes

## Daemon Architecture

`crit` manages a background daemon for seamless multi-round reviews:

1. **First `crit`**: starts background daemon (`crit _serve`), opens browser, blocks for feedback
2. **Subsequent `crit`**: connects to existing daemon (same cwd + args), signals round-complete, blocks for feedback
3. **`crit plan.md`**: looks up daemon by hash(cwd + "plan.md") — reuses if alive, starts new if dead
4. **Ctrl+C**: kills the daemon the client started
5. **`crit stop`**: kills the daemon for current cwd (no args). `crit stop --all` kills all daemons for current cwd
6. **Idle timeout**: daemon exits after 4 hours of no HTTP activity

### Session Registry

Daemon state lives in `~/.crit/sessions/` with one file per session, keyed by `sha256(cwd + "\0" + sorted(args))[:12]`:

```
~/.crit/sessions/
├── a1b2c3d4e5f6.json   # crit (git mode) in /path/to/repo
├── f6e5d4c3b2a1.json   # crit plan.md in /path/to/repo
└── ...
```

Session file format: `{"pid", "port", "cwd", "args", "started_at"}`. `.crit.json` is purely review data — no daemon state.

Internal command: `crit _serve` runs the server in foreground (used by daemon spawning, not user-facing).

## Releasing

Releases are fully automated via GitHub Actions (`.github/workflows/release.yml`). To cut a release:

Before tagging, bump the version in `flake.nix`:

```nix
version = "0.x.y";
```

**Nix vendor hash**: `flake.nix` also contains a pinned `vendorHash`. Whenever Go dependencies change (`go.mod`/`go.sum`), this hash must be updated. The release workflow runs `nix build .` and will fail with the correct replacement hash if it's stale. To update locally: set `vendorHash = pkgs.lib.fakeHash;`, run `nix build .`, and copy the hash from the error output.

Then commit, tag, and push:

```bash
git add flake.nix && git commit -m "chore: bump Nix flake version to v0.x.y"
git tag v0.x.y && git push origin main v0.x.y
```

Pushing the tag triggers the workflow, which:

1. Runs tests (including Nix build verification)
2. Cross-compiles binaries for darwin/linux (arm64/amd64) with the version injected via ldflags
3. Generates SHA256 checksums
4. Creates a GitHub release with auto-generated notes and all binaries attached
5. Updates the Homebrew tap formula (`tomasz-tomczyk/homebrew-tap`)

The version string lives in `main.go` as `var version = "dev"` and is overridden at build time. There is no version constant to update manually — the tag is the single source of truth.

### Release Notes

After CI creates the release, update it with proper release notes using `gh release edit`. List each change as a bullet point:

- PRs: link to the PR (e.g., `[#4](https://github.com/tomasz-tomczyk/crit/pull/4)`)
- Direct commits: link to the commit with short SHA (e.g., ``[`e283708`](https://github.com/tomasz-tomczyk/crit/commit/<full-sha>)``)
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

| File         | Description                                                |
| ------------ | ---------------------------------------------------------- |
| `.crit.json` | Structured JSON with per-file comments — read by AI agents |
