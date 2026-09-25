import { test, expect, type Page } from '@playwright/test';
import { loadPage, clearAllComments, fileListState, waitForScrollStable } from './helpers';

// Perf guardrails for large reviews (300 files / ~9k changed lines fixture).
//
// Approach B (file-list virtualization): #filesContainer only mounts a window
// of .file-section nodes; off-screen files are HeightIndex spacers, not empty
// <details> with deferred bodies. Budgets still gate "mount everything"
// regressions via mounted body count + DOM size + longtask TBT.
//
// Wall clock is logged, not gated.

const TOTAL_FILES = 301;

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
});

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

async function fileListTotalHeight(page: Page): Promise<number> {
  return page.evaluate(() => {
    const container = document.getElementById('filesContainer');
    const controller = container && (container as unknown as {
      _critFileListVirtualizer?: { totalHeight: () => number };
    })._critFileListVirtualizer;
    if (!controller) return 0;
    return controller.totalHeight();
  });
}

test('large review initial render stays within DOM and longtask budgets', async ({ page }) => {
  await installLongtaskObserver(page);
  await loadPage(page);

  const hasController = await page.evaluate(() => {
    const el = document.getElementById('filesContainer');
    return !!(el && (el as unknown as { _critFileListVirtualizer?: unknown })._critFileListVirtualizer);
  });
  expect(hasController).toBe(true);

  // Only a window of file sections is in the DOM — a viewport's worth plus
  // overscan, not a large slice of the 301 (a leaked pin or runaway overscan
  // would still pass a "< TOTAL_FILES" bound).
  const sectionCount = await page.locator('#filesContainer .file-section').count();
  expect(sectionCount).toBeGreaterThan(0);
  expect(sectionCount).toBeLessThanOrEqual(25);

  // Height index still accounts for every file (spacers reserve off-screen space).
  const totalH = await fileListTotalHeight(page);
  expect(totalH).toBeGreaterThan(10_000);

  await expect.poll(() => mountedBodies(page).count()).toBeLessThanOrEqual(25);
  const mounted = await mountedBodies(page).count();

  const nodes = await domNodeCount(page);
  console.log(`initial render: mounted=${mounted} sections=${sectionCount} totalH=${totalH} domNodes=${nodes}`);
  expect(nodes).toBeLessThan(200_000);

  const tbt = await longtaskTBT(page);
  console.log(`initial render: longtaskTBT=${Math.round(tbt)}ms`);
  expect(tbt).toBeLessThan(8000);
});

const DWELL_OFFSETS = [2000, 4000, 6000];

test('scrolling a large review windows the file list and leaves the tail unmounted', async ({ page }) => {
  await installLongtaskObserver(page);
  await loadPage(page);

  const tbtBefore = await longtaskTBT(page);
  const wallStart = Date.now();

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

  expect(mounted).toBeLessThanOrEqual(25);
  expect(nodes).toBeLessThan(200_000);
  expect(scrollTBT).toBeLessThan(8000);

  // Files scrolled past are unmounted again, not merely left behind.
  const order = await page.evaluate(() =>
    (document.getElementById('filesContainer') as unknown as {
      _critFileListVirtualizer: { items: { key: string }[] };
    })._critFileListVirtualizer.items.map(i => i.key));
  await expect(page.locator('[id="file-section-' + order[0] + '"]')).toHaveCount(0);

  // Tail file stays out of the mounted window (spacer / not in DOM).
  // Intentionally unmounted (off-window) — do not call fileSection (that mounts).
  await expect(page.locator('#file-section-plan-big\\.md')).toHaveCount(0);
});

test('sidebar jump to a deep file does not push the target out of view', async ({ page }) => {
  await loadPage(page);

  const treeFiles = page.locator('.tree-file');
  const count = await treeFiles.count();
  expect(count).toBeGreaterThan(50);
  const target = treeFiles.nth(Math.min(80, count - 1));
  const path = await target.getAttribute('data-tree-path');
  expect(path).toBeTruthy();

  await target.click();

  // CSS.escape is browser-only; paths in this fixture are simple identifiers.
  const section = page.locator('[id="file-section-' + path + '"]');
  await expect(section).toBeVisible({ timeout: 10_000 });
  await waitForScrollStable(page);

  // The shove this guards against lands late (lazy prefetch refining
  // neighbour heights), so sample for a while instead of once.
  const tops: number[] = [];
  const deadline = Date.now() + 2000;
  while (Date.now() < deadline) {
    tops.push(await section.evaluate((el) => el.getBoundingClientRect().top));
    await page.evaluate(() => new Promise(r => requestAnimationFrame(r)));
  }
  // Near sticky header (~49px), not shoved down by adjacent mounts expanding.
  expect(Math.min(...tops)).toBeGreaterThanOrEqual(0);
  expect(Math.max(...tops)).toBeLessThan(200);
});

test('repeated sidebar jumps keep the mounted window bounded', async ({ page }) => {
  await loadPage(page);
  const tree = page.locator('.tree-file');
  const count = await tree.count();
  for (const i of [40, 150, 260, 120, 207]) {
    const path = await tree.nth(Math.min(i, count - 1)).getAttribute('data-tree-path');
    // Attribute locator + click (which scrolls and retries on detach): tree
    // rows re-render while lazy files load, so a separate scroll step races.
    await page.locator(`.tree-file[data-tree-path="${path}"]`).click();
    await expect(page.locator('[id="file-section-' + path + '"]')).toBeInViewport();
  }
  // Earlier jump targets must not stay pinned (each would keep a full file
  // body and its row virtualizer alive for the rest of the session).
  await page.mouse.move(700, 400);
  await page.mouse.wheel(0, 200);
  await expect.poll(async () => (await fileListState(page)).pinned).toEqual([]);
  await expect.poll(() => page.locator('#filesContainer .file-section').count()).toBeLessThanOrEqual(25);
});
