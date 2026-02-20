# Expand/Collapse Sections

## Goal

For long plans (100+ lines), let users collapse markdown sections to their headings for quick navigation and focused review. Similar to how GitHub collapses file diffs.

## Design

**Collapse model:** Each heading defines a "section" that spans from that heading to the next heading of equal or higher level (or end of document). Collapsing a section hides all blocks in that range except the heading itself. This mirrors standard document outlining.

**Building the section tree:** After `buildLineBlocks()`, compute a `sections` map from `headingBlockIndex` to section info:

```js
// sections[headingBlockIndex] = { startLine, endLine, level, collapsed }
```

Walk `tocItems` to determine each section's range. A section ends where the next same-or-higher-level heading begins, or at the document end.

**Toggle interaction:** Add a small chevron icon (`>` / `v`) to the left of each heading's line number in the gutter. Clicking it toggles that section's `collapsed` state. The chevron rotates 90 degrees on collapse (CSS transition).

**Rendering collapsed sections:** In `renderDocument()`, when iterating `lineBlocks`, check if the current block falls within a collapsed section (and is not the heading itself). If so, skip rendering it entirely. After the heading block, render a single "N lines collapsed" summary row that's clickable to expand.

**Collapse all / Expand all:** A single toggle button in the header near the TOC toggle. Shows "Collapse All" when all sections are expanded, "Expand All" when any are collapsed. Only visible when the document has headings.

**Persistence:** Save collapsed section state to `localStorage` as `crit-collapsed-{filename}` — an array of heading start lines that are collapsed. Restore on page load.

**Interaction with comments:** If a section contains comments, show a small badge on the collapsed heading: "(3 comments)". Clicking a comment in the TOC auto-expands its parent section and scrolls to it.

**Interaction with keyboard shortcuts (`j`/`k`):** Skip over collapsed blocks. Navigation only stops on visible blocks.

## CSS

```css
.section-chevron {
  cursor: pointer;
  transition: transform 0.15s ease;
  color: var(--fg-muted);
}
.section-chevron:hover {
  color: var(--fg-primary);
}
.section-chevron.collapsed {
  transform: rotate(-90deg);
}
.collapsed-summary {
  color: var(--fg-muted);
  font-size: 12px;
  padding: 4px 0 4px 48px;
  cursor: pointer;
  user-select: none;
}
.collapsed-summary:hover {
  color: var(--accent);
}
```

## Edge Cases

- **Nested sections:** Collapsing an H2 hides all content up to the next H2, including any H3/H4 subsections within it. Expanding it restores the previous collapse state of child sections (they remember their own state independently).
- **Comments on collapsed lines:** The comment count badge on the heading is the only indicator. Comments are not lost, just hidden.
- **Single heading documents:** No chevrons shown — nothing to collapse.
- **Diff view:** Sections are not collapsible in diff view (too confusing with side-by-side layout). Chevrons only appear in the main document view.

## Scope

Frontend-only change to `index.html`. No backend modifications.
