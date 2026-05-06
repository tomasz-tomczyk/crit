# Design Mode — E2E Testing (addendum)

Status: spec, pre-implementation
Companion to: `2026-05-06-design-mode.md` (which carries the unit + Go-integration test plan)
Scope: `crit/` only. No `crit-web/` parity, no GitHub roundtrip, no `make e2e-share` scenarios — design mode in v1 doesn't touch any of those layers.

## Principle

Unit tests live in the main spec under `## Testing` and cover everything that doesn't need a real browser — proxy header rewriting, anchor data model, storage key, dispatcher, guard rails, session token, idle timeout, routing exemptions. Author them first.

This addendum covers only the things that genuinely need a real DOM, a real iframe, and a real `MutationObserver`: pin/navigate behaviour, marker positioning, html2canvas capture, postMessage protocol, route-change detection, and WebSocket upgrade pass-through.

Order of test authoring follows risk, not file layout. Unit tests prove the foundation; E2E lands once the core protocol is sound.

## Playwright project: `design-mode` (port 3129)

Follows the existing 6-project convention (`git-mode 3123`, `file-mode 3124`, etc.) — see `crit/CLAUDE.md` "Projects" table. New entry in `playwright.config.ts` with `workers: 1` and a test glob of `*.designmode.spec.ts`.

### Fixture: `setup-fixtures-designmode.sh`

Spins up a self-contained upstream **Go fixture binary** (recommended over `python -m http.server` — Python isn't guaranteed in CI's Go-toolchain image; a Go fixture compiles deterministically). Lives at `e2e/fixtures/designmode-upstream/main.go` and serves:

- `/` — minimal HTML with a header, a card list, a couple of buttons. Stable structure for selector tests.
- `/dashboard` — a second route. Used for route-change tests.
- `/redirect-same` — 302 to `/dashboard` (same-origin redirect rewrite).
- `/redirect-cross` — 302 to `http://example.invalid/x` (cross-origin redirect stub).
- `/spa` — page that uses `history.pushState` on button click. Tests SPA hooks.
- `/mutator` — page that appends/removes DOM nodes on a 200ms interval. Tests `MutationObserver` reposition path.
- `/ws` — WebSocket echo endpoint. Tests proxy 101 upgrade.
- `/cookie` — sets `Set-Cookie: foo=bar; Domain=upstream.test; Path=/`. Sanity-checks cookie scoping (the unit test is authoritative).
- `/slow` — 2s delayed response. Tests `ErrorHandler` only when killed mid-response.

Fixture script:
1. Build the fixture binary into `$E2E_TMP/upstream`.
2. Launch it on a free port; capture origin.
3. Launch `crit design <origin> --no-open --port 3129`.
4. Export both PIDs for teardown.

Self-contained — no `crit-web`, no Postgres, no `gh` auth.

### Test files (`e2e/tests/*.designmode.spec.ts`)

| Scenario | Validates | File |
| --- | --- | --- |
| iframe loads upstream `/`; agent posts `agent-ready` | proxy + injection + agent boot | `boot.designmode.spec.ts` |
| Toggle Pin via toolbar; click element; composer opens with selector + thumbnail | core pin flow end-to-end | `pin.designmode.spec.ts` |
| Toggle Pin via `p`; `Esc` exits; `p` suppressed when iframe input is focused | keyboard + `focus-state` protocol | `pin.designmode.spec.ts` |
| Right-click in Pin mode opens ancestor depth menu; selecting a level pins to that ancestor | ancestor menu | `pin.designmode.spec.ts` |
| Pin element where `html2canvas` fails (cross-origin `<img>`) → comment row renders `outerHTML` preview, not broken thumbnail | screenshot null fallback | `pin.designmode.spec.ts` |
| Save pin → numbered marker renders inside iframe at element's bounding rect | marker rendering | `marker.designmode.spec.ts` |
| Click marker in iframe → side-panel scrolls to thread | `pin-clicked` postMessage round-trip | `marker.designmode.spec.ts` |
| Visit `/mutator` — markers reposition within an animation frame after DOM mutation | `MutationObserver` + rAF batch | `marker.designmode.spec.ts` |
| Resize viewport via toolbar preset → `set-viewport` → `viewport-applied` → markers re-resolve | viewport protocol round-trip | `viewport.designmode.spec.ts` |
| Navigate via link in iframe to `/dashboard` → chrome current-route header updates; comment list filters | route-change postMessage | `navigation.designmode.spec.ts` |
| `pushState` on `/spa` → same `route-change` flow | SPA hook | `navigation.designmode.spec.ts` |
| Click `/redirect-same` → iframe ends up at `localhost:<proxy-port>/dashboard` | same-origin redirect rewrite (integration check; unit test is authoritative) | `navigation.designmode.spec.ts` |
| Click `/redirect-cross` → chrome surfaces "open in real browser" affordance | cross-origin stub + postMessage | `navigation.designmode.spec.ts` |
| WebSocket on `/ws` echoes through proxy | 101 upgrade pass-through (the only WS test in the suite) | `websocket.designmode.spec.ts` |
| Pin element on round 1; round-start hook on round 2 with element still present → resolved | anchor durability happy path | `rounds.designmode.spec.ts` |
| Pin, mutate fixture's HTML between rounds, restart, observe `Drifted = true` flag in side panel | drift detection | `rounds.designmode.spec.ts` |
| Forge a `fetch('http://localhost:<api-port>/api/file/comments', ...)` from `page.evaluate` running in iframe (proxy origin) → blocked by browser CORS | two-port origin separation (E2E sanity check; absence of CORS headers is the authoritative unit-level check) | `security.designmode.spec.ts` |

Helpers in `e2e/tests/designmode-helpers.ts` (sibling to `helpers.ts`): `enterPinMode(page)`, `clickInIframe(page, selector)`, `waitForAgentReady(page)`, `getMarkerCount(page)`, `forgeUnauthedWrite(page, body)`. Don't reach into `helpers.ts` — its assumptions about file-mode state don't carry.

## What we deliberately don't test

- **html2canvas correctness across frameworks.** v1 accepts capture failure with `screenshot: null`. We test the fallback, not pixels.
- **Real-world apps' selector drift.** v1 ships `Drifted` and trusts it. No fixture matrix of "Tailwind / styled-components / vanilla / Phoenix LiveView" — that's dogfooding, not test infra.
- **Service workers in upstream apps.** Spec acknowledges they may misbehave. Not testing.
- **Visual regression on markers.** Marker styling isn't load-bearing; positioning is, and we assert on `getBoundingClientRect`.
- **Every `Set-Cookie` attribute combination.** Strip-`Domain`, prefix-`Path` is unit-tested with a representative case; not enumerating `SameSite` × `Secure` × `HttpOnly`.
- **Cross-browser.** Existing E2E is Chromium-only; design mode follows suit.
- **`crit-web` parity.** v1 spec says crit-web is untouched.
- **GitHub roundtrip.** Design pins don't sync; the guard-rail unit tests prove they're filtered.

## CI integration

The e2e job in `.github/workflows/test.yml` runs `bash e2e/run.sh`, which invokes Playwright across all configured projects. **Adding the `design-mode` project to `playwright.config.ts` makes it run in CI automatically — no workflow YAML changes needed.**

One CI prerequisite: the fixture binary must be built before tests run. Have `setup-fixtures-designmode.sh` `go build` the fixture in-place at setup time. CI already has the Go toolchain (it's used to build crit). Keeps the change scoped to the `e2e/` subtree.

The `nix` build job needs no change (fixture binary is test-only). The `unit` job picks up the new `*_test.go` files automatically. Confirm in PR: `go test ./...` and `make e2e` pass locally before pushing.

## Scenarios from manual testing (add to Phase F)

Found 2026-05-06 while exercising `./crit design http://localhost:4000` against a real Phoenix LiveView app. Capture as Phase F scenarios so regressions don't recur.

| # | Scenario | Validates | Likely spec file |
| --- | --- | --- | --- |
| M1 | `crit design <url>` opens browser to `/design` (not bare root) and stdout contains a literal "open …/design" line | CLI output + browser-open URL match what regular `crit` does | `boot.designmode.spec.ts` |
| M2 | Pin click captures html2canvas screenshot successfully against a vanilla page (no `capture-failed` agent-error) | `/crit-vendor/html2canvas.js` actually serves the library; `window.html2canvas` populated after `script.onload` | `pin.designmode.spec.ts` |
| M3 | Composer chip shows `accessible_name` + small route badge + optional thumbnail, NOT the raw CSS selector path or `outerHTML` block | Composer is concise (Tidewave-style), full anchor still in payload | `pin.designmode.spec.ts` |
| M4 | Composer Save / Cancel buttons render with crit's existing `.btn` / `.btn-secondary` classes (visual + DOM assertion) | Reuse-of-chrome directive holds for buttons | `pin.designmode.spec.ts` |
| M5 | Side-panel comment row offers Expand, Edit, Resolve, Reply controls — same affordances as code-review `.comment-card` | Design mode lets users manage comments without a "main view" | `pin.designmode.spec.ts` (or new `panel.designmode.spec.ts`) |
| M6 | Round counter renders in the same DOM slot as regular crit's round counter (e.g. inside `.viewed-count` or matching position in `.header-right`) | Header layout consistency | `boot.designmode.spec.ts` |
| M7 | Click `#settingsToggle` in design mode → settings overlay opens with theme pill functional | Settings overlay listeners are wired in design mode | `boot.designmode.spec.ts` |
| M8 | Switch viewport from Desktop 1280 to Mobile 390 → reload page → viewport is still Mobile 390 (persisted via `crit-settings` cookie) | Viewport persistence across reloads | `viewport.designmode.spec.ts` |
| M9 | After iframe loads, click Pin button → mode flips to Pin reliably on first try (no need for refresh); Pin button is visually disabled until `agent-ready` fires | Pin-mode race fix; agent-ready handshake | `pin.designmode.spec.ts` |
| M10 | No "Phase E" / placeholder text remains visible anywhere in design-mode chrome | Placeholder cleanup | `boot.designmode.spec.ts` |
| M11 | After clicking an element in Pin mode, the element stays outlined (sustained highlight) for the entire time the composer is open. Highlight clears on Save or Cancel. | Anchor element visibility during compose | `pin.designmode.spec.ts` |
| M12 | Comments toggle button in navbar (same DOM slot/class as regular crit's `#commentsPanelToggle`) shows unresolved-pin count badge; clicking it toggles the side panel open/closed. State persists across reloads via `crit-settings` cookie (matching regular crit's behavior). | Reuse existing `#commentsPanelToggle` + count badge pattern | `boot.designmode.spec.ts` (or new `panel.designmode.spec.ts`) |
| M13 | Comments side panel is horizontally resizable via the same drag-handle pattern regular crit uses. User-chosen width persists via `crit-settings` cookie. Width can be set such that it collides with the iframe's viewport-preset width — that's accepted behavior (user's choice). | Reuse existing panel-resize logic from `app.js` | `panel.designmode.spec.ts` |
| M14 | Comments-panel filter pill (`All / Open / Resolved`) works in design mode — same pattern and counts as regular crit. Long comment bodies have an "Expand" affordance that toggles full body display. Both behaviours come from reusing the existing `.comment-card` and `.comments-filter-toggle` components rather than reimplementing. | Reuse of existing filter + expand affordances | `panel.designmode.spec.ts` |
| M15 | The panel's close (`✕`) button in the panel header closes the comments panel — same affordance and DOM hook as regular crit. Reopening via the navbar Comments toggle (M12) restores prior state (and persisted width from M13). | Reuse of existing panel-close button | `panel.designmode.spec.ts` |
| M16 | Composer keyboard shortcuts work: `Cmd/Ctrl+Enter` submits the comment; `Esc` cancels (see M17 for confirm rule) and clears M11's sustained highlight. Reply composer in the side panel honours the same shortcuts. Behaviour matches regular crit's `.comment-form` exactly because the same component is mounted. | Reuse of existing comment-form keyboard handlers | `pin.designmode.spec.ts` |
| M17 | `Esc` on an empty composer cancels immediately (closes composer, clears highlight). `Esc` on a composer with unsaved text triggers a `window.confirm` (or matching crit pattern) before discarding — same UX as regular crit's comment form. Cancel-button click follows the same rule. | Match crit's confirm-before-discard pattern | `pin.designmode.spec.ts` |
| M18 | Reply on a design-pin row posts to `POST /api/comment/{id}/replies?path=<pathname>` with `{body}`. New reply renders below the parent comment with author + timestamp + body. `Cmd/Ctrl+Enter` submits; `Esc` with text in the composer triggers `window.confirm` before discarding (mirrors M17); `Esc` on empty closes immediately. | Reply parity with code-review threads — MVP requirement | `panel.designmode.spec.ts` |

Each scenario should follow Phase F's existing test conventions (auto-retrying assertions, `frameLocator` for iframe access, helpers from `designmode-helpers.ts`).

## Risk-ordered E2E rollout

Each step is PR-sized; expect E2E to land *after* the unit-test PRs from the main spec.

1. **`boot` + `pin`.** Earliest signal that the postMessage protocol is sound. If these are flaky, fix the agent, not the test.
2. **`marker` + `viewport`.** Layout-coupled; lands once positioning logic stabilises.
3. **`navigation` + `websocket`.** Highest-fragility paths; lands once route-change + WS upgrade work in dogfooding.
4. **`rounds` + `security`.** Cross-cutting; lands last so prerequisite flows are stable.

If any step's tests are flaky on the first run, treat that as a signal to fix the implementation, not the test — design mode's whole value proposition is reliability across rounds.
