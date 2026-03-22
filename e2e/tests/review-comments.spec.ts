import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// ============================================================
// Review-Level (General) Comments — Git Mode
// ============================================================
test.describe('Review-level comments — Git Mode', () => {
  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('G shortcut opens review comment form', async ({ page }) => {
    await page.keyboard.press('Shift+G');

    const panel = page.locator('#commentsPanel');
    await expect(panel).not.toHaveClass(/comments-panel-hidden/);

    const form = page.locator('#commentsPanelBody .comment-form textarea');
    await expect(form).toBeVisible();
    await expect(form).toBeFocused();
  });

  test('can add a review-level comment via G shortcut', async ({ page }) => {
    await page.keyboard.press('Shift+G');
    await page.locator('#commentsPanelBody .comment-form textarea').fill('General feedback');
    await page.locator('#commentsPanelBody .comment-form .btn-primary').click();

    const cards = page.locator('#commentsPanelBody .comment-card');
    await expect(cards).toHaveCount(1);
    await expect(cards.first()).toContainText('General feedback');
  });

  test('review comments added via API render on load', async ({ page, request }) => {
    await request.post('/api/comments', { data: { body: 'api review comment' } });
    await loadPage(page);

    // Open the comments panel
    await page.locator('#commentCount').click();

    const panel = page.locator('#commentsPanel');
    await expect(panel).not.toHaveClass(/comments-panel-hidden/);
    await expect(page.locator('#commentsPanelBody .comment-card')).toHaveCount(1);
    await expect(page.locator('#commentsPanelBody .comment-card').first()).toContainText('api review comment');
  });

  test('can delete review comments', async ({ page, request }) => {
    await request.post('/api/comments', { data: { body: 'to delete' } });
    await loadPage(page);

    // Open the comments panel
    await page.locator('#commentCount').click();

    const card = page.locator('#commentsPanelBody .comment-card').first();
    await expect(card).toBeVisible();
    await card.locator('.delete-btn').click();

    await expect(page.locator('#commentsPanelBody .comment-card')).toHaveCount(0);
  });

  test('Add button in panel opens form', async ({ page }) => {
    // Open the comments panel via keyboard shortcut (no comments yet so badge not visible)
    await page.keyboard.press('Shift+C');
    await expect(page.locator('#commentsPanel')).not.toHaveClass(/comments-panel-hidden/);

    await page.locator('#panelAddCommentBtn').click();

    const form = page.locator('#commentsPanelBody .comment-form textarea');
    await expect(form).toBeVisible();
  });

  test('Escape closes review comment form', async ({ page }) => {
    await page.keyboard.press('Shift+G');
    const textarea = page.locator('#commentsPanelBody .comment-form textarea');
    await expect(textarea).toBeVisible();

    await textarea.press('Escape');

    await expect(page.locator('#commentsPanelBody .comment-form')).toHaveCount(0);
  });

  test('Ctrl+Enter submits review comment', async ({ page }) => {
    await page.keyboard.press('Shift+G');
    const textarea = page.locator('#commentsPanelBody .comment-form textarea');
    await textarea.fill('submitted with ctrl+enter');
    await textarea.press('Control+Enter');

    await expect(page.locator('#commentsPanelBody .comment-card')).toHaveCount(1);
    await expect(page.locator('#commentsPanelBody .comment-card').first()).toContainText('submitted with ctrl+enter');
  });

  test('review comments included in comment count', async ({ page, request }) => {
    await request.post('/api/comments', { data: { body: 'count test' } });
    await loadPage(page);

    const badge = page.locator('#commentCount');
    await expect(badge).toBeVisible();
  });

  test('can edit a review comment', async ({ page, request }) => {
    await request.post('/api/comments', { data: { body: 'original review' } });
    await loadPage(page);

    // Open the comments panel
    await page.locator('#commentCount').click();

    const card = page.locator('#commentsPanelBody .comment-card').first();
    await expect(card).toBeVisible();

    // Click Edit
    await card.locator('button[title="Edit"]').click();

    const textarea = page.locator('#commentsPanelBody .comment-form textarea');
    await expect(textarea).toBeVisible();
    await expect(textarea).toHaveValue('original review');

    await textarea.clear();
    await textarea.fill('updated review');
    await page.locator('#commentsPanelBody .comment-form .btn-primary').click();

    await expect(page.locator('#commentsPanelBody .comment-card .comment-body')).toContainText('updated review');
  });
});
