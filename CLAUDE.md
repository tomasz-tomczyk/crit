# Crit — Development Guide

## What This Is

A single-binary Go CLI tool that opens a browser-based UI for reviewing markdown files with GitHub PR-style inline commenting. Comments are written in real-time to a `.review.md` file designed to be handed to an AI coding agent.

## Project Structure

```
crit/
├── main.go              # Entry point: CLI parsing (--share-url/CRIT_SHARE_URL), server setup, graceful shutdown, browser open
├── server.go            # HTTP handlers: REST API (comments CRUD, document, finish, stale, share, config, files)
├── document.go          # Core state: file loading, comment storage, JSON/review file persistence
├── output.go            # Generates .review.md (original markdown + interleaved comment blockquotes)
├── server_test.go       # API handler tests (CRUD, validation, path traversal prevention)
├── document_test.go     # Document operations tests (CRUD, concurrent access, file reload, SSE)
├── output_test.go       # Review MD generation tests (formatting, ordering, multi-line)
├── frontend/
│   ├── index.html       # Complete SPA — all HTML, CSS, JS in one file (~1900 lines)
│   ├── markdown-it.min.js    # Markdown parser (provides source line mappings via token.map)
│   ├── highlight.min.js      # Syntax highlighter core
│   ├── hljs-*.min.js         # Language packs (js, ts, go, python, elixir, etc.)
│   └── mermaid.min.js        # Mermaid diagram renderer
├── go.mod
├── Makefile             # build / build-all (cross-compile) / update-deps / clean
├── package.json         # Frontend dependency management (markdown-it, highlight.js, mermaid)
├── copy-deps.js         # Copies npm deps to frontend/ for embedding
├── test-plan.md         # Sample file for development testing
├── LICENSE              # MIT
└── README.md
```

## Key Architecture Decisions

1. **All frontend assets embedded** via Go's `embed.FS` — produces a true single binary
2. **No frontend build step** — vanilla JS, no npm/webpack/framework. npm is only for fetching vendor libs.
3. **markdown-it for parsing** — chosen because it provides `token.map` (source line mappings per block)
4. **Block-level splitting** — lists, code blocks, tables, blockquotes are split into per-item/per-line/per-row blocks so each source line is independently commentable
5. **Comments reference source line numbers** — the `.review.md` output uses `> **[REVIEW COMMENT — Lines X-Y]**:` format
6. **Real-time output** — `.review.md` and `.comments.json` written on every comment change (200ms debounce)
7. **GitHub-style gutter interaction** — click-and-drag on line numbers to select ranges
8. **Live file watching** — polls source file every second, reloads via SSE on change for multi-round review workflow
9. **Localhost only** — server binds to `127.0.0.1`, no CORS headers needed

## Build & Run

```bash
go build -o crit .                                    # Build
go test ./...                                         # Run all tests
./crit test-plan.md                                   # Run (opens browser)
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

## API Endpoints

- `GET  /api/document` — raw markdown content + filename
- `GET  /api/comments` — all comments
- `POST /api/comments` — add comment `{start_line, end_line, body}` (1MB body limit)
- `PUT  /api/comments/:id` — edit comment `{body}` (1MB body limit)
- `DELETE /api/comments/:id` — delete comment
- `POST /api/finish` — write final files, return prompt for agent
- `GET  /api/events` — SSE stream for file-changed events
- `GET  /api/stale` — check if file changed since last session
- `DELETE /api/stale` — dismiss stale notice
- `GET  /api/config` — returns `{share_url, hosted_url, delete_token}` for the Share button
- `POST /api/share-url` — persist `{url, delete_token}` to `.comments.json` after upload
- `DELETE /api/share-url` — unpublish: calls crit-web DELETE and clears local persisted URL
- `GET  /files/<path>` — serve files from document directory (path traversal protected)

## Security

- Server binds to `127.0.0.1` only
- `/files/` endpoint validates paths, blocks `..` traversal, verifies resolved path stays within document directory
- Request body size limited to 1MB via `http.MaxBytesReader`
- HTTP server has `ReadTimeout: 15s`, `IdleTimeout: 60s` (no `WriteTimeout` — SSE needs open connections)
- Comment renderer uses `html: false` to prevent XSS in user comments
- Document renderer uses `html: true` intentionally (reviewing your own local files)
- Filename escaped in innerHTML contexts

## Frontend Architecture (index.html)

The trickiest part is **source line mapping**. The approach:

1. Parse markdown with `markdown-it` to get tokens with `token.map` (source line ranges)
2. `buildLineBlocks()` walks the token stream and creates a flat array of commentable blocks
3. Container tokens (lists, tables, blockquotes) are drilled into — each list item, table row, or blockquote child becomes its own block
4. Code blocks (`fence` tokens) are split into per-line blocks with syntax highlighting preserved via `splitHighlightedCode()` which handles `<span>` tags crossing line boundaries
5. Each block gets a gutter entry with its source line number(s)
6. Comments are keyed by `end_line` and displayed after their referenced block

### Known Complexities

- **markdown-it token.map quirks**: The last item in a list often claims a trailing blank line (e.g., map `[94, 96]` for a single-line item). The code trims trailing blank lines from item ranges.
- **Table separator lines** (`|---|---|`): Not represented in tokens, appear as gap lines. Detected via regex and hidden with CSS.
- **Per-row tables**: Each row wrapped in its own `<table>` with `table-layout: fixed` + `<colgroup>` for column alignment.
- **Highlighted code splitting**: `splitHighlightedCode()` tracks open `<span>` tags across lines to properly close/reopen them.

## Theme System

The header has a 3-button theme pill (System / Light / Dark) replacing the old single toggle:
- No `data-theme` attribute → system preference via `prefers-color-scheme`
- `data-theme="light"` / `data-theme="dark"` → explicit override
- CSS vars are set in `:root` (dark fallback), `@media (prefers-color-scheme: light) html:not([data-theme])`, `[data-theme="dark"]`, and `[data-theme="light"]` blocks.
- Theme choice persisted to `localStorage` as `crit-theme` (`"system"` | `"light"` | `"dark"`).

## Share Feature

When `--share-url` (or `CRIT_SHARE_URL`) is set:
- The Share button appears in the header.
- Clicking it POSTs the current document + comments to `{share_url}/api/reviews` (crit-web API).
- The response `{url, delete_token}` is persisted to `.comments.json` via `POST /api/share-url`.
- A share-notice banner shows the URL with Copy / Unpublish actions.
- Unpublish calls `DELETE {share_url}/api/reviews?delete_token=...` then clears local state.
- Share URL and delete token survive file-hash changes (loaded unconditionally from `.comments.json`).

## Output Files

| File | Description |
|------|-------------|
| `plan.review.md` | Original markdown + comments as blockquotes — hand to your AI agent |
| `.plan.comments.json` | Hidden dotfile for resume support (stores file hash, share_url, delete_token) |
