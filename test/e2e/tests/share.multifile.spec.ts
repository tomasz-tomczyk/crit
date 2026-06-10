import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// ============================================================
// Share Feature — Multi-File Mode
// Share is handled server-side via POST /api/share. The Go server
// builds the payload from session state and forwards to crit-web.
// These tests verify the UI integration and API contract.
// ============================================================

test.describe('Share — Multi-File Mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('share button is visible when share URL is configured', async ({ page }) => {
    await loadPage(page);

    const shareBtn = page.locator('#shareBtn');
    await expect(shareBtn).toBeVisible();
  });

  test('config API returns a share_url', async ({ request }) => {
    const res = await request.get('/api/config');
    const config = await res.json();
    expect(config.share_url).toBeTruthy();
  });

  test('share button shows error toast when share service is unreachable', async ({ page, request }) => {
    await loadPage(page);

    // Add a comment so there's something to share
    await request.post('/api/file/comments?path=main.go', {
      data: { start_line: 1, end_line: 1, body: 'Test comment' },
    });
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // Click share — server will try to reach localhost:19999 which is down
    const shareBtn = page.locator('#shareBtn');
    await shareBtn.click();

    // Should show error toast (share service unreachable)
    await expect(page.locator('#toast-share')).toBeVisible();

    // Button should revert to default state
    await expect(shareBtn).toHaveText('Share');
  });

  test('session has multiple files confirming multi-file path is used', async ({ request }) => {
    const res = await request.get('/api/session');
    const session = await res.json();
    expect(session.files.length).toBeGreaterThan(1);
  });
});
