import { test, expect, type Page } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, addComment, getMdPath } from './helpers';

function commentsPanel(page: Page) {
  return page.locator('#commentsPanel');
}

function panelCards(page: Page) {
  return page.locator('.comments-panel-card');
}

// ============================================================
// Comments Panel — File Mode
// ============================================================
test.describe('Comments Panel — File Mode', () => {
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
    await addComment(request, mdPath, 1, 'File mode panel test');
    await loadPage(page);

    const countEl = page.locator('#commentCount');
    await expect(countEl).toBeVisible();
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
    await addComment(request, mdPath, 1, 'First file mode comment');
    await addComment(request, mdPath, 3, 'Second file mode comment');
    await loadPage(page);

    await page.keyboard.press('Shift+C');
    await expect(panelCards(page)).toHaveCount(2);
    await expect(panelCards(page).first().locator('.comments-panel-card-body')).toContainText('First file mode comment');
  });

  test('panel updates when comment is added via UI', async ({ page }) => {
    await loadPage(page);

    // Open panel
    await page.keyboard.press('Shift+C');
    await expect(page.locator('.comments-panel-empty')).toBeVisible();

    // Add a comment through the UI (file mode defaults to document view)
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Added in file mode');
    await page.locator('.comment-form .btn-primary').click();

    // Panel should now show the comment
    await expect(panelCards(page)).toHaveCount(1);
    await expect(panelCards(page).first().locator('.comments-panel-card-body')).toContainText('Added in file mode');
  });

  test('clicking panel card scrolls to inline comment', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Scroll target file mode');
    await loadPage(page);

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

    await loadPage(page);
    await expect(commentsPanel(page)).toHaveClass(/comments-panel-hidden/);
  });

  test('panel updates when comment is deleted', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Will be deleted in file mode');
    await loadPage(page);

    await page.keyboard.press('Shift+C');
    await expect(panelCards(page)).toHaveCount(1);

    // Delete through UI
    const section = mdSection(page);
    const deleteBtn = section.locator('.comment-card .delete-btn');
    await deleteBtn.click();

    await expect(page.locator('.comments-panel-empty')).toBeVisible();
  });
});
