import { test, expect, type Page } from '@playwright/test';

async function loadPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
}

// ============================================================
// Share Feature — Git Mode (default share URL: crit.live)
// ============================================================
test.describe('Share — Default Configuration', () => {
  test('share button is visible by default', async ({ page }) => {
    await loadPage(page);

    const shareBtn = page.locator('#shareBtn');
    await expect(shareBtn).toBeVisible();
    await expect(shareBtn).toHaveText('Share');
  });

  test('config API returns default share_url', async ({ request }) => {
    const res = await request.get('/api/config');
    const config = await res.json();
    expect(config.share_url).toBeTruthy();
  });

  test('config API returns empty hosted_url initially', async ({ request }) => {
    const res = await request.get('/api/config');
    const config = await res.json();
    expect(config.hosted_url).toBe('');
  });

  test('config API returns empty delete_token initially', async ({ request }) => {
    const res = await request.get('/api/config');
    const config = await res.json();
    expect(config.delete_token).toBe('');
  });

  test('no share-related toasts are shown on initial load', async ({ page }) => {
    await loadPage(page);

    const shareToast = page.locator('#toast-share');
    await expect(shareToast).toHaveCount(0);
  });

  test('share button is enabled and clickable', async ({ page }) => {
    await loadPage(page);

    const shareBtn = page.locator('#shareBtn');
    await expect(shareBtn).toBeEnabled();
  });
});
