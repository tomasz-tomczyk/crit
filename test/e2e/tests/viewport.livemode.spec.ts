import { test, expect } from '@playwright/test';
import {
  clearAllLivePins,
  getIframe,
  openPinComposer,
  setViewportPreset,
} from './livemode-helpers';

test.describe('viewport — preset round-trip (Scenario 9)', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('Mobile preset resizes iframe frame to 390', async ({ page }) => {
    await page.goto('/live');
    await expect(page.locator('#critLiveIframe')).toBeVisible();

    await setViewportPreset(page, 'mobile');
    await expect.poll(async () => {
      const w = await page.locator('.crit-live-iframe-frame').boundingBox();
      return Math.round(w?.width ?? 0);
    }).toBe(390);
  });

  test('marker re-resolves after viewport change', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('Pin');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);
    await expect(getIframe(page).locator('.crit-live-marker')).toHaveCount(1);
    await setViewportPreset(page, 'mobile');
    // After viewport change the marker still tracks one element.
    await expect(getIframe(page).locator('.crit-live-marker')).toHaveCount(1);
    await expect(getIframe(page).locator('.crit-live-marker')).toBeVisible();
  });
});
