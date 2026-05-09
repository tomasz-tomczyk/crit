// Regression coverage for 529e244 ("hide pin markers for resolved comments").
//
// The pin marker overlay inside the iframe receives `set-pins` from the
// design-mode chrome. design-mode-pin-filter.js drops resolved pins from
// that payload, so the marker for a resolved pin must vanish from the
// iframe even though the row remains in the side panel (filter pill
// covers panel visibility separately — see panel.designmode.spec.ts).
//
// Pre-fix: a resolved pin's marker stayed visible inside the iframe,
// occluded the underlying element, and was indistinguishable from open pins.
import { test, expect } from '@playwright/test';
import { clearAllDesignPins, getIframe, openPinComposer } from './designmode-helpers';

test.describe('design-mode markers — resolved pin visibility', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('reloading after a pin is resolved hides its marker', async ({ page, request }) => {
    // Author a pin and seed it as resolved via the resolve endpoint, then
    // load the chrome. design-mode-pin-filter.js drops resolved pins from
    // the set-pins payload, so the iframe overlay must render zero markers
    // even though the panel row remains.
    await openPinComposer(page);
    await page.locator('.crit-design-composer-body').fill('soon to be resolved');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);
    await expect(getIframe(page).locator('.crit-design-marker')).toHaveCount(1);

    const list = await request.get('/api/file/comments?path=%2F');
    const [comment] = await list.json() as Array<{ id: string }>;
    expect(comment?.id).toBeTruthy();
    const put = await request.put(
      `/api/comment/${comment.id}/resolve?path=%2F`,
      { data: { resolved: true } },
    );
    expect(put.ok()).toBeTruthy();

    // The resolve endpoint persists state but does not currently fire SSE,
    // so the live overlay can lag until the next chrome boot. Reloading is
    // the user-visible state we want to lock in: a resolved pin must NOT
    // paint a marker on a freshly-loaded chrome.
    await page.reload();
    await expect(page.locator('#critDesignIframe')).toBeVisible();
    await expect(getIframe(page).locator('.crit-design-marker')).toHaveCount(0);
    // Panel row is still present (resolved → goes to Resolved filter, but
    // the underlying row exists in DOM so the count badge stays accurate).
    await expect(page.locator('#commentsPanelBody .crit-design-comment-row')).toHaveCount(1);
  });

  test('reopening a resolved pin restores its marker on next chrome load', async ({ page, request }) => {
    await openPinComposer(page);
    await page.locator('.crit-design-composer-body').fill('toggle resolve');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);
    await expect(getIframe(page).locator('.crit-design-marker')).toHaveCount(1);

    const list = await request.get('/api/file/comments?path=%2F');
    const [comment] = await list.json() as Array<{ id: string }>;
    expect(comment?.id).toBeTruthy();

    await request.put(`/api/comment/${comment.id}/resolve?path=%2F`, { data: { resolved: true } });
    await page.reload();
    await expect(getIframe(page).locator('.crit-design-marker')).toHaveCount(0);

    await request.put(`/api/comment/${comment.id}/resolve?path=%2F`, { data: { resolved: false } });
    await page.reload();
    await expect(getIframe(page).locator('.crit-design-marker')).toHaveCount(1);
  });
});
