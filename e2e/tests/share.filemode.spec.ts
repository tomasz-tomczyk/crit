import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

// ============================================================
// Share Feature — File Mode (share button hidden without --share-url)
// ============================================================
test.describe('Share — File Mode', () => {
  test('share button is hidden when no share URL configured', async ({ page }) => {
    await loadPage(page);

    const shareBtn = page.locator('#shareBtn');
    await expect(shareBtn).toBeHidden();
  });

  test('no share-related toasts are shown on initial load', async ({ page }) => {
    await loadPage(page);

    const shareToast = page.locator('#toast-share');
    await expect(shareToast).toHaveCount(0);
  });
});
