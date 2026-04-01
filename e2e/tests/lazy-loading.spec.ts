import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments } from './helpers';

test.describe('Lazy loading', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('session API does not mark files as lazy when under threshold', async ({ request }) => {
    const res = await request.get('/api/session');
    const session = await res.json();

    // Git-mode fixture has <100 files — none should be lazy
    expect(session.files.length).toBeGreaterThan(0);
    expect(session.files.length).toBeLessThan(100);

    for (const file of session.files) {
      expect(file.lazy).toBeFalsy();
    }
  });

  test('all files render fully when under threshold', async ({ page }) => {
    await loadPage(page);

    // All file sections should be present and expandable
    const sections = page.locator('.file-section');
    const count = await sections.count();
    expect(count).toBeGreaterThan(0);

    // No file should have the loading spinner class
    await expect(page.locator('.file-section-loading')).toHaveCount(0);
  });
});
