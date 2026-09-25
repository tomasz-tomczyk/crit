import { test, expect, type Page } from '@playwright/test';
import { loadPage, clearAllComments, fileHeader, fileItem, reviewScroller } from './helpers';

// A review tall enough that the file list passes 2^22px (~2,500 files).
// Pierre's CodeView owns the scroll container (#filesContainer) and
// virtualizes items, so this guards deep navigation at a height where
// GPU-composited Chrome has stopped painting and hit-testing before (a
// single-axis overflow clip on a tall ancestor blanked everything past the
// line; stylelint bans that CSS).
//
// That paint bug only reproduces with GPU compositing (headed Chrome);
// headless Chromium paints either way. CI runs these headless as functional
// deep-navigation and bounded-work checks. To exercise the paint assertions
// for real:  cd test/e2e && bash run.sh --project=huge --headed

const CHROME_BREAK_Y = 4_194_304; // 2^22

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
});

async function scrollState(page: Page) {
  return reviewScroller(page).evaluate(el => ({ top: el.scrollTop, height: el.scrollHeight }));
}

async function reviewOrder(page: Page): Promise<string[]> {
  return page.locator('.tree-file[data-tree-path]').evaluateAll(els =>
    els.map(el => (el as HTMLElement).dataset.treePath as string));
}

// Screenshot a region and count pixels that differ from its first pixel.
// Decoded in the page with a canvas, so no image dependency.
async function paintedFraction(page: Page, clip: { x: number; y: number; width: number; height: number }) {
  const png = await page.screenshot({ clip });
  return page.evaluate(async (b64) => {
    const img = await createImageBitmap(await (await fetch('data:image/png;base64,' + b64)).blob());
    const canvas = new OffscreenCanvas(img.width, img.height);
    const ctx = canvas.getContext('2d')!;
    ctx.drawImage(img, 0, 0);
    const { data } = ctx.getImageData(0, 0, img.width, img.height);
    const bg = [data[0], data[1], data[2]];
    let differs = 0;
    for (let i = 0; i < data.length; i += 4) {
      if (Math.abs(data[i] - bg[0]) + Math.abs(data[i + 1] - bg[1]) + Math.abs(data[i + 2] - bg[2]) > 24) differs++;
    }
    return differs / (data.length / 4);
  }, png.toString('base64'));
}

async function expectPainted(page: Page) {
  const content = await reviewScroller(page).boundingBox();
  const side = await page.locator('.file-tree-panel').boundingBox();
  expect(content && side).toBeTruthy();
  const contentPaint = await paintedFraction(page, { x: content!.x + 10, y: content!.y + 10, width: content!.width - 20, height: content!.height - 20 });
  const sidebarPaint = await paintedFraction(page, { x: side!.x, y: side!.y + 60, width: side!.width, height: Math.min(300, side!.height - 80) });
  expect(contentPaint, 'diff content is painted').toBeGreaterThan(0.02);
  expect(sidebarPaint, 'sidebar is painted').toBeGreaterThan(0.01);
}

// A point inside a Pierre item hit-tests to the item: elementFromPoint
// retargets shadow content to its host, light-DOM children are inside it.
async function expectItemHitTests(page: Page, path: string) {
  await expect.poll(() => fileItem(page, path).evaluate(el => {
    const r = el.getBoundingClientRect();
    const box = document.getElementById('filesContainer')!.getBoundingClientRect();
    const y = Math.max(r.top, box.top) + 40;
    const hit = document.elementFromPoint(r.left + Math.min(300, r.width / 2), y);
    return !!hit && (hit === el || el.contains(hit)) ? 'ok' : `hit ${hit ? hit.tagName + '.' + hit.className : 'nothing'} at y=${Math.round(y)}`;
  }), { message: `${path} hit-tests` }).toBe('ok');
  const side = await page.locator('.file-tree-panel').boundingBox();
  const sideHit = await page.evaluate(({ x, y }) => {
    const hit = document.elementFromPoint(x, y);
    return !!(hit && hit.closest('.file-tree-panel'));
  }, { x: side!.x + side!.width / 2, y: side!.y + side!.height / 2 });
  expect(sideHit, 'sidebar hit-tests').toBe(true);
}

// After a jump the file's header must stay where it landed while Pierre
// hydrates and measures the neighbours — no drift, no stick jitter.
async function expectSettledAt(page: Page, path: string) {
  const header = fileHeader(page, path);
  await expect(header).toBeInViewport();
  // Pierre can remount the item once as it measures; start sampling when
  // the header is back, then require it to hold.
  await expect.poll(async () => (await header.boundingBox()) !== null).toBe(true);
  const tops: number[] = [];
  for (let i = 0; i < 10; i++) {
    const box = await header.boundingBox();
    tops.push(box ? Math.round(box.y) : NaN);
    await page.waitForFunction(() => new Promise(r => setTimeout(() => r(true), 100)));
  }
  expect(Math.max(...tops) - Math.min(...tops), `header top samples ${tops.join(',')}`).toBeLessThanOrEqual(2);
}

async function mountedItems(page: Page) {
  return page.locator('#filesContainer diffs-container').count();
}

test('fixture is taller than the 2^22px line (guards the guard)', async ({ page }) => {
  await loadPage(page);
  await expect.poll(async () => (await scrollState(page)).height).toBeGreaterThan(CHROME_BREAK_Y + 200_000);
});

test('tree jump to the deepest files lands, settles, paints and hit-tests', async ({ page }) => {
  await loadPage(page);
  const order = await reviewOrder(page);
  expect(order.length).toBeGreaterThan(2000);
  for (const path of [order[order.length - 1], order[order.length - 40], order[Math.floor(order.length * 0.9)]]) {
    await page.locator(`.tree-file[data-tree-path="${path}"]`).click();
    await expectSettledAt(page, path);
    await expectPainted(page);
    await expectItemHitTests(page, path);
  }
  expect((await scrollState(page)).top).toBeGreaterThan(CHROME_BREAK_Y);
  // Free scroll afterwards: the jump does not pin the reader.
  const before = (await scrollState(page)).top;
  const box = await reviewScroller(page).boundingBox();
  await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2);
  await page.mouse.wheel(0, -600);
  await expect.poll(async () => (await scrollState(page)).top).toBeLessThan(before - 300);
});

test('scrolling to the bottom renders, paints and hit-tests', async ({ page }) => {
  await loadPage(page);
  // CodeView sizes the list on its first layout; scroll once it has.
  await expect.poll(async () => {
    await reviewScroller(page).evaluate(el => { el.scrollTop = el.scrollHeight; });
    return (await scrollState(page)).top;
  }).toBeGreaterThan(CHROME_BREAK_Y);
  const order = await reviewOrder(page);
  const last = order[order.length - 1];
  await expect(fileHeader(page, last)).toBeInViewport();
  await expectPainted(page);
  await expectItemHitTests(page, last);
});

test('deep navigation keeps mounted work bounded', async ({ page }) => {
  await loadPage(page);
  const order = await reviewOrder(page);
  const initial = await mountedItems(page);
  for (const path of [order[Math.floor(order.length / 2)], order[order.length - 1], order[10]]) {
    await page.locator(`.tree-file[data-tree-path="${path}"]`).click();
    await expect(fileHeader(page, path)).toBeInViewport();
  }
  const mounted = await mountedItems(page);
  const nodes = await page.evaluate(() => document.getElementsByTagName('*').length);
  console.log(`[huge] mounted items initial=${initial} after jumps=${mounted}; light DOM nodes=${nodes}`);
  // Virtualized: a handful of items around the viewport, not thousands.
  expect(mounted).toBeLessThanOrEqual(40);
  // The tree lists every file (2,500 rows); the diff surface must not add
  // per-file DOM on top of that.
  expect(nodes).toBeLessThan(60_000);
});

// The server's lazy flag only means "not loaded on the server yet". Once
// every file is warm, a reload must not fetch the whole review at once:
// thousands of concurrent requests fail (ERR_INSUFFICIENT_RESOURCES) and
// those files would render empty.
test('reload after the server has loaded every file keeps file requests bounded', async ({ page, request }) => {
  test.setTimeout(180_000);
  const session = await (await request.get('/api/session')).json();
  for (const f of session.files as { path: string }[]) {
    await request.get(`/api/file?path=${encodeURIComponent(f.path)}`);
  }
  const warmed = await (await request.get('/api/session')).json();
  expect(warmed.files.filter((f: { lazy?: boolean }) => f.lazy)).toHaveLength(0);

  let inFlight = 0;
  let maxInFlight = 0;
  let failed = 0;
  const isFileRequest = (url: string) => new URL(url).pathname.startsWith('/api/file');
  page.on('request', r => { if (isFileRequest(r.url())) maxInFlight = Math.max(maxInFlight, ++inFlight); });
  page.on('requestfinished', r => { if (isFileRequest(r.url())) inFlight--; });
  page.on('requestfailed', r => { if (isFileRequest(r.url())) { inFlight--; failed++; } });

  await loadPage(page);
  await expect(page.locator('#filesContainer diffs-container').first()).toBeVisible();
  const order = await reviewOrder(page);
  await page.locator(`.tree-file[data-tree-path="${order[order.length - 1]}"]`).click();
  await expect(fileHeader(page, order[order.length - 1])).toBeInViewport();

  console.log(`[huge] warm reload: max in-flight file requests ${maxInFlight}`);
  expect(maxInFlight).toBeLessThanOrEqual(100);
  expect(failed).toBe(0);
});

test('the off-canvas comments panel adds no horizontal page scroll', async ({ page }) => {
  await loadPage(page);
  await expect.poll(async () => {
    await reviewScroller(page).evaluate(el => { el.scrollTop = el.scrollHeight; });
    return (await scrollState(page)).top;
  }).toBeGreaterThan(CHROME_BREAK_Y);
  expect(await page.evaluate(() => {
    const se = document.documentElement;
    return se.scrollWidth <= se.clientWidth;
  })).toBe(true);
});
