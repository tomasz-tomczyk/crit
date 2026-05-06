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

  test('round-complete endpoint accepts POST', async ({ request }) => {
    const resp = await request.post('/api/round-complete');
    expect(resp.ok()).toBeTruthy();
    const body = await resp.json();
    expect(body).toEqual({ status: 'ok' });
  });

  test.fixme('round 2 resolves cleanly when target element is unchanged', async () => {
    // FIXME: depends on agent-ready handshake (Bug 2).
  });

  test.fixme('round 2 flags Drifted when DOM mutated between rounds', async () => {
    // FIXME: depends on agent-ready handshake (Bug 2).
  });
});
