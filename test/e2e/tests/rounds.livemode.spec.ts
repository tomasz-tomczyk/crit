import { test, expect } from '@playwright/test';
import { clearAllLivePins, getIframe, seedLivePin, waitForAgentReady } from './livemode-helpers';

// Round-bumping in live mode happens via POST /api/round-complete (Phase E).
// The server emits a `live-round-start` SSE event; the chrome's
// applyRoundStart() resets per-pin _roundResolved and re-runs the agent
// resolution scan for the current path. The agent's pin-resolution-result
// then rebuilds markers after the iframe's anchor-resolution scan completes.

test.describe('rounds — round-start re-resolution (Scenarios 15–16)', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('round 2 re-resolves the same marker when its target is unchanged', async ({ page, request }) => {
    // Seed a pin against #primary-btn, then bump the round. The pin must
    // survive carry-forward in /api/file/comments?path=/ with its anchor
    // intact and Drifted unchanged (false). This guards against the gap
    // where live pins were dropped from the comment array on round bump.
    await waitForAgentReady(page);
    const seeded = await seedLivePin(request, 'stable pin', {
      pathname: '/',
      css_selector: '#primary-btn',
      tag_chain: ['BUTTON'],
    });
    const marker = getIframe(page).locator(`.crit-live-marker[data-pin-id="${seeded.id}"]`);
    await expect(marker).toHaveCount(1);

    // Flush the in-memory pin to disk before round-complete; the carry-forward
    // pipeline reloads PreviousComments from disk in handleRoundCompleteFiles.
    const finish = await request.post('/api/finish');
    await expect(finish).toBeOK();

    const beforeSession = await request.get('/api/session');
    await expect(beforeSession).toBeOK();
    const { review_round: previousRound } = await beforeSession.json() as { review_round: number };

    await page.evaluate(() => {
      (window as unknown as { __critLiveMessages?: unknown[] }).__critLiveMessages = [];
    });

    const bump = await request.post('/api/round-complete');
    await expect(bump).toBeOK();

    await expect(page.locator('#liveRoundCounter')).toHaveText(
      `Round #${previousRound + 1}`,
      { timeout: 15_000 },
    );
    await expect.poll(async () => {
      return page.evaluate(() => {
        const log = (window as unknown as { __critLiveMessages?: { type: string }[] })
          .__critLiveMessages || [];
        return log.some(message => message.type === 'agent-ready');
      });
    }, { timeout: 15_000 }).toBe(true);
    await expect.poll(() => page.evaluate(() => {
      return (window as unknown as {
        crit?: { live?: { resolutionCache?: Record<string, string> } };
      }).crit?.live?.resolutionCache?.['/'] ?? null;
    }), { timeout: 15_000 }).toBe('fresh');
    await expect(marker).toHaveCount(1);

    const after = await request.get('/api/file/comments?path=%2F');
    await expect(after).toBeOK();
    const afterBody = await after.json() as Array<{
      id: string;
      drifted?: boolean;
      drifted_on_round?: number;
      dom_anchor?: { css_selector?: string };
      carried_forward?: boolean;
    }>;
    const pin = afterBody.find((c) => c.dom_anchor?.css_selector === '#primary-btn');
    expect(pin, 'pin must survive round-complete').toBeDefined();
    expect(pin!.id).toBe(seeded.id);
    expect(pin!.dom_anchor?.css_selector).toBe('#primary-btn');
    expect(pin!.carried_forward).toBe(true);
    expect(pin!.drifted ?? false).toBe(false);
    expect(pin!.drifted_on_round ?? 0).toBe(0);
  });

});
