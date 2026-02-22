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
./crit go --wait 3000                                 # Signal round complete + wait for review, print prompt to stdout
./crit wait 3000                                      # Wait for review (no round-complete signal), print prompt to stdout, exit
```

## Linting

```bash
gofmt -l .                        # Check formatting (should be clean)
golangci-lint run ./...           # Lint (should be clean)
```

## API Endpoints

- `GET  /api/document` — raw markdown content + filename
- `GET  /api/comments` — all comments
- `POST /api/comments` — add comment `{start_line, end_line, body}` (10MB body limit)
- `PUT  /api/comments/:id` — edit comment `{body}` (10MB body limit)
- `DELETE /api/comments/:id` — delete comment
- `POST /api/finish` — write final files, return prompt for agent
- `GET  /api/events` — SSE stream for file-changed events
- `GET  /api/stale` — check if file changed since last session
- `DELETE /api/stale` — dismiss stale notice
- `GET  /api/config` — returns `{share_url, hosted_url, delete_token, agent_waiting}` for the Share button
- `GET  /api/await-review` — long-polls until review is finished, returns `{prompt, review_file}` (used by `crit go --wait` and `crit wait`)
- `POST /api/share-url` — persist `{url, delete_token}` to `.comments.json` after upload
- `DELETE /api/share-url` — unpublish: calls crit-web DELETE and clears local persisted URL
- `POST /api/round-complete` — agent signals all edits are done; triggers new round in the browser
- `GET  /api/previous-round` — returns previous round's content and comments for diff rendering
- `GET  /api/diff` — returns line-level diff between previous and current round content
- `GET  /files/<path>` — serve files from document directory (path traversal protected)

## Security

- Server binds to `127.0.0.1` only
- `/files/` endpoint validates paths, blocks `..` traversal, verifies resolved path stays within document directory
- Request body size limited to 10MB for comments, 1MB for share-url via `http.MaxBytesReader`
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

## Multi-Round Review

When the agent runs `crit go <PORT>` (or calls `POST /api/round-complete`), the browser transitions to a new review round:
- A side-by-side diff panel (toggle in header) shows what changed since the previous round
- Previous comments marked as `resolved: true` in `.comments.json` appear as collapsed green cards at their `resolution_lines` positions
- The waiting modal shows a live count of file edits while the agent is working

## Agent Auto-Notification

Both `crit wait <port>` and `crit go --wait <port>` block until the reviewer clicks Finish, then print the review prompt to stdout. The agent reads it directly — no manual copy-paste needed.

**Two commands, two use cases:**

- **`crit wait <port>`** — Round 1. Just the long-poll; no round-complete signal. Start crit in the background separately, then call `crit wait` to block. Exits cleanly when Finish is clicked.
- **`crit go --wait <port>`** — Round 2+. Signals round-complete (browser transitions to new round with diff), then blocks waiting for the next Finish click.

**Typical agent flow:**
```
# Round 1 (background start + wait):
crit plan.md --no-open --port 3001 &
crit wait 3001          # blocks, prints prompt on Finish, exits

# Round 2+ (signal + wait):
crit go --wait 3001     # signals round-complete, blocks, prints prompt, exits
```

- Status messages go to stderr, prompt goes to stdout
- If there are no comments, stdout is empty (agent should continue normally)
- The frontend shows "Review feedback sent to your agent" instead of "Paste to clipboard" when an agent is waiting
- `GET /api/config` includes `agent_waiting: true/false` to indicate whether an agent is connected

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
| `plan.review.md` | Original markdown + comments as blockquotes — hand to your AI agent |
| `.plan.comments.json` | Hidden dotfile for resume support (stores file hash, share_url, delete_token) |
