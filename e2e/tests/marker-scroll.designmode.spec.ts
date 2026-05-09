// Regression coverage for 2f6afe7 ("anchor pin marker to document not viewport").
//
// Pre-fix the marker was `position: fixed` and only recomputed on
// MutationObserver callbacks — scrolling the proxied page did NOT move the
// marker, so it drifted away from the element it annotated. The fix moved
// markers to `position: absolute` in document coords (under #crit-marker-root
// at the document origin, transform = rect + scrollY/scrollX).
//
// Test contract: pin a target, scroll the iframe, and assert the target's
// viewport-Y and the marker's viewport-Y change by the SAME delta. If the
// marker were viewport-anchored again, its viewport-Y would stay constant
// while the target's dropped — the difference would diverge.
import { test, expect } from '@playwright/test';
import { clearAllDesignPins, getIframe, openPinComposer } from './designmode-helpers';

test.describe('design-mode markers — scroll anchoring', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('marker tracks its element when the iframe scrolls', async ({ page }) => {
    // Pin the primary button on the upstream root.
    await openPinComposer(page, '#primary-btn');
    await page.locator('.crit-design-composer-body').fill('scroll anchor pin');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);

    const ifr = getIframe(page);
    const target = ifr.locator('#primary-btn');
    const marker = ifr.locator('.crit-design-marker').first();
    await expect(marker).toBeVisible();

    // Make the iframe document tall enough to scroll. Inject a tall spacer
    // BEFORE the target so scrolling the document moves both the target
    // and (if correctly anchored) the marker. Element identity is preserved.
    await ifr.locator('body').evaluate((body) => {
      const spacer = document.createElement('div');
      spacer.id = '__crit_test_spacer';
      spacer.style.height = '1500px';
      spacer.style.background = 'transparent';
      body.insertBefore(spacer, body.firstChild);
    });

    // Wait for the agent's MutationObserver to re-anchor the marker to the
    // target's new document-relative position. Without this we'd race the
    // observer and the "before" measurement could capture the marker still
    // at its pre-spacer location.
    await expect.poll(
      async () => {
        const t = await target.boundingBox();
        const m = await marker.boundingBox();
        if (!t || !m) return null;
        // Marker anchors at the element's top-left corner; settle when both
        // are within a few px of each other vertically.
        return Math.abs(t.y - m.y) < 5;
      },
      { timeout: 5_000 },
    ).toBe(true);

    // Pre-scroll measurement.
    const before = {
      target: await target.boundingBox(),
      marker: await marker.boundingBox(),
    };
    expect(before.target).not.toBeNull();
    expect(before.marker).not.toBeNull();

    // Scroll the iframe document by a fixed amount.
    const scrollDelta = 600;
    await ifr.locator('body').evaluate((_, dy) => {
      window.scrollTo(0, dy);
    }, scrollDelta);

    // Both the target and the marker must shift by approximately the same
    // amount in viewport coords. Allow a small tolerance for sub-pixel
    // rounding inside transform translation.
    await expect.poll(
      async () => {
        const t = await target.boundingBox();
        const m = await marker.boundingBox();
        if (!t || !m || !before.target || !before.marker) return null;
        return {
          targetDelta: Math.round(before.target.y - t.y),
          markerDelta: Math.round(before.marker.y - m.y),
        };
      },
      { timeout: 5_000 },
    ).toEqual({ targetDelta: scrollDelta, markerDelta: scrollDelta });
  });
});
