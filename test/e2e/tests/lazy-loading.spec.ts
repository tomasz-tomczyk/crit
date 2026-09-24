import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments, treeFiles, reviewFileOrder } from './helpers';

test.describe('Lazy loading', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('session API does not mark files as lazy when under threshold', async ({ request }) => {
    const res = await request.get('/api/session');
    const session = await res.json();

    // Git-mode fixture has <25 files — none should be lazy
    expect(session.files.length).toBeGreaterThan(0);
    expect(session.files.length).toBeLessThan(25);

    for (const file of session.files) {
      expect(file.lazy).toBeFalsy();
    }
  });

  test('all files render fully when under threshold', async ({ page }) => {
    await loadPage(page);

    // Sidebar lists every file; file-list virt mounts a window of sections.
    await expect(treeFiles(page).first()).toBeVisible();
    expect((await reviewFileOrder(page)).length).toBeGreaterThan(0);
    await expect(page.locator('#filesContainer .file-section').first()).toBeVisible();

    // No file should have the loading spinner class
    await expect(page.locator('.file-section-loading')).toHaveCount(0);
  });
});
