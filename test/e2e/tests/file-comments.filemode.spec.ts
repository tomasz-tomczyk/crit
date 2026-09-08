import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection } from './helpers';

test.describe('File-level comments — File Mode', () => {
  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('can add a file-level comment via header button in file mode', async ({ page }) => {
    const fileCommentBtn = page.locator('.file-comment-btn').first();
    await fileCommentBtn.click();

    const textarea = page.locator('.file-comments .comment-form textarea');
    await expect(textarea).toBeVisible();
    await textarea.fill('File-mode file comment');
    await page.locator('.file-comments .comment-form .btn-primary').click();

    const fileComments = page.locator('.file-comments .comment-card');
    await expect(fileComments).toHaveCount(1);
    await expect(fileComments.first()).toContainText('File-mode file comment');
  });
});
