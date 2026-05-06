import { test, expect } from '@playwright/test';
import { clearAllDesignPins, seedDesignPin } from './designmode-helpers';

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

  test('round 2 resolves cleanly when target element is unchanged', async ({ request }) => {
    // Seed a pin against #primary-btn, then bump the round. The pin must
    // survive carry-forward in /api/file/comments?path=/ with its anchor
    // intact and Drifted unchanged (false). This guards against the gap
    // where design pins were dropped from the comment array on round bump.
    const seeded = await seedDesignPin(request, 'stable pin', {
      pathname: '/',
      css_selector: '#primary-btn',
      tag_chain: ['BUTTON'],
    });

    // Flush the in-memory pin to disk before round-complete; the carry-forward
    // pipeline reloads PreviousComments from disk in handleRoundCompleteFiles.
    await request.post('/api/finish');

    const before = await request.get('/api/file/comments?path=%2F');
    expect(before.ok()).toBeTruthy();
    const beforeBody = await before.json() as Array<{ id: string; dom_anchor?: unknown }>;
    expect(beforeBody.some((c) => c.id === seeded.id)).toBeTruthy();

    const bump = await request.post('/api/round-complete');
    expect(bump.ok()).toBeTruthy();

    await expect.poll(async () => {
      const r = await request.get('/api/file/comments?path=%2F');
      if (!r.ok()) return null;
      const body = await r.json() as Array<{
        id: string;
        drifted?: boolean;
        drifted_on_round?: number;
        dom_anchor?: { css_selector?: string };
        carried_forward?: boolean;
      }>;
      return body.find((c) => c.dom_anchor?.css_selector === '#primary-btn') ?? null;
    }, { timeout: 10_000 }).not.toBeNull();

    const after = await request.get('/api/file/comments?path=%2F');
    const afterBody = await after.json() as Array<{
      id: string;
      drifted?: boolean;
      drifted_on_round?: number;
      dom_anchor?: { css_selector?: string };
      carried_forward?: boolean;
    }>;
    const pin = afterBody.find((c) => c.dom_anchor?.css_selector === '#primary-btn');
    expect(pin, 'pin must survive round-complete').toBeDefined();
    expect(pin!.drifted ?? false).toBe(false);
    expect(pin!.drifted_on_round ?? 0).toBe(0);
  });

  test('round 2 preserves drifted_on_round stamped in round 1', async ({ request }) => {
    // A pin already classified as Drifted in round 1 must keep its drift
    // metadata across round 2. The frontend's drift surfacing
    // ("Drifted on round N") depends on this round-trip — without it,
    // every round bump silently relabels drifted pins as fresh.
    const seeded = await seedDesignPin(request, 'drifted pin', {
      pathname: '/',
      css_selector: '#missing-element',
      tag_chain: ['BUTTON'],
    });

    // Stamp the pin as drifted in round 1.
    const stamp = await request.put(`/api/comment/${seeded.id}?path=%2F`, {
      data: { drifted: true, drifted_on_round: 1 },
    });
    expect(stamp.ok()).toBeTruthy();

    // Flush in-memory state to disk; carry-forward reads PreviousComments
    // from the review file, not from memory.
    await request.post('/api/finish');

    const bump = await request.post('/api/round-complete');
    expect(bump.ok()).toBeTruthy();

    await expect.poll(async () => {
      const r = await request.get('/api/file/comments?path=%2F');
      if (!r.ok()) return null;
      const body = await r.json() as Array<{
        id: string;
        drifted?: boolean;
        drifted_on_round?: number;
        carried_forward?: boolean;
      }>;
      // Carried-forward pins get a fresh ID, so look up by drift metadata
      // (the pin authored against #missing-element is the only one in the
      // session) instead of by seeded ID.
      const pin = body.find((c) => c.carried_forward);
      if (!pin) return null;
      return { drifted: pin.drifted ?? false, on: pin.drifted_on_round ?? 0 };
    }, { timeout: 10_000 }).toEqual({ drifted: true, on: 1 });
  });
});
