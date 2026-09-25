# Frontend: the review list on `@pierre/diffs`

Crit's review UI is vanilla JS embedded in the Go binary (no bundler at
runtime). Every diff and every code file is rendered by
[`@pierre/diffs`](https://diffs.com/docs) 1.5.1; Crit owns the product around
it. This doc is the map: who renders what, how Crit's model becomes Pierre
items, and the scroll, focus and styling contracts that hold it together.

## Who owns what

| Concern | Owner |
| --- | --- |
| Review list (git and files mode): virtualization, sticky headers, scroll | Pierre `CodeView` in `#filesContainer` |
| Diff/code rendering: Shiki highlighting, word diffs, split/unified, context expansion | Pierre (tokenization in its worker pool) |
| Story chapter diffs | Pierre `FileDiff` per chapter file group (`renderPierreInlineDiff`) |
| File header (viewed, collapse, Document/Diff toggle, badges, comment button) | Crit, in Pierre's custom header slot (`buildPierreFileHeader`) |
| Comment threads, forms, outdated block, placeholders | Crit elements as Pierre annotations |
| Rendered markdown (Document view) | Crit's line-block document (`renderDocumentView`), hosted as an annotation |
| Fenced code in comments and documents | Shiki via Pierre's worker pool (`crit-code-highlight.js`) |
| Keyboard (j/k, visual range, `c`), comment nav, tree jumps | Crit, calling CodeView's scroll/selection APIs |
| Session, API, comment model, sidebar, settings, live mode | Crit |

There is no second diff engine and no highlight.js.

## Files

| File | Role |
| --- | --- |
| `web/crit-pierre-view.js` | `createPierreView(opts)`: the CodeView wrapper. Items, lazy hydration, annotation element cache, type switches, scroll/selection helpers. Pierre is injected (`opts.pierre`), so it unit-tests against a fake. |
| `web/crit-pierre-adapter.js` | Pure mapping: Crit hunks → git patch → `FileDiffMetadata`, old-file reconstruction, comments/forms → annotations, keyboard rows, themes, language overrides. |
| `web/crit-code-highlight.js` | Shiki for fenced code outside Pierre surfaces (prime/lines/upgrade). |
| `web/app.js` (Pierre section) | Lifecycle (`ensurePierreView`, `renderPierreFiles`, `refreshPierreFile`, `disposePierreView`), annotation builders, keyboard, jumps, selection, quote highlights, line tints, touch. |
| `scripts/build-pierre.mjs` | Builds `web/pierre/` (see Bundle). |
| `scripts/code-themes.mjs` | Crit's code themes (see Themes). |
| `internal/server/precompressed.go` | Serves `web/pierre/*.js.gz` with `Content-Encoding: gzip`. |

## Bundle

`scripts/build-pierre.mjs` (run by `copy-deps.js`) bundles `@pierre/diffs`,
its worker and Shiki with esbuild into split ESM chunks, gzips them
deterministically and writes `web/pierre/`. `index.html` loads the entry as a
module and exposes `window.critPierreReady`; `app.js` awaits it before the
first render. The trim plugin keeps 146 Shiki grammars (`SHIKI_LANGS`),
drops Shiki's bundled themes (Crit registers its two) and stubs the
Oniguruma WASM engine (Crit uses Shiki's JS regex engine). Budgets:
`asset-budget.json` (`pierreDirBytes`, entry and worker caps), provenance in
`ASSETS-PROVENANCE.txt` (directory hash over per-file hashes).

## Items

`crit-pierre-view.js` publishes one CodeView item per file, keyed by path:

| File state | Item |
| --- | --- |
| Loaded diff (git mode, files-mode round diffs) | `diff` item. The old side is rebuilt from the new content and the hunks (`reconstructOldContent`) so Pierre can expand context; if content and hunks disagree it falls back to the patch alone. |
| Files-mode code file | `file` item with the whole file (`buildFileContents`). |
| Markdown in Document view | Empty `file` item whose line-0 annotation is the rendered document (`kind: 'document'`). |
| Deleted/renamed with nothing to show, orphaned | Empty `file` item with a placeholder annotation. |
| Not fetched yet (server-side lazy) | Stub `diff` item (see Loading). |

Parsed diffs and file contents are memoized per path on what they were
built from (content hash, status, hunk shape); the key is also Pierre's
`cacheKey`, so re-publishing a file for a comment, form or collapse costs no
re-parse and no worker re-highlight. The empty-file object is one per path:
Pierre prepares a collapsed item's layout against the file object and throws
if a later render passes another. `updateItem` cannot change an item's type,
so a Document/Diff toggle goes through `setItems` (same index, new record),
restoring the reader's position if they were on that file.

## Annotations

Annotation metadata is `{ kind, id }`; kinds are `thread`, `form`,
`outdated`, `document`, `placeholder`, `loading`. Elements are cached by
`kind:id` and metadata objects are kept stable, so Pierre re-uses the node
across virtualization: an open form keeps its caret and typed text while its
file scrolls away (`formTextarea` reads unmounted forms from the cache).

- `refreshFile(file, keys)` rebuilds only the named keys (thread cards are
  cheap; forms are not rebuilt).
- A full `setFiles` rebuilds everything except forms and documents.
- Documents are re-rendered **in place** (`refreshPierreDocument`). Swapping
  the element makes Pierre drop it before measuring the replacement; the item
  collapses for a frame and the list scroll clamps to the top. A document
  that is not mounted is only marked stale and re-renders when it mounts.

## Loading

The server marks files beyond the first 25 lazy (numstat only), and the
client caps up-front loads at 25 regardless (`EAGER_FILE_LIMIT`): after
background loading warms the server every file reports `lazy: false`, and
loading them all at once overflowed Chrome's request limit. Lazy files are
stub items: a patch of blank rows sized from numstat, so the scroll height is
close to final and deep jumps land. Pierre has no unloaded-item concept (its
`loadDiffFiles` hydrates contents for a patch it already has), so the stub
is the placeholder; its host carries `data-crit-stub`, the rows are hidden
and a "Loading diff…" annotation shows. Stubs hydrate when rendered
(concurrency 4) or before any jump into them (`ensureLoaded`).

## Scroll and jump contracts

- The list scrolls `#filesContainer`, not the window. `reviewScroller()` /
  `scrollReviewToElement()` pick the right scroller (TOC, heading anchors).
- File jumps (tree, mobile picker, comment nav): `scrollToFile` loads the
  file if needed, then `CodeView.scrollTo({ type: 'item' })`. Line jumps use
  `scrollTo({ type: 'line' })`; an empty (document) item redirects to the
  file. The huge e2e project checks that a jump to the deepest file lands and
  holds within 2px while neighbours hydrate, then scrolls freely.
- Comment jumps center the line (`pierreJumpToComment`); in a rendered
  document the card itself is scrolled into view.
- New forms are revealed only if off screen: a mounted form scrolls itself
  into view; an unmounted one asks Pierre to bring its line (or file) in.
- Pierre's default pause of pointer events for ~120ms after a scroll is off
  (`pointerEventsOnScroll: true`; no measured scroll cost), so clicks right
  after a jump land. Story chapter `FileDiff`s are not virtualized and never
  paused.

## Keyboard

`pierreFocus` is the model: `{ path, line, side }`, plus `block`/`endLine`
in a rendered document. Rows per file: diff rows (`navRowsForHunks`; split
pairs a deletion with its addition), every line of a code file, or the
document's blocks. Diff focus is shown with Pierre's line selection, which
also pins Pierre's hover "+", so the selection hides as soon as the mouse is
over code (the model stays). Document focus uses the classic `.focused` /
`.selected` classes. Opening a form records its line as the focus, so j/k
continue from it after the form closes.

## Styling contracts

- **Themes.** `crit-dark` (Tokyo Night) and `crit-light` (GitHub Light
  Default), generated by `scripts/code-themes.mjs`: every token colour that
  falls under WCAG AA on any background Pierre paints code on (plain,
  added, deleted, word-level tints; measured for Pierre 1.5.1) is moved
  toward white/black until it passes. Crit's theme pill maps to Pierre's
  `themeType` (`themeTypeFor`).
- **Fenced code.** Token spans carry `--diffs-token-light/dark`, the same
  contract as Pierre; `theme.css` picks one for `.crit-code`.
- **Into the shadow roots.** Static rules go through Pierre's `unsafeCSS`
  option (`PIERRE_UNSAFE_CSS`: quote highlight, readable line numbers, stub
  rows, touch affordances). Comment-range and open-form tints change with
  the comment model, so they live in one constructable stylesheet adopted by
  every Pierre shadow root; changing `unsafeCSS` instead re-renders every
  item. Rules key on the host's `data-crit-path` (set on every render).
- **Quote highlights** use the CSS Custom Highlight API (`::highlight(crit-quote)`).
- **Select-to-comment** reads `Selection.getComposedRanges({ shadowRoots })`
  (Chromium reports a shadow selection as collapsed on the Selection itself).
- **Tabs** are 8 columns (`--diffs-tab-size`), as before.

## Language coverage

146 grammars ship. Pierre detects a file's language from its name;
`LANGUAGE_OVERRIDES` maps `.heex`/`.leex` → HTML, `.svg` → XML, `.gradle` →
Groovy (Pierre left those as plain text). Fence names written for
highlight.js resolve through `FENCE_ALIASES` (`dos` → bat, `arduino` → C++,
`delphi` → Pascal, `mathematica` → Wolfram, `1c` → BSL, …).

Compared with the previous highlight.js bundle (193 languages + HEEx, Vue,
Astro), 115 keep highlighting. These 81 have no Shiki grammar and render as
plain text: abnf, accesslog, angelscript, arcade, aspectj, autohotkey,
autoit, axapta, basic, bnf, brainfuck, cal, capnproto, ceylon, clean, cos,
crmsh, csp, dns, dsconfig, dts, dust, ebnf, excel, fix, flix, freedesktop,
gams, gauss, gcode, gml, golo, hsp, inform7, irpf90, isbl, jboss-cli, lasso,
ldif, leaf, livecodeserver, livescript, lsl, maxima, mel, mercury, mizar,
mojolicious, monkey, moonscript, n1ql, nestedtext, oxygene, parser3, pf,
pony, processing, profile, purebasic, q, reasonml, rib, roboconf, routeros,
rsl, ruleslanguage, scilab, smali, sml, sqf, stan, step21, subunit,
taggerscript, tap, thrift, tp, wren, xl, xquery, zephir.

Shiki has grammars Crit could add (about 110 more); each costs binary size
(`pierreDirBytes`). Add to `SHIKI_LANGS` and re-run `npm run update-deps`.
