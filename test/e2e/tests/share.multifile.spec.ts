import { test, expect } from '@playwright/test';
import { addComment, clearAllComments, loadPage } from './helpers';

// ============================================================
// Share Feature — Multi-File Mode
// Share is handled server-side via POST /api/share. The Go server
// builds the payload from session state and forwards to crit-web.
// The fixture passes --share-url http://localhost:19999 (nothing listens).
// ============================================================

test.describe('Share — Multi-File Mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('configured share URL shows the button; an unreachable service shows an error toast', async ({ page, request }) => {
    // --share-url is exposed through the config API.
    const config = await (await request.get('/api/config')).json();
    expect(config.share_url).toBeTruthy();

    // Add a comment so there's something to share, before the page loads.
    await addComment(request, 'main.go', 1, 'Test comment');
    await loadPage(page);

    const shareBtn = page.locator('#shareBtn');
    await expect(shareBtn).toBeVisible();

    // Click share — server will try to reach localhost:19999 which is down
    await shareBtn.click();
    await expect(page.locator('#toast-share')).toBeVisible();

    // Button should revert to default state
    await expect(shareBtn).toHaveText('Share');
  });
});
