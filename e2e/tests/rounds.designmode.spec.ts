import { test } from '@playwright/test';

// Phase F brings the design-mode runner online; until then this spec is a
// behavioural anchor for the round + integration coverage Phase E ships.
test.skip(true, 'phase F design-mode runner');

test.describe('design mode — rounds + integration polish', () => {
  test('round-start re-resolves current path eagerly, others lazily on visit', async () => {});
  test('drifted tray splits "this round" vs "earlier"', async () => {});
  test('#pin=<id> on /design opens chrome focused on that pin', async () => {});
  test('opening a pin updates the URL fragment via replaceState', async () => {});
  test("navigating away from pin's pathname clears the fragment", async () => {});
  test('Esc cancels an armed re-anchor', async () => {});
  test('ancestor menu — Up/Down/Enter/Esc keyboard flow', async () => {});
  test('round counter tooltip shows carried/resolved/drifted', async () => {});
  test('smoke test prints framework notes (Phoenix / Vite / Next)', async () => {});
});
