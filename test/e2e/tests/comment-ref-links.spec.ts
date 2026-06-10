import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, getMdPath, switchToDocumentView, addComment } from './helpers';

test.describe('Comment reference links', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToDocumentView(page);
  });

  test('bare comment ID in body renders as a .comment-ref chip', async ({ page, request }) => {
    const path = await getMdPath(request);
    const c1 = await addComment(request, path, 1, 'First comment');
    await addComment(request, path, 2, `See also ${c1.id} for context`);

    await page.reload();
    await switchToDocumentView(page);

    const chips = page.locator('.comment-ref');
    await expect(chips).toHaveCount(1);
    await expect(chips.first()).toContainText(c1.id);
  });

  test('backtick-wrapped comment ID renders as a .comment-ref chip', async ({ page, request }) => {
    const path = await getMdPath(request);
    const c1 = await addComment(request, path, 1, 'First comment');
    await addComment(request, path, 2, `Related to \`${c1.id}\``);

    await page.reload();
    await switchToDocumentView(page);

    const chips = page.locator('.comment-ref');
    await expect(chips).toHaveCount(1);
    await expect(chips.first()).toContainText(c1.id);
  });

  test('clicking a chip scrolls to and flashes the referenced comment card', async ({ page, request }) => {
    const path = await getMdPath(request);
    const c1 = await addComment(request, path, 1, 'First comment');
    await addComment(request, path, 5, `See ${c1.id} above`);

    await page.reload();
    await switchToDocumentView(page);

    const chip = page.locator('.comment-ref').first();
    await expect(chip).toBeVisible();
    await chip.click();

    const targetCard = page.locator(`.comment-card[data-comment-id="${c1.id}"]`);
    await expect(targetCard).toBeVisible();
    await expect(targetCard).toHaveClass(/comment-ref-flash/);
  });

  test('clicking chip with ID absent from page does not throw', async ({ page, request }) => {
    const path = await getMdPath(request);
    // Reference a non-existent ID — scrollToCommentRef should be a no-op
    await addComment(request, path, 1, 'See c_000000 for details');

    await page.reload();
    await switchToDocumentView(page);

    const chip = page.locator('.comment-ref').first();
    await expect(chip).toBeVisible();

    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));
    await chip.click();
    expect(errors).toHaveLength(0);
  });

  test('comment IDs inside code blocks are not linkified', async ({ page, request }) => {
    const path = await getMdPath(request);
    // Fenced code block — should not be processed by linkifyCommentRefsInDom
    await addComment(request, path, 1, '```\nc_aabbcc\n```');

    await page.reload();
    await switchToDocumentView(page);

    await expect(page.locator('.comment-ref')).toHaveCount(0);
  });
});
