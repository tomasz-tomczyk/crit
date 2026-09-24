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
| Tree / sidebar file jump | `stickToKey(path)` once (= pending scroll target), `scrollToItem`, mount body, one `setItemHeight` + pin. No timed settle loop. |
| While pending | `FileListVirtualizer.update()` re-applies `pinKeyToViewportTop` until device-pixel settle (`roundToDevicePixel`), then `releasePendingScrollTarget`. |
| User cancel | `clearStickToKey` on wheel / touchstart / touchmove / pointerdown / Page/Arrow/Home/End/Space (not every key — j/k must not clear). |
| Height refine | `restoreAfterHeightChange`: file-top pin **only** while `_stickKey` is set; otherwise DOM/line anchor. |
| Comment jump | `ensureFileVisibleForComment` mounts without stick-to-file-top; holds pin + scroll-lock until `scrollToRow` finishes (success or fail), then clears. |
| Overscan | paint window overscroll **200**; keep-alive margin **4000** (`1000×4`). |
| Header estimate | file header **44** (or measured). |
| Paged rebase | only when `maxScroll > 11e6` (`SCROLL_REBASE_*`); dormant otherwise. |

Tests: `web/__tests__/crit-file-list-virtualizer.test.js`, `crit-diff-virtualizer.test.js`, `scroll-to-file-settle-stick.test.js`.

## Key modules

- `web/crit-diff-virtualizer.js` — unified/split row windows, `HeightIndex`, `estimateDiffBodyHeight`, `noteChildHeightChange` → parent file-list restore
- `web/crit-file-list-virtualizer.js` — multi-file list window, stick/settle, paged rebase, scroll anchors
- `web/app.js` — wires `#filesContainer` when `files.length >= FILE_LIST_VIRTUALIZE_MIN_FILES` (currently `1`); `scrollToFile` / comment nav as above
