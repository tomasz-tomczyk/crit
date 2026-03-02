import { test, expect } from '@playwright/test';

test('server is running and page loads', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
  await expect(page.locator('.file-section')).not.toHaveCount(0);
});
