import { test, expect } from '@playwright/test';
import { clearAllDesignPins, setViewportPreset } from './designmode-helpers';

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

  test.fixme('marker re-resolves after viewport change', async () => {
    // FIXME: depends on agent-ready handshake (Bug 2). Once Pin works,
    // pin → switch viewport → assert marker still at element rect.
  });
});
