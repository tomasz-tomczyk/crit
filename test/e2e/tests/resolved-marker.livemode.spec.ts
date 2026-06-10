// Regression coverage for 529e244 ("hide pin markers for resolved comments").
//
// The pin marker overlay inside the iframe receives `set-pins` from the
// live-mode chrome. live-mode-pin-filter.js drops resolved pins from
// that payload, so the marker for a resolved pin must vanish from the
// iframe even though the row remains in the side panel (filter pill
// covers panel visibility separately — see panel.livemode.spec.ts).
//
// Pre-fix: the local resolve-click handler updated state and refreshed the
// panel but did NOT call pushPinsToAgent(), so the iframe overlay still
// painted the marker for a just-resolved pin until the user reloaded.
// Post-fix: the originating tab repaints in-place — no reload required.
import { test, expect } from '@playwright/test';
import { clearAllLivePins, getIframe, openPinComposer } from './livemode-helpers';

test.describe('live-mode markers — resolved pin visibility', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('resolving a pin in the panel hides its marker without reload', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('soon to be resolved');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);
    await expect(getIframe(page).locator('.crit-live-marker')).toHaveCount(1);

    // Click the panel-row resolve button. The local handler PUTs /resolve,
    // mutates state, refreshes the panel, AND must re-push pins to the
    // iframe agent so the marker overlay drops the now-resolved pin.
    await page.locator('#commentsPanelBody .crit-live-comment-resolve').first().click();

    // No reload — the originating tab must repaint in-place.
    await expect(getIframe(page).locator('.crit-live-marker')).toHaveCount(0);
    // Panel row is still present (resolved → moves to Resolved filter, but
    // the underlying row exists in DOM so the count badge stays accurate).
    await expect(page.locator('#commentsPanelBody .crit-live-comment-row')).toHaveCount(1);
  });

  test('reopening a resolved pin restores its marker without reload', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('toggle resolve');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);
    await expect(getIframe(page).locator('.crit-live-marker')).toHaveCount(1);

    // Resolve via panel row → marker disappears in-place.
    const resolveBtn = page.locator('#commentsPanelBody .crit-live-comment-resolve').first();
    await resolveBtn.click();
    await expect(getIframe(page).locator('.crit-live-marker')).toHaveCount(0);

    // Click again — same button toggles to "reopen". Marker must reappear
    // without reload.
    await page.locator('#commentsPanelBody .crit-live-comment-resolve').first().click();
    await expect(getIframe(page).locator('.crit-live-marker')).toHaveCount(1);
  });
});
