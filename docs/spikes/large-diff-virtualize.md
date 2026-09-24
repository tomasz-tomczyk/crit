# Approach B — Virtualize crit’s own renderer

> **Living state document.** This file is the source of truth for this stream.
> Multiple agents may work in this worktree. Whenever you learn something,
> change a decision, finish a probe, or hit a blocker — **update this file in
> the same turn** so the next agent (or human) sees current reality. Do not
> leave contradictory notes in chat-only memory.

## Goal

Keep crit’s light-DOM, CSS, and comment model. Make large **code** (and later
markdown) file bodies cheap by mounting only viewport ± overscan rows, with
spacers for correct scroll height — stealing the *recipe* from Pierre/diffs.com
without taking the dependency.

## Non-goals

- Adopting `@pierre/diffs` in this stream
- Full production ship in the first probe (design + throwaway OK)

## Worktree

- Path: `crit.spike-large-diff-virtualize`
- Branch: `spike/large-diff-virtualize`
- Hot paths today: `web/app.js` (`renderFileSection`, deferred body mount),
  `web/crit-diff-renderer.js`, `web/crit-line-blocks.js`; crit-web mirror
  `assets/js/document-renderer.js`

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
- [ ] Specify windowing algorithm (height estimate, overscan, spacer, recycle)
- [ ] List comment lifecycle requirements when rows unmount/remount
- [ ] Throwaway prototype plan (isolated under `spikes/` — do not wire prod yet)
- [ ] Difficulty/effort estimate with confidence
- [ ] Decision: pursue / park / reject

## Open questions

- Start with code diffs only, or markdown line-blocks too?
- Fixed vs measured row heights when comment cards are open?
- How does split↔unified scroll restore interact with recycled rows?

## Decisions log

| When | Decision | Why |
|------|----------|-----|
| seed | Keep crit DOM contracts | Preserve comment UX + embed story |
| seed | #954 out of band | File discovery ≠ row virtualization |

## Agent notes

_Update this section as you work. Newest notes at the top._

- (seed) Stream started. Prefer updating this file over long chat summaries.
