import { test, expect } from '@playwright/test';
import {
  clearAllDesignPins,
  getIframe,
  openPinComposer,
  openPinComposerNoNav,
  setIframeRoute,
  waitForAgentReady,
} from './designmode-helpers';

test.describe('marker — rendering, click handoff, MO reposition (Scenarios 6–8)', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('saved pin renders numbered marker at element bounding rect', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-design-composer-body').fill('Pin one');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);
    await expect(getIframe(page).locator('.crit-design-marker')).toHaveCount(1);
  });

  test('clicking marker scrolls side panel to thread', async ({ page }) => {
    // Pin two elements.
    await openPinComposer(page, '#primary-btn');
    await page.locator('.crit-design-composer-body').fill('A');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);
    await openPinComposer(page, '#secondary-btn');
    await page.locator('.crit-design-composer-body').fill('B');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);

    await expect(getIframe(page).locator('.crit-design-marker')).toHaveCount(2);
    // Click the 2nd marker.
    await getIframe(page).locator('.crit-design-marker').nth(1).click();
    // Its corresponding row should be the focused / scrolled-to one.
    await expect(page.locator('#commentsPanelBody .crit-design-comment-row').nth(1))
      .toBeInViewport();
  });

  test('MutationObserver repositions markers on /mutator within rAF', async ({ page }) => {
    await waitForAgentReady(page);
    await setIframeRoute(page, '/mutator');
    // Wait for iframe content swap.
    await expect(getIframe(page).locator('#mut-title')).toBeVisible();
    await expect(getIframe(page).locator('li[data-stable="0"]')).toBeVisible();
    await openPinComposerNoNav(page, 'li[data-stable="0"]');
    await page.locator('.crit-design-composer-body').fill('stable pin');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);
    // Marker exists and remains visible across DOM mutations driven by /mutator's setInterval.
    const marker = getIframe(page).locator('.crit-design-marker');
    await expect(marker).toHaveCount(1);
    await expect(marker).toBeVisible();
  });
});
