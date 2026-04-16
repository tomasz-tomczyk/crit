---
paths:
  - "frontend/style.css"
  - "frontend/theme.css"
---

# CSS Rules

## Theming (Stylelint catches hardcoded colors; check-css-vars.sh catches undefined vars)
- When adding themed elements, define color values in ALL 4 theme blocks in theme.css: `:root` (dark fallback), `prefers-color-scheme: light`, `[data-theme="dark"]`, `[data-theme="light"]`.

## Selectors
- After renaming a CSS class or DOM element ID, search for and remove ALL references to the old name in CSS AND JS.
- Check that all CSS selectors referenced in JS (`querySelector`, `classList.contains`) match actual class names in CSS.
