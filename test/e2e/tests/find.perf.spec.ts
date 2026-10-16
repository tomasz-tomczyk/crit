import { test, expect, type Page } from '@playwright/test';
import { loadPage, clearAllComments } from './helpers';

// Find in diff (#1050). The review list is virtualized and files past the
// eager limit (25) load lazily, so the browser's own find misses most of the
// 300-file perf fixture. Each fileN.go adds a unique `GoodbyeN()` on the
// branch.
const FIND = process.platform === 'darwin' ? 'Meta+f' : 'Control+f';

const bar = (page: Page) => page.locator('.crit-find-bar');
const input = (page: Page) => page.locator('.crit-find-input');
const count = (page: Page) => page.locator('.crit-find-count');

// Path, line and text of the current-match highlight, or null.
function currentMatch(page: Page) {
  return page.evaluate(() => {
    const h = CSS.highlights.get('crit-find-current');
    if (!h) return null;
    const range = [...h][0] as Range;
    if (!range.startContainer.isConnected) return null;
    const host = (range.startContainer.getRootNode() as ShadowRoot).host as HTMLElement;
    const row = range.startContainer.parentElement?.closest('[data-line]') as HTMLElement | null;
    const rect = range.getBoundingClientRect();
    return { path: host.dataset.critPath, line: row?.dataset.line, text: range.toString(), visible: rect.height > 0 && rect.top >= 0 && rect.bottom <= window.innerHeight };
  });
}

async function openFind(page: Page) {
  await page.locator('#filesContainer').click({ position: { x: 5, y: 5 } });
  await page.keyboard.press(FIND);
  await expect(bar(page)).toBeVisible();
  await expect(input(page)).toBeFocused();
}

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
});

test('finds and reveals a match in a lazy, off-screen file', async ({ page }) => {
  await loadPage(page);
  await openFind(page);
  await input(page).fill('Goodbye287()');

  await expect(count(page)).toHaveText('1 of 1', { timeout: 30_000 });
  await expect.poll(() => currentMatch(page), { timeout: 10_000 }).toMatchObject({
    path: 'file287.go', text: 'Goodbye287()', visible: true,
  });
  await expect(page.locator('.tree-file[data-tree-path="file287.go"]')).toHaveClass(/active/);
});

test('Esc closes the bar, clears highlights and stays put', async ({ page }) => {
  await loadPage(page);
  await openFind(page);
  await input(page).fill('Goodbye3()');
  await expect(count(page)).toHaveText('1 of 1', { timeout: 30_000 });

  // Typing then Esc inside the debounce must not jump the list afterwards.
  await input(page).fill('Goodbye290(');
  await page.keyboard.press('Escape');
  await expect(bar(page)).toBeHidden();
  // Watch the list for longer than the input debounce (120ms).
  const scrolled = await page.locator('#filesContainer').evaluate((el) => new Promise<boolean>((resolve) => {
    let moved = false;
    el.addEventListener('scroll', () => { moved = true; });
    setTimeout(() => resolve(moved), 500);
  }));
  expect(scrolled).toBe(false);
  expect(await page.evaluate(() => CSS.highlights.has('crit-find') || CSS.highlights.has('crit-find-current'))).toBe(false);
});

test('collapsed files are not searched', async ({ page }) => {
  await loadPage(page);
  await openFind(page);
  await input(page).fill('Goodbye1()');
  await expect(count(page)).toHaveText('1 of 1', { timeout: 30_000 });
  await page.keyboard.press('Escape');

  await page.locator('.pierre-file-header[data-file-path="file1.go"] .file-header-chevron').click();
  await openFind(page);
  await expect(count(page)).toHaveText('No results', { timeout: 30_000 });
});
