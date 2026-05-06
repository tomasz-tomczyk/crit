import { test, expect } from '@playwright/test';
import { clearAllDesignPins } from './designmode-helpers';

// Round-bumping in design mode happens via POST /api/round-complete (Phase E).
// The server emits a `design-round-start` SSE event; the chrome's
// applyRoundStart() resets per-pin _roundResolved and re-runs the agent
// resolution scan for the current path. The agent's pin-resolution-result
// then drives a PUT /api/comment/{id} with drifted_on_round when a pin's
// anchor no longer resolves.

test.describe('rounds — round-start re-resolution (Scenarios 15–16)', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('round-complete endpoint accepts POST', async ({ request }) => {
    const resp = await request.post('/api/round-complete');
    expect(resp.ok()).toBeTruthy();
    const body = await resp.json();
    expect(body).toEqual({ status: 'ok' });
  });

  // FIXME(design-rounds-carryforward): Both scenarios below require design
  // pins to survive round-complete carry-forward. Empirically, both
  // seedDesignPin() and a real composer-driven save produce a pin in
  // /api/file/comments?path=/ in round 1, but POST /api/round-complete
  // wipes the comment array (`comment_count: 0` post-bump). Until the
  // backend carry-forward path threads design pins through round 2 the
  // same way it does code-review comments (watch.go#carryForwardAllComments
  // for type=design-route entries), neither the "stable pin" nor the
  // "drift PUT" round-trip can be observed at the test boundary.
  //
  // Spec text intentionally preserved so the regression contract is
  // documented even while behaviour is gated on the backend fix.
  test.fixme('round 2 resolves cleanly when target element is unchanged', async () => {
    // Seed a pin against #primary-btn (selector matches + tag_chain verifies),
    // wait for agent-ready, bumpRound(), then assert the pin is still in
    // /api/file/comments?path=/ with drifted=false and that no PUT carrying
    // {drifted_on_round: 2} fired.
  });

  test.fixme('round 2 flags Drifted when DOM mutated between rounds', async () => {
    // Seed a pin, wait for agent-ready, mutate the DOM so the pin no longer
    // resolves (rename id, drop accessible name), bumpRound(), then poll
    // /api/file/comments?path=/ until the pin reports drifted:true and
    // drifted_on_round>=2. Today the pin disappears after round-complete,
    // making this assertion impossible at the API boundary.
  });
});
