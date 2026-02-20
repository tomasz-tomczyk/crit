# Unified Diff View

**Goal:** Add a unified (single-column) diff view as an alternative to the current side-by-side split view.

**Context:** The split view works well on wide screens but is cramped on narrower ones. A unified view shows removed blocks (red) then added blocks (green) inline in a single column, like GitHub's unified diff mode.

## Scope

Frontend only — no backend changes needed. All required data (`/api/diff` entries, `/api/previous-round` content) already exists.

## Design

Add a small "Split / Unified" toggle next to the "Toggle Diff" button (only visible when diff is active). Default to split on wide screens, unified on narrow.

### Unified layout

Single full-width column. Walk both old and new block lists together using the existing diff entries:
- **Unchanged blocks** — render once, no highlight
- **Removed blocks** — render from previous content, red background + strikethrough (same styles as split view left side)
- **Added blocks** — render from current content, green background
- **Resolved comment cards** — appear after added blocks at their `resolution_lines`, same as split view right side

### Interleaving algorithm

```
oldIdx = 0, newIdx = 0
while oldIdx < oldBlocks.length || newIdx < newBlocks.length:
  oldBlock = oldBlocks[oldIdx]
  newBlock = newBlocks[newIdx]

  if oldBlock is "removed":
    emit oldBlock with diff-removed class
    oldIdx++
  else if newBlock is "added":
    emit newBlock with diff-added class
    newIdx++
  else:
    // both unchanged — emit one copy, advance both
    emit newBlock (unchanged)
    oldIdx++
    newIdx++
```

The classification uses the existing `classifyBlock()` function which maps line ranges to diff entries.

### Reuse

- `buildLineBlocks()` — already parses markdown into commentable blocks
- `classifyBlock()` — already tags blocks as added/removed/unchanged
- `renderDiffSide()` — can be adapted or its block-rendering loop extracted into a shared helper
- `buildResolvedMap()` / `createResolvedElement()` — resolved comment rendering unchanged

## Tasks

1. Extract block rendering from `renderDiffSide()` into a shared helper (renders a single block with gutter + content + optional diff class + optional resolved card)
2. Add `renderUnifiedDiffView()` — implements the interleaving algorithm above, renders into `#diffView` as a single column
3. Add CSS for unified layout (`.diff-view-unified` — single column, full width)
4. Add Split/Unified toggle UI — small pill or segmented control, only visible when diff is active
5. Persist preference to `localStorage` (key: `crit-diff-mode`, values: `split` | `unified`)
6. Auto-select unified on screens < 1200px wide, split on wider (respects saved preference)
7. Update mobile CSS — unified view should be visible on mobile (split is currently hidden)
