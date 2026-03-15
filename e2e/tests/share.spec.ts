import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

// ============================================================
// Share Feature — Git Mode (share button hidden even with default share_url)
// ============================================================
test.describe('Share — Git Mode', () => {
  test('share button is hidden in git mode', async ({ page }) => {
    await loadPage(page);

    const shareBtn = page.locator('#shareBtn');
    await expect(shareBtn).toBeHidden();
  });

  test('config API returns default share_url', async ({ request }) => {
    const res = await request.get('/api/config');
    const config = await res.json();
    expect(config.share_url).toBe('https://crit.live');
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
