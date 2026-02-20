# Keyboard Shortcuts

## Goal

Let power users navigate and interact with the review entirely from the keyboard, without needing to click gutters or buttons.

## Bindings

| Key | Action | Context |
|-----|--------|---------|
| `j` | Move focus to next block | No textarea focused |
| `k` | Move focus to previous block | No textarea focused |
| `c` | Open comment form on focused block | No textarea focused, block focused |
| `e` | Edit focused comment | No textarea focused, comment focused |
| `d` | Delete focused comment (with confirmation) | No textarea focused, comment focused |
| `Escape` | Cancel comment form (already works) / clear selection / clear focus | Always |
| `Ctrl+Enter` / `Cmd+Enter` | Submit comment | Textarea focused (already works) |
| `?` | Toggle keyboard shortcut help overlay | No textarea focused |
| `f` | Finish review | No textarea focused |
| `t` | Toggle TOC panel | No textarea focused |

## Design

**Focus tracking:** Add a `focusedBlockIndex` state variable (integer or null). When set, the focused block gets a `focused` CSS class — a subtle left-border highlight (similar to the `has-comment` indicator but using `--accent`). Comments within a focused block also get a `focused` class.

**Guard against text input:** All single-key shortcuts (`j`, `k`, `c`, `e`, `d`, `f`, `t`, `?`) are ignored when the active element is a `textarea`, `input`, or `[contenteditable]`. This prevents shortcuts from firing while typing a comment.

**Navigation (`j`/`k`):** Steps through `lineBlocks` array. On `j`, increment `focusedBlockIndex` (wrapping or clamping at end). On `k`, decrement. After updating, scroll the focused block into view using `scrollIntoView({ block: 'nearest', behavior: 'smooth' })`. If a focused block has comments, the focus visually encompasses the comment cards too.

**Comment on focused block (`c`):** Equivalent to clicking the gutter `+` on that block — sets `activeForm` with `afterBlockIndex` = `focusedBlockIndex`, the block's `startLine`/`endLine`, and renders the form.

**Edit/delete (`e`/`d`):** Only active when `focusedBlockIndex` points to a block that has comments. `e` opens inline edit for the first (or only) comment on that block. `d` shows a brief confirmation (a small inline "Delete? y/n" prompt, not a browser `confirm()` dialog) before deleting.

**Help overlay (`?`):** A simple modal listing all shortcuts. Styled consistently with the existing waiting-overlay. Dismissed by pressing `?` again or `Escape`.

**Finish (`f`):** Triggers the same action as clicking "Finish Review". Guarded behind a `confirm()` if there are unsaved drafts.

## CSS

```css
.line-block.focused {
  border-left: 2px solid var(--accent);
}
.line-block.focused .line-gutter {
  background: var(--accent-subtle);
}
```

## Discoverability

Add a small `?` button in the header (next to the TOC hamburger) that opens the help overlay. Tooltip: "Keyboard shortcuts (?)".

## Scope

Frontend-only change to `index.html`. No backend modifications.
