import { test, expect, type APIRequestContext } from '@playwright/test';
import {
  clearAllComments, getMdPath, loadPage, switchToDocumentView, recordClassAdditions, expectClassAddedTo, classAdditions,
  goSection, selectInLineAndPressC, expectPaintedQuote, quotePoint, quoteFlash,
} from './helpers';

// A comment on selected text and its highlighted quote link to each other, in
// the rendered document and in code view. plan.md line 5 contains "API key
// authentication".
const FLASH = 'comment-ref-flash';

async function seed(request: APIRequestContext, path: string, line: number, body: string, quote?: string) {
  const res = await request.post(`/api/file/comments?path=${encodeURIComponent(path)}`, {
    data: { start_line: line, end_line: line, body, quote },
  });
  expect(res.ok()).toBeTruthy();
  return (await res.json()) as { id: string };
}

test.describe('Quoted text and its comment', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('the highlight goes to its comment, and the comment label goes back to the text', async ({ page, request }) => {
    const path = await getMdPath(request);
    const quoted = await seed(request, path, 5, 'Which keys?', 'API key authentication');
    const plain = await seed(request, path, 3, 'Whole-line comment');
    await loadPage(page);
    const doc = await switchToDocumentView(page);
    await recordClassAdditions(page, FLASH);

    const mark = doc.locator(`mark.quote-highlight[data-comment-id="${quoted.id}"]`);
    const card = doc.locator(`.comment-card[data-comment-id="${quoted.id}"]`);
    await expectClassAddedTo(page, FLASH, () => mark.click(), quoted.id);
    await expect(card).toBeInViewport();

    const label = card.locator('.comment-line-ref');
    await expect(label).toHaveAttribute('role', 'link');
    await expectClassAddedTo(page, FLASH, () => label.click(), quoted.id);
    await expect(mark).toBeInViewport();

    await label.focus();
    await expectClassAddedTo(page, FLASH, () => page.keyboard.press('Enter'), quoted.id);

    // A whole-line comment sits under its line already: its label is plain text.
    await expect(doc.locator(`.comment-card[data-comment-id="${plain.id}"] .comment-line-ref`)).not.toHaveAttribute('role', 'link');
  });

  test('selecting text inside a highlight (drag or double-click) does not jump to the comment', async ({ page, request }) => {
    const path = await getMdPath(request);
    const quoted = await seed(request, path, 5, 'Which keys?', 'API key authentication');
    await loadPage(page);
    const doc = await switchToDocumentView(page);
    await recordClassAdditions(page, FLASH);

    const mark = doc.locator(`mark.quote-highlight[data-comment-id="${quoted.id}"]`);
    const box = (await mark.boundingBox())!;
    await page.mouse.move(box.x + 2, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width - 2, box.y + box.height / 2, { steps: 5 });
    await page.mouse.up();
    expect(await page.evaluate(() => window.getSelection()!.toString().length)).toBeGreaterThan(0);
    await page.evaluate(() => window.getSelection()!.removeAllRanges());
    await mark.dblclick();
    expect(await page.evaluate(() => window.getSelection()!.toString().length)).toBeGreaterThan(0);

    // A jump flashes the card some time after the click (it may scroll the
    // list first), so wait out that window before checking nothing flashed.
    // This checks that something does not happen, so there is no state to
    // wait on instead.
    await page.waitForTimeout(1500);
    expect(await classAdditions(page, FLASH)).toEqual([]);
  });

  test('a resolved comment has no highlight, so its label is not a link', async ({ page, request }) => {
    const path = await getMdPath(request);
    const quoted = await seed(request, path, 5, 'Which keys?', 'API key authentication');
    const res = await request.put(`/api/comment/${quoted.id}/resolve?path=${encodeURIComponent(path)}`, {
      data: { resolved: true },
    });
    expect(res.ok()).toBeTruthy();
    await loadPage(page);
    const doc = await switchToDocumentView(page);

    const card = doc.locator(`.comment-card[data-comment-id="${quoted.id}"]`);
    await expect(card).toBeAttached();
    await expect(doc.locator(`.quote-highlight[data-comment-id="${quoted.id}"]`)).toHaveCount(0);
    await expect(card.locator('.comment-line-ref')).not.toHaveAttribute('role', 'link');
  });

  test('in code view, the quote goes to its comment and the label goes back to the quote', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);
    const selected = (await selectInLineAndPressC(page, item, 24)).trim();
    const textarea = item.locator('.comment-form textarea');
    await textarea.fill('Code quote');
    await textarea.press('Control+Enter');
    const card = item.locator('.comment-card', { hasText: 'Code quote' });
    await expect(card).toBeVisible();
    const id = (await card.getAttribute('data-comment-id'))!;
    await expectPaintedQuote(page);
    await recordClassAdditions(page, FLASH);

    // Pierre paints the quote as a range, so the pointer cursor comes from
    // the host while the quote is under the mouse.
    const point = await quotePoint(page, selected);
    await page.mouse.move(point.x, point.y);
    await expect(item).toHaveAttribute('data-crit-quote-hover', '');
    await expectClassAddedTo(page, FLASH, () => page.mouse.click(point.x, point.y), id);

    const label = card.locator('.comment-line-ref');
    await expect(label).toHaveAttribute('role', 'link');
    await label.click();
    await expect.poll(() => quoteFlash(page)).toEqual([selected]);
    await expect.poll(() => quoteFlash(page)).toEqual([]);
  });
});
