# Frontend JS (crit CLI)

Vanilla JS under `web/`, embedded via Go `embed.FS`. No bundler.

## Diff virtualization

Flat multi-file reviews use **dual virtualization**:

1. **File-list** (`web/crit-file-list-virtualizer.js`) — one `HeightIndex` over files; off-screen files are spacers; near-viewport slots mount a real `.file-section`.
2. **Row** (`web/crit-diff-virtualizer.js`) — each mounted file body windows unified/split hunks.

Story mode still owns `#storyPane` (row virt only; no file-list virt).

### Scroll / jump contract

Constants and settle semantics follow `@pierre/diffs` CodeView; Crit keeps the spacer dual-virt shape:

| Concern | Behavior |
| --- | --- |
| Tree / sidebar file jump | `stickToKey(path)` once, `scrollToItem(..., 'start')`, mount body, one height/pin pass. |
| While pending | `update()` re-pins; releases pending when already at target *before* that pin (so settle spans a layout frame), or when the pin can make no progress (clamped at a scroll edge, or a sub-pixel offset the scroller cannot represent). Lock + pin padding kept until user scroll. |
| `scrollToItem` lock | Default 2s; never shortens an existing longer lock (stick uses 60s). |
| User cancel | `pointerdown` and non-scroll keys (j/k, n/N…) clear the pending stick only. Wheel / touchstart / touchmove / Page/Arrow/Home/End/Space call `clearStickToKey` (pending + lock + padding). Keys typed into inputs/textareas/contenteditable are ignored. |
| Height refine | File-top pin only while `_stickKey` is set; otherwise DOM/line anchor. |
| Comment jump | Drops any pending tree-jump stick, then `scrollToItem(..., 'nearest')` (no file-top pin); pin + scroll-lock until `scrollToRow` finishes, then clear. |
| Mount ownership | `pinnedKeys` are caller-owned (open line forms, comment jump) and must be unpinned by that caller. Jump targets are kept mounted by the pending stick / scroll lock, and `ensureMounted(key, ms)` is a timed hold (default 5s) — neither adds a pin. |
| Rebuild | `setItems` detaches every child through `detachNode` (unobserve + `onUnmount`), same as window exits — detached nodes must never feed the HeightIndex. |
| Overscan | paint window overscroll **200**; keep-alive margin **4000** (`1000×4`). |
| Header estimate | file header **44** (or measured). |
| Paged rebase | only when `maxScroll > 11e6` (`SCROLL_REBASE_*`); dormant otherwise. |
| Small surfaces | `calculateWindow` keeps every row when viewport + 2×overscan covers the whole surface. Pierre's `createWindowFromScrollPosition` starts at `scrollTop` in that case, which is only safe when `scrollTop` is the scroller's own position — per-file surfaces use a local offset inside a taller page. |
| Row re-render | `VirtualWindow.captureAnchor` records the anchor row's on-screen top; `restoreAnchor` corrects by that DOM delta after index math (a fresh controller's heights are estimates). |

Tests: `web/__tests__/crit-file-list-virtualizer.test.js`, `crit-diff-virtualizer.test.js`, `scroll-to-file-settle-stick.test.js`.

## Key modules

- `web/crit-diff-virtualizer.js` — unified/split row windows, `HeightIndex`, `estimateDiffBodyHeight`, `noteChildHeightChange` → parent file-list restore
- `web/crit-file-list-virtualizer.js` — multi-file list window, stick/settle, paged rebase, scroll anchors
- `web/app.js` — wires `#filesContainer` for flat reviews whenever the module is present (not gated on file count); `scrollToFile` / comment nav as above. E2e mounts off-window files via tree click (`fileSection` / `mdSection` helpers) rather than asserting on a fully populated DOM.
