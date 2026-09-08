import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

test.describe('Review-level comments — File Mode', () => {
  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('G shortcut opens, creates, and deletes a review comment in file mode', async ({ page, request }) => {
    await page.keyboard.press('Shift+G');
    const textarea = page.locator('#reviewConversation .comment-form textarea');
    await expect(textarea).toBeVisible();
    await expect(textarea).toBeFocused();

    await textarea.fill('File mode review comment');
    await page.locator('#reviewConversation .comment-form .btn-primary').click();

    const cards = page.locator('#reviewConversation .comment-card');
    await expect(cards).toHaveCount(1);
    await expect(cards.first()).toContainText('File mode review comment');

    await cards.first().locator('.delete-btn').click();
    await expect(page.locator('#reviewConversation .comment-card')).toHaveCount(0);
  });
});
