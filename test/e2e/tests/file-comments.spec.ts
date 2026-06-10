import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection } from './helpers';

// ============================================================
// File-Level Comments (git mode)
// ============================================================
test.describe('File-level comments — Git Mode', () => {
  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('can add a file-level comment via header button', async ({ page }) => {
    // Find a file section's header and click the file comment button
    const fileCommentBtn = page.locator('.file-comment-btn').first();
    await fileCommentBtn.click();

    // Fill and submit the form
    const textarea = page.locator('.file-comments .comment-form textarea');
    await expect(textarea).toBeVisible();
    await textarea.fill('This file needs restructuring');
    await page.locator('.file-comments .comment-form .btn-primary').click();

    // Verify comment appears
    const fileComments = page.locator('.file-comments .comment-card');
    await expect(fileComments).toHaveCount(1);
    await expect(fileComments.first()).toContainText('This file needs restructuring');
  });

  test('file-level comment added via API renders on load', async ({ page, request }) => {
    await request.post('/api/file/comments?path=plan.md', {
      data: { body: 'file comment via api', scope: 'file' },
    });
    await loadPage(page);

    const fileComments = page.locator('.file-comments .comment-card');
    await expect(fileComments).toHaveCount(1);
    await expect(fileComments.first()).toContainText('file comment via api');
  });

  test('file-level comments included in comment count', async ({ page, request }) => {
    await request.post('/api/file/comments?path=plan.md', {
      data: { body: 'file comment for count', scope: 'file' },
    });
    await loadPage(page);

    const badge = page.locator('#commentCount');
    await expect(badge).toBeVisible();
  });

  test('can delete a file-level comment', async ({ page, request }) => {
    await request.post('/api/file/comments?path=plan.md', {
      data: { body: 'delete me file comment', scope: 'file' },
    });
    await loadPage(page);

    const card = page.locator('.file-comments .comment-card').first();
    await expect(card).toBeVisible();

    // Click the delete button
    await card.locator('.comment-actions .delete-btn').click();

    await expect(page.locator('.file-comments .comment-card')).toHaveCount(0);
  });

  test('can edit a file-level comment', async ({ page, request }) => {
    await request.post('/api/file/comments?path=plan.md', {
      data: { body: 'original file comment', scope: 'file' },
    });
    await loadPage(page);

    const section = mdSection(page);
    const card = section.locator('.file-comments .comment-card').first();
    await expect(card).toBeVisible();

    // Click Edit
    await card.locator('.comment-actions button[title="Edit"]').click();

    const textarea = section.locator('.file-comments .comment-form textarea').first();
    await expect(textarea).toBeVisible();
    await expect(textarea).toHaveValue('original file comment');

    await textarea.clear();
    await textarea.fill('updated file comment');
    await section.locator('.file-comments .comment-form .btn-primary').first().click();

    await expect(section.locator('.file-comments .comment-card .comment-body')).toContainText('updated file comment');
  });

  test('multiple file-level comments on different files', async ({ page, request }) => {
    await request.post('/api/file/comments?path=plan.md', {
      data: { body: 'comment on plan', scope: 'file' },
    });
    await request.post('/api/file/comments?path=server.go', {
      data: { body: 'comment on server', scope: 'file' },
    });
    await loadPage(page);

    const allFileComments = page.locator('.file-comments .comment-card');
    await expect(allFileComments).toHaveCount(2);
  });
});
