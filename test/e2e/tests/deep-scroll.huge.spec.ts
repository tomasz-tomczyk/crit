import { test, expect, type Page } from '@playwright/test';
import { loadPage, clearAllComments, reviewFileOrder, fileListState } from './helpers';

// Reviews tall enough that the flat document passes 2^22px. A single-axis
// `overflow-x: clip` on .main-layout made GPU-composited Chrome stop painting
// and hit-testing everything below that line: the whole page under the header
// (sticky sidebar included) went white on deep navigation.
//
// The paint bug only reproduces with GPU compositing (headed Chrome); headless
// Chromium paints and hit-tests correctly either way, and no DOM API reports
// it. CI therefore guards the cause with a stylelint rule (single-axis overflow
// clip is banned) and runs these specs headless as functional deep-navigation
// checks. To exercise the paint assertions for real:
//   cd test/e2e && bash run.sh --project=huge --headed
// (verified: the old CSS fails there every run; the fix passes).

const CHROME_BREAK_Y = 4_194_304; // 2^22

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
});

// Screenshot a region and count pixels that differ from the page background.
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
  const vp = page.viewportSize()!;
  const side = await page.locator('.file-tree-panel').boundingBox();
  // Content column and sidebar separately: the bug blanked both.
  const content = await paintedFraction(page, { x: side!.x + side!.width + 10, y: 80, width: vp.width - side!.width - 20, height: vp.height - 100 });
  const sidebar = await paintedFraction(page, { x: side!.x, y: side!.y + 60, width: side!.width, height: Math.min(300, side!.height - 80) });
  expect(content, 'content below the header is painted').toBeGreaterThan(0.02);
  expect(sidebar, 'sticky sidebar is painted').toBeGreaterThan(0.01);
}

async function hitTestsInto(page: Page, selector: string, x: number, y: number) {
  return page.evaluate(({ selector, x, y }) => {
    const hit = document.elementFromPoint(x, y);
    return !!(hit && hit.closest(selector));
  }, { selector, x, y });
}

async function expectRendersAtDepth(page: Page, path: string) {
  const section = page.locator(`[id="file-section-${path}"]`);
  await expect(section).toBeInViewport();
  await expectPainted(page);
  // The section's own content answers hit-tests (not the layout behind it).
  await expect.poll(() => section.evaluate(el => {
    const r = el.getBoundingClientRect();
    const hit = document.elementFromPoint(r.left + Math.min(200, r.width / 2), Math.max(r.top, 0) + 20);
    return !!hit && el.contains(hit) ? 'ok' : `hit ${hit ? hit.tagName + '.' + hit.className : 'nothing'} at top=${Math.round(r.top)} y=${Math.round(window.scrollY)}`;
  }), { message: `${path} renders and hit-tests` }).toBe('ok');
  // Sticky sidebar paints/hit-tests too, not just the scrolled content.
  const side = await page.locator('.file-tree-panel').boundingBox();
  expect(side).toBeTruthy();
  expect(await hitTestsInto(page, '.file-tree-panel', side!.x + side!.width / 2, side!.y + side!.height / 2)).toBe(true);
}

test('fixture is taller than the 2^22px line (guards the guard)', async ({ page }) => {
  await loadPage(page);
  const height = await page.evaluate(() => document.documentElement.scrollHeight);
  expect(height).toBeGreaterThan(CHROME_BREAK_Y + 200_000);
});

test('tree jump to the deepest files renders and hit-tests below 2^22px', async ({ page }) => {
  await loadPage(page);
  const order = await reviewFileOrder(page);
  for (const path of [order[order.length - 1], order[order.length - 40], order[Math.floor(order.length * 0.9)]]) {
    // click() scrolls the row into view and retries if the tree re-renders.
    await page.locator(`.tree-file[data-tree-path="${path}"]`).click();
    await expectRendersAtDepth(page, path);
  }
  expect(await page.evaluate(() => window.scrollY)).toBeGreaterThan(CHROME_BREAK_Y);
  // Deep jumps must not leave pins behind either.
  await page.mouse.move(700, 400);
  await page.mouse.wheel(0, 100);
  await expect.poll(async () => (await fileListState(page)).pinned).toEqual([]);
});

test('scrolling to the bottom renders content and sidebar', async ({ page }) => {
  await loadPage(page);
  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBeGreaterThan(CHROME_BREAK_Y);
  const vp = page.viewportSize()!;
  await expect.poll(() => hitTestsInto(page, '.file-section', vp.width * 0.6, vp.height / 2)).toBe(true);
  await expectPainted(page);
  const side = await page.locator('.file-tree-panel').boundingBox();
  expect(await hitTestsInto(page, '.file-tree-panel', side!.x + side!.width / 2, side!.y + side!.height / 2)).toBe(true);
});

test('the off-canvas comments panel still adds no horizontal scroll', async ({ page }) => {
  // What the original overflow-x clip was for; both-axis clip must keep it.
  await loadPage(page);
  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  expect(await page.evaluate(() => {
    const se = document.documentElement;
    return se.scrollWidth <= se.clientWidth;
  })).toBe(true);
});
