import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection } from './helpers';

// ============================================================
// File-Level Comments — File Mode
// ============================================================
test.describe('File-level comments — File Mode', () => {
  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('can add a file-level comment via header button', async ({ page }) => {
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

  test('file-level comment added via API renders on load', async ({ page, request }) => {
    await request.post('/api/file/comments?path=plan.md', {
      data: { body: 'api file comment filemode', scope: 'file' },
    });
    await loadPage(page);

    const fileComments = page.locator('.file-comments .comment-card');
    await expect(fileComments).toHaveCount(1);
    await expect(fileComments.first()).toContainText('api file comment filemode');
  });

  test('file-level comments included in comment count', async ({ page, request }) => {
    await request.post('/api/file/comments?path=plan.md', {
      data: { body: 'count check', scope: 'file' },
    });
    await loadPage(page);

    const badge = page.locator('#commentCount');
    await expect(badge).toBeVisible();
  });

  test('can delete a file-level comment', async ({ page, request }) => {
    await request.post('/api/file/comments?path=plan.md', {
      data: { body: 'delete me', scope: 'file' },
    });
    await loadPage(page);

    const card = page.locator('.file-comments .comment-card').first();
    await expect(card).toBeVisible();
    await card.locator('.comment-actions .delete-btn').click();

    await expect(page.locator('.file-comments .comment-card')).toHaveCount(0);
  });

  test('can edit a file-level comment', async ({ page, request }) => {
    await request.post('/api/file/comments?path=plan.md', {
      data: { body: 'original', scope: 'file' },
    });
    await loadPage(page);

    const section = mdSection(page);
    const card = section.locator('.file-comments .comment-card').first();
    await expect(card).toBeVisible();

    await card.locator('.comment-actions button[title="Edit"]').click();

    const textarea = section.locator('.file-comments .comment-form textarea').first();
    await expect(textarea).toBeVisible();
    await expect(textarea).toHaveValue('original');

    await textarea.clear();
    await textarea.fill('updated');
    await section.locator('.file-comments .comment-form .btn-primary').first().click();

    await expect(section.locator('.file-comments .comment-card .comment-body')).toContainText('updated');
  });
});
