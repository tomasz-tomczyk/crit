import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// The single-file fixture reviews one markdown file outside any git
// repository, so it covers two things no other fixture reaches:
//  - the single-file layout (filename in the header, no file tree, no file header)
//  - crit's VCS-absent files-mode path (no branch, no VCS backend)

test.describe('Single File Mode — layout and no-VCS session', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('header shows the filename; file tree and file header are hidden', async ({ page }) => {
    await loadPage(page);
    await expect(page.locator('#filesContainer .document-wrapper')).toBeVisible();

    const context = page.locator('#branchContext');
    await expect(context).toBeVisible();
    await expect(page.locator('#branchName')).toHaveText('plan.md');
    await expect(context.locator('.header-copy-path')).toHaveAttribute('aria-label', 'Copy file path');

    await expect(page.locator('#fileTreePanel')).toBeHidden();
    await expect(page.locator('.pierre-file-header[data-file-path="plan.md"]')).toBeHidden();
  });

  test('session API reports files mode with no branch and no VCS', async ({ request }) => {
    const res = await request.get('/api/session');
    await expect(res).toBeOK();
    const session = await res.json();
    expect(session.mode).toBe('files');
    expect(session.branch).toBeFalsy();
    expect(session.vcs_name).toBeFalsy();
    expect(session.files.map((f: { path: string }) => f.path)).toEqual(['plan.md']);
  });

  test('creates and reloads a line comment without a repository', async ({ page, request }) => {
    await loadPage(page);
    const doc = page.locator('#filesContainer .document-wrapper');
    await doc.locator('.line-block').first().hover();
    await doc.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('#filesContainer .comment-form textarea');
    await expect(textarea).toBeFocused();
    await textarea.fill('No-git persisted comment');
    await textarea.press('Control+Enter');
    await expect(page.locator('#filesContainer .comment-form')).toHaveCount(0);
    const card = page.locator('#filesContainer .comment-card', { hasText: 'No-git persisted comment' });
    await expect(card).toHaveCount(1);

    const response = await request.get('/api/file/comments?path=plan.md');
    await expect(response).toBeOK();
    expect(await response.json()).toEqual([
      expect.objectContaining({ body: 'No-git persisted comment', start_line: 1, end_line: 1 }),
    ]);

    // The comment comes back from the server on a fresh load.
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await expect(card).toHaveCount(1);
  });
});
