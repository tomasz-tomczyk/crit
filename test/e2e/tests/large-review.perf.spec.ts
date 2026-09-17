import { test, expect, type Page } from '@playwright/test';
import { loadPage, clearAllComments } from './helpers';

// Perf guardrails for large reviews (300 files / ~9k changed lines fixture).
//
// These assert structural invariants — mounted body count, DOM size, and
// longtask-blocked main thread — rather than wall-clock time, so they only
// fail on real regressions (e.g. mounting every file eagerly, the bug class
// fixed in 27f33c8) and not on runner noise. Wall clock is logged, not gated.
//
// How lazy bodies actually behave: every file renders a section header, but
// only the eager set (lazyFileThreshold = 25) mounts its body at load. The rest
// mount on demand when an IntersectionObserver sees the section come within a
// viewport of the screen, each via a server round trip. Nothing un-mounts
// because of scrolling — the only path that defers an already-mounted body is
// collapsing its <details>. So the mounted count is a function of how far down
// the review the reader has scrolled, and it never comes back down.
//
// That makes the count meaningful only once mounting has caught up with the
// current scroll offset, so the scroll test dwells at fixed offsets and waits
// for the count to stop moving (settledMountedCount) before sampling. The
// settled value still grows with machine slowness, because a busy main thread
// batches more observer entries into one callback and each batch mounts
// everything then in range: dwelling through the first 6k px settles at 33
// bodies on an idle machine, 47 under 8x CPU throttling and 68 under 16x.
// Budgets are sized for the slow end of that range, well below the 301 a
// mount-everything regression would produce.
//
// Reference measurements (idle machine, 1280x720): 25 mounted bodies / ~13k DOM
// nodes at load, 33 mounted / ~14k nodes after the dwell, ~0ms blocked.

const TOTAL_FILES = 301;

// plan-big.md is the largest file and sorts last, so it sits at the bottom of
// the review — the reader in these tests never gets near it.
const TAIL_BODY = '#file-section-plan-big\\.md .file-body[data-body-deferred]';

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
});

// Total Blocking Time (sum of longtask duration beyond 50ms each).
async function installLongtaskObserver(page: Page) {
  await page.addInitScript(() => {
    (window as unknown as { __longtasks: number[] }).__longtasks = [];
    try {
      new PerformanceObserver((list) => {
        const acc = (window as unknown as { __longtasks: number[] }).__longtasks;
        for (const entry of list.getEntries()) acc.push(entry.duration);
      }).observe({ type: 'longtask', buffered: true });
    } catch {
      // PerformanceObserver/longtask unavailable — metrics read as zero.
    }
  });
}

async function longtaskTBT(page: Page): Promise<number> {
  const durations = await page.evaluate(
    () => (window as unknown as { __longtasks?: number[] }).__longtasks ?? []
  );
  return durations.reduce((sum, d) => sum + Math.max(0, d - 50), 0);
}

async function domNodeCount(page: Page): Promise<number> {
  return page.evaluate(() => document.getElementsByTagName('*').length);
}

function mountedBodies(page: Page) {
  return page.locator('.file-body:not([data-body-deferred])');
}

// Mounting a body costs an IntersectionObserver callback plus a server round
// trip, so a count read straight after scrolling races that work. Poll until
// the count has held still for a quiet window instead.
//
// Safe failure mode: a contended runner can look quiet before mounting has
// finished, but that only ever reports a *lower* count than the settled one,
// which still satisfies the budgets below. Runner slowness cannot turn this
// into a false failure — which is why nothing here asserts a lower bound on
// the count.
async function settledMountedCount(page: Page, quietMs = 500, timeoutMs = 5_000): Promise<number> {
  const deadline = Date.now() + timeoutMs;
  let count = await mountedBodies(page).count();
  let lastChange = Date.now();
  while (Date.now() - lastChange < quietMs && Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 100));
    const next = await mountedBodies(page).count();
    if (next !== count) {
      count = next;
      lastChange = Date.now();
    }
  }
  return count;
}

test('large review initial render stays within DOM and longtask budgets', async ({ page }) => {
  await installLongtaskObserver(page);
  await loadPage(page);

  // All 301 files render as sections (headers are cheap; bodies defer).
  await expect(page.locator('.file-section')).toHaveCount(TOTAL_FILES);

  // Only the eager set (~25) plus viewport slack is mounted before the reader
  // scrolls anywhere. Polled, not snapshotted: mount/defer settles
  // asynchronously after load.
  await expect.poll(() => mountedBodies(page).count()).toBeLessThanOrEqual(40);
  const mounted = await mountedBodies(page).count();

  const nodes = await domNodeCount(page);
  console.log(`initial render: mounted=${mounted} domNodes=${nodes}`);
  expect(nodes).toBeLessThan(200_000);

  const tbt = await longtaskTBT(page);
  console.log(`initial render: longtaskTBT=${Math.round(tbt)}ms`);
  expect(tbt).toBeLessThan(8000);
});

// The first few screens a reader passes through. The eager set spans roughly
// the first 6k px of the ~17k px initial document, so the last stop is where
// on-demand mounting starts; all three stay far above the deferred tail.
const DWELL_OFFSETS = [2000, 4000, 6000];

test('scrolling a large review mounts bodies on demand and leaves the tail deferred', async ({ page }) => {
  await installLongtaskObserver(page);
  await loadPage(page);
  await expect(page.locator('.file-section')).toHaveCount(TOTAL_FILES);

  const tbtBefore = await longtaskTBT(page);
  const wallStart = Date.now();

  // Dwell at each offset the way a reader would, letting mounting settle
  // before moving on. Only the final settled state is budgeted — the
  // per-stop counts vary with machine speed, so they are logged, not asserted.
  let mounted = 0;
  for (const y of DWELL_OFFSETS) {
    await page.evaluate((offset) => window.scrollTo(0, offset), y);
    mounted = await settledMountedCount(page);
    console.log(`scroll: y=${y} mounted=${mounted}`);
  }

  const wallMs = Date.now() - wallStart;
  const scrollTBT = (await longtaskTBT(page)) - tbtBefore;
  const nodes = await domNodeCount(page);
  console.log(`scroll: wall=${wallMs}ms scrollTBT=${Math.round(scrollTBT)}ms mounted=${mounted} domNodes=${nodes}`);

  // Mounting only reached the files the reader scrolled past — a fraction of
  // the 301 sections, not all of them.
  expect(mounted).toBeLessThan(120);
  expect(nodes).toBeLessThan(200_000);
  expect(scrollTBT).toBeLessThan(8000);

  // Nothing touched the bottom of the review: the largest file in the fixture
  // is still an unmounted placeholder.
  await expect(page.locator(TAIL_BODY)).toHaveCount(1);
});
