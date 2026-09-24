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
| While pending | `update()` re-pins; releases pending only when already at target *before* that pin (so settle spans a layout frame). Lock + pin padding kept until user scroll. |
| `scrollToItem` lock | Default 2s; never shortens an existing longer lock (stick uses 60s). |
| User cancel | `pointerdown` clears pending stick only. Wheel / touchstart / touchmove / Page/Arrow/Home/End/Space call `clearStickToKey` (pending + lock + padding). |
| Height refine | File-top pin only while `_stickKey` is set; otherwise DOM/line anchor. |
| Comment jump | `scrollToItem(..., 'nearest')` (no file-top pin); pin + scroll-lock until `scrollToRow` finishes, then clear. |
| Overscan | paint window overscroll **200**; keep-alive margin **4000** (`1000×4`). |
| Header estimate | file header **44** (or measured). |
| Paged rebase | only when `maxScroll > 11e6` (`SCROLL_REBASE_*`); dormant otherwise. |

Tests: `web/__tests__/crit-file-list-virtualizer.test.js`, `crit-diff-virtualizer.test.js`, `scroll-to-file-settle-stick.test.js`.

## Key modules

- `web/crit-diff-virtualizer.js` — unified/split row windows, `HeightIndex`, `estimateDiffBodyHeight`, `noteChildHeightChange` → parent file-list restore
- `web/crit-file-list-virtualizer.js` — multi-file list window, stick/settle, paged rebase, scroll anchors
- `web/app.js` — wires `#filesContainer` when `files.length >= FILE_LIST_VIRTUALIZE_MIN_FILES` (currently `1`); `scrollToFile` / comment nav as above
