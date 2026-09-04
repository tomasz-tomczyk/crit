import { test, expect, type Page } from '@playwright/test';
import { loadPage, clearAllComments } from './helpers';

// Perf guardrails for large reviews (300 files / ~9k changed lines fixture).
//
// These assert structural invariants — mounted body count, DOM size, and
// longtask-blocked main thread — rather than wall-clock time, so they only
// fail on real regressions (e.g. mounting every file eagerly, the bug class
// fixed in 27f33c8) and not on runner noise. Wall clock is logged, not gated.
//
// Budgets carry 2-4x headroom over the post-lazy-loading steady state
// (25 mounted bodies, ~93k DOM nodes, ~0ms blocked on the reference
// 300-file measurement).

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

test('large review initial render stays within DOM and longtask budgets', async ({ page }) => {
  await installLongtaskObserver(page);
  await loadPage(page);

  // All 301 files render as sections (headers are cheap; bodies defer).
  await expect(page.locator('.file-section')).toHaveCount(301);

  // Only the eager set (~25) plus viewport slack is ever mounted. Polled,
  // not snapshotted: mount/defer settles asynchronously after load.
  await expect.poll(() => mountedBodies(page).count()).toBeLessThanOrEqual(40);
  const mounted = await mountedBodies(page).count();

  const nodes = await domNodeCount(page);
  console.log(`initial render: mounted=${mounted} domNodes=${nodes}`);
  expect(nodes).toBeLessThan(200_000);

  const tbt = await longtaskTBT(page);
  console.log(`initial render: longtaskTBT=${Math.round(tbt)}ms`);
  expect(tbt).toBeLessThan(8000);
});

test('scrolling a large review keeps bodies deferred and main thread free', async ({ page }) => {
  await installLongtaskObserver(page);
  await loadPage(page);
  await expect(page.locator('.file-section')).toHaveCount(301);

  const tbtBefore = await longtaskTBT(page);
  const wallStart = Date.now();

  // Scripted top-down scroll in viewport steps, yielding two frames per step
  // so the IntersectionObserver mounter runs as it would for a human reader.
  await page.evaluate(async () => {
    const height = () => document.documentElement.scrollHeight;
    for (let y = 0; y < height(); y += 600) {
      window.scrollTo(0, y);
      await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
    }
    window.scrollTo(0, 0);
  });

  const wallMs = Date.now() - wallStart;
  const scrollTBT = (await longtaskTBT(page)) - tbtBefore;

  // Steady state after a full pass: back near the eager set, not 300 mounted.
  // Polled: far-offscreen bodies defer asynchronously after scrolling stops.
  await expect.poll(() => mountedBodies(page).count()).toBeLessThanOrEqual(40);
  const mounted = await mountedBodies(page).count();
  const nodes = await domNodeCount(page);
  console.log(`scroll: wall=${wallMs}ms scrollTBT=${Math.round(scrollTBT)}ms mounted=${mounted} domNodes=${nodes}`);

  expect(nodes).toBeLessThan(200_000);
  expect(scrollTBT).toBeLessThan(8000);
});
