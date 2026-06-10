// Regression coverage for the round-transition fix bundle:
//
//   0a1c1d6  fix(live): reload iframe on round transition
//   4c9b46e  fix(live): refetch comments on round transition
//   3d0cbde  fix(live): refresh /api/session before each comment reload
//
// Pre-fix symptoms (caught by the user, not by E2E):
//   * Iframe kept the previous round's DOM after the agent shipped fixes,
//     so reviewers compared comments to stale UI.
//   * Comments authored or replied to during the previous round didn't
//     show up in the panel after the round bumped.
//
// The contract verified here:
//   1. POST /api/round-complete causes the in-iframe document to reload
//      (Playwright observes a fresh navigation request to the proxy origin).
//   2. The chrome's panel re-renders against the post-bump comment list,
//      including replies that were posted while the round was about to end.
//
// 5d4ca72 already added a unit-level reply-rendering regression. This file
// covers the integration shape — chrome + iframe + SSE + server.
import { test, expect } from '@playwright/test';
import { clearAllLivePins, getIframe, openPinComposer, waitForAgentReady } from './livemode-helpers';

test.describe('live-mode round transition', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('round-complete reloads the proxied iframe', async ({ page, request }) => {
    await waitForAgentReady(page);

    // Track main-frame navigations the iframe makes inside its proxy origin.
    // `framenavigated` fires on initial load too — count from a baseline so we
    // only assert on the bump-driven reload.
    const proxyNavigations: string[] = [];
    page.on('framenavigated', (frame) => {
      // Skip the chrome's top frame (only the proxied iframe is a child here).
      if (frame === page.mainFrame()) return;
      proxyNavigations.push(frame.url());
    });

    // Establish baseline navigation count after agent is ready.
    const baseline = proxyNavigations.length;

    const resp = await request.post('/api/round-complete');
    expect(resp.ok()).toBeTruthy();

    // The chrome's live-mode-sse handler fires reloadIframe() in response
    // to live-round-start. That triggers a fresh document load in the
    // child frame, which Playwright surfaces as another framenavigated.
    await expect.poll(
      () => proxyNavigations.length,
      { timeout: 10_000 },
    ).toBeGreaterThan(baseline);
  });

  test('round-complete refreshes the comment panel against the post-bump list', async ({ page, request }) => {
    // Seed a pin and a reply, bump the round, and verify both are still
    // visible in the panel after re-render. Pre-3d0cbde the panel could
    // miss replies posted around the bump because /api/session was cached.
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('round one pin');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);

    const row = page.locator('#commentsPanelBody .crit-live-comment-row').first();
    const pinId = await row.getAttribute('data-comment-id');
    expect(pinId).toBeTruthy();

    // Reply via the API (mirrors `crit comment --reply-to`).
    const reply = await request.post(
      `/api/comment/${pinId}/replies?path=%2F`,
      { data: { body: 'reply before round bump' } },
    );
    expect(reply.ok()).toBeTruthy();

    // Confirm the reply landed in the panel via SSE before bumping the
    // round — otherwise we can't tell whether a missing post-bump reply
    // means "carry-forward dropped it" or "SSE never rendered it".
    await expect(
      page.locator('#commentsPanelBody .crit-live-comment-replies .reply-body'),
    ).toContainText('reply before round bump');

    // Force a round transition.
    await request.post('/api/round-complete');

    // Panel still shows the parent + reply post-bump (the chrome re-renders
    // from the canonical comment list after live-round-start).
    await expect(page.locator('#commentsPanelBody .crit-live-comment-row')).toHaveCount(1);
    await expect(
      page.locator('#commentsPanelBody .crit-live-comment-replies .reply-body'),
    ).toContainText('reply before round bump');

    // Round counter reflects the bump (>1 → "Round #N").
    await expect(page.locator('#liveRoundCounter')).toHaveText(/Round #\d+/);
  });
});
