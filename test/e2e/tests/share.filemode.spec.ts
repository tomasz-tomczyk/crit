import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// ============================================================
// Share Feature — File Mode (default share_url, no --share-url flag)
// ============================================================
test.describe('Share — File Mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('share button is visible with default share_url and starts in its idle state', async ({ page }) => {
    await loadPage(page);

    const shareBtn = page.locator('#shareBtn');
    await expect(shareBtn).toBeVisible();
    await expect(shareBtn).toHaveText('Share');
    await expect(shareBtn).not.toHaveClass(/btn-success/);
    // No share toast or popover on initial load.
    await expect(page.locator('#toast-share')).toHaveCount(0);
    await expect(page.locator('.share-overlay')).toHaveCount(0);
  });
});
