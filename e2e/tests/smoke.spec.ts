import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

test('server is running and page loads', async ({ page }) => {
  await loadPage(page);
  await expect(page.locator('.file-section')).not.toHaveCount(0);
});
