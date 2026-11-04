import { test, expect, type Page } from '@playwright/test';
import { loadPage, clearAllComments, revealFile, diffLine } from './helpers';

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

test.beforeEach(async ({ page, request }) => {
  await clearAllComments(request);
  // A slow runner can miss the 5s worker start limit, and the list rebuilt by
  // that fallback can lose a find jump. Log it, so a failure here shows whether
  // the fallback ran and when.
  const start = Date.now();
  page.on('console', (m) => {
    if (m.text().includes('Highlight workers unavailable')) {
      console.log(`highlight worker fallback ${Date.now() - start}ms into: ${test.info().title}`);
    }
  });
});

test('finds and reveals a match in a lazy, off-screen file', async ({ page }) => {
  // The list is in path order (file1, file10, file100, ...), so file40.go is
  // at index 235 and find loads most lazy files before it reaches the match.
  // On a slow runner that alone can take most of the default 30s.
  test.slow();
  await loadPage(page);
  await openFind(page);
  await input(page).fill('Goodbye40()');

  // Wait for the jump, not for a plain "1 of 1": that only shows once every
  // lazy file has been searched ("1 of 1…" until then).
  await expect.poll(() => currentMatch(page), { timeout: 60_000 }).toMatchObject({
    path: 'file40.go', text: 'Goodbye40()', visible: true,
  });
  await expect(count(page)).toHaveText(/^1 of 1/);
  await expect(page.locator('.tree-file[data-tree-path="file40.go"]')).toHaveClass(/active/);
});

test('a match found while the highlight workers start stays on screen when they fail', async ({ page }) => {
  // Never answer the worker script, so the workers miss their 5s start limit
  // and the page falls back to rendering without them, building a new list.
  // Track fallback in-page so the jump-vs-fallback race is checked atomically
  // (a Node-side flag read after toHaveText can miss a jump that already happened).
  await page.addInitScript(() => {
    const w = window as unknown as { __critWorkerFellBack?: boolean; __critJumpedBeforeFallback?: boolean };
    w.__critWorkerFellBack = false;
    w.__critJumpedBeforeFallback = false;
    const warn = console.warn.bind(console);
    console.warn = (...args: unknown[]) => {
      if (String(args[0] ?? '').includes('Highlight workers unavailable')) {
        // Snapshot the count in the same turn as the fallback warn — a later
        // Playwright poll can miss a jump that already happened on the old list.
        const text = document.querySelector('.crit-find-count')?.textContent ?? '';
        if (/^1 of 1/.test(text)) w.__critJumpedBeforeFallback = true;
        w.__critWorkerFellBack = true;
      }
      warn(...args);
    };
  });
  await page.route('**/pierre/pierre-worker.js', () => {});
  await loadPage(page);
  await openFind(page);
  await input(page).fill('Goodbye130()');
  // Wait for the fallback. Skip when find had not jumped yet — the jump would
  // land on the new list and there is nothing to test.
  await expect.poll(
    () => page.evaluate(() => !!(window as unknown as { __critWorkerFellBack?: boolean }).__critWorkerFellBack),
    { timeout: 30_000 },
  ).toBe(true);
  const jumpedBefore = await page.evaluate(
    () => !!(window as unknown as { __critJumpedBeforeFallback?: boolean }).__critJumpedBeforeFallback,
  );
  test.skip(!jumpedBefore, 'the fallback came before the match was found');

  await expect.poll(() => currentMatch(page), { timeout: 10_000 }).toMatchObject({
    path: 'file130.go', text: 'Goodbye130()', visible: true,
  });
});

test('keeps the found file active in the tree until it scrolls out of view', async ({ page }) => {
  const treeRow = (path: string) => page.locator(`.tree-file[data-tree-path="${path}"]`);
  const scrollList = (by: number) => page.locator('#filesContainer').evaluate(async (el, by) => {
    el.scrollTop += by;
    // The scroll event and the tree sync it queues each take a frame.
    for (let i = 0; i < 3; i++) await new Promise(requestAnimationFrame);
  }, by);

  await loadPage(page);
  await openFind(page);
  // file125.go is lazy and off screen, but the first lazy files load first.
  // A match deeper in the list waits for ~180 lazy files to load, which can
  // outrun the poll on a slow runner.
  await input(page).fill('Goodbye125()');
  await expect.poll(() => currentMatch(page), { timeout: 30_000 }).toMatchObject({ path: 'file125.go', visible: true });
  await expect(treeRow('file125.go')).toHaveClass(/active/);

  // The centered match leaves file124.go at the top of the list. A scroll
  // after the tree's 400ms post-jump window (on a slow runner, the jump's
  // own) must not hand the tree to it.
  await page.waitForTimeout(500);
  await scrollList(1);
  await expect(treeRow('file125.go')).toHaveClass(/active/);

  await scrollList(2000);
  await expect(treeRow('file125.go')).not.toHaveClass(/active/);
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

test('prefills the bar from a selection inside the diff', async ({ page }) => {
  await loadPage(page);
  const item = await revealFile(page, 'file1.go');
  const row = diffLine(item, 13);
  // Select "Goodbye1" in `func Goodbye1() string {` (shadow DOM; Chromium
  // reports such a selection as collapsed on window.getSelection()). Pierre
  // swaps the row's text nodes when highlighting lands, which drops a
  // selection made just before, so retry until it sticks.
  await expect(async () => {
    if (await bar(page).isVisible()) await page.keyboard.press('Escape');
    await row.evaluate((el) => {
      const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
      let text = '';
      const nodes: { node: Text; start: number }[] = [];
      let n: Node | null;
      while ((n = walker.nextNode())) { nodes.push({ node: n as Text, start: text.length }); text += n.textContent; }
      const at = text.indexOf('Goodbye1');
      const find = (pos: number) => nodes.filter((x) => x.start <= pos).pop()!;
      const a = find(at);
      const b = find(at + 'Goodbye1'.length);
      window.getSelection()!.setBaseAndExtent(a.node, at - a.start, b.node, at + 'Goodbye1'.length - b.start);
    });
    await page.keyboard.press(FIND);
    await expect(input(page)).toHaveValue('Goodbye1', { timeout: 1_000 });
  }).toPass({ timeout: 15_000 });
});
