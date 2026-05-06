import { test, expect } from '@playwright/test';

// Phase F runner not online yet. Scenarios scaffolded for later activation.

test.describe('design-mode pin composer — M11 sustained highlight', () => {
  test.skip(true, 'phase F runner');

  test('clicking an element in Pin mode keeps it outlined while composer is open', async ({ page }) => {
    await page.goto('/design');
    // Switch to Pin mode (M5)
    await page.locator('[data-mode="pin"]').click();
    // Click an element inside the iframe (placeholder selector — depends on
    // Phase F fixture target site).
    const iframe = page.frameLocator('#critDesignIframe');
    await iframe.locator('[data-test="pin-target"]').click();
    // Composer should be visible
    await expect(page.locator('.crit-design-composer')).toBeVisible();
    // Captured element should carry the pending-highlight class
    await expect(iframe.locator('.crit-design-pending-highlight')).toHaveCount(1);
  });

  test('Cancel removes the highlight', async ({ page }) => {
    await page.goto('/design');
    await page.locator('[data-mode="pin"]').click();
    const iframe = page.frameLocator('#critDesignIframe');
    await iframe.locator('[data-test="pin-target"]').click();
    await expect(iframe.locator('.crit-design-pending-highlight')).toHaveCount(1);
    await page.locator('.crit-design-composer-cancel').click();
    await expect(iframe.locator('.crit-design-pending-highlight')).toHaveCount(0);
  });

  test('Escape removes the highlight', async ({ page }) => {
    await page.goto('/design');
    await page.locator('[data-mode="pin"]').click();
    const iframe = page.frameLocator('#critDesignIframe');
    await iframe.locator('[data-test="pin-target"]').click();
    await page.locator('.crit-design-composer-body').focus();
    await page.keyboard.press('Escape');
    await expect(iframe.locator('.crit-design-pending-highlight')).toHaveCount(0);
  });

  test('Save removes the highlight after the comment is created', async ({ page }) => {
    await page.goto('/design');
    await page.locator('[data-mode="pin"]').click();
    const iframe = page.frameLocator('#critDesignIframe');
    await iframe.locator('[data-test="pin-target"]').click();
    await page.locator('.crit-design-composer-body').fill('A pin comment.');
    await page.locator('.crit-design-composer-save').click();
    await expect(iframe.locator('.crit-design-pending-highlight')).toHaveCount(0);
  });

  test('route change auto-clears the highlight', async ({ page }) => {
    await page.goto('/design');
    await page.locator('[data-mode="pin"]').click();
    const iframe = page.frameLocator('#critDesignIframe');
    await iframe.locator('[data-test="pin-target"]').click();
    // Trigger an SPA navigation inside the iframe.
    await iframe.locator('[data-test="nav-other"]').click();
    await expect(iframe.locator('.crit-design-pending-highlight')).toHaveCount(0);
  });
});
