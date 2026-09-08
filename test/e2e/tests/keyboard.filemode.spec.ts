import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

test.describe('Keyboard Comment Shortcuts — File Mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('e edits and d deletes a comment in file-mode document view', async ({ page, request }) => {
    await request.post(`/api/file/comments?path=plan.md`, {
      data: { start_line: 1, end_line: 1, body: 'Filemode shortcut test' },
    });

    await loadPage(page);
    const section = page.locator('.file-section').filter({ hasText: 'plan.md' });
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Navigate to the first block and edit
    await page.keyboard.press('j');
    await expect(page.locator('.line-block.kb-nav.focused')).toHaveCount(1);
    await page.keyboard.press('e');

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await expect(textarea).toHaveValue('Filemode shortcut test');

    await textarea.fill('Filemode edited');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card .comment-body')).toContainText('Filemode edited');

    // Delete via shortcut
    await page.keyboard.press('d');
    await expect(section.locator('.comment-card')).toHaveCount(0);
  });
});
