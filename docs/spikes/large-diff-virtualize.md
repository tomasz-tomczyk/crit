# Approach B — Virtualize crit’s own renderer

> **Living state document — SOURCE OF TRUTH.**
> Agents (and humans) pick up unfinished work **from this file**. Whenever you
> learn something, finish a step, hit a blocker, or change a decision — **update
> this file in the same turn**. Chat memory is not durable across token limits
> or agent switches.

## Goal (implementation)

**Implement** row virtualization for crit’s **own** code-diff renderer in
**this worktree** (`web/`). Keep light-DOM, CSS, and comment model. Mount only
viewport ± overscan rows for large code diffs. Markdown virtualization is
out of scope for the first slice unless this file says otherwise.

This is **product code in the worktree**, not a `spikes/` demo.

## Non-goals

- Adopting `@pierre/diffs` in this stream
- Design-only updates without shipping windowing in `web/`
- Depending on PR #954 (orthogonal)

## Worktree

- Path: `crit.spike-large-diff-virtualize`
- Branch: `spike/large-diff-virtualize`
- Hot paths: `web/app.js`, `web/crit-diff-renderer.js`, `web/crit-line-blocks.js`
- crit-web parity later — note if deferred

## Implementation status

- [x] Living doc + design notes (windowing / comment lifecycle) recorded below
- [x] Fair baseline recorded; bench at `bench/large-diff/`
- [x] `buildDiffRows` (or equivalent) logical row model for unified code diffs
- [x] Height index + spacer + overscan window controller
- [x] Wire into mount path for large diffs (keep small diffs on eager path OK)
- [x] Comments / forms as logical rows with remount-safe state
- [x] Selection / drag / keyboard / scroll-restore implemented for virtualized rows
- [x] Split mode explicitly deferred (remains eager)
- [x] Tests for windowing helpers (`web/__tests__/crit-diff-virtualizer.test.js` 8/8); browser E2E interaction still open
- [x] Re-run fair benchmark; fill Post-implementation results
- [x] crit-web port explicitly deferred until Crit browser validation

## Handoff rule

If you run out of tokens or stop mid-task: leave **Current work** and
**Next concrete step** updated below so the next agent continues without
re-deriving context.

### Current work

Unified large-diff virtualization is implemented and committed (`1aed8a6`).
Fair post-impl bench is recorded below: unified first paint ~21 ms (was ~268),
DOM nodes ~1.7k (was ~69k), rendered rows 99 (was 3997). Split remains eager
(unchanged). Focused unit/frontend checks pass. Browser interaction validation
(gutter drag, selection, j/k, comment nav, draft focus) is still open.

### Next concrete step

1. Focused browser/E2E interaction pass on a large unified diff (scrollbar
   jumps, gutter drag, native selection, j/k, comment navigation, draft editor
   focus/cursor).
2. Fix any browser-only regressions. Then decide whether to extend the same row
   model to split mode and port the settled implementation to crit-web.


## Fair benchmark (shared protocol)

**Source of truth for comparison.** Do not invent alternate harnesses.

- Harness in this worktree: `bench/large-diff/` (same script as measure stream).
- Run: `cd test/e2e && npm ci && npx playwright install chromium; cd ../.. && mise exec -- node bench/large-diff/measure.mjs`
- Fixture: 10k-line TS file / side, every 20th line changed (500 hunks), split + unified, 1440×900 Chromium, 180-frame scroll.
- Compare against the **baseline** below (crit with no Pierre/virtualize changes). When this implementation is done, re-run the **same** command in this worktree and paste results here under “Post-implementation results”.
- Fairness rule: measure the **production Crit UI** in this worktree (whatever renderer this branch ships). Do not compare against the old standalone `@pierre/diffs` Vite demo.

### Baseline (crit, no renderer changes) — recorded 2026-09-24

- Revision: `ddbcb87`
- Chromium: `147.0.7727.15`
- Machine: `{'platform': 'darwin', 'arch': 'arm64', 'cpu': 'Apple M4 Max'}`
- Discovery (small disposable repo): first clickable row ~1219 ms

| Layout | file_body_first_paint_ms | mount_task_ms | rendered_rows | mounted_diff_dom_nodes | scroll_p95_frame_ms | frames_over_32 |
|--------|-------------------------:|--------------:|--------------:|-----------------------:|--------------------:|---------------:|
| split | 453.6 | 390.3 | 3497 | 99420 | 24.3 | 0 |
| unified | 267.9 | 232.4 | 3997 | 68953 | 26.2 | 2 |

### Post-implementation results — recorded 2026-09-24

- Revision: `1aed8a6`
- Chromium: `147.0.7727.15`
- Machine: darwin arm64 Apple M4 Max
- Command: `mise exec -- node bench/large-diff/measure.mjs`

| Layout | file_body_first_paint_ms | mount_task_ms | rendered_rows | mounted_diff_dom_nodes | scroll_p95_frame_ms | frames_over_32 |
|--------|-------------------------:|--------------:|--------------:|-----------------------:|--------------------:|---------------:|
| split | 439.8 | 379.9 | 3497 | 99420 | 29.1 | 2 |
| unified | 21.4 | 15.8 | 99 | 1712 | 28.2 | 0 |

**Delta vs baseline (unified):** first paint ~12.5× faster; mounted DOM nodes
~40× fewer; rendered rows 3997 → 99 (viewport ± overscan). Split unchanged
(still eager). Scroll p95 comparable.


---

## Reference notes (from earlier research — still useful)

## Related work — does PR #954 change this?

**PR:** https://github.com/tomasz-tomczyk/crit/pull/954 — VCS status snapshot
reuse for huge working trees (OPEN).

**Impact on this stream:** **Orthogonal.** Crit already defers file **bodies**
and lazy-fetches content; #954 speeds **discovering which files exist**. This
stream targets **rows inside an open file**. Merge #954 if it helps large-repo
startup, but it does not remove the need for row virtualization if scroll/paint
of big files is the pain.

Existing related patterns to reuse, not replace:
- `setupBodyMountObserver` / deferred bodies (file-level windowing)
- Lazy file content load from backend
- Perf budgets under `web/__tests__/perf/` (pre-DOM CPU only today)

## Prior findings (seed)

- Once a file body mounts, crit builds full line DOM (no row virtualization).
- Comment/selection/scroll-restore assume persistent row nodes — that is the
  hard part (same class of problem as a Pierre adapter, on light DOM we own).
- Solo-team tradeoff: more renderer ownership forever vs no shadow-DOM/Shiki/
  embed pipeline change.

## Current status

- [x] Living doc seeded
- [x] Specify windowing algorithm (height estimate, overscan, spacer, remount)
- [x] List comment lifecycle requirements when rows unmount/remount
- [x] List selection/navigation/scroll-restore assumptions that stop holding
- [x] Define the first prototype slice (isolated; no production wiring in this wave)
- [x] Difficulty/effort estimate with confidence
- [x] Decision: **pursue a time-boxed, measured code-diff prototype** (urgency softened after patch-vs-Pierre measure — see agent notes)
- [ ] Commit requested documentation update — blocked by sandbox permissions

## Scope decision

Start with **unified code diffs only**. Add split code diffs after the row
model/window controller works. Do not include markdown in the first production
slice: markdown blocks have much less predictable heights, native tables,
headings/TOC anchors, mermaid, and source blocks spanning several lines. Those
are a second project, not a small extension of code-row virtualization.

Keep the existing `diffTooLarge` gate initially. The first integration would
replace the synchronous full mount after **Load diff** for diffs over 1,000
lines; small diffs retain today's renderer. This gives an A/B boundary and
limits regression exposure while the interaction contracts move from DOM to
model state.

## Concrete windowing design

### 1. Flatten to logical rows before creating DOM

Introduce a pure code-diff row builder (prototype name `buildDiffRows`) that
turns hunks into an ordered array. A row has a stable key, kind, semantic
anchor, estimated/measured height, and enough data to render itself:

```text
line:u:<hunk identity>:<line identity>       unified code line
header:<hunk identity>                       hunk header
gap:<previous hunk>:<next hunk>              expand control
comment:<comment id>                         thread attached after its line
form:<form key>                              compose/edit form after its line
outdated:<comment id>                        outdated tail thread
```

Comments and forms are separate logical rows, not height hidden inside the
code-line estimate. They remain adjacent to their anchor line in model order.
That makes height changes, hide-resolved, navigation, and pinning explicit.
Split mode will use one logical row for the paired left/right visual row; its
semantic anchor carries both old and new locations.

Stable keys must not use the array index alone. Hunk expansion and comment
insertion change indices. Line keys should combine hunk identity with type and
old/new line number; comment/form IDs are already stable. Rebuilding the row
array creates new indices but preserves the semantic key used for focus and
scroll anchoring.

Do **not** recycle one DOM node for a different logical row. Reconcile by key:
retain overlapping mounted nodes, create nodes entering the window, and discard
nodes leaving it. Event delegation already makes remount cheap. Reassigning a
node to a different row would make focus, native Selection, animations, and
async comment UI substantially harder to reason about.

### 2. Height estimates and measurements

Maintain `heights[]` and `offsets[]`, where `offsets[i]` is the prefix sum
before row `i`. Initial estimates are CSS-derived and mode/width-aware:

| Row kind | Initial estimate |
|---|---:|
| Unified code line | 20 px |
| Split code row, desktop | 20 px |
| Hunk header | 34 px |
| One-button gap/spacer | 22 px |
| Two-button gap/spacer | 42 px |
| Collapsed comment thread | 44 px |
| Expanded comment thread | 128 px + 28 px per visible reply |
| Comment form/editor | 190 px |

These are startup estimates, not invariants. `.diff-content` uses
`white-space: pre-wrap` plus `overflow-wrap: anywhere`, so long code lines can
wrap. Split row height is the larger side (and mobile stacks both sides).
Observe mounted row wrappers with one `ResizeObserver`; batch changed heights
into one animation frame, then recompute the prefix array from the lowest dirty
index. Ten thousand numeric additions after an occasional measurement batch
are cheaper and simpler than a Fenwick tree. Revisit the data structure only if
profiles show prefix recomputation is material.

Cache measured heights by `{view mode, container width bucket, row key}` for
the life of the file body. Invalidate line estimates on width/font/theme
changes; invalidate comment/form estimates when collapse, replies, edit mode,
hide-resolved, or textarea growth changes their content.

When a measured height changes above the reading anchor, compensate
`window.scrollY` by the exact delta in the same animation frame. Use one
explicit anchor policy and disable browser scroll anchoring on the virtual
surface if it double-compensates.

### 3. Window and overscan

The scroll container remains the page. On passive `scroll` and `resize`, queue
at most one `requestAnimationFrame` update:

1. Convert the viewport top/bottom into offsets relative to the virtual
   surface.
2. Binary-search `offsets[]` for the first and last visible row.
3. Expand by `overscanPx = clamp(1.5 * viewportHeight, 800, 2400)` on each side.
4. Union the viewport interval with pinned interaction intervals.
5. Key-reconcile only when the resulting interval set changes.

Pixel overscan is required because comment/form and wrapped-line heights vary.
At a 900 px viewport this keeps roughly 67 code lines on each side (about 180
including the visible rows), while bounding mounted DOM during a large jump.

### 4. Spacers and pinned islands

For the normal case render:

```text
top spacer (sum of rows before window)
mounted window
bottom spacer (sum of rows after window)
```

An active textarea, native text selection, gutter drag endpoint/range, or row
with keyboard focus may need to survive outside overscan. Represent those as
additional mounted intervals and emit a spacer for every unmounted gap:

```text
spacer / pinned editor island / spacer / viewport window / spacer
```

Adjacent/overlapping intervals are merged. This lets a focused comment editor
stay live if the reviewer scrolls away without keeping all intervening code
rows mounted. At typical Crit scale there are only a handful of active forms,
so islands remain bounded.

Rows containing ordinary comment cards are allowed to unmount. Their durable
state is the comment model plus `commentCollapseOverrides`; expanded reply and
edit composers are pinned until their DOM state has been written through to
model state. A pinned row's measured height participates in the prefix sums at
its real position.

### 5. Remount and scroll-to behavior

The virtualizer owns these model-first operations:

- `ensureMounted(rowKey)`: add a temporary pinned interval, reconcile, resolve
  after the row has been measured.
- `scrollToRow(rowKey, alignment)`: set an estimated scroll position from the
  prefix array, mount/measure, then correct to the exact position.
- `captureAnchor()`: return `{rowKey, intraRowOffset}` for the first visible
  semantic row (or the center row for view switches).
- `restoreAnchor(anchor)`: map the semantic key into the rebuilt model,
  estimate-scroll, mount, and correct.
- `invalidateRows(keys)`: rebuild only comment/form presentation and heights
  for affected keys; structural hunk changes rebuild the row model while
  preserving a semantic anchor.

Split↔unified restore must map through a semantic source location, not a DOM
node or visual index. Prefer the new/right line for context/additions, retain
old side for deletions, and fall back to the containing hunk key when that
exact line has no representation in the new mode. This formalizes the intent
already present in `readingLineAnchor()`.

### 6. Mount lifecycle

On mount, render one row and run row-local post-processing only: syntax HTML,
comment card/form creation, quote highlighting for comments in that row, and
measurement observation. Do not run whole-file queries or
`renderMermaidBlocks()` from a code-row window update.

On unmount, first write through mutable UI state: textarea value and selection,
reply composer content, collapse override, focused semantic target, and native
selection pin state. Remove `ResizeObserver` observation and discard the node.
Delegated container listeners remain installed once.

File-level deferral stays outside the row virtualizer. Deferring a file body
destroys its virtualizer instance after saving its height cache/anchor;
remounting the body recreates the surface and restores through logical keys.

## Breaking assumptions in current code

The following are requirements, not optional cleanup. Each current path assumes
that all rows in a mounted file remain in the DOM.

### Comment lifecycle

- `scrollToCommentRef`, `scrollToComment`, and `highlightNavComment` mount a
  file body and immediately query for a card. They must map comment ID → row
  key, `ensureMounted`, then flash the returned node.
- `openForm`, submit/cancel/edit/reply flows, and `comments-changed` call
  `renderFileByPath`, replacing the entire section. They must update the row
  model and invalidate the anchor/comment/form rows instead.
- `saveOpenFormContent` only captures textareas found in current DOM.
  Virtualized editors require write-through `input` state plus saved cursor/
  selection offsets; otherwise an overscan exit loses draft state.
- `activeReplyForms` and `commentCollapseOverrides` are useful model state, but
  expanded edit/reply forms need explicit pin/unpin rules and focus restore.
- `syncCommentHighlightsInSection` scans all rendered lines. It may update only
  mounted nodes; future mounts must derive `has-comment` from comment indices.
- `highlightQuotesInSection` is whole-section post-processing today. It must
  become idempotent and row-local on comment-card mount.
- Hide-resolved changes both row presence and heights. It must rebuild affected
  comment rows and anchor-compensate, not just rely on CSS over absent rows.

### Selection and dragging

- `getLineRangeFromSelection` discovers the selected lines by querying every
  row and calling `Range.intersectsNode`. It can only see mounted rows.
- `tryOpenFormFromSelection` reconstructs full selected text and quote offsets
  by querying line contents. It must combine the native range endpoints with
  row-model text; DOM is used only for exact offsets inside endpoint rows.
- Native browser Selection breaks if an endpoint or an interior selected node
  is removed. While a pointer/keyboard text selection is active, pin the
  contiguous selected interval. A deliberately huge selection may transiently
  mount many rows; correctness wins during that gesture, and the pin is
  released after copy, `c`, selection collapse, or pointer-up.
- `handleDragMove` and `handleDiffDragMove` use `elementFromPoint`; that works
  only for visible rows. Auto-scroll during a drag must advance through the row
  model and expand the pinned interval as new rows enter the viewport.
- Unified `handleDiffDragEnd` reconstructs selected visual lines by querying
  mounted `.diff-line` nodes. Resolve the range from logical rows/visual
  indices instead.
- `updateDragSelectionVisuals` and `refreshVisualSelectionVisuals` can remain
  mounted-DOM updates, provided every remounted row derives selection classes
  from model state.

### Keyboard and change navigation

- `rebuildNavList`/`navElements`, `navigateBlock`, and `focusedElement` treat
  DOM order as the navigation model. The logical row array must become the
  source of truth; keep `focusedRowKey`, mount the target, then apply focus.
- `buildChangeGroups` derives groups from DOM queries and sibling adjacency.
  Compute change groups from logical rows so offscreen changes remain
  navigable and counters stay correct.
- `restoreKeyboardFocus` cannot find an unmounted target. It must ask the
  virtualizer to mount/scroll to the semantic focus key before applying CSS.
- Every `scrollIntoView` call aimed at a line/change/comment needs a model-first
  equivalent. Calling it on a missing row must never silently do nothing.

### Scroll restore and structural updates

- `readingLineAnchor` only scans mounted DOM. Capturing the visible window is
  fine, but the returned identity must be a stable row key plus intra-row
  offset rather than only line number/top.
- `restoreReadingLineAnchor` queries for a node that may not be mounted.
  Restore must go through prefix offsets, then exact measurement correction.
- `renderAllFilesKeepingPlace` remounts whole file bodies up to the reading
  position. A virtual body should restore its own row anchor without mounting
  preceding rows.
- Split↔unified, hunk expansion, auto-expansion, round refresh, and
  hide-resolved all change row structure. Preserve an anchor before rebuilding
  the row model and map by semantic location afterward.
- `deferFileBody` currently uses `innerHTML = ''` with no row-level teardown.
  It must dispose observers and persist virtual state first.

### Browser behavior outside Crit's custom controls

- Native Ctrl/Cmd+F and screen-reader browse mode only see mounted DOM. Before
  shipping broadly, decide whether the >1,000-row path accepts this limitation
  or needs a Crit find UI / accessible non-visual representation. This is a
  release decision, not something spacers solve.
- Copying a normal visible selection remains native. “Select all” over a
  virtual surface cannot rely on DOM contents and needs either an explicit
  copy-all action or documented behavior.

## First prototype slice

Do **not** add a throwaway implementation in this wave: a stub without Crit's
real comment/navigation contracts would prove only prefix-sum arithmetic. The
first useful prototype should be an isolated harness under
`spikes/virtualize/`, then wired only after its measurements are recorded:

1. Flatten the real 10k/500-hunk fixture into unified logical rows.
2. Render read-only unified code lines with 20 px estimates, measured wrapped
   heights, the overscan policy above, and top/bottom spacers.
3. Add keyed reconciliation and scroll-anchor compensation.
4. Add one anchored comment card and one pinned active form as disjoint
   intervals.
5. Measure first paint, long tasks, mounted node count, and fast scrollbar/
   wheel scrolling against the current **Load diff** path and Pierre numbers.

Pass criteria for continuing into production integration:

- No blank window during wheel, trackpad, Page Down, or scrollbar-thumb jumps.
- At most roughly 400 code rows mounted in the ordinary 900 px viewport case
  (comment/form islands excluded).
- First visible rows within 500 ms after data/highlight preparation on the
  10k/500 fixture, with no >100 ms window-maintenance long task while scrolling.
- Scroll drift under 2 px after wrapped-row measurements settle.
- Comment/form island preserves textarea content, cursor, focus, and position.

The benchmark must report preprocessing separately. Row virtualization removes
DOM construction/layout cost, but `preHighlightFile`, hunk word-diff building,
and row-model construction may still dominate CPU if left eager.

## Effort estimate

**First measured prototype:** 3–5 engineering days.

**Production-ready code-diff virtualization:** 10–15 engineering days
(roughly 2–3 weeks for one engineer), broken down as:

- 3–4 days: logical row builder, height index, keyed window controller,
  measurement/anchor correction.
- 4–6 days: comments/forms/replies, text and gutter selection, keyboard/change
  navigation, comment navigation, split↔unified restore.
- 3–5 days: split/mobile behavior, file-deferral lifecycle, accessibility/find
  decision, unit/perf/E2E coverage, regression fixing.

Confidence is **medium (60%)**. The window math is straightforward; native
Selection, focused editors, model-first navigation, and current whole-file
rerenders drive the range. Markdown virtualization would be a separate
additional **5–8 days** after code diffs, with low-to-medium confidence (40%)
because tables, headings, mermaid, and highly variable block heights expand the
surface substantially.

## Lean

**Pursue**, but as the measured 3–5 day unified code-diff prototype above—not
as a direct `app.js` rewrite. Pierre shows that a virtual renderer can scroll
smoothly at the target size, while its 3.4–6.9 s first-paint numbers also warn
that non-DOM preprocessing matters. Crit's existing >1,000-line load gate gives
a safe integration boundary if the prototype hits its budgets.

## Decisions log

| When | Decision | Why |
|------|----------|-----|
| 2026-09-24 | Pursue a time-boxed unified code-diff prototype | It isolates the highest-value loaded-large-diff path and produces evidence before production interaction rewrites |
| 2026-09-24 | Use estimated then measured variable heights | Code wraps; split rows and comment/form rows are not fixed height |
| 2026-09-24 | Use keyed remount, not positional DOM recycling | Stable interaction identity matters more than saving row creation at a ~400-row mount cap |
| 2026-09-24 | Model comments/forms as rows; pin active editors as islands | Keeps height accounting explicit and preserves focused mutable UI without mounting intervening code |
| 2026-09-24 | Keep navigation and anchors in the logical row model | Offscreen rows cannot be discovered or restored through DOM queries |
| 2026-09-24 | Defer markdown virtualization | Its blocks/tables/TOC/mermaid need a separate design and estimate |
| 2026-09-24 | Keep split mode eager in the first production slice | Unified exercises the row/comment lifecycle with less paired-row and mobile complexity; validate it before extending the model |
| 2026-09-24 | Defer the crit-web port until Crit browser validation | Avoid copying an implementation before production interaction and benchmark results are known |
| seed | Keep crit DOM contracts | Preserve comment UX + embed story |
| seed | #954 out of band | File discovery ≠ row virtualization |

## Agent notes

_Update this section as you work. Newest notes at the top._

- (2026-09-24, commit blocker) Final commit attempt with subject
  `feat(web): virtualize large unified diffs` was blocked. Git cannot create
  the linked worktree administrative lock at
  `crit/.git/worktrees/crit.spike-large-diff-virtualize/index.lock` because it
  is outside the writable sandbox. All implementation/doc changes remain
  uncommitted in this worktree.
- (2026-09-24, verification) Focused virtualizer/load-order tests pass (8/8),
  the existing frontend suite passes, ESLint and Stylelint pass, JS syntax and
  `git diff --check` pass, a Go build succeeds with an isolated `GOCACHE`, and
  the asset budget remains within caps. The broad Node glob reports 614/615
  passing; the one failure is the existing `range-focus-diff-scope.test.js`
  extraction harness missing an `initialViewMode` stub, unrelated to these
  changes. `go test ./...` proceeds with isolated cache but packages using
  `httptest` fail because this sandbox forbids localhost binds.
- (2026-09-24, benchmark blocker) The required fair benchmark was attempted.
  Playwright Chromium exits before Crit starts because macOS denies its Mach
  rendezvous registration (`Permission denied (1100)`). No post-implementation
  numbers were fabricated; an unsandboxed rerun is the next measurable step.
- (2026-09-24, implementation follow-up) Active reply editors now dynamically
  pin/unpin their logical comment row. Expand/collapse-all writes overrides for
  offscreen logical comments and resets estimated heights while preserving a
  visible anchor. The 900 px pure window calculation mounts 181 ordinary rows
  at mid-file (under the roughly 400-row design budget, before browser layout).
- (2026-09-24, implementation) Added the production logical-row/windowing
  implementation. `web/crit-diff-virtualizer.js` owns pure unified row
  construction, prefix heights, binary lookup, pixel overscan, keyed row
  reconciliation, ResizeObserver measurement/anchor compensation, pinned
  islands, native-selection pinning, and model-first scroll helpers.
  `web/app.js` now uses it only for loaded large unified diffs. Comments/forms
  are distinct rows; active compose/edit/reply UI is pinned; draft text and
  cursor selection write through. Added virtual-aware comment navigation,
  reading restore, gutter drag range resolution, keyboard row navigation,
  quote re-highlighting, hide-resolved rebuild, and disposal on body/section
  teardown. Verification is the next step.
- (2026-09-24, implementation) Confirmed the worktree starts clean and mapped
  the production seam. Virtualization will activate only for loaded large
  unified code diffs; the 1,000-hunk-line threshold is the existing
  `diffTooLarge` boundary. Split stays eager initially. Crit-specific row DOM
  remains in `app.js`; a new module will own pure row construction, prefix
  heights, overscan, keyed reconciliation, measurement, and pinning.
- (2026-09-24, parent) Cross-stream: measure head-to-head shows Crit patch UI
  first-paint ~266–428 ms vs Pierre ~6.8 s on the same *source* fixture, but
  Crit painted ~4k patch rows while Pierre painted ~10k full-file rows. Scroll
  p95 comparable. **Default-patch speed is not a proven reason to virtualize**;
  keep this stream aimed at memory/DOM (69k–99k nodes) and full-file / many-file
  cases. Revisit pursue vs park after a fair full-file Crit measure.
- (2026-09-24) Commit attempt with subject
  `docs(spike): advance virtualize large-diff approach` was blocked: Git could
  not create the linked worktree's `.git/worktrees/.../index.lock` because that
  administrative path is outside the writable sandbox. The doc change remains
  intact and unstaged.
- (2026-09-24) Inspected `app.js` file deferral, raw/rendered diff renderers,
  comments/forms, selection/drag, keyboard/change navigation, and scroll
  restore. The primary cost is not the window arithmetic; it is replacing
  mounted-DOM-as-model assumptions. No production code changed in this wave.
- (2026-09-24) CSS confirms code rows are only nominally 20 px:
  `.diff-content` wraps anywhere, split uses the taller side, and mobile stacks
  sides. Fixed-height-only virtualization is not viable.
- (2026-09-24) Chose not to add an isolated arithmetic stub. The first useful
  prototype must use the real 10k/500-hunk row shape and exercise one comment
  plus one active form; otherwise it would understate the integration risk.
- (seed) Stream started. Prefer updating this file over long chat summaries.
