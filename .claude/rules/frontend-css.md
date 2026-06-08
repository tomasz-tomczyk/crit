---
paths:
  - "cmd/crit/frontend/style.css"
  - "cmd/crit/frontend/theme.css"
---

# CSS Rules

## Theming (Stylelint catches hardcoded colors; check-css-vars.sh catches undefined vars)
- When adding themed elements, define color values in ALL 4 theme blocks in theme.css: `:root` (dark fallback), `prefers-color-scheme: light`, `[data-theme="dark"]`, `[data-theme="light"]`.

## Responsive & Touch
- Mobile layout rules go inside `@media (max-width: 768px)`. Touch affordances go inside `@media (pointer: coarse)`. Desktop-only hover interactions go inside `@media (pointer: fine)`.
- Hover-only interactive elements (`.line-add`, `.diff-comment-btn`) must be `display: none` by default and `display: flex` inside `@media (pointer: fine)`. This prevents flash-on-tap on touch devices where `.drag-endpoint` briefly applies.
- Touch targets must be ≥ 44×44px under `@media (pointer: coarse)` (WCAG 2.5.5).
- Comment textarea and reply input must be `font-size: 16px` under mobile to suppress iOS Safari focus-zoom.

## Selectors
- After renaming a CSS class or DOM element ID, search for and remove ALL references to the old name in CSS AND JS.
- Check that all CSS selectors referenced in JS (`querySelector`, `classList.contains`) match actual class names in CSS.
