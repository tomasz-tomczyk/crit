import { test, expect, type Page } from '@playwright/test';
import { loadPage, clearAllComments, fileHeader, fileItem } from './helpers';

// Perf guardrails for large reviews (300 files / ~9k changed lines fixture).
//
// These assert structural invariants — mounted Pierre items, rendered diff
// rows, DOM size (shadow roots included), lazy file loads, and
// longtask-blocked main thread — rather than wall-clock time, so they only
// fail on real regressions (e.g. mounting or loading every file eagerly, the
// bug class fixed in 27f33c8) and not on runner noise. Wall clock is logged,
// not gated.
//
// How the Pierre surface behaves: every file is listed in the tree, but the
// diff list is a @pierre/diffs CodeView that virtualizes — only items near
// the viewport of #filesContainer have DOM (a <diffs-container> whose code
// rows live in its shadow root). Files past the eager set (lazyFileThreshold
// = 25) start as line-count stubs and hydrate (server round trip) only when
// CodeView renders them. So mounted work tracks the viewport, not the review
// size, and file loads track how far the reader has gone.
//
// Reference measurements (idle machine, 1280x720, 124k px list):
//   load:  3 items / 33 rows / ~3.4k nodes (incl. shadow) / 25 files loaded / TBT ~40ms
//   dwell at 12k/24k/36k px: ≤7 items / ≤84 rows / ~3.8k nodes / 40 files loaded / TBT ~0ms
//   6x CPU throttle: same counts, load TBT ~700ms, scroll TBT ~70ms
// A mount-everything regression would show 301 items and tens of thousands
// of rows/nodes; a load-everything regression would request all 301 files.

const TOTAL_FILES = 301;

// plan-big.md is the largest file and sorts last, so it sits at the bottom of
// the review — the reader in the scroll test never gets near it.
const TAIL_FILE = 'plan-big.md';

// Budgets (measured values above, with headroom for slow runners).
const MAX_MOUNTED_ITEMS = 15;
const MAX_RENDERED_ROWS = 400;
const MAX_DOM_NODES = 12_000;
const MAX_TBT_MS = 4_000;
const MAX_FILES_LOADED_AFTER_SCROLL = 100;

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

// Record which files the page fetched (/api/file, /api/file/diff, ...).
function trackFileLoads(page: Page): Set<string> {
  const paths = new Set<string>();
  page.on('request', (r) => {
    const url = new URL(r.url());
    const p = url.searchParams.get('path');
    if (url.pathname.startsWith('/api/file') && p) paths.add(p);
  });
  return paths;
}

type Work = { items: number; rows: number; nodes: number };

// Mounted Pierre items, rendered diff rows, and DOM nodes including every
// open shadow root (CodeView's code rows live in shadow DOM).
async function mountedWork(page: Page): Promise<Work> {
  return page.evaluate(() => {
    let nodes = 0;
    let rows = 0;
    const walk = (root: Document | ShadowRoot) => {
      const all = root.querySelectorAll('*');
      nodes += all.length;
      for (const el of all) {
        if (el.shadowRoot) {
          rows += el.shadowRoot.querySelectorAll('[data-line]').length;
          walk(el.shadowRoot);
        }
      }
    };
    walk(document);
    return { items: document.querySelectorAll('#filesContainer diffs-container').length, rows, nodes };
  });
}

// Wait until the list's scroll offset/height and mounted item count hold
// still for a few frames (CodeView re-render, stub hydration re-measure).
async function waitForListStable(page: Page) {
  await page.locator('#filesContainer').evaluate((el) => new Promise<void>((resolve) => {
    let last = '';
    let stable = 0;
    const check = () => {
      const now = `${el.scrollTop}:${el.scrollHeight}:${el.querySelectorAll('diffs-container').length}`;
      if (now === last) {
        if (++stable >= 10) return resolve();
      } else {
        stable = 0;
        last = now;
      }
      requestAnimationFrame(check);
    };
    requestAnimationFrame(check);
  }));
}

function expectBounded(work: Work) {
  expect(work.items).toBeLessThanOrEqual(MAX_MOUNTED_ITEMS);
  expect(work.rows).toBeLessThanOrEqual(MAX_RENDERED_ROWS);
  expect(work.nodes).toBeLessThan(MAX_DOM_NODES);
}

test('large review initial render stays within DOM and longtask budgets', async ({ page }) => {
  await installLongtaskObserver(page);
  const loaded = trackFileLoads(page);
  await loadPage(page);

  // Every file is listed; the diff list shows real rows.
  await expect(page.locator('.tree-file')).toHaveCount(TOTAL_FILES);
  await expect(page.locator('#filesContainer diffs-container [data-line]').first()).toBeVisible();
  await waitForListStable(page);

  const work = await mountedWork(page);
  const tbt = await longtaskTBT(page);
  console.log(`initial render: items=${work.items} rows=${work.rows} domNodes=${work.nodes} filesLoaded=${loaded.size} longtaskTBT=${Math.round(tbt)}ms`);

  // Only the viewport's files are mounted, not the 301 in the review.
  expectBounded(work);
  // Lazy files past the eager set are not fetched before the reader
  // scrolls anywhere; the tail file is certainly not.
  expect(loaded.size).toBeLessThanOrEqual(30);
  expect(loaded.has(TAIL_FILE)).toBe(false);
  expect(tbt).toBeLessThan(MAX_TBT_MS);
});

// Stops a reader passes through, past the eager set (~25 files ≈ 10k px at
// 1280x720) so on-demand hydration runs, and far above the ~124k px tail.
const DWELL_OFFSETS = [12_000, 24_000, 36_000];

test('scrolling a large review mounts bodies on demand and leaves the tail deferred', async ({ page }) => {
  await installLongtaskObserver(page);
  const loaded = trackFileLoads(page);
  await loadPage(page);
  await expect(page.locator('#filesContainer diffs-container [data-line]').first()).toBeVisible();
  await waitForListStable(page);

  const tbtBefore = await longtaskTBT(page);
  const loadedBefore = loaded.size;
  const wallStart = Date.now();

  // Dwell at each offset the way a reader would, letting CodeView render and
  // hydrate before moving on. Mounted work is bounded at every stop.
  let maxWork: Work = { items: 0, rows: 0, nodes: 0 };
  for (const y of DWELL_OFFSETS) {
    await page.locator('#filesContainer').evaluate((el, offset) => { el.scrollTop = offset; }, y);
    await waitForListStable(page);
    // CodeView overscans neighbouring items, which can still be hidden loading
    // stubs. Require real hydrated code at this stop rather than the first
    // synthetic row in DOM order.
    await expect.poll(() => page.locator('#filesContainer diffs-container:not([data-crit-stub]) [data-line]')
      .filter({ hasText: /\S/ }).evaluateAll(lines => {
        const viewport = document.getElementById('filesContainer')!.getBoundingClientRect();
        return lines.some(line => {
          const box = line.getBoundingClientRect();
          return box.height > 0 && box.bottom > viewport.top && box.top < viewport.bottom;
        });
      })).toBe(true);
    const work = await mountedWork(page);
    console.log(`scroll: y=${y} items=${work.items} rows=${work.rows} domNodes=${work.nodes} filesLoaded=${loaded.size}`);
    expectBounded(work);
    maxWork = {
      items: Math.max(maxWork.items, work.items),
      rows: Math.max(maxWork.rows, work.rows),
      nodes: Math.max(maxWork.nodes, work.nodes),
    };
  }

  const wallMs = Date.now() - wallStart;
  const scrollTBT = (await longtaskTBT(page)) - tbtBefore;
  console.log(`scroll: wall=${wallMs}ms scrollTBT=${Math.round(scrollTBT)}ms maxItems=${maxWork.items} maxRows=${maxWork.rows} maxDomNodes=${maxWork.nodes} filesLoaded=${loadedBefore}->${loaded.size}`);

  // Loading only reached the files the reader scrolled past — a fraction of
  // the 301, not all of them.
  expect(loaded.size).toBeLessThan(MAX_FILES_LOADED_AFTER_SCROLL);
  expect(scrollTBT).toBeLessThan(MAX_TBT_MS);

  // Nothing touched the bottom of the review: the largest file in the
  // fixture was never fetched and is not mounted.
  expect(loaded.has(TAIL_FILE)).toBe(false);
  await expect(fileHeader(page, TAIL_FILE)).toHaveCount(0);
});

// A tree jump to the far end of the review lands on the file and stays put
// while its lazy diff hydrates and CodeView re-measures around it.
test('tree jump to the deepest file lands on it and stays put while content settles', async ({ page }) => {
  await loadPage(page);
  await expect(page.locator('#filesContainer diffs-container [data-line]').first()).toBeVisible();

  await page.locator(`.tree-file[data-tree-path="${TAIL_FILE}"]`).click();

  const header = fileHeader(page, TAIL_FILE);
  await expect(header).toBeInViewport();
  // Hydrated: the real markdown diff, not the blank line-count stub.
  await expect(fileItem(page, TAIL_FILE).locator('[data-line]').filter({ hasText: /\S/ }).first()).toBeVisible();

  // Header sits at the top of the list and does not drift over ~1s.
  const tops: number[] = [];
  for (let i = 0; i < 10; i++) {
    const box = await header.boundingBox();
    tops.push(box ? Math.round(box.y) : NaN);
    await page.waitForFunction(() => new Promise((r) => setTimeout(() => r(true), 100)));
  }
  console.log(`tree jump: header top samples ${tops.join(',')}`);
  expect(Math.max(...tops) - Math.min(...tops), `header top samples ${tops.join(',')}`).toBeLessThanOrEqual(2);
  const pane = (await page.locator('#filesContainer').boundingBox())!;
  expect(tops[tops.length - 1]).toBeLessThan(pane.y + 60);

  const work = await mountedWork(page);
  console.log(`tree jump: items=${work.items} rows=${work.rows} domNodes=${work.nodes}`);
  expectBounded(work);
});
