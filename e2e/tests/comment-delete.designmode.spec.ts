// Regression coverage for the design-mode comment-delete affordance:
//
//   27ec877  fix(design): add delete affordance to comment cards
//   2c8b87d  fix(design): delete endpoint with SSE fanout
//
// Pre-fix users had no UI to delete a design pin in the chrome — they had
// to fall back to API or CLI. The fix added a `.crit-design-comment-delete`
// button on each card and wired it through DELETE /api/comment/{id}, which
// in turn now broadcasts comments-changed (the SSE fan-out half is covered
// in sse-fanout.designmode.spec.ts).
//
// This file covers the local-tab UX: the button exists, clicking it removes
// the row, and the marker disappears from the iframe overlay.
import { test, expect } from '@playwright/test';
import { clearAllDesignPins, getIframe, openPinComposer } from './designmode-helpers';

test.describe('design-mode comment delete', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('delete button removes the row and the marker', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-design-composer-body').fill('to be deleted');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);

    const row = page.locator('#commentsPanelBody .crit-design-comment-row').first();
    await expect(row).toBeVisible();
    await expect(getIframe(page).locator('.crit-design-marker')).toHaveCount(1);

    // Hover-gated affordance — dispatch directly.
    await row.locator('.crit-design-comment-delete').dispatchEvent('click');

    await expect(page.locator('#commentsPanelBody .crit-design-comment-row')).toHaveCount(0);
    await expect(getIframe(page).locator('.crit-design-marker')).toHaveCount(0);
  });

  test('delete fires DELETE /api/comment/{id} with the right path query', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-design-composer-body').fill('delete API check');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);

    const row = page.locator('#commentsPanelBody .crit-design-comment-row').first();
    const pinId = await row.getAttribute('data-comment-id');
    expect(pinId).toBeTruthy();

    const deletePromise = page.waitForResponse((r) =>
      r.request().method() === 'DELETE'
      && r.url().includes(`/api/comment/${pinId}`)
      && r.url().includes('path=%2F'),
    );
    await row.locator('.crit-design-comment-delete').dispatchEvent('click');
    const resp = await deletePromise;
    expect(resp.ok()).toBeTruthy();
  });
});
