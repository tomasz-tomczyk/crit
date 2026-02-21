# Contributing a Theme

This guide covers everything needed to add a new color theme to Crit.

## Overview

Themes are defined entirely in `frontend/index.html` as CSS. Each theme needs:

1. A **CSS custom properties block** — UI colors (backgrounds, text, borders, etc.)
2. A **highlight.js block** — syntax highlighting colors for code blocks
3. **Wiring** — a few small additions so the theme appears in the UI and works with mermaid diagrams

## Step 1: CSS Custom Properties

Add a `[data-theme="your-theme"]` block in `frontend/index.html` after the existing theme blocks (look for the `/* ===== Community Themes ===== */` comment). Define these variables (all required except `--accent-fg`):

```css
[data-theme="your-theme"] {
  /* Backgrounds */
  --bg-primary: ...;       /* Main page background */
  --bg-secondary: ...;     /* Secondary surfaces (footer, panels, cards) */
  --bg-tertiary: ...;      /* Tertiary surfaces (theme pill, disabled states) */
  --bg-hover: ...;         /* Hover state backgrounds */
  --bg-gutter: ...;        /* Line number gutter */

  /* Foreground / text */
  --fg-primary: ...;       /* Primary text */
  --fg-secondary: ...;     /* Secondary text (slightly muted) */
  --fg-muted: ...;         /* Muted text (inactive buttons, line numbers) */
  --fg-dimmed: ...;        /* Dimmest text (even less emphasis) */

  /* Accent */
  --accent: ...;           /* Primary accent (links, active states, focus rings, primary buttons) */
  --accent-hover: ...;     /* Accent on hover */
  --accent-fg: ...;        /* Text color ON accent backgrounds (buttons). Optional — defaults to #fff */
  --accent-subtle: ...;    /* Very transparent accent, e.g. rgba(..., 0.1) */
  --accent-bg: ...;        /* Slightly more opaque accent bg, e.g. rgba(..., 0.15) */

  /* Semantic colors */
  --green: ...;            /* Success, additions in diffs */
  --red: ...;              /* Error, deletions in diffs */
  --orange: ...;           /* Warnings */
  --yellow: ...;           /* Highlights */

  /* Borders */
  --border: ...;           /* General borders */
  --border-comment: ...;   /* Comment card left border (often same as --accent) */

  /* Components */
  --comment-bg: ...;       /* Comment card background */
  --selection-bg: ...;     /* Line selection background, e.g. rgba(..., 0.08) */
  --shadow: ...;           /* Box shadow, e.g. 0 2px 8px rgba(0,0,0,0.3) */
  --code-bg: ...;          /* Code block background */
  --table-stripe: ...;     /* Alternating table row stripe, e.g. rgba(..., 0.04) */
  --blockquote-border: ... /* Blockquote left border */
  --blockquote-bg: ...;    /* Blockquote background */
  --scrollbar-bg: ...;     /* Scrollbar track */
  --scrollbar-thumb: ...;  /* Scrollbar thumb */
}
```

**Tips:**
- `--bg-secondary` should be slightly darker (dark themes) or lighter (light themes) than `--bg-primary`
- `--accent` is used for the primary button background ("Finish Review", comment submit) — pick a color that's readable as a button. If your accent is light (pastel purple, frost blue), set `--accent-fg` to a dark color from your palette (e.g. `--bg-primary`). If omitted, button text defaults to white.
- `--accent-subtle` and `--accent-bg` are typically `rgba()` versions of `--accent` at 8-15% opacity
- `--selection-bg` works best at very low opacity (6-10%) so text stays readable
- `--shadow` should use higher opacity for dark themes (`0.3`) and lower for light themes (`0.08`)
- Look at an existing theme like `monokai` or `solarized-light` as a starting point

## Step 2: Highlight.js Syntax Colors

Add a block after the existing highlight.js themes (look for `/* ===== highlight.js Nord Theme ===== */` as the last one). The block needs these token groups:

```css
/* ===== highlight.js Your Theme ===== */
[data-theme="your-theme"] .hljs{color:____;background:____}

/* Keywords, types, language constructs */
[data-theme="your-theme"] .hljs-doctag,
[data-theme="your-theme"] .hljs-keyword,
[data-theme="your-theme"] .hljs-meta .hljs-keyword,
[data-theme="your-theme"] .hljs-template-tag,
[data-theme="your-theme"] .hljs-template-variable,
[data-theme="your-theme"] .hljs-type,
[data-theme="your-theme"] .hljs-variable.language_{color:____}

/* Function and class names */
[data-theme="your-theme"] .hljs-title,
[data-theme="your-theme"] .hljs-title.class_,
[data-theme="your-theme"] .hljs-title.class_.inherited__,
[data-theme="your-theme"] .hljs-title.function_{color:____}

/* Attributes, numbers, operators, selectors */
[data-theme="your-theme"] .hljs-attr,
[data-theme="your-theme"] .hljs-attribute,
[data-theme="your-theme"] .hljs-literal,
[data-theme="your-theme"] .hljs-meta,
[data-theme="your-theme"] .hljs-number,
[data-theme="your-theme"] .hljs-operator,
[data-theme="your-theme"] .hljs-selector-attr,
[data-theme="your-theme"] .hljs-selector-class,
[data-theme="your-theme"] .hljs-selector-id,
[data-theme="your-theme"] .hljs-variable{color:____}

/* Strings, regex */
[data-theme="your-theme"] .hljs-meta .hljs-string,
[data-theme="your-theme"] .hljs-regexp,
[data-theme="your-theme"] .hljs-string{color:____}

/* Built-ins, symbols */
[data-theme="your-theme"] .hljs-built_in,
[data-theme="your-theme"] .hljs-symbol{color:____}

/* Comments */
[data-theme="your-theme"] .hljs-code,
[data-theme="your-theme"] .hljs-comment,
[data-theme="your-theme"] .hljs-formula{color:____}

/* HTML/XML tags, quotes, pseudo-selectors */
[data-theme="your-theme"] .hljs-name,
[data-theme="your-theme"] .hljs-quote,
[data-theme="your-theme"] .hljs-selector-pseudo,
[data-theme="your-theme"] .hljs-selector-tag{color:____}

/* Substitutions */
[data-theme="your-theme"] .hljs-subst{color:____}

/* Section headings */
[data-theme="your-theme"] .hljs-section{color:____;font-weight:700}

/* List bullets */
[data-theme="your-theme"] .hljs-bullet{color:____}

/* Emphasis and strong */
[data-theme="your-theme"] .hljs-emphasis{color:____;font-style:italic}
[data-theme="your-theme"] .hljs-strong{color:____;font-weight:700}

/* Diff additions/deletions */
[data-theme="your-theme"] .hljs-addition{color:____;background-color:____}
[data-theme="your-theme"] .hljs-deletion{color:____;background-color:____}
```

**Tips:**
- `.hljs` background should match your theme's `--code-bg` or `--bg-primary`
- `.hljs` foreground color should match `--fg-primary`
- Comments should use a muted color (similar to `--fg-muted`)
- Addition/deletion backgrounds should be subtle tinted versions of green/red

## Step 3: Wire It Up

Three places need a small addition:

### 3a. Footer dropdown

Find the `<select id="themeSelect">` element and add an `<option>` inside the `Community` optgroup:

```html
<option value="your-theme">Your Theme</option>
```

The `value` must exactly match the `data-theme` name used in your CSS.

### 3b. Theme pill indicator (CSS)

The header pill needs to know whether your theme is light or dark so the sliding indicator highlights the correct button. Add your theme's selector to the appropriate group:

For **dark** themes, add to the `left: 66.666%` rule:
```css
html[data-theme="your-theme"] .theme-pill-indicator,
```

For **light** themes, add to the `left: 33.333%` rule:
```css
html[data-theme="your-theme"] .theme-pill-indicator,
```

### 3c. Theme pill button highlight (CSS)

Similarly, add your theme to the active-button color rule:

For **dark** themes:
```css
html[data-theme="your-theme"] .theme-pill-btn[data-for-theme="dark"],
```

For **light** themes:
```css
html[data-theme="your-theme"] .theme-pill-btn[data-for-theme="light"],
```

### 3d. Light theme registration (JS) — light themes only

If your theme is **light**, add its name to the `LIGHT_THEMES` array in the JavaScript:

```js
var LIGHT_THEMES = ['light', 'solarized-light', 'your-theme'];
```

Dark themes need no JS change (dark is the default assumption).

## Checklist

- [ ] CSS custom properties block with all 28 variables
- [ ] highlight.js block with all token groups
- [ ] `<option>` added to footer `<select>`
- [ ] Theme pill indicator CSS updated
- [ ] Theme pill button highlight CSS updated
- [ ] `LIGHT_THEMES` array updated (light themes only)
- [ ] Tested: page background, text readability, code syntax colors, comment cards, gutter, scrollbars, mermaid diagrams

## Existing Themes

| Name | Key | Type |
|------|-----|------|
| Default Dark | `dark` | dark |
| Default Light | `light` | light |
| Monokai | `monokai` | dark |
| Dracula | `dracula` | dark |
| Solarized Dark | `solarized-dark` | dark |
| Solarized Light | `solarized-light` | light |
| Nord | `nord` | dark |
