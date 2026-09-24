# Frontend JS (crit CLI)

Vanilla JS under `web/`, embedded via Go `embed.FS`. No bundler.

## Diff virtualization spikes

Two approaches are being compared for large multi-file reviews:

| Branch | Shape |
| --- | --- |
| `spike/large-diff-virtualize` (A+C) | Keep every `.file-section` in the DOM; defer heavy `.file-body` until near the viewport; reserve **estimated min-height** on deferred bodies; scroll-anchor when mounts refine height. |
| `spike/large-diff-file-list-virtualize` (B, this branch) | **File-list virtualization** (`web/crit-file-list-virtualizer.js`): one HeightIndex over files; off-screen files are spacers; near-viewport slots mount a real section whose body still uses row virtualization (`web/crit-diff-virtualizer.js`). Scroll anchoring via `getScrollAnchor` / `restoreAnchor` on height refine. |

Row virtualization (in-file) is shared. Crit-web parity is out of scope for these spikes.

## Key modules

- `web/crit-diff-virtualizer.js` — unified/split row windows, `HeightIndex`, `estimateDiffBodyHeight`
- `web/crit-file-list-virtualizer.js` — multi-file list window + scroll fix
- `web/app.js` — wires `#filesContainer` to the file-list virtualizer when
  `files.length >= 40` (flat review); smaller reviews keep deferred bodies.
  Story mode still owns `#storyPane`.
