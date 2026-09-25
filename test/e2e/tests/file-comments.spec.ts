import { test, expect, type Locator } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, goSection, fileHeader } from './helpers';

// File-level threads and the file compose form are Pierre annotations on
// line 0 (above the first line), so they render in that slot of the item.
function fileLevel(item: Locator): Locator {
  return item.locator('[slot="annotation-additions-0"]');
}

// ============================================================
// File-Level Comments (git mode)
// ============================================================
test.describe('File-level comments — Git Mode', () => {
  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('can add a file-level comment via header button', async ({ page, request }) => {
    const section = await goSection(page);
    await fileHeader(page, 'server.go').locator('.file-comment-btn').click();

    // Fill and submit the form
    const textarea = fileLevel(section).locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await expect(fileLevel(section).locator('.comment-form')).toHaveCount(1);
    await textarea.fill('This file needs restructuring');
    await fileLevel(section).locator('.comment-form .btn-primary').click();

    // Verify comment appears above the code, and the form closes
    const fileComments = fileLevel(section).locator('.comment-card');
    await expect(fileComments).toHaveCount(1);
    await expect(fileComments.first()).toContainText('This file needs restructuring');
    await expect(section.locator('.comment-form')).toHaveCount(0);

    // Stored as a file-scope comment on server.go
    await expect.poll(async () => {
      const comments = await (await request.get('/api/file/comments?path=server.go')).json();
      return comments.map((c: { scope: string; body: string }) => `${c.scope}:${c.body}`);
    }).toEqual(['file:This file needs restructuring']);
  });

  test('file-level comment added via API renders on load', async ({ page, request }) => {
    await request.post('/api/file/comments?path=plan.md', {
      data: { body: 'file comment via api', scope: 'file' },
    });
    await loadPage(page);

    const section = await mdSection(page);
    const fileComments = fileLevel(section).locator('.comment-card');
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
    await expect(page.locator('#commentCountNumber')).toHaveText('1');
  });

  test('can delete a file-level comment', async ({ page, request }) => {
    await request.post('/api/file/comments?path=plan.md', {
      data: { body: 'delete me file comment', scope: 'file' },
    });
    await loadPage(page);

    const section = await mdSection(page);
    const card = fileLevel(section).locator('.comment-card').first();
    await expect(card).toBeVisible();

    // Click the delete button
    await card.locator('.comment-actions .delete-btn').click();

    await expect(fileLevel(section).locator('.comment-card')).toHaveCount(0);
    await expect.poll(async () => (await (await request.get('/api/file/comments?path=plan.md')).json()).length).toBe(0);
  });

  test('can edit a file-level comment', async ({ page, request }) => {
    await request.post('/api/file/comments?path=plan.md', {
      data: { body: 'original file comment', scope: 'file' },
    });
    await loadPage(page);

    const section = await mdSection(page);
    const card = fileLevel(section).locator('.comment-card').first();
    await expect(card).toBeVisible();

    // Click Edit
    await card.locator('.comment-actions button[title="Edit"]').click();

    // Regression: editing must open one form (inline editor), not both the
    // file-scope compose form and the inline editor at once.
    const forms = section.locator('.comment-form');
    await expect(forms).toHaveCount(1);
    await expect(forms.locator('.comment-form-header')).toHaveText('Editing file comment');
    await expect(forms.locator('.btn-primary')).toHaveText('Update Comment');

    const textarea = forms.locator('textarea');
    await expect(textarea).toBeVisible();
    await expect(textarea).toHaveValue('original file comment');

    await textarea.clear();
    await textarea.fill('updated file comment');
    await forms.locator('.btn-primary').click();

    await expect(fileLevel(section).locator('.comment-card .comment-body')).toContainText('updated file comment');
    await expect(section.locator('.comment-form')).toHaveCount(0);
  });

  test('multiple file-level comments on different files', async ({ page, request }) => {
    await request.post('/api/file/comments?path=plan.md', {
      data: { body: 'comment on plan', scope: 'file' },
    });
    await request.post('/api/file/comments?path=server.go', {
      data: { body: 'comment on server', scope: 'file' },
    });
    await loadPage(page);

    // Each file shows only its own file-level comment.
    const plan = await mdSection(page);
    await expect(fileLevel(plan).locator('.comment-card')).toHaveCount(1);
    await expect(fileLevel(plan).locator('.comment-card')).toContainText('comment on plan');

    const server = await goSection(page);
    await expect(fileLevel(server).locator('.comment-card')).toHaveCount(1);
    await expect(fileLevel(server).locator('.comment-card')).toContainText('comment on server');
  });
});
