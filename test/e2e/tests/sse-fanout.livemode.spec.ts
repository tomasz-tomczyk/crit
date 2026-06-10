// Regression coverage for the live-mode SSE fan-out bugs.
//
//   ea5297e  fix(live): live-render comment replies via SSE
//   db0a12f  fix(live): broadcast comment mutations over SSE in live mode
//   2c8b87d  fix(live): delete endpoint with SSE fanout
//   3d0cbde  fix(live): refresh /api/session before each comment reload
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
// live-mode.sse.js subscribes to /api/events and reacts to
// `comments-changed` by re-fetching the canonical list and re-rendering the
// panel. This file asserts the user-visible side of that contract.
import { test, expect, type BrowserContext, type Page } from '@playwright/test';
import { clearAllLivePins, openPinComposer, waitForAgentReady } from './livemode-helpers';

async function openSecondTab(context: BrowserContext): Promise<Page> {
  const page = await context.newPage();
  await waitForAgentReady(page);
  return page;
}

test.describe('live-mode SSE — cross-tab comment fan-out', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('a pin saved in tab A appears in tab B without reload', async ({ page, context }) => {
    const tabB = await openSecondTab(context);
    await expect(tabB.locator('#commentsPanelBody .crit-live-comment-row')).toHaveCount(0);

    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('cross-tab pin');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);

    // Tab B's panel re-renders via comments-changed SSE.
    await expect(tabB.locator('#commentsPanelBody .crit-live-comment-row')).toHaveCount(1);
    await expect(tabB.locator('#commentsPanelBody .comment-body')).toContainText('cross-tab pin');
    await expect(tabB.locator('#commentsPanelCountBadge')).toHaveText('1');
  });

  test('a CLI-style reply (POST /api/comment/{id}/replies) fans out live', async ({ page, request }) => {
    // Seed a pin via the UI so the panel is wired up.
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('parent pin');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);

    const row = page.locator('#commentsPanelBody .crit-live-comment-row').first();
    const pinId = await row.getAttribute('data-comment-id');
    expect(pinId).toBeTruthy();

    // Pre-fix this POST did not bump comments-changed in live mode, so the
    // panel never picked up the reply.
    const reply = await request.post(
      `/api/comment/${pinId}/replies?path=%2F`,
      { data: { body: 'reply via CLI' } },
    );
    expect(reply.ok()).toBeTruthy();

    const replyRow = page.locator(
      '#commentsPanelBody .crit-live-comment-replies .crit-live-comment-reply',
    ).first();
    await expect(replyRow).toBeVisible();
    await expect(replyRow.locator('.reply-body')).toContainText('reply via CLI');
  });

  test('deleting a pin in tab A removes its row + marker in tab B', async ({ page, context }) => {
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('to be deleted');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);

    const tabB = await openSecondTab(context);
    await expect(tabB.locator('#commentsPanelBody .crit-live-comment-row')).toHaveCount(1);
    const ifrB = tabB.frameLocator('#critLiveIframe');
    await expect(ifrB.locator('.crit-live-marker')).toHaveCount(1);

    // Delete in tab A through the same UI path that surfaced the bug.
    // The delete button is hover-gated; dispatchEvent bypasses hover.
    // Live-mode delete has no confirm prompt (parity with code-review).
    const row = page.locator('#commentsPanelBody .crit-live-comment-row').first();
    await row.locator('.crit-live-comment-delete').dispatchEvent('click');

    // Tab B picks up the SSE comments-changed and clears its row + marker.
    await expect(tabB.locator('#commentsPanelBody .crit-live-comment-row')).toHaveCount(0);
    await expect(ifrB.locator('.crit-live-marker')).toHaveCount(0);
  });
});
