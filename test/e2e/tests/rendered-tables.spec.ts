import { test, expect, type Locator, type Page } from '@playwright/test';
import {
  clearAllComments, clearFocus, loadPage, switchToDocumentView, mdDocument,
  waitUntilHittable, pressUntil,
} from './helpers';

function decisionRow(page: Page, label: string): Locator {
  return mdDocument(page).getByRole('cell', { name: label, exact: true }).locator('..');
}

async function selectPhrase(cell: Locator, phrase: string) {
  await cell.evaluate((element, selectedPhrase) => {
    const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
    let node;
    while ((node = walker.nextNode())) {
      const start = node.textContent?.indexOf(selectedPhrase) ?? -1;
      if (start === -1) continue;
      const range = document.createRange();
      range.setStart(node, start);
      range.setEnd(node, start + selectedPhrase.length);
      const selection = window.getSelection();
      selection?.removeAllRanges();
      selection?.addRange(range);
      return;
    }
    throw new Error(`Phrase not found: ${selectedPhrase}`);
  }, phrase);
}

test.describe('Native rendered tables', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToDocumentView(page);
  });

  test('uses one auto-layout table without generated column widths or outer border', async ({ page }) => {
    const table = mdDocument(page).locator('table.native-table').first();
    await expect(table).toBeVisible();
    await expect(table.locator('thead tr.table-row')).toHaveCount(1);
    await expect(table.locator('tbody tr.table-row')).toHaveCount(3);
    await expect(table.locator('colgroup')).toHaveCount(0);

    const layout = await table.evaluate(element => getComputedStyle(element).tableLayout);
    expect(layout).toBe('auto');
    const wrap = await table.locator('thead th.line-content').first()
      .evaluate(element => getComputedStyle(element).overflowWrap);
    expect(wrap).toBe('break-word');

    const wrapper = table.locator('..');
    const borderWidth = await wrapper.evaluate(element => getComputedStyle(element).borderTopWidth);
    expect(borderWidth).toBe('0px');

    const widths = await table.locator('thead th.line-content').evaluateAll(cells =>
      cells.map(cell => cell.getBoundingClientRect().width),
    );
    expect(new Set(widths.map(width => Math.round(width))).size).toBeGreaterThan(1);
  });

  test('wrapped code lines do not let table columns split words', async ({ page }) => {
    await page.getByRole('button', { name: 'Settings', exact: true }).click();
    await page.locator('#codeOverflowSelect').selectOption('wrap');
    await page.keyboard.press('Escape');
    const table = mdDocument(page).locator('table.native-table').first();
    await expect(table).toBeVisible();
    // Pierre's wrap mode sets word-break: break-word on its host; inherited,
    // it lets auto-layout columns shrink below a word ("Ty|pe").
    await expect(table.locator('td').first()).toHaveCSS('word-break', 'normal');
    // Every word sits on one line: a word split across lines has two rects.
    const split = await table.evaluate(element => {
      const out: string[] = [];
      const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
      let node;
      while ((node = walker.nextNode())) {
        const text = node.textContent || '';
        for (const m of text.matchAll(/\S+/g)) {
          const range = document.createRange();
          range.setStart(node, m.index!);
          range.setEnd(node, m.index! + m[0].length);
          if (range.getClientRects().length > 1) out.push(m[0]);
        }
      }
      return out;
    });
    expect(split).toEqual([]);
  });

  test('table-row comment forms cancel with both button and Escape', async ({ page }) => {
    let row = decisionRow(page, 'Auth method');
    await row.hover();
    await row.locator('.line-comment-gutter').click();
    await expect(page.locator('.comment-form')).toBeVisible();
    await page.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(page.locator('.comment-form')).toHaveCount(0);

    row = decisionRow(page, 'Auth method');
    await row.hover();
    await row.locator('.line-comment-gutter').click();
    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeFocused();
    await textarea.press('Escape');
    await expect(page.locator('.comment-form')).toHaveCount(0);
  });

  test('selected phrases in any table cell are highlighted when commenting', async ({ page }) => {
    const optionsCell = mdDocument(page).getByRole('cell', { name: 'OAuth, API keys, JWT', exact: true });
    await selectPhrase(optionsCell, 'API keys');
    await page.keyboard.press('c');

    await expect(page.locator('.comment-form textarea')).toBeFocused();
    await expect(mdDocument(page).locator('mark.quote-highlight')).toHaveText('API keys');
    await page.getByRole('button', { name: 'Cancel', exact: true }).click();

    const row = decisionRow(page, 'Auth method');
    await row.evaluate(element => {
      const cells = element.querySelectorAll('.line-content');
      const first = cells[0].firstChild;
      const second = cells[1].firstChild;
      if (!first || !second) throw new Error('Expected text in adjacent table cells');
      const range = document.createRange();
      range.setStart(first, 0);
      range.setEnd(second, 5);
      const selection = window.getSelection();
      selection?.removeAllRanges();
      selection?.addRange(range);
    });
    await page.keyboard.press('c');
    await expect(mdDocument(page).locator('mark.quote-highlight')).toHaveCount(2);
    await expect(mdDocument(page).locator('mark.quote-highlight').nth(0)).toHaveText('Auth method');
    await expect(mdDocument(page).locator('mark.quote-highlight').nth(1)).toHaveText('OAuth');
  });

  test('row stripes and interaction backgrounds do not shift around annotations', async ({ page }) => {
    const evenRow = decisionRow(page, 'Key storage');
    const oddRow = decisionRow(page, 'Header format');
    await expect(evenRow).toHaveClass(/table-even/);
    await expect(oddRow).not.toHaveClass(/table-even/);
    const before = await Promise.all([
      evenRow.locator('td.line-content').first().evaluate(cell => getComputedStyle(cell).backgroundColor),
      oddRow.locator('td.line-content').first().evaluate(cell => getComputedStyle(cell).backgroundColor),
    ]);
    expect(before[0]).not.toBe(before[1]);

    const firstRow = decisionRow(page, 'Auth method');
    await firstRow.locator('.line-comment-gutter').click();
    const after = await Promise.all([
      decisionRow(page, 'Key storage').locator('td.line-content').first().evaluate(cell => getComputedStyle(cell).backgroundColor),
      decisionRow(page, 'Header format').locator('td.line-content').first().evaluate(cell => getComputedStyle(cell).backgroundColor),
    ]);
    expect(after).toEqual(before);
    await expect(decisionRow(page, 'Auth method')).toHaveClass(/selected|form-selected/);
  });

  test('drag highlights every selected table row with one endpoint utility and no bracket', async ({ page }) => {
    const first = decisionRow(page, 'Auth method').locator('.line-comment-gutter');
    const last = decisionRow(page, 'Header format').locator('.line-comment-gutter');
    // Center the middle row so all three rows are on screen, then wait for
    // pointer events to resume before pressing.
    const middle = decisionRow(page, 'Key storage').locator('.line-comment-gutter');
    await middle.evaluate(el => el.scrollIntoView({ block: 'center' }));
    await waitUntilHittable(middle);
    const firstBox = await first.boundingBox();
    const lastBox = await last.boundingBox();
    expect(firstBox).toBeTruthy();
    expect(lastBox).toBeTruthy();
    if (!firstBox || !lastBox) return;

    await page.mouse.move(firstBox.x + firstBox.width / 2, firstBox.y + 10);
    await page.mouse.down();
    await page.mouse.move(lastBox.x + lastBox.width / 2, lastBox.y + 10, { steps: 5 });

    const selected = mdDocument(page).locator('.native-table .line-block.selected');
    await expect(selected).toHaveCount(3);
    await expect(selected.locator('.line-comment-gutter.drag-endpoint')).toHaveCount(1);
    await expect(last).toHaveClass(/drag-endpoint/);
    expect(await last.evaluate(el => getComputedStyle(el, '::after').content)).toBe('none');

    await page.mouse.up();
    await expect(page.locator('.comment-form')).toBeVisible();
    await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  });

  test('keyboard commenting and submitted comments stay anchored to a table row', async ({ page }) => {
    // j/k focus spans every file's rows in order; walk it to the table row.
    await clearFocus(page);
    await pressUntil(page, 'j', () => page.evaluate(text => {
      const focused = document.querySelector('[id="file-section-plan.md"] .line-block.focused');
      return !!focused && !!Array.from(focused.querySelectorAll('td, th')).find(c => c.textContent?.trim() === text);
    }, 'Key storage'), { max: 1000 });
    await expect(decisionRow(page, 'Key storage')).toHaveClass(/focused/);
    await page.keyboard.press('c');
    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeFocused();
    await textarea.fill('Table row comment');
    await textarea.press('Control+Enter');

    const row = decisionRow(page, 'Key storage');
    const annotation = row.locator('xpath=following-sibling::tr[1]');
    await expect(annotation).toHaveClass(/native-table-annotation/);
    await expect(annotation.locator('.comment-card')).toContainText('Table row comment');
  });
});
