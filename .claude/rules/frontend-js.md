---
paths:
  - "frontend/app.js"
  - "frontend/*.js"
---

# Frontend JS Rules (semantic — ESLint handles syntax/style)

## DOM & Events
- Use `addEventListener`, not inline `onclick` handlers in new code.
- Cache repeated DOM queries into const variables.

## State Management
- When async operations can be triggered from multiple sources, add dedup guards (e.g., `reloadInFlight` promise pattern).
- Reset ALL navigation/UI state when context changes (scope change, session reload, comment delete).
- When implementing UI state logic that counts comments, account for ALL comment types (file-scoped AND review-level).

## Persisted Settings (cookies, not localStorage)
- Use cookies (`getCookie`/`setCookie`) for any setting that should persist across `crit` invocations. Crit defaults to a random port (`port=0`), and localStorage is scoped per origin (scheme + host + **port**), so localStorage settings reset every run. Cookies are host-scoped and survive port changes.
- Existing cookie-backed settings: `crit-theme`, `crit-width`, `crit-diff-mode`, `crit-diff-scope`, `crit-toc`, `crit-templates`, `crit-hide-resolved`. Match this pattern for new persisted settings.
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
