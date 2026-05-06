import { test, expect } from '@playwright/test';
import {
  clearAllDesignPins,
  getIframe,
  openPinComposer,
  setViewportPreset,
} from './designmode-helpers';

test.describe('viewport — preset round-trip (Scenario 9)', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('Mobile preset resizes iframe frame to 390', async ({ page }) => {
    await page.goto('/design');
    await expect(page.locator('#critDesignIframe')).toBeVisible();

    await setViewportPreset(page, 'mobile');
    await expect.poll(async () => {
      const w = await page.locator('.crit-design-iframe-frame').boundingBox();
      return Math.round(w?.width ?? 0);
    }).toBe(390);
  });

  test('marker re-resolves after viewport change', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-design-composer-body').fill('Pin');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);
    await expect(getIframe(page).locator('.crit-design-marker')).toHaveCount(1);
    await setViewportPreset(page, 'mobile');
    // After viewport change the marker still tracks one element.
    await expect(getIframe(page).locator('.crit-design-marker')).toHaveCount(1);
    await expect(getIframe(page).locator('.crit-design-marker')).toBeVisible();
  });
});
