import { test, expect, type APIRequestContext } from '@playwright/test';
import { clearAllComments, getMdPath, loadPage, switchToDocumentView, recordClassAdditions, expectClassAddedTo, classAdditions } from './helpers';

// A comment on selected text and its highlighted quote link to each other in
// the rendered document. plan.md line 5 contains "API key authentication".
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
    await expect(mark).toHaveClass(new RegExp(FLASH));
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
    await page.waitForTimeout(1500);
    expect(await classAdditions(page, FLASH)).toEqual([]);
  });
});
