import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

// ============================================================
// Share Feature — File Mode (share button visible with default share_url)
// ============================================================
test.describe('Share — File Mode', () => {
  test('share button is visible with default share_url', async ({ page }) => {
    await loadPage(page);

    const shareBtn = page.locator('#shareBtn');
    await expect(shareBtn).toBeVisible();
  });

  test('no share-related toasts are shown on initial load', async ({ page }) => {
    await loadPage(page);

    const shareToast = page.locator('#toast-share');
    await expect(shareToast).toHaveCount(0);
  });
});
