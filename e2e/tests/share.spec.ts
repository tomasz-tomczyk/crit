import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

// ============================================================
// Share Feature — Git Mode (share button hidden)
// ============================================================
test.describe('Share — Git Mode', () => {
  test('share button is hidden in git mode', async ({ page }) => {
    await loadPage(page);

    const shareBtn = page.locator('#shareBtn');
    await expect(shareBtn).toBeHidden();
  });

  test('config API returns empty share_url by default', async ({ request }) => {
    const res = await request.get('/api/config');
    const config = await res.json();
    expect(config.share_url).toBe('');
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
});
