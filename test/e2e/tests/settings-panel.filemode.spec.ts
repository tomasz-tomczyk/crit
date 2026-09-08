import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments } from './helpers';

test.describe('Settings Panel — File Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('settings panel opens and theme toggle works in file mode', async ({ page }) => {
    await page.click('#settingsToggle');
    await expect(page.locator('.settings-overlay')).toHaveClass(/active/);
    await expect(page.locator('.settings-tab.active')).toHaveText('Settings');

    await page.click('[data-settings-theme="dark"]');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await page.click('[data-settings-theme="light"]');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  });

  test('no old shortcuts overlay in DOM', async ({ page }) => {
    await expect(page.locator('#shortcutsOverlay')).toHaveCount(0);
  });
});
