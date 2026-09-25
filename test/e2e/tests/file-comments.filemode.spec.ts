import { test, expect, type Locator } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, fileHeader } from './helpers';

// File-level threads and the file compose form are annotations on line 0 of
// the file's Pierre item (above its first line / rendered document).
function fileLevel(item: Locator): Locator {
  return item.locator('[slot="annotation-0"]');
}

test.describe('File-level comments — File Mode', () => {
  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('can add a file-level comment via header button in file mode', async ({ page, request }) => {
    const section = await mdSection(page);
    await fileHeader(page, 'plan.md').locator('.file-comment-btn').click();

    const textarea = fileLevel(section).locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await textarea.fill('File-mode file comment');
    await fileLevel(section).locator('.comment-form .btn-primary').click();

    const fileComments = fileLevel(section).locator('.comment-card');
    await expect(fileComments).toHaveCount(1);
    await expect(fileComments.first()).toContainText('File-mode file comment');
    await expect(section.locator('.comment-form')).toHaveCount(0);

    await expect.poll(async () => {
      const comments = await (await request.get('/api/file/comments?path=plan.md')).json();
      return comments.map((c: { scope: string; body: string }) => `${c.scope}:${c.body}`);
    }).toEqual(['file:File-mode file comment']);
  });
});
