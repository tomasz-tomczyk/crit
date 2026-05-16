# Frontend Architecture

## Two-Paradigm Page Fork

`index.html` serves both modes from a single HTML shell. A script block at load time checks `window.location.pathname`:
- `/design` → design mode (iframe-based pin review)
- Everything else → code-review mode (file tree + diff/document views)

Each mode dynamically loads its own script set. They share: theme pill, settings overlay, and extracted modules.

## Module Pattern

All custom JS uses the IIFE + dual-export pattern:

```javascript
(function () {
  'use strict';
  // ... implementation ...
  var api = { publicFn1, publicFn2 };
  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.<namespace> = api;
  }
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
```

- Runtime: accessed via `window.crit.<namespace>`
- Tests: required via `module.exports` (Node.js `--test`)
- Never use ES modules (`import`/`export`) — no build step exists

## Shared Modules (used by both modes)

| Module | Namespace | Purpose |
|--------|-----------|---------|
| `crit-shared.js` | `window.crit.shared` | Cookie helpers, theme, tip rotation, image upload |
| `crit-renderer.js` | `window.crit.renderer` | ContentRenderer registry (register/deregister/current) |
| `crit-sse.js` | `window.crit.sse` | SSE client factory (createSSE) |
| `crit-draft.js` | `window.crit.draft` | Autosave drafts to localStorage |
| `crit-comment-templates.js` | `window.crit.commentTemplates` | Template bar + saved-snippet CRUD |
| `crit-comment-form.js` | `window.crit.commentForm` | Shared comment form creation |
| `crit-comment-card.js` | `window.crit.commentCard` | Comment card rendering + reply threading |
| `crit-comment-card-helpers.js` | `window.crit.commentCardHelpers` | Author colors, timestamps, markdown rendering |
| `crit-settings-overlay.js` | `window.crit.settingsOverlay` | Settings dialog lifecycle |
| `crit-settings-panes.js` | `window.crit.settingsPanes` | Settings tab content |

## Code-Review Modules (used only by code-review mode)

| Module | Namespace | Purpose |
|--------|-----------|---------|
| `crit-icons.js` | `window.crit.icons` | SVG icon constants (ICON_CHEVRON, ICON_EDIT, etc.) |
| `crit-line-blocks.js` | `window.crit.lineBlocks` | buildLineBlocks, splitHighlightedCode, buildCodeLineBlocks |
| `crit-diff-renderer.js` | `window.crit.diffRenderer` | Word-level diff computation (lineSimilarity, wordDiff, etc.) |

## ContentRenderer Interface

Modes register a renderer that the shared chrome (comment cards, settings) can call without knowing the active mode:

```javascript
window.crit.renderer.register({
  scrollToAnchor(anchor),     // scroll viewport to a comment's target
  highlightAnchor(anchor),    // visually highlight the target
  clearHighlight(),           // remove highlight
  onAnnotationIntent(cb),     // subscribe to "user wants to comment here"
  getMode(),                  // "code-review" | "design"
  getAnchorType(),            // "line" | "dom"
});
```

Code-review registers its renderer in `app.js`. Design-mode registers in `design-mode.js`.

## Script Loading

No bundler. Scripts are loaded dynamically with `async=false` (preserves execution order while loading in parallel). A Promise-based boot gate waits for all dependencies before loading the mode's main entry point:

1. Early scripts (shared helpers) load first
2. `designDeps` array lists all sub-modules
3. `Promise.all(bootGate)` waits for all load events
4. Only then loads `design-mode.js` (or `app.js` for code-review)

When adding a new shared module:
- Add to `designDeps` array in `index.html` if design-mode needs it
- Add to the code-review script chain if code-review needs it
- Both modes must load shared modules BEFORE their main entry point

## Design-Mode Sub-Modules

Design-mode splits into focused files under `window.crit.design.<name>`:

| File | Namespace | Concern |
|------|-----------|---------|
| `design-mode.dispatch.js` | `.design.dispatch` | Message dispatch table |
| `design-mode.toggle.js` | `.design.toggle` | Pin/Browse mode toggle |
| `design-mode.composer.js` | `.design.composer` | Comment composition UI |
| `design-mode.panel.js` | `.design.panel` | Side panel lifecycle |
| `design-mode.panel-render.js` | `.design.panelRender` | Panel card rendering |
| `design-mode.sse.js` | `.design.sse` | Design-mode SSE handlers |
| `design-mode.size.js` | `.design.size` | Panel resize logic |
| `design-mode.queue.js` | `.design.queue` | Batched pin push queue |
| `design-mode.origin.js` | `.design.origin` | Origin/proxy URL resolution |
| `design-mode.row.js` | `.design.row` | Per-route section rendering |

## Adding a New Module

1. Create the IIFE file with the dual-export pattern
2. Add it to `designDeps` or code-review script chain in `index.html`
3. Create a matching `frontend/__tests__/<name>.test.js` using Node's `--test`
4. Add the test file to `Makefile` `e2e-design-utils` target (if design-mode)
5. Document dependencies in a header comment (which `window.crit.*` namespaces it reads)
