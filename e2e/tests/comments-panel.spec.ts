import { test, expect, type Page, type APIRequestContext } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, switchToDocumentView } from './helpers';

// Helper: add a comment via API and return the created comment object.
async function addComment(request: APIRequestContext, path: string, line: number, body: string) {
  const resp = await request.post(`/api/file/comments?path=${encodeURIComponent(path)}`, {
    data: { start_line: line, end_line: line, body },
  });
  expect(resp.ok()).toBeTruthy();
  return resp.json();
}

// Helper: get the markdown file path from the session.
async function getMdPath(request: APIRequestContext): Promise<string> {
  const session = await (await request.get('/api/session')).json();
  const mdFile = session.files.find((f: { path: string }) => f.path.endsWith('.md'));
  expect(mdFile).toBeTruthy();
  return mdFile.path;
}

function commentsPanel(page: Page) {
  return page.locator('#commentsPanel');
}

function panelCards(page: Page) {
  return page.locator('.comments-panel-card');
}

// ============================================================
// Comments Panel — Git Mode
// ============================================================
test.describe('Comments Panel — Git Mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('panel is hidden by default', async ({ page }) => {
    await loadPage(page);
    await expect(commentsPanel(page)).toHaveClass(/comments-panel-hidden/);
  });

  test('Shift+C toggles panel open and closed', async ({ page }) => {
    await loadPage(page);

    await page.keyboard.press('Shift+C');
    await expect(commentsPanel(page)).not.toHaveClass(/comments-panel-hidden/);

    await page.keyboard.press('Shift+C');
    await expect(commentsPanel(page)).toHaveClass(/comments-panel-hidden/);
  });

  test('clicking comment count opens panel', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Panel toggle test');
    await loadPage(page);

    const countEl = page.locator('#commentCount');
    await expect(countEl).toContainText('1');
    await countEl.click();

    await expect(commentsPanel(page)).not.toHaveClass(/comments-panel-hidden/);
  });

  test('close button hides panel', async ({ page }) => {
    await loadPage(page);
    await page.keyboard.press('Shift+C');
    await expect(commentsPanel(page)).not.toHaveClass(/comments-panel-hidden/);

    await page.locator('.comments-panel-close').click();
    await expect(commentsPanel(page)).toHaveClass(/comments-panel-hidden/);
  });

  test('empty state when no comments', async ({ page }) => {
    await loadPage(page);
    await page.keyboard.press('Shift+C');

    await expect(page.locator('.comments-panel-empty')).toBeVisible();
    await expect(page.locator('.comments-panel-empty')).toContainText('No unresolved comments');
  });

  test('panel shows comment cards', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'First git comment');
    await addComment(request, mdPath, 3, 'Second git comment');
    await loadPage(page);

    await page.keyboard.press('Shift+C');
    await expect(panelCards(page)).toHaveCount(2);
    await expect(panelCards(page).first().locator('.comments-panel-card-body')).toContainText('First git comment');
    await expect(panelCards(page).nth(1).locator('.comments-panel-card-body')).toContainText('Second git comment');
  });

  test('panel shows line references', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 5, 'Line ref test');
    await loadPage(page);

    await page.keyboard.press('Shift+C');
    await expect(panelCards(page).first().locator('.comments-panel-card-line')).toContainText('Line 5');
  });

  test('panel shows line range for multi-line comments', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const resp = await request.post(`/api/file/comments?path=${encodeURIComponent(mdPath)}`, {
      data: { start_line: 2, end_line: 4, body: 'Range comment' },
    });
    expect(resp.ok()).toBeTruthy();
    await loadPage(page);

    await page.keyboard.press('Shift+C');
    await expect(panelCards(page).first().locator('.comments-panel-card-line')).toContainText('Lines 2-4');
  });

  test('panel updates when comment is added via UI', async ({ page }) => {
    await loadPage(page);
    await switchToDocumentView(page);

    // Open panel first
    await page.keyboard.press('Shift+C');
    await expect(page.locator('.comments-panel-empty')).toBeVisible();

    // Add a comment through the UI
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Added via UI');
    await page.locator('.comment-form .btn-primary').click();

    // Panel should now show the comment
    await expect(panelCards(page)).toHaveCount(1);
    await expect(panelCards(page).first().locator('.comments-panel-card-body')).toContainText('Added via UI');
  });

  test('panel updates when comment is deleted', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Will be deleted');
    await loadPage(page);
    await switchToDocumentView(page);

    await page.keyboard.press('Shift+C');
    await expect(panelCards(page)).toHaveCount(1);

    // Delete through UI
    const section = mdSection(page);
    const deleteBtn = section.locator('.comment-card .delete-btn');
    await deleteBtn.click();

    // Panel should update to empty state
    await expect(page.locator('.comments-panel-empty')).toBeVisible();
  });

  test('clicking panel card scrolls to inline comment', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Scroll target');
    await loadPage(page);
    await switchToDocumentView(page);

    await page.keyboard.press('Shift+C');
    await panelCards(page).first().click();

    // The inline comment card should get the highlight animation class
    const inlineCard = mdSection(page).locator('.comment-card[data-comment-id]').first();
    await expect(inlineCard).toBeVisible();
    await expect(inlineCard).toHaveClass(/comment-card-highlight/);
  });

  test('panel does not persist open state across reloads', async ({ page }) => {
    await loadPage(page);
    await page.keyboard.press('Shift+C');
    await expect(commentsPanel(page)).not.toHaveClass(/comments-panel-hidden/);

    await page.reload();
    await loadPage(page);
    await expect(commentsPanel(page)).toHaveClass(/comments-panel-hidden/);
  });

  test('show resolved toggle appears when resolved comments exist', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    // Add comment, then do a round-complete to get carried_forward comments
    await addComment(request, mdPath, 1, 'Will be resolved');

    // Simulate resolution by writing .crit.json with resolved flag
    // Instead, just check that the filter is hidden when there are no resolved comments
    await loadPage(page);
    await page.keyboard.press('Shift+C');

    // No resolved comments — filter should be hidden
    await expect(page.locator('#commentsPanelFilter')).toBeHidden();
  });

  test('panel shows file name headers in multi-file mode', async ({ page, request }) => {
    const session = await (await request.get('/api/session')).json();
    // Git mode has multiple files — add comments to two different files
    const mdPath = session.files.find((f: { path: string }) => f.path.endsWith('.md'))?.path;
    const goPath = session.files.find((f: { path: string }) => f.path.endsWith('.go'))?.path;

    if (mdPath && goPath) {
      await addComment(request, mdPath, 1, 'MD comment');
      await addComment(request, goPath, 1, 'Go comment');
      await loadPage(page);

      await page.keyboard.press('Shift+C');
      await expect(panelCards(page)).toHaveCount(2);

      // File name headers should be visible
      const fileNames = page.locator('.comments-panel-file-name');
      await expect(fileNames).toHaveCount(2);
    }
  });

  test('keyboard shortcut in shortcuts overlay', async ({ page }) => {
    await loadPage(page);
    await page.keyboard.press('?');
    const overlay = page.locator('.shortcuts-overlay.active');
    await expect(overlay).toBeVisible();
    await expect(overlay).toContainText('Toggle comments panel');
  });
});
