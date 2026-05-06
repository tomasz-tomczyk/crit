import { test } from '@playwright/test';
import { clearAllDesignPins } from './designmode-helpers';

// Marker rendering, click handoff, and MutationObserver reposition all
// require pinning a comment, which requires Pin mode + agent-ready.
// Pinned to fixme until the concurrent bug-fix round lands the handshake.

test.describe('marker — rendering, click handoff, MO reposition (Scenarios 6–8)', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test.fixme('saved pin renders numbered marker at element bounding rect', async () => {
    // FIXME: depends on agent-ready handshake (Bug 2).
    // Author flow: pinViaComposer #primary-btn → expect 1 .crit-design-marker
    // inside iframe with bbox aligned ±8px to #primary-btn bbox.
  });

  test.fixme('clicking marker scrolls side panel to thread', async () => {
    // FIXME: depends on agent-ready handshake (Bug 2).
    // Author flow: pin two elements → click 2nd marker → expect
    // [data-comment-id] for that pin to be in viewport in panel.
  });

  test.fixme('MutationObserver repositions markers on /mutator within rAF', async () => {
    // FIXME: depends on agent-ready handshake (Bug 2).
    // Author flow: setIframeRoute('/mutator') → pin li[data-stable="0"] →
    // marker rect tracks target rect ±8px through 200ms churn.
  });
});
