import { test, expect, type Locator } from '@playwright/test';
import {
  clearAllComments, loadPage, mdSection, goSection, jsSection, fileHeader, mdDocument,
  submitFileLevelComment,
} from './helpers';

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

  // Rendered markdown: the card must land above the document, not below it
  // (Pierre appends a new annotation after the document in the same slot).
  test('file-level comments on a rendered document show above it, twice in a row', async ({ page, request }) => {
    const section = await mdSection(page);
    const opts = { header: fileHeader(page, 'plan.md'), scope: fileLevel(section), content: mdDocument(page) };
    await submitFileLevelComment(page, { ...opts, body: 'File-mode file comment' });
    await submitFileLevelComment(page, { ...opts, body: 'Second file-mode comment' });
    await expect(fileLevel(section).locator('.comment-card')).toHaveCount(2);

    await expect.poll(async () => {
      const comments = await (await request.get('/api/file/comments?path=plan.md')).json();
      return comments.map((c: { scope: string; body: string }) => `${c.scope}:${c.body}`);
    }).toEqual(['file:File-mode file comment', 'file:Second file-mode comment']);
  });

  test('cancel then reopen gives a fresh file composer', async ({ page }) => {
    const section = await mdSection(page);
    const textarea = fileLevel(section).locator('.comment-form textarea');
    await fileHeader(page, 'plan.md').locator('.file-comment-btn').click();
    await textarea.fill('Discarded draft');
    page.once('dialog', d => d.accept());
    await fileLevel(section).getByRole('button', { name: 'Cancel' }).click();
    await expect(section.locator('.comment-form')).toHaveCount(0);

    await submitFileLevelComment(page, {
      header: fileHeader(page, 'plan.md'), scope: fileLevel(section), content: mdDocument(page), body: 'Kept comment',
    });
    await expect(fileLevel(section).locator('.comment-card')).toHaveCount(1);
  });

  // server.go sits below the rendered plan.md: this also catches a wrong
  // height for the document item, which put later files at wrong offsets.
  for (const [name, open] of [['server.go', goSection], ['handler.js', jsSection]] as const) {
    test(`file-level comments on code file ${name} show above line 1, twice in a row`, async ({ page }) => {
      const section = await open(page);
      const opts = { header: fileHeader(page, name), scope: fileLevel(section), content: section.locator('[data-line="1"]').first() };
      await submitFileLevelComment(page, { ...opts, body: 'First on ' + name });
      await submitFileLevelComment(page, { ...opts, body: 'Second on ' + name });
    });
  }
});
