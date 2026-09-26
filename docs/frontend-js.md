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
| `web/crit-pierre-adapter.js` | Pure mapping: Crit hunks → git patch → `FileDiffMetadata`, old-file reconstruction, comments/forms → annotations, keyboard rows, shared display settings, themes, language overrides. |
| `web/crit-pierre-dom.js` | Shadow-root decorations, source-line and selection mapping, quote highlights and accessibility labels. |
| `web/crit-pierre-runtime.js` | Lazy file loading, request invalidation and worker-pool lifecycle. |
| `web/crit-palette.css`, `web/pierre/palettes.js` | Semantic token mapping and generated palette foundations for every bundled theme. |
| `web/crit-code-highlight.js` | Shiki for fenced code outside Pierre surfaces (prime/lines/upgrade). |
| `web/app.js` (Pierre section) | Lifecycle (`ensurePierreView`, `renderPierreFiles`, `refreshPierreFile`, `disposePierreView`), annotation builders, keyboard, jumps, selection, quote highlights, line tints, touch. |
| `scripts/build-pierre.mjs` | Builds `web/pierre/` (see Bundle). |
| `scripts/crit-theme-palette.mjs` | Theme → Crit UI palette (surfaces, accent, border, status colours). Borders are derived from the editor background, not the theme's `panel.border`. `web/__tests__/crit-theme-palette.test.js` checks every bundled theme. |
| `web/crit-theme-boost.js` | Shared colour maths (also used by the palette build) and the opt-in "Syntax contrast: Increased" setting: registers a copy of the selected theme with Pierre (`registerCustomTheme`) where token colours under 4.6:1 on code backgrounds move in lightness only. Off by default; themes are shown as designed. |
| `web/themes.html`, `web/crit-theme-preview.js` | `/themes`: fixed samples (diff with a comment, file-level thread and form, rendered Markdown) for flipping through themes and saving one. Linked from Settings. |
| `internal/server/precompressed.go` | Serves `web/pierre/*.js.gz` with `Content-Encoding: gzip`. |

## Bundle

`scripts/build-pierre.mjs` (run by `copy-deps.js`) bundles `@pierre/diffs`,
its worker and Shiki with esbuild into split ESM chunks, gzips them
deterministically and writes `web/pierre/`. `index.html` loads the entry as a
module and exposes `window.critPierreReady`; `app.js` awaits it before the
first render. A fine-grained Shiki bundle uses documented factories, keeps
146 grammars (`SHIKI_LANGS`) and all themes, and uses the JavaScript regex
engine. No generated dependency source is rewritten. Budgets:
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

Parsed diffs and file contents are memoized per path against their exact
inputs, including content and hunk text. Each parsed version receives a unique
Pierre `cacheKey`, so a new review round cannot reuse old base text, while
re-publishing a file for a comment, form or collapse costs no re-parse or worker
re-highlight. The empty-file object is one per path:
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

- **Themes.** Settings chooses a light and dark palette from all bundled Shiki
  themes, filtered by their declared mode. Defaults are Tokyo Night / GitHub
  Light Default. The System/Light/Dark pill picks which half is active. Code,
  rendered markdown, diagrams, comments and chrome share the selected palette.
  Shiki syntax tokens are unchanged by default. The opt-in Syntax contrast
  setting raises token contrast on code and diff backgrounds by adjusting
  lightness in a registered theme copy; upstream themes remain available.
- **Palette boundary.** `scripts/crit-theme-palette.mjs` derives small Crit
  foundations from public Shiki theme data at vendor-build time. Ordinary card
  surfaces are derived from the editor background/foreground, not unrelated
  VS Code sidebar/input colours (which may invert the palette). Short and alpha
  hex colours are expanded/composited. Only Crit UI foreground
  roles are adjusted for contrast against card and hover surfaces (accent roles
  also check their tinted backgrounds). Syntax contrast adjustments are separate
  and opt-in.
  `web/crit-theme-palette.js` resolves the two halves and applies foundation values to html.
  `web/crit-palette.css` maps these onto existing `--crit-*` semantic tokens.
  Components keep their existing layout classes; there are no per-theme CSS
  copies. Live mode uses the same palette foundations and light/dark selection. Future bundled
  Shiki themes appear automatically; custom adaptations belong in the foundation
  mapping, not component selectors or Pierre's internal DOM.
- **Display options.** `overflow` selects scrolling or wrapping; `lineDiffType`
  controls inline highlighting; `diffIndicators` selects markers/bars/none;
  `expandUnchanged` selects collapsed or full context. Separators always use
  expandable `line-info`. `crit-pierre-adapter.js` validates these settings for
  the review list, story diffs and theme preview. All use Pierre's public options. The documented
  `--diffs-bg-separator-override` blends 5% foreground into the code background
  to soften the separator without modifying its markup or expansion controls.
- **Fenced code.** Token spans carry `--diffs-token-light/dark`, the same
  contract as Pierre; `theme.css` picks one for `.crit-code`.
- **Into the shadow roots.** Static rules go through Pierre's `unsafeCSS`
  option (`PIERRE_UNSAFE_CSS`: quote highlight, stub
  rows, touch affordances). Comment-range and open-form tints change with
  the comment model, so they live in one constructable stylesheet adopted by
  every Pierre shadow root; changing `unsafeCSS` instead re-renders every
  item. Rules key on the host's `data-crit-path` (set on every render).
- **Quote highlights** use the CSS Custom Highlight API (`::highlight(crit-quote)`).
- **Select-to-comment** reads `Selection.getComposedRanges({ shadowRoots })`
  (Chromium reports a shadow selection as collapsed on the Selection itself).
- **Tabs** are 8 columns (`--diffs-tab-size`), as before.

## Integration boundaries and upgrade risks

Prefer Pierre's public rendering options and callbacks over querying its DOM.
Themes, wrapping, hunk separators, range selection and touch commenting use
those APIs. Do not reproduce Crit's old multi-row gutter bracket by measuring
Pierre rows: the public gutter utility currently attaches to one endpoint.
Rendered markdown still uses Crit's document renderer, but also shows one
endpoint utility with range highlighting, without the old connecting bracket.

The following adaptations remain intentionally; removing them without an
upstream replacement would remove functionality:

- Lazy stubs reserve list space before patches arrive. A public unresolved-item
  size estimate would replace synthetic diffs; eagerly fetching all patches
  instead would sacrifice large-review performance.
- Rendered markdown lives in a supported annotation on an empty file item.
  An arbitrary-content CodeView item would remove the empty-file adaptation
  while preserving one mixed list and its navigation.
- Comment-range tints and quote highlights style shadow-root code. A public
  decoration API would replace the selectors; removing these styles now loses
  contextual highlighting. Accessibility labels also inspect built-in controls.
- Markdown fences use public worker cache methods and their typed HAST results.
  This shares one highlighter, but couples the adapter to that result structure.
  A dedicated public snippet-highlighting method would simplify it.

`unsafeCSS` is Pierre's CSS escape hatch inside shadow roots, not a statement
that the CSS executes code. Rules targeting internal attributes are an upgrade
compatibility risk, as is the adopted range stylesheet. Keep them localized and
exercise commenting, lazy loading, context expansion and touch tests on upgrades.
No dependency source is patched. Syntax contrast is an opt-in theme copy
registered through Pierre’s public API; selectable upstream themes alone do not
guarantee AA contrast.

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
