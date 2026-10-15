import { test, expect, type Locator } from '@playwright/test';
import {
  clearAllComments, loadPage, mdSection, goSection, fileHeader, mdDocument, setDiffStyle,
  switchToDocumentView, submitFileLevelComment,
} from './helpers';

// File-level threads and the file compose form are Pierre annotations on
// line 0 (above the first line), so they render in that slot of the item.
// Files with an old side put them on the deletions side (left of a split
// diff); new files on the additions side.
function fileLevel(item: Locator): Locator {
  return item.locator('[slot="annotation-deletions-0"], [slot="annotation-additions-0"]');
}

// ============================================================
// File-Level Comments (git mode)
// ============================================================
test.describe('File-level comments — Git Mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  // The card must land on screen above the file's content, including a
  // second submit on the same file (the composer DOM is not reused).
  for (const style of ['split', 'unified'] as const) {
    test(`file-level comments show above line 1 in ${style} diffs, twice in a row`, async ({ page, request }) => {
      await loadPage(page);
      await setDiffStyle(page, style);
      const section = await goSection(page);
      const opts = { header: fileHeader(page, 'server.go'), scope: fileLevel(section), content: section.locator('[data-line="1"]').first() };
      await submitFileLevelComment(page, { ...opts, body: `First ${style} file comment` });
      await submitFileLevelComment(page, { ...opts, body: `Second ${style} file comment` });
      // server.go is modified, so its file-level threads take the old side.
      await expect(section.locator('[slot="annotation-deletions-0"] .comment-card')).toHaveCount(2);
      await expect(section.locator('.comment-form')).toHaveCount(0);

      // Stored as file-scope comments on server.go
      await expect.poll(async () => {
        const comments = await (await request.get('/api/file/comments?path=server.go')).json();
        return comments.map((c: { scope: string; body: string }) => `${c.scope}:${c.body}`);
      }).toEqual([`file:First ${style} file comment`, `file:Second ${style} file comment`]);
    });
  }

  test('file-level comments show above a rendered document, twice in a row', async ({ page }) => {
    await loadPage(page);
    await switchToDocumentView(page);
    const section = await mdSection(page);
    // Document view is a single-sided file item: its file-level slot has no side.
    const opts = { header: fileHeader(page, 'plan.md'), scope: section.locator('[slot="annotation-0"]'), content: mdDocument(page) };
    await submitFileLevelComment(page, { ...opts, body: 'First document file comment' });
    await submitFileLevelComment(page, { ...opts, body: 'Second document file comment' });
  });

  test('API-added file-level comments render per file, count, edit and delete', async ({ page, request }) => {
    await request.post('/api/file/comments?path=plan.md', {
      data: { body: 'original file comment', scope: 'file' },
    });
    await request.post('/api/file/comments?path=server.go', {
      data: { body: 'comment on server', scope: 'file' },
    });
    await loadPage(page);

    // Included in the comment count.
    await expect(page.locator('#commentCount')).toBeVisible();
    await expect(page.locator('#commentCountNumber')).toHaveText('2');

    // Each file shows only its own file-level comment.
    const server = await goSection(page);
    await expect(fileLevel(server).locator('.comment-card')).toHaveCount(1);
    await expect(fileLevel(server).locator('.comment-card')).toContainText('comment on server');

    const section = await mdSection(page);
    await expect(fileLevel(section).locator('.comment-card')).toHaveCount(1);
    const card = fileLevel(section).locator('.comment-card').first();
    await expect(card).toContainText('original file comment');

    // Edit. Regression: editing must open one form (inline editor), not both
    // the file-scope compose form and the inline editor at once.
    await card.locator('.comment-actions button[title="Edit"]').click();
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

    // Delete.
    await fileLevel(section).locator('.comment-card').first().locator('.comment-actions .delete-btn').click();
    await expect(fileLevel(section).locator('.comment-card')).toHaveCount(0);
    await expect.poll(async () => (await (await request.get('/api/file/comments?path=plan.md')).json()).length).toBe(0);
  });
});
