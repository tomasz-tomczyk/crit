---
paths:
  - "web/app.js"
  - "web/*.js"
---

# Frontend JS Rules (semantic — ESLint handles syntax/style)

## Large code-diff virtualization
- Mounted code diffs (unified **and** split) always render through `web/crit-diff-virtualizer.js`, including story chapter bodies (filtered hunk clones via the same `renderDiffHunks`). Do not reintroduce full-file line DOM for those mounts — measured wins are ~5× fewer nodes from medium sizes up and order-of-magnitude on huge diffs.
- The virtualizer resolves the nearest overflow scroll parent (`#storyPane` in story mode, otherwise the window) so viewport windowing and `scrollToRow` track the element that actually scrolls.
- Keep the virtualizer thin: logical row model + height index + viewport ± overscan + remount-safe comment/form state. Prefer extending this module over embedding a third-party diff engine (Shiki/shadow-DOM stacks cost megabytes and seconds of first paint for the same windowing idea).
- File-body deferral (Load Diff / `LARGE_DIFF_LINE_THRESHOLD`) stays separate: it skips fetching/showing huge bodies until click. Virtualization is how those (and all smaller) bodies render once mounted.
- When changing comment create/edit/reply, gutter drag, keyboard nav, or scroll-restore on diffs, update the virtualizer’s model-first paths (`pin` / `rowKeyForLine` / rebuild) in the same change. Remounted rows must not lose draft text or selection anchors.
- Port behavior to crit-web when the Crit path ships — review-page parity still applies (`app.js` ↔ `document-renderer.js`, shared class names).
- Protect always-on virtualization with: focused unit tests under `web/__tests__/crit-diff-virtualizer.test.js`, e2e `diff-virtualization.spec.ts`, and the existing diff/comment/drag/draft/story suites (they now exercise the virtual path by default).
- Prefer production-UI measurement over throwaway demos when comparing approaches.

## DOM & Events
- Use `addEventListener`, not inline `onclick` handlers in new code.
- Cache repeated DOM queries into const variables.

## State Management
- When async operations can be triggered from multiple sources, add dedup guards (e.g., `reloadInFlight` promise pattern).
- Reset ALL navigation/UI state when context changes (scope change, session reload, comment delete).
- When implementing UI state logic that counts comments, account for ALL comment types (file-scoped AND review-level).

## Persisted Settings (cookies, not localStorage)
- Use cookies for any setting that should persist across `crit` invocations. Crit defaults to a random port (`port=0`), and localStorage is scoped per origin (scheme + host + **port**), so localStorage settings reset every run. Cookies are host-scoped and survive port changes.
- Persisted settings live in a single `crit-settings` JSON cookie via `getSetting(key, fallback)` / `setSetting(key, value)`. The exception is `crit-templates`, which stays in its own cookie because it's user-defined and can be longer.
- localStorage is fine for transient per-session data (e.g., `crit-draft-*` autosave keys that are review-specific anyway).
- Before adding persistence to a setting, ask whether it should be sticky. Transient view state (active filter, sort order on a list) usually shouldn't be — users coming back to a new review with "resolved-only" active would miss new open comments. Persist preferences (theme, width, hide-resolved), not transient views.

## Error Handling
- Always check `response.ok` after `fetch()` calls. Throw on unexpected statuses.
- Every async operation must have error recovery that restores interactivity (re-attach listeners, undo optimistic UI).
- Never call `.remove()` directly on elements with CSS exit animations. Use class toggle + animationend listener.

## Accessibility (axe-core in E2E catches missing aria-labels, dialog roles, tab roles)
- Never call `.blur()` on interactive elements — it breaks keyboard navigation. axe-core catches the effect (no focus indicator) but not the cause.

## SSE Events
- When an SSE event signals a specific data change (e.g., `comments-changed`), only re-fetch that data — not everything.
