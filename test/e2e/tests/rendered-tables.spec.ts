import { test, expect, type Locator, type Page } from '@playwright/test';
import {
  clearAllComments,
  focusKbNavElement,
  loadPage,
  mdSection,
  switchToDocumentView,
} from './helpers';

function decisionRow(page: Page, label: string): Locator {
  return mdSection(page).getByRole('cell', { name: label, exact: true }).locator('..');
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
    const table = mdSection(page).locator('table.native-table').first();
    await expect(table).toBeVisible();
    await expect(table.locator('thead tr.table-row')).toHaveCount(1);
    await expect(table.locator('tbody tr.table-row')).toHaveCount(3);
    await expect(table.locator('colgroup')).toHaveCount(0);

    const layout = await table.evaluate(element => getComputedStyle(element).tableLayout);
    expect(layout).toBe('auto');

    const wrapper = table.locator('..');
    const borderWidth = await wrapper.evaluate(element => getComputedStyle(element).borderTopWidth);
    expect(borderWidth).toBe('0px');

    const widths = await table.locator('thead th.line-content').evaluateAll(cells =>
      cells.map(cell => cell.getBoundingClientRect().width),
    );
    expect(new Set(widths.map(width => Math.round(width))).size).toBeGreaterThan(1);
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
    const optionsCell = mdSection(page).getByRole('cell', { name: 'OAuth, API keys, JWT', exact: true });
    await selectPhrase(optionsCell, 'API keys');
    await page.keyboard.press('c');

    await expect(page.locator('.comment-form textarea')).toBeFocused();
    await expect(mdSection(page).locator('mark.quote-highlight')).toHaveText('API keys');
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
    await expect(mdSection(page).locator('mark.quote-highlight')).toHaveCount(2);
    await expect(mdSection(page).locator('mark.quote-highlight').nth(0)).toHaveText('Auth method');
    await expect(mdSection(page).locator('mark.quote-highlight').nth(1)).toHaveText('OAuth');
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

  test('drag connector fills every selected table row without gaps', async ({ page }) => {
    const first = decisionRow(page, 'Auth method').locator('.line-comment-gutter');
    const last = decisionRow(page, 'Header format').locator('.line-comment-gutter');
    await first.scrollIntoViewIfNeeded();
    const firstBox = await first.boundingBox();
    const lastBox = await last.boundingBox();
    expect(firstBox).toBeTruthy();
    expect(lastBox).toBeTruthy();
    if (!firstBox || !lastBox) return;

    await page.mouse.move(firstBox.x + firstBox.width / 2, firstBox.y + 10);
    await page.mouse.down();
    await page.mouse.move(lastBox.x + lastBox.width / 2, lastBox.y + 10, { steps: 5 });

    const segments = await mdSection(page).locator('.native-table .line-comment-gutter.drag-range')
      .evaluateAll(gutters => gutters.map(gutter => {
        const rect = gutter.getBoundingClientRect();
        return { top: rect.top, bottom: rect.bottom, height: rect.height };
      }));
    expect(segments).toHaveLength(3);
    for (let index = 0; index < segments.length - 1; index++) {
      expect(Math.abs(segments[index].bottom - segments[index + 1].top)).toBeLessThanOrEqual(0.5);
      expect(segments[index].height).toBeGreaterThan(20);
    }

    await page.mouse.up();
    await expect(page.locator('.comment-form')).toBeVisible();
    await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  });

  test('keyboard commenting and submitted comments stay anchored to a table row', async ({ page }) => {
    let row = decisionRow(page, 'Key storage');
    await focusKbNavElement(page, row);
    await page.keyboard.press('c');
    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeFocused();
    await textarea.fill('Table row comment');
    await textarea.press('Control+Enter');

    row = decisionRow(page, 'Key storage');
    const annotation = row.locator('xpath=following-sibling::tr[1]');
    await expect(annotation).toHaveClass(/native-table-annotation/);
    await expect(annotation.locator('.comment-card')).toContainText('Table row comment');
  });
});
