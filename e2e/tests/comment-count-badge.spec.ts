import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, switchToDocumentView, addComment, getMdPath } from './helpers';

// ============================================================
// Comment Count Badge — header badge shows comment count
// Shows unresolved count when unresolved comments exist,
// falls back to total count when all are resolved.
// ============================================================
test.describe('Comment Count Badge', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToDocumentView(page);
  });

  test('badge is hidden when there are no comments', async ({ page }) => {
    const countEl = page.locator('#commentCount');
    const badgeEl = page.locator('#commentCountNumber');
    await expect(countEl).toBeHidden();
    await expect(badgeEl).toHaveText('');
  });

  test('badge shows 1 after adding a comment', async ({ page }) => {
    const section = mdSection(page);
    const badgeEl = page.locator('#commentCountNumber');

    // Add a comment via UI
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();
    await page.locator('.comment-form textarea').fill('Badge test comment');
    await page.locator('.comment-form .btn-primary').click();

    await expect(page.locator('#commentCount')).toBeVisible();
    await expect(badgeEl).toHaveText('1');
  });

  test('badge shows unresolved count, not total', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const countEl = page.locator('#commentCount');
    const badgeEl = page.locator('#commentCountNumber');

    // Add a comment, finish round to resolve it, then add a new unresolved one
    await addComment(request, mdPath, 1, 'Will be resolved');
    await request.post('/api/finish');
    await request.post('/api/round-complete');

    // Now add 2 new unresolved comments (round 2)
    await addComment(request, mdPath, 1, 'Unresolved one');
    await addComment(request, mdPath, 2, 'Unresolved two');

    await loadPage(page);

    // 1 resolved + 2 unresolved → badge should show 2 (unresolved only)
    await expect(countEl).toBeVisible();
    await expect(countEl).not.toHaveClass(/comment-count-resolved/);
    await expect(badgeEl).toHaveText('2');
  });

  test('badge increments when adding multiple comments', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const badgeEl = page.locator('#commentCountNumber');

    // Add two comments via API
    await addComment(request, mdPath, 1, 'First comment');
    await addComment(request, mdPath, 2, 'Second comment');

    // Reload to pick up API-added comments
    await loadPage(page);

    await expect(page.locator('#commentCount')).toBeVisible();
    await expect(badgeEl).toHaveText('2');
  });

  test('badge decrements when a comment is deleted', async ({ page }) => {
    const section = mdSection(page);
    const badgeEl = page.locator('#commentCountNumber');

    // Add two comments via UI
    const lineBlocks = section.locator('.line-block');

    await lineBlocks.first().hover();
    await section.locator('.line-comment-gutter').first().click();
    await page.locator('.comment-form textarea').fill('Comment one');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toHaveCount(1);

    const secondBlock = lineBlocks.nth(1);
    await secondBlock.hover();
    await section.locator('.line-comment-gutter').nth(1).click();
    await page.locator('.comment-form textarea').fill('Comment two');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toHaveCount(2);
    await expect(badgeEl).toHaveText('2');

    // Delete first comment
    await section.locator('.comment-actions .delete-btn').first().click();
    await expect(section.locator('.comment-card')).toHaveCount(1);
    await expect(badgeEl).toHaveText('1');
  });

  test('badge disappears when all comments are deleted', async ({ page }) => {
    const section = mdSection(page);
    const countEl = page.locator('#commentCount');
    const badgeEl = page.locator('#commentCountNumber');

    // Add a comment
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();
    await page.locator('.comment-form textarea').fill('Temporary comment');
    await page.locator('.comment-form .btn-primary').click();
    await expect(countEl).toBeVisible();
    await expect(badgeEl).toHaveText('1');

    // Delete it
    await section.locator('.comment-actions .delete-btn').click();
    await expect(countEl).toBeHidden();
    await expect(badgeEl).toHaveText('');
  });

  test('clicking the badge number toggles comments panel', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const badgeEl = page.locator('#commentCountNumber');

    // Add a comment via API and reload
    await addComment(request, mdPath, 1, 'Panel toggle test');
    await loadPage(page);

    await expect(badgeEl).toBeVisible();

    // Click the number to toggle the comments panel
    await badgeEl.click();
    const panel = page.locator('#commentsPanel');
    await expect(panel).not.toHaveClass(/comments-panel-hidden/);

    // Click again to close
    await badgeEl.click();
    await expect(panel).toHaveClass(/comments-panel-hidden/);
  });

  test('badge falls back to total count with resolved styling when only resolved comments exist', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const countEl = page.locator('#commentCount');
    const badgeEl = page.locator('#commentCountNumber');

    // Add a comment, then finish the round to resolve it
    await addComment(request, mdPath, 1, 'Will be resolved');
    await request.post('/api/finish');
    await request.post('/api/round-complete');

    await loadPage(page);

    // 0 unresolved + 1 resolved → badge shows total (1) with muted styling
    await expect(countEl).toBeVisible();
    await expect(countEl).toHaveClass(/comment-count-resolved/);
    await expect(badgeEl).toHaveText('1');
  });
});
