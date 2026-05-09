// Regression coverage for the design-mode SSE fan-out bugs.
//
//   ea5297e  fix(design): live-render comment replies via SSE
//   db0a12f  fix(design): broadcast comment mutations over SSE in design mode
//   2c8b87d  fix(design): delete endpoint with SSE fanout
//   3d0cbde  fix(design): refresh /api/session before each comment reload
//
// Pre-fix symptoms (caught by the user, not by E2E):
//   * CLI-driven `crit comment --reply-to` writes never reached an open chrome.
//   * Comment deletes via the comment-card UI did not propagate to other tabs.
//   * Replies posted in tab A never showed up in tab B.
//
// These tests open two pages on the same daemon and verify that mutations
// performed in one (or via direct API — same path the CLI takes via the
// session's HTTP server) become visible in the other without a manual reload.
//
// design-mode.sse.js subscribes to /api/events and reacts to
// `comments-changed` by re-fetching the canonical list and re-rendering the
// panel. This file asserts the user-visible side of that contract.
import { test, expect, type BrowserContext, type Page } from '@playwright/test';
import { clearAllDesignPins, openPinComposer, waitForAgentReady } from './designmode-helpers';

async function openSecondTab(context: BrowserContext): Promise<Page> {
  const page = await context.newPage();
  await waitForAgentReady(page);
  return page;
}

test.describe('design-mode SSE — cross-tab comment fan-out', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('a pin saved in tab A appears in tab B without reload', async ({ page, context }) => {
    const tabB = await openSecondTab(context);
    await expect(tabB.locator('#commentsPanelBody .crit-design-comment-row')).toHaveCount(0);

    await openPinComposer(page);
    await page.locator('.crit-design-composer-body').fill('cross-tab pin');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);

    // Tab B's panel re-renders via comments-changed SSE.
    await expect(tabB.locator('#commentsPanelBody .crit-design-comment-row')).toHaveCount(1);
    await expect(tabB.locator('#commentsPanelBody .comment-body')).toContainText('cross-tab pin');
    await expect(tabB.locator('#commentsPanelCountBadge')).toHaveText('1');
  });

  test('a CLI-style reply (POST /api/comment/{id}/replies) fans out live', async ({ page, request }) => {
    // Seed a pin via the UI so the panel is wired up.
    await openPinComposer(page);
    await page.locator('.crit-design-composer-body').fill('parent pin');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);

    const row = page.locator('#commentsPanelBody .crit-design-comment-row').first();
    const pinId = await row.getAttribute('data-comment-id');
    expect(pinId).toBeTruthy();

    // Pre-fix this POST did not bump comments-changed in design mode, so the
    // panel never picked up the reply.
    const reply = await request.post(
      `/api/comment/${pinId}/replies?path=%2F`,
      { data: { body: 'reply via CLI' } },
    );
    expect(reply.ok()).toBeTruthy();

    const replyRow = page.locator(
      '#commentsPanelBody .crit-design-comment-replies .crit-design-comment-reply',
    ).first();
    await expect(replyRow).toBeVisible();
    await expect(replyRow.locator('.reply-body')).toContainText('reply via CLI');
  });

  test('deleting a pin in tab A removes its row + marker in tab B', async ({ page, context }) => {
    await openPinComposer(page);
    await page.locator('.crit-design-composer-body').fill('to be deleted');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);

    const tabB = await openSecondTab(context);
    await expect(tabB.locator('#commentsPanelBody .crit-design-comment-row')).toHaveCount(1);
    const ifrB = tabB.frameLocator('#critDesignIframe');
    await expect(ifrB.locator('.crit-design-marker')).toHaveCount(1);

    // Delete in tab A through the same UI path that surfaced the bug.
    // The delete button is hover-gated; dispatchEvent bypasses hover.
    // Design-mode delete has no confirm prompt (parity with code-review).
    const row = page.locator('#commentsPanelBody .crit-design-comment-row').first();
    await row.locator('.crit-design-comment-delete').dispatchEvent('click');

    // Tab B picks up the SSE comments-changed and clears its row + marker.
    await expect(tabB.locator('#commentsPanelBody .crit-design-comment-row')).toHaveCount(0);
    await expect(ifrB.locator('.crit-design-marker')).toHaveCount(0);
  });
});
