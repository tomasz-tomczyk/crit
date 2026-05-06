import { test, expect } from '@playwright/test';
import { clearAllDesignPins } from './designmode-helpers';

// Round-bumping in design mode happens via POST /api/round-complete (Phase E).
// The full re-resolution flow (round-start SSE → re-anchor → drift flagging)
// requires saving a pin first, which requires Pin mode + agent-ready (Bug 2).
// Authored against intent; fixme until handshake lands.

test.describe('rounds — round-start re-resolution (Scenarios 15–16)', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test.fixme('round-complete endpoint accepts POST and bumps round', async () => {
    // FIXME: there is no /api/round-complete endpoint in server.go yet; the
    // plan referenced it but it isn't shipped. /api/review-cycle is the
    // long-poll endpoint that signals round-complete internally. This needs
    // either a dedicated endpoint or a different round-bump path. Track in
    // Phase E follow-up.
  });

  test.fixme('round 2 resolves cleanly when target element is unchanged', async () => {
    // FIXME: depends on agent-ready handshake (Bug 2).
  });

  test.fixme('round 2 flags Drifted when DOM mutated between rounds', async () => {
    // FIXME: depends on agent-ready handshake (Bug 2).
  });
});
