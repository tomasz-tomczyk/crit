import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

// ============================================================
// Share Feature — File Mode
// ============================================================
test.describe('Share — File Mode', () => {
  test('share button is visible with default share_url', async ({ page }) => {
    await loadPage(page);

    const shareBtn = page.locator('#shareBtn');
    await expect(shareBtn).toBeVisible();
    await expect(shareBtn).toHaveText('Share');
  });

  test('share button does not have success styling initially', async ({ page }) => {
    await loadPage(page);

    const shareBtn = page.locator('#shareBtn');
    await expect(shareBtn).not.toHaveClass(/btn-success/);
  });

  test('no share-related toasts on initial load', async ({ page }) => {
    await loadPage(page);

    const shareToast = page.locator('#toast-share');
    await expect(shareToast).toHaveCount(0);
  });

  test('no share popover on initial load', async ({ page }) => {
    await loadPage(page);

    const modal = page.locator('.share-overlay');
    await expect(modal).toHaveCount(0);
  });
});
