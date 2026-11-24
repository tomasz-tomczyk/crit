import { test, expect, type Page } from '@playwright/test';
import { loadPage, clearAllComments, revealFile, diffLine } from './helpers';

// Find on the 300-file perf fixture. Files past the eager limit (25) are
// blank placeholders until they load, and Pierre's search only sees text
// already in an item, so Crit loads them while the panel is open (#1050).
// Each fileN.go adds a unique `GoodbyeN()` on the branch.
const FIND = process.platform === 'darwin' ? 'Meta+f' : 'Control+f';

const panel = (page: Page) => page.locator('[data-search-panel-overlay]');
const input = (page: Page) => panel(page).locator('input[data-search]');
const count = (page: Page) => panel(page).locator('[data-matches]');

// Path, line and text of the current-match highlight, or null.
function currentMatch(page: Page) {
  return page.evaluate(() => {
    const h = CSS.highlights.get('diffs-search-current');
    if (!h || h.size === 0) return null;
    const range = [...h][0] as Range;
    if (!range.startContainer.isConnected) return null;
    const host = (range.startContainer.getRootNode() as ShadowRoot).host as HTMLElement;
    const row = range.startContainer.parentElement?.closest('[data-line]') as HTMLElement | null;
    const rect = range.getBoundingClientRect();
    return { path: host.dataset.critPath, line: row?.dataset.line, text: range.toString(), visible: rect.height > 0 && rect.top >= 0 && rect.bottom <= window.innerHeight };
  });
}

async function openFind(page: Page) {
  await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
  await page.keyboard.press(FIND);
  await expect(input(page)).toBeVisible();
  await expect(input(page)).toBeFocused();
}

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
});

test('finds and reveals a match in a lazy, off-screen file', async ({ page }) => {
  // The list is in path order (file1, file10, file100, ...), so file40.go is
  // at index 235 and most lazy files load before the match is found. On a
  // slow runner that alone can take most of the default 30s.
  test.slow();
  await loadPage(page);
  await openFind(page);
  await input(page).fill('Goodbye40()');

  // Results that arrive after typing (lazy files load while the panel is
  // open) are counted; Enter goes to the first.
  await expect(count(page)).toHaveText('1 result', { timeout: 60_000 });
  await input(page).press('Enter');
  await expect(count(page)).toHaveText('1/1');
  await expect.poll(() => currentMatch(page), { timeout: 30_000 }).toMatchObject({
    path: 'file40.go', text: 'Goodbye40()', visible: true,
  });
});

test('prefills the panel from a selection inside the diff', async ({ page }) => {
  await loadPage(page);
  const item = await revealFile(page, 'file1.go');
  const row = diffLine(item, 13);
  // Select "Goodbye1" in `func Goodbye1() string {` (shadow DOM). Pierre
  // swaps the row's text nodes when highlighting lands, which drops a
  // selection made just before, so retry until it sticks.
  await expect(async () => {
    if (await panel(page).count()) await input(page).press('Escape');
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
