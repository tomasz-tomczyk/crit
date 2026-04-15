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

## Error Handling
- Always check `response.ok` after `fetch()` calls. Throw on unexpected statuses.
- Every async operation must have error recovery that restores interactivity (re-attach listeners, undo optimistic UI).
- Never call `.remove()` directly on elements with CSS exit animations. Use class toggle + animationend listener.

## Accessibility (axe-core in E2E catches missing aria-labels, dialog roles, tab roles)
- Never call `.blur()` on interactive elements — it breaks keyboard navigation. axe-core catches the effect (no focus indicator) but not the cause.

## SSE Events
- When an SSE event signals a specific data change (e.g., `comments-changed`), only re-fetch that data — not everything.
