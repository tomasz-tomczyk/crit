import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

// Single-file mode hides the file header (the header bar names the file), so
// there is no Viewed box or chevron: a and o must leave the file alone.
test.describe('File Shortcuts — Single File Mode', () => {
  test('a and o do not hide the only file', async ({ page }) => {
    await loadPage(page);
    const doc = page.locator('#filesContainer .line-block').first();
    await expect(doc).toBeVisible();

    await doc.hover();
    await page.keyboard.press('j');
    await page.keyboard.press('a');
    await page.keyboard.press('o');

    await expect(doc).toBeVisible();
    await expect(page.locator('.pierre-file-header.collapsed')).toHaveCount(0);
  });
});
