# Performance: benchmarks, budgets, and guardrails

crit has no production profiler running anywhere — it's a localhost CLI — so
perf is guarded by benchmarks and budgets in CI, not by dashboards. This doc
lists what exists, how to run it, and when to recalibrate.

## Go benchmarks

Located next to the code they cover; all use `b.Loop()` + `b.ReportAllocs()`.

| Benchmark | File | What it covers |
| --- | --- | --- |
| `BenchmarkComputeLineDiff` (100–10k lines) | `internal/diff/diff_test.go` | Myers/Hirschberg diff over plan-sized inputs |
| `BenchmarkDiffEntriesToHunks` | `internal/diff/diff_test.go` | Hunk grouping over 10k entries / ~500 hunks |
| `BenchmarkReviewSaveLoad` (10×10, 50×50) | `internal/session/review_bench_test.go` | Full review-file roundtrip (marshal + atomic write + read + unmarshal) on every debounced save |
| `BenchmarkCarryForward` | `internal/session/review_bench_test.go` | Round-complete comment remap for one 10k-line plan + 50 comments (diff + anchor remap, on the server before `file-changed`) |
| `BenchmarkVisibleInFocus` (n=10–1000) | `internal/session/focus_test.go` | Comment visibility filter behind every `GetComments` call |

Run:

```bash
make bench   # count=6, ./internal/diff/ ./internal/session/
```

Compare a branch against a base (same machine, back to back):

```bash
git worktree add /tmp/crit-base origin/main
(cd /tmp/crit-base && go test -run='^$' -bench=. -benchmem -count=6 ./internal/diff/ ./internal/session/ > ../bench-old.txt)
go test -run='^$' -bench=. -benchmem -count=6 ./internal/diff/ ./internal/session/ > bench-new.txt
make bench-compare   # benchstat + scripts/bench-compare.py gating
```

Reference numbers (M4 Max, 2026-09; CI runners are slower — never gate on
absolute ns/op, only on deltas):

- `ComputeLineDiff/10000_lines`: ~250ms/op — the worst-case large-plan cost.
- `ReviewSaveLoad/50x50`: ~13ms/op — per debounced save at 2500 comments.
- `CarryForward`: ~370ms/op — per 10k-line plan on round-complete (stacks with N large plans).
- `VisibleInFocus/n=1000`: ~15µs/op, 2 allocs (one hoisted focus-key format).

### CI `bench` job

`.github/workflows/test.yml` runs head-vs-base on the same runner and fails
only on (see `scripts/bench-compare.py`):

- time/op: statistically significant slowdown **≥ 20%** (benchstat `~` rows ignored).
- allocs/op: **any** significant increase — allocs are deterministic, so 0→N
  means a heap escape worth a look. B/op regressions are reported, not gated.

New benchmarks appear in the comparison with no baseline — they report without
gating until they exist on both sides. A missing baseline fails the job
rather than skipping the gate. The gate's own parser is locked by
`scripts/bench-compare_test.py` (run as a CI step before comparing).

## Frontend perf microbenchmarks

`web/__tests__/perf/*.perf.test.mjs`, run with `npm run test:perf` (also in the
`test-frontend` CI job). Deterministic seeded fixtures, median-of-5, generous
budgets (10–50× measured) that only fail on algorithmic regressions:

- markdown-it parse + `buildLineBlocks` on a 10k-line plan fixture.
- `splitHighlightedCode` on a 2000-line highlighted block.
- `bestWordDiffPairing` (capped 4×4) + `wordDiff` (capped ~500-char lines).

Each bench asserts its fixture does real work (e.g. `wordDiff` returns
non-null) so budgets can't silently assert against an early-out path.

## Playwright perf specs

`test/e2e/tests/large-review.perf.spec.ts` (project `perf`, port 3134,
fixture `test/e2e/setup-fixtures-perf.sh`: 300 files / ~9k changed lines +
one large markdown, mirroring the 27f33c8 measurement). Wired into `run.sh`
like every other project. Asserts structural invariants, not wall clock:

- mounted `.file-body` sections ≤ 40 (eager set is 25).
- DOM nodes < 200,000.
- longtask TBT (Σ(duration − 50ms)) < 8000ms for load and for a full scroll.

Measured steady state: 25 mounted bodies, ~12k DOM nodes, 0ms TBT.

## Asset + binary budgets

crit embeds all of `web/` in the binary, so a vendor bump inflates page
weight and binary size together. Caps live in `asset-budget.json`
(~15–20% above measured); `scripts/check-asset-budget.sh` enforces them in
the `asset-budget` CI job:

- per-file gzip caps (mermaid, highlight, app.js, live-mode.js, style.css,
  markdown-it, …),
- glob totals (`web/crit-*.js` shared modules),
- total `web/*.js` gzip cap,
- linux/amd64 binary cap (trimpath, `-s -w`).

If growth is intentional (e.g. a vendor major bump), run the script, verify
the delta, and update the caps in `asset-budget.json`.

## Lint

- `perfsprint` is enabled in `.golangci.yml` (Sprintf→concat/Itoa,
  hex formatting, concat-in-loop). Its `error-format` check is excluded as
  style churn with zero perf value.
- No ESLint perf plugins: nothing published applies to vanilla DOM code.
  The rule that matters is architectural — event listeners are delegated per
  container (`attachDiffMouseHandler`, `attachDocGutterMouseHandler`), never
  per line — and it is covered by the perf E2E spec, not by lint.

## Known hot paths and past fixes

- Focus-key formatting is hoisted out of per-comment loops
  (`visibleInFocusKey`; `BenchmarkVisibleInFocus` locks the assumption).
  Don't call `focusKeyFor` per comment.
- Document-view comment gutters use one delegated mousedown per container
  (same bug class as #657 for diff buttons: ~10k listeners on a 10k-line file).
- `highlightQuotesInSection` indexes content elements once per mount instead
  of scanning the section per quoted line.
- `getFormsForFile` results are threaded down from top-level renders, not
  recomputed per block/line/drag-frame.
- `ComputeLineDiff` at 10k lines costs ~250ms: keep large-markdown work off
  the critical render path (lazy bodies already do this).

## Deferred follow-ups (reviewed, not yet done)

From external review of this branch; each needs its own PR with measurements:

1. **Full `renderFileByPath` on gutter mousedown.** Delegation removed the
   listener cost, but the first click on a 10k-line plan still pays a full
   file remount to start a selection (`beginDocGutterDrag`). Highest remaining
   interaction cost for large markdown — update drag visuals without a full
   remount, or defer non-selection work.
2. **Mermaid on mount paths.** `renderMermaidBlocks()` runs after many
   remounts with no budget; diagrams on large plans can dominate TBT.
   Consider lazy-rendering mermaid (only when `code.language-mermaid`
   exists) and a perf-E2E fixture with diagrams.
3. **Session-init bench.** Myers and review JSON are benched; `NewSessionFromGit`
   / `RefreshFileList` (eager ×25 + numstat for N files, gating daemon
   readiness) is not. Needs a temp-git-repo fixture — keep it out of the
   hermetic bench set until readiness latency shows up in practice.
4. **Progressive mount after "Load diff".** The `diffTooLarge` placeholder is
   good, but clicking it mounts all hunk DOM synchronously. Progressive
   append (rAF batches) or in-file IntersectionObserver for the loaded path.
5. **`file-changed` full reload.** The handler re-fetches everything; lazy
   stubs soften it, but nothing asserts files past the threshold stay
   `lazy: true` after round-complete. A Go unit test on the refresh path
   (no new browser minutes) is the cheap guardrail.

## Recalibration

- Budgets: quarterly, or after an intentional vendor bump / feature that
  legitimately moves them. Measure on `main`, set caps ~1.2× measured,
  note the reason in the PR.
- The `bench` CI job starts strict from day one on allocs (deterministic)
  and ≥20%-significant on time. If it ever false-positives on a PR, don't
  bump the threshold silently — link the run in `docs/performance.md` or the
  PR and adjust with evidence.
