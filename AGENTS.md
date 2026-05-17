# Crit — Development Guide

Single-binary Go CLI that opens a browser-based UI for reviewing code changes and markdown files with GitHub PR-style inline commenting. Multi-file review with git diff rendering and structured review file output for AI coding agents.

## Project map

```
crit/
├── main.go              # Entry point: subcommand dispatcher + individual runX() functions
├── server.go            # HTTP handlers: REST API (session, file, comments CRUD, finish, share, config)
├── session.go           # Core state: multi-file session, comment storage, review file persistence, SSE
├── watch.go             # File/git watching, round-complete handlers, comment carry-forward
├── git.go               # Git integration: branch detection, changed files, diff parsing
├── github.go            # GitHub PR sync: fetch/post PR comments, crit comment CLI, review file I/O
├── config.go            # Config file loading: ~/.crit.config.json + .crit.config.json merge, ignore patterns
├── diff.go              # LCS-based line diff for inter-round markdown comparison
├── status.go            # Terminal status output formatting
├── daemon.go            # Daemon lifecycle: spawn, connect, stop, session registry
├── share.go             # Share/unpublish to crit-web, share CLI subcommand
├── plans.go             # Plan file detection and handling
├── integrations.go      # Integration config installation (crit install <agent>)
├── vcs.go / git_vcs.go / sapling.go / jj.go  # VCS abstraction (git + sapling + jj)
├── auth.go              # Hosted crit-web auth flow (login/logout, token storage)
├── focus_*.go / picker.go  # Focus mode (range, stacked) + file picker backend
├── review_file.go       # Review file (~/.crit/reviews/<key>.json) read/write — saveCritJSON
├── pr_cache.go / pr_fetch.go / push_buckets.go  # GitHub PR fetch/cache, comment bucketing
├── remote_files.go      # Fetch files from a remote PR for cross-PR comparisons
├── browser.go / lru_bytes.go    # Browser open, base-branch detection, byte cache
├── comment_cli.go       # `crit comment` headless implementation
├── gen_integration_hashes.go / integration_hashes_gen.go  # Build-time integration manifest
├── *_test.go            # Tests (testutil_test.go has shared helpers; *_integration_test.go behind build tag)
├── design.go            # `crit design <url>` command — design-mode session bootstrap
├── preview.go           # `crit preview <file.html>` command — preview-mode session bootstrap + content serving
├── proxy.go             # Reverse proxy for design-mode iframe (HTML injection, redirect rewriting)
├── frontend/
│   ├── index.html       # HTML shell — two-paradigm fork loads code-review OR design-mode scripts
│   ├── app.js           # Code-review mode JS (file tree, rendering, comments, SSE, shortcuts)
│   ├── design-mode.js   # Design-mode entry point (iframe chrome, pin workflow, panel)
│   ├── design-mode.*.js # Design-mode sub-modules (toggle, composer, panel, sse, etc.)
│   ├── crit-agent.js    # Injected into iframe — captures DOM clicks for pin anchoring
│   ├── agent-*.js       # Agent sub-modules (protocol, marker overlay, mutation batcher)
│   ├── crit-shared.js   # Shared helpers (cookies, theme, image upload) — window.crit.shared
│   ├── crit-renderer.js # ContentRenderer registry — window.crit.renderer
│   ├── crit-sse.js      # Shared SSE client factory — window.crit.sse
│   ├── crit-draft.js    # Draft autosave — window.crit.draft
│   ├── crit-comment-*.js # Shared comment form, card, templates — window.crit.comment*
│   ├── crit-icons.js    # SVG icon constants — window.crit.icons
│   ├── crit-line-blocks.js    # buildLineBlocks, splitHighlightedCode — window.crit.lineBlocks
│   ├── crit-diff-renderer.js  # Word-level diff computation — window.crit.diffRenderer
│   ├── style.css        # Code-review layout, diff rendering, file sections, components
│   ├── style-design.css # Design-mode layout (iframe pane, panel, markers)
│   ├── theme.css        # Color themes (light/dark/system CSS variables)
│   ├── __tests__/       # Node.js unit tests for extracted modules (node --test)
│   └── *.min.js         # Vendored markdown-it, highlight.js, mermaid
├── integrations/        # Drop-in config files for AI coding tools (claude-code, cursor, aider, etc.)
├── e2e/                 # Playwright E2E tests for the frontend (multi-project setup, see below)
├── Makefile             # build / build-all (cross-compile) / update-deps / clean / e2e
├── package.json         # Frontend dependency management (markdown-it, highlight.js, mermaid)
└── copy-deps.js         # Copies npm deps to frontend/ for embedding
```

## Key architecture decisions

1. **All frontend assets embedded** via Go's `embed.FS` — produces a true single binary
2. **No frontend build step** — vanilla JS, no npm/webpack/framework. npm is only for fetching vendor libs.
3. **Two modes**: "git" mode (auto-detect from git) and "files" mode (explicit file arguments)
4. **markdown-it for parsing** — chosen because it provides `token.map` (source line mappings per block)
5. **Block-level splitting** — lists, code blocks, tables, blockquotes split into per-item/per-line/per-row blocks so each source line is independently commentable
6. **Diff hunk rendering** — code files show git diffs with dual gutters (old/new line numbers)
7. **Comments reference source line numbers** — stored in `~/.crit/reviews/<key>.json` with per-file sections
8. **Real-time output** — review file written on every comment change (200ms debounce)
9. **File watching** — git mode polls `git status --porcelain`; files mode polls mtimes; reloads via SSE
10. **Localhost by default** — server binds to `127.0.0.1` (no CORS headers needed). Configurable via `--host` / `CRIT_HOST` / `host` config key (e.g. `0.0.0.0` for LAN). No auth, so non-loopback binds are explicit opt-in.
11. **Two-level config** — `~/.crit.config.json` (global) merged with `.crit.config.json` (project), CLI flags override both. `agent_cmd`, `auth_token`, and `share_url` are global-only (prevents malicious repos from hijacking the agent command or redirecting share requests to steal auth tokens)
12. **Headless CLI comment** — `crit comment` writes directly to the review file without starting the server; SSE notifies any running server
13. **Comment threading** — comments support nested replies and a `resolved` boolean. Review file schema nests replies inside each comment's `replies` array.
14. **Centralized review storage** — `~/.crit/reviews/<key>.json` keyed by cwd + branch (git mode) or cwd + args (file mode)
15. **VCS abstraction** — `vcs.go` defines a backend interface; `git_vcs.go`, `sapling.go`, and `jj.go` are the implementations. Auto-detected, overridable via `--vcs` flag or `vcs` config key. Subcommands not yet threaded through (see TODO at `main.go:1826`).
16. **Focus mode** — sub-views over the file list: file focus, range focus (`--range A..B`), stacked focus (range layer in a stacked PR). Lives in `focus_*.go` and `/api/focus`.

<important if="you need to build, test, lint, or run crit">

```bash
go build -o crit .                                    # Build
go test ./...                                         # Run all tests
gofmt -l .                                            # Check formatting (should be clean)
golangci-lint run ./...                               # Lint (should be clean)
make build-all                                        # Cross-compile to dist/
./crit                                                # Git mode (auto-detect changed files)
./crit test-plan.md                                   # Review specific file(s)
./crit --no-open --port 3000 test-plan.md             # Headless on fixed port
```
</important>

<important if="you need to know what crit subcommands do or are adding/modifying a CLI subcommand">

Subcommands are dispatched via `commandDispatch` in `main.go`. Anything not in the table falls through to `runReview`.

```
crit                          # Review git changes (starts daemon, blocks for feedback)
crit <file|dir> [...]         # Review specific files or directories (falls through to runReview)
crit review [...]             # Explicit review invocation (same as default)
crit design <url>             # Review a running web app in design mode (also: crit <url>)
crit preview <file.html>      # Review a local HTML file in preview mode (also: crit <file.html>)
crit stop [--all]             # Stop daemon(s) for current directory
crit status [--json]          # Show review file path, daemon status, comment stats
crit cleanup [--days N] [--force]  # Delete stale review files from ~/.crit/reviews/
crit pull [pr-number]         # Fetch GitHub PR comments into the review file
crit push [--dry-run] [--event <type>] [-m <msg>] [pr]  # Post review comments as a GitHub PR review
crit pr <num|url>             # Thin shim — forwards to `crit review --pr <n>`
crit fetch ...                # Fetch remote artefacts (see runFetch)
crit comment <path>:<line[-end]> <body>         # Add a comment (no server needed)
crit comment --reply-to <id> [--resolve] <body> # Reply to a comment
crit comment --json [--file <path>] [--author <name>]  # Bulk add comments from JSON (stdin or --file; - = stdin)
crit share <file> [file...]   # Share files to crit-web, print URL
crit unpublish                # Remove shared review from crit-web
crit config [--generate]      # Print resolved config (or starter template)
crit install <agent>          # Install integration config for an AI tool
crit auth ...                 # Auth flow for hosted crit-web (login/logout)
crit plan [...]               # Plan-file workflow
crit plan-hook                # Internal hook used by plan flow
crit check                    # Self-check (env, git, gh availability)
crit _serve                   # Internal: foreground server (used by daemon spawn)
crit --version | -v           # Version
crit help | --help | -h       # Show help
```
</important>

<important if="you are working with config files (~/.crit.config.json or .crit.config.json) or adding a config key">

Two-level JSON config files, merged (project overrides global):

- **Global**: `~/.crit.config.json` — user-wide defaults
- **Project**: `.crit.config.json` in repo root — per-project overrides

Config keys: `port`, `host`, `no_open`, `share_url`, `quiet`, `output`, `author`, `base_branch`, `ignore_patterns`, `agent_cmd`, `auth_token`, `auth_user_name`, `auth_user_email`, `auth_user_id`, `cleanup_on_approve`, `no_update_check`, `no_integration_check`, `vcs`, `proxy_auth`.

- `base_branch` overrides auto-detected default branch (used as diff base in git mode, and by `crit pull`/`crit push`/`crit comment`)
- `author` falls back to the configured VCS user name if not set
- `agent_cmd`, `auth_token`, `share_url`, and `proxy_auth` are **global config only**; project-level config cannot override (security — prevents malicious repos from hijacking the agent command or redirecting share requests to an attacker-controlled host)
- `proxy_auth` (default: `false`) — when `true`, share/pull/unpublish use browser popup relay instead of direct CLI HTTP. Global-only for security.
- `cleanup_on_approve` (default: `true`) — auto-delete review file when reviewer approves with no unresolved comments
- `ignore_patterns` are unioned (global + project both apply); types: `*.ext`, `dir/`, `exact.file`, `path/*.ext`
- `vcs` selects backend: `"git"` (default), `"sl"` (sapling), or `"jj"` (Jujutsu)
- `auth_*` keys hold cached hosted-crit-web credentials (set by `crit auth`); treat as secrets
- CLI flags override config file values
</important>

<important if="you are working with crit pull, crit push, or GitHub PR sync">

Requires `gh` CLI installed and authenticated.

- `crit pull` fetches PR review comments (RIGHT-side only) and merges them into the review file, deduplicating by author+lines+body
- `crit push` reads the review file and posts unresolved comments as a GitHub PR review
- `crit push --dry-run` shows what would be posted without creating the review
- `crit push --event approve` submits an approval; `--event request-changes` requests changes (default: `comment`)
- `crit push -m 'message'` adds a review-level body message
- PR number auto-detected from current branch, or pass explicitly: `crit pull 42`
- Any code path that imports comments from an external source (GitHub PR, crit-web) into the local review file MUST dedup against local state first: `buildLocalIDSet` + `buildLocalFingerprintIndex` + `dropDuplicateWebComment`. This applies to direct HTTP paths AND browser relay paths. Calling `mergeWebComments` without pre-filtering causes duplicate comments on repeated pull.
</important>

<important if="you are writing, running, or modifying Playwright E2E tests in e2e/">

The `e2e/` directory contains Playwright tests against a real compiled `crit` binary — no mocking.

### Running

```bash
make e2e                                              # Full suite
cd e2e && npx playwright test tests/comments.spec.ts  # One file
cd e2e && npx playwright test --headed                # Visible browser
E2E_DEBUG=1 make e2e                                  # Enable video + trace capture on failure
make e2e-report                                       # View HTML report with screenshots
```

### Projects

Six Playwright projects, each with its own fixture script and port. Test naming convention determines which project runs which file:

| Project | Port | Fixture | Test glob |
| --- | --- | --- | --- |
| `git-mode` | 3123 | `setup-fixtures.sh` (git repo + feature branch) | `*.spec.ts` (excludes other suffixes) |
| `file-mode` | 3124 | `setup-fixtures-filemode.sh` (plain files, no git) | `*.filemode.spec.ts` |
| `single-file-mode` | 3125 | `setup-fixtures-singlefile.sh` (one markdown file) | `*.singlefile.spec.ts` |
| `no-git-mode` | 3126 | `setup-fixtures-nogit.sh` (file mode without git) | `*.nogit.spec.ts` |
| `multi-file-mode` | 3127 | `setup-fixtures-multifile.sh` (code + markdown files) | `*.multifile.spec.ts` |
| `range-mode` | 3128 | `setup-fixtures-range-mode.sh` (`--range A..B` stacked git) | `*.rangemode.spec.ts` |
| `design-mode` | 3129 | `setup-fixtures-designmode.sh` (Go upstream + crit design) | `*.designmode.spec.ts` |

CI runs E2E on push to `main` and PRs via `.github/workflows/test.yml`. Failed test artifacts are uploaded.

### Best practices

- **Never `waitForTimeout` / `setTimeout`** for state. Use auto-retrying assertions (`toPass()`, `toHaveClass()`, `toBeVisible()`). Sleep is OK only inside polling loops where you're already retrying.
- **Never `.count()` followed by `expect(count).toBe(N)`** — that's a snapshot. Use `await expect(locator).toHaveCount(N)` or wrap in `toPass()`.
- **Always import shared helpers from `./helpers`** (the file is `e2e/tests/helpers.ts`, plus `range-helpers.ts` for range-mode tests): `clearAllComments`, `loadPage`, `mdSection`, `goSection`, `jsSection`, `switchToDocumentView`, `dragBetween`, `clearFocus`, `addComment`, `getMdPath` (and `rangeFixture`, `ensureRangeFocus`, `ensureStackedFocus` from `range-helpers`). Don't redefine locally. Use `Page` types, not `any`.
- **Always call `clearAllComments(request)` in `beforeEach`** — server persists comments across tests. This calls `DELETE /api/comments` (bulk endpoint).
- **Markdown defaults**: git mode → diff view (call `switchToDocumentView()`); file mode → document view (no toggle).
- **Parallel execution**: projects run in parallel via shell. Within a project, tests run sequentially (`workers: 1`) — don't change this; they share server state.
- **Scroll before interact**: in file-mode (multiple files below the fold), call `scrollIntoViewIfNeeded()` before hover/click/drag.
- **CSS selectors**: check existing tests for class names (e.g. `.tree-comment-badge`, not `.tree-file-comments`).
</important>

<important if="you are running or modifying share integration tests (build tag: integration)">

`share_integration_test.go` exercises the crit ↔ crit-web share flow. When modifying share logic, the share payload, comment sync, or any crit-web interaction:

1. Run: `make e2e-share` (or `./scripts/e2e-share.sh`)
2. Add new test cases for new share functionality — name them `TestShareSync*`
3. Inspect on web: `./scripts/e2e-share.sh --serve` starts crit-web and logs review URLs

Requires a local crit-web checkout at `../crit-web` and PostgreSQL. See `scripts/AGENTS.md` for full details.
</important>

<important if="you are modifying crit pull, crit push, GitHub PR comment sync, the review-file ↔ GitHub roundtrip, or anything in `github.go` / `pr_cache.go` / `pr_fetch_test.go` / `push_buckets.go` / `comment_cli.go` reply handling">

`roundtrip_integration_test.go` (build tag `e2e_github`) exercises the crit ↔ GitHub PR roundtrip against a real sandbox PR. When modifying pull/push, GitHub-comment-bucket logic, reply posting, or `mergeGHComments*` dedup:

1. Run: `make e2e-roundtrip` (or `./scripts/e2e-roundtrip.sh -run <TestName> -v` for one scenario)
2. Add new `TestRoundtrip_<Name>` scenarios for new state transitions — see `test/roundtrip/README.md` for authoring notes
3. If a scenario is currently `t.Skip`'d against an issue and your change fixes the underlying bug, REMOVE the skip and run the scenario

Requires `gh` authenticated and `CRIT_ROUNDTRIP_REPO=<owner>/crit-roundtrip-sandbox` exported. Each scenario opens-then-closes a real PR (~10-25s each, ~100s suite). Tests are local-only (build tag keeps them out of CI / default `go test ./...`).
</important>

<important if="you are adding or modifying HTTP API endpoints in server.go">

All routes wrapped with `s.withReady` return 503 until session init completes — except `/api/health` and `/api/qr`.

Session-scoped:

- `GET  /api/health` — liveness probe (no readiness gate; used for daemon health checks)
- `GET  /api/qr` — QR code for current shared URL
- `GET  /api/session` — session metadata
- `GET  /api/config` — `{share_url, hosted_url, delete_token, version, latest_version, ...}`
- `GET  /api/review-cycle` — review-cycle metadata (round number, edits-since-last)
- `POST /api/share` — perform a share (POST to crit-web `/api/reviews`); returns URL+delete_token
- `POST /api/share-url` / `DELETE /api/share-url` — persist or unpublish shared URL
- `POST /api/finish` — write review file, return prompt for agent
- `GET  /api/events` — SSE stream (file-changed, edit-detected, server-shutdown)
- `GET  /api/wait-for-event` — long-poll until finish (used by `crit` daemon mode)
- `POST /api/round-complete` — agent signals all edits done; triggers new round
- `…/api/focus` — set/clear focus (file or range scope)
- `…/api/picker` — file-picker UI backend
- `POST /api/agent/request` — send comment to configured `agent_cmd`
- `GET  /api/branches` — list local branches (for base-branch picker)
- `GET|POST /api/base-branch` — read/update active base branch
- `GET  /api/commits` — list commits between base ref and HEAD (git mode only)
- `GET  /api/files/list` — list session files (lighter than `/api/session`)
- `GET|POST /api/comments` — list/add review-level comments
- `PUT|DELETE /api/review-comment/{id}` (and `/replies[/{rid}]`, `/resolve`) — review-comment CRUD

File-scoped (require `?path=X`):

- `GET  /api/file?path=X` — file content + metadata
- `GET  /api/file/diff?path=X` — diff hunks (git diff for code; inter-round diff for markdown)
- `GET|POST /api/file/comments?path=X` — list/add comments (10MB body limit on POST)
- `PUT|DELETE /api/comment/{id}?path=X` — update or delete (10MB body limit on PUT)
- `POST|PUT|DELETE /api/comment/{id}/replies[/{rid}]?path=X` — reply CRUD
- `PUT /api/comment/{id}/resolve?path=X` — set resolved state

Static: `GET /files/<path>` — serve files from repo root (path traversal protected). `GET /` — embedded frontend assets.
</important>

<important if="you are modifying server security, request handling, or path-validation logic">

- Server binds to `127.0.0.1` by default; user can opt into a different host via `--host` / `CRIT_HOST` / `host` config key. There is no auth, so any non-loopback bind exposes file content + comment-write API to anyone who can reach the port — that's why it's an explicit opt-in (CLI flag / env var / config key).
- `/files/` validates paths, blocks `..` traversal, verifies resolved path stays within repo root
- Body size: 10MB for comments, 1MB for share-url via `http.MaxBytesReader`
- HTTP server: `ReadTimeout: 15s`, `IdleTimeout: 60s` (no `WriteTimeout` — SSE needs open connections)
- Comment renderer uses `html: false` (XSS prevention in user comments)
- Document renderer uses `html: true` intentionally (reviewing local files)
</important>

<important if="you are modifying frontend/ — app.js, style.css, theme.css, or index.html">

Frontend split: `index.html` (HTML shell), `app.js` (all logic), `style.css` (layout/components), `theme.css` (theme variables).

### Multi-file state model

Three top-level globals in `app.js`: `session` (mode, branch, base_ref, review_round, files), `files` (per-file render state with comments, lineBlocks), `activeForms` (multiple comment forms can be open simultaneously). See top of `app.js` for shapes.

### Source line mapping (markdown)

1. Parse with `markdown-it` to get tokens with `token.map` (source line ranges)
2. `buildLineBlocks()` dispatches to per-token-type handlers: `handleFenceToken`, `handleListToken`, `handleTableToken`, `handleBlockquoteToken`
3. Container tokens (lists, tables, blockquotes) are drilled into — each item/row/child becomes its own block
4. Code blocks (`fence` tokens) split into per-line blocks with syntax highlighting preserved via `splitHighlightedCode()`
5. Each block gets a gutter entry with its source line number(s)
6. Comments are keyed by `end_line` and displayed after their referenced block

### Diff hunk rendering (code files)

Hunk headers (`@@ -27,6 +31,23 @@`), dual gutters, colored backgrounds for additions/deletions, spacers between hunks, inline comment via gutter `+` buttons.

### Known complexities

- `markdown-it` token.map quirks: last list item often claims a trailing blank line — code trims trailing blank lines from item ranges.
- Table separators (`|---|---|`): not in tokens, appear as gap lines. Detected via regex and hidden with CSS.
- Per-row tables: each row in its own `<table>` with `table-layout: fixed` + `<colgroup>` for column alignment.
- `splitHighlightedCode()` tracks open `<span>` tags across lines to properly close/reopen them.
</important>

<important if="you are adding CSS variables or modifying theme.css">

Header has a 3-button theme pill (System / Light / Dark):

- No `data-theme` attribute → system preference via `prefers-color-scheme`
- `data-theme="light"` / `data-theme="dark"` → explicit override
- CSS vars are set in `:root` (dark fallback), `@media (prefers-color-scheme: light) html:not([data-theme])`, `[data-theme="dark"]`, and `[data-theme="light"]` blocks. **Define every new variable in all four blocks.**
- Theme choice persisted via `crit-settings` cookie (`theme` key, `"system"` | `"light"` | `"dark"`).
- Use CSS custom properties from `theme.css` for all colors. Never hardcode hex values.
</important>

<important if="you are modifying share, unpublish, or share-button UI in crit/">

Sharing is opt-in. When `--share-url` (or `CRIT_SHARE_URL` env var, or `share_url` in config file) is set:

- Share button appears in the header
- Click POSTs document + comments to `{share_url}/api/reviews` (crit-web API)
- Response `{url, delete_token}` persisted to review file via `POST /api/share-url`
- Share-notice banner shows the URL with Copy / Unpublish actions
- Unpublish calls `DELETE {share_url}/api/reviews?delete_token=...` then clears local state
</important>

<important if="you are modifying multi-round logic, round-complete, or finish handling">

When the agent runs `crit` again (or calls `POST /api/round-complete`):

- **Markdown files**: snapshot content, carry forward unresolved comments, re-read from disk
- **Code files**: re-run git diff against base ref to get updated hunks
- **File list**: re-run `ChangedFiles()` to detect new/removed files
- Waiting modal shows live count of file edits while the agent works
- Diff toggle for markdown shows inter-round changes
</important>

<important if="you are modifying daemon spawning, session lookup, or ~/.crit/sessions/">

`crit` manages a background daemon for seamless multi-round reviews:

1. **First `crit`**: starts background daemon (`crit _serve`), opens browser, blocks for feedback
2. **Subsequent `crit`**: connects to existing daemon (same cwd + args), signals round-complete, blocks
3. **`crit plan.md`**: looks up daemon by hash(cwd + "plan.md") — reuses if alive, starts new if dead
4. **Ctrl+C**: kills the daemon the client started
5. **`crit stop`**: kills daemon for current cwd; `crit stop --all` kills all daemons for cwd
6. **Lifetime**: daemon runs until killed (Ctrl+C, `crit stop`, or SIGINT/SIGTERM/SIGHUP). No idle timeout — walking away from a review session is fine.

### Deferred initialization & readiness

The daemon signals readiness (via OS pipe) as soon as the HTTP port is bound, but session init (git, file reads) continues in the background. Until `SetSession()` is called, most endpoints return **503 Service Unavailable**.

**Any client connecting to a daemon must poll `/api/session` until it stops returning 503 before calling other endpoints.** See `runReviewClient` and `runReviewClientRaw` for the canonical readiness loop. Skipping this poll causes races where endpoints return 503, and error-fallback paths may silently allow/approve when they shouldn't.

### Session registry

Daemon state in `~/.crit/sessions/`, one file per session.
- Git mode (no args): `sha256(cwd + "\0" + branch)[:12]`
- File mode (args present): `sha256(cwd + "\0" + args...)[:12]` (branch excluded — file reviews aren't branch-dependent)

Session file: `{"pid", "port", "cwd", "args", "branch", "review_path", "started_at"}`. Review data lives at `~/.crit/reviews/<key>.json` (same key).

`crit _serve` runs the server in foreground (used by daemon spawning, not user-facing).
</important>

<important if="you are reviewing code or evaluating audit findings for this project">

Calibrate against the tool's actual scale before flagging issues. False-positive filters:

- **"Real problem at this scale?"** Localhost-only, single-user CLI. Patterns that matter for cloud services (context propagation, map-based lookups, connection pooling) often don't apply. Typical sessions: 5–50 files, <50 comments.
- **"Does the execution model make this possible?"** JavaScript is single-threaded — there are no race conditions between synchronous scope assignments and async fetches. Verify the threading model before claiming races.
- **"Realistic inputs?"** Markdown files can be 10,000+ lines (AI-generated plans) — perf concerns for large markdown are legitimate. Perf concerns for file lists or comment lists are not.
- **"Simpler than the duplication?"** For a single-file vanilla JS app and a flat Go CLI, inline code is often clearer than extracted helpers. Don't abstract for fewer than 3 call sites.

Project-specific calibration:

- **Unexport what isn't needed.** This is `package main` (a binary, not a library). If a function/type is only used within the package, it should be unexported.
- **Don't add `context.Context` to local git operations.** All git commands here are read-only local ops (diff, status, log, rev-parse). They complete in milliseconds and don't touch the network. The one path that benefits from context (`fileDiffUnifiedCtx` for lazy loading) already has it.
- **O(n) scans over file lists are fine.** A linear scan of `fileByPathLocked` is nanoseconds. Don't add map indices unless profiling shows a real bottleneck.
- **Mechanical duplication can be OK.** Comment CRUD (review vs file-scoped) is structurally identical but stable. Don't abstract stable boilerplate unless adding new operations would grow the duplication.
- **Don't fight browser built-ins.** `EventSource` auto-reconnects natively. `<details>`/`<summary>` handles keyboard natively. Don't reimplement.
</important>

<important if="you are about to claim work is complete — pre-completion checklist">

These issues recur in AI-generated code for this project. Only items NOT caught by automated tooling (golangci-lint, ESLint, Stylelint, axe-core) are listed.

### Go backend
1. Forgetting to clear review-level comments when clearing file comments
2. Missing fields in struct construction (silent data loss)
3. Creating wrapper functions that just delegate
4. Inline reimplementation of existing helper functions
5. Mixing `git status --porcelain` and `git diff --name-status` inconsistently
6. Not validating daemon health check response body

### Frontend JS
1. Missing `aria-label` on icon-only buttons (axe-core catches in E2E, but prevent at source)
2. Not checking `response.ok` after `fetch()`
3. Not resetting navigation state on context changes
4. SSE handlers re-fetching more data than changed
5. Async operations without dedup guards when triggerable from multiple sources
6. `.remove()` on elements with CSS exit animations (use animationend)

### Frontend CSS
1. Not defining new CSS variables in all 4 theme blocks
2. Leaving dead selectors after renaming CSS classes or DOM IDs
3. Referencing undefined CSS variables (check-css-vars.sh catches in pre-commit)
</important>

<important if="you are compacting context or summarizing this session">

Always preserve:
- The list of files modified in this session
- Any unresolved review comments or crit feedback
- The current phase of any multi-phase workflow (review, ship, audit)
- Which worktree you're working in
</important>
