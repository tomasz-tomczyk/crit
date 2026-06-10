import { test, expect, type Page } from '@playwright/test';
import { clearAllComments } from './helpers';

// The bullet prefix the app prepends to document.title when the badge is active.
const BADGE_PREFIX = '\u25CF ';

// Override document.visibilityState / document.hidden in the page context so
// the app sees the tab as hidden or visible on demand, then dispatch
// visibilitychange so listeners (the badge-clearing one) fire.
async function setVisibility(page: Page, visible: boolean) {
  await page.evaluate((isVisible) => {
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => (isVisible ? 'visible' : 'hidden'),
    });
    Object.defineProperty(document, 'hidden', {
      configurable: true,
      get: () => !isVisible,
    });
    document.dispatchEvent(new Event('visibilitychange'));
  }, visible);
}

// Load the page with ?test query param so the tab-badge test hook is exposed.
async function loadPageWithTestParam(page: Page) {
  await page.goto('/?test');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
  await page.waitForFunction(() => !!(window as unknown as { __critTabBadge?: unknown }).__critTabBadge);
}

test.describe('Tab-Ready Indicator', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('title prefixed on file-changed when tab hidden', async ({ page, request }) => {
    await loadPageWithTestParam(page);
    const originalTitle = await page.title();
    expect(originalTitle.startsWith(BADGE_PREFIX)).toBe(false);

    // Simulate user tabbing away.
    await setVisibility(page, false);

    // Trigger a round-complete — the server emits file-changed via SSE.
    await request.post('/api/round-complete');

    await expect.poll(() => page.title(), { timeout: 5000 }).toMatch(new RegExp('^' + BADGE_PREFIX));
  });

  test('no prefix when tab visible during round-complete', async ({ page, request }) => {
    await loadPageWithTestParam(page);

    // Force visible state so test behaves identically in headed and headless runs.
    await setVisibility(page, true);

    await request.post('/api/round-complete');

    // Wait for the round-complete side effect (overlay deactivates) then assert.
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/, { timeout: 5000 });

    expect((await page.title()).startsWith(BADGE_PREFIX)).toBe(false);
  });

  test('prefix clears when visibility returns to visible', async ({ page, request }) => {
    await loadPageWithTestParam(page);

    await setVisibility(page, false);
    await request.post('/api/round-complete');
    await expect.poll(() => page.title(), { timeout: 5000 }).toMatch(new RegExp('^' + BADGE_PREFIX));

    await setVisibility(page, true);

    await expect.poll(() => page.title(), { timeout: 2000 }).not.toMatch(new RegExp('^' + BADGE_PREFIX));
  });

  test('rapid set/clear cycles end in the correct state', async ({ page }) => {
    await loadPageWithTestParam(page);

    // Direct helper calls — exercises idempotency without SSE races.
    await page.evaluate(() => {
      const api = (window as unknown as { __critTabBadge: { set: () => void; clear: () => void } }).__critTabBadge;
      api.set();
      api.set(); // idempotent
      api.clear();
      api.clear(); // idempotent
      api.set();
      api.clear();
    });
    expect((await page.title()).startsWith(BADGE_PREFIX)).toBe(false);

    await page.evaluate(() => {
      (window as unknown as { __critTabBadge: { set: () => void } }).__critTabBadge.set();
    });
    await expect.poll(() => page.title(), { timeout: 2000 }).toMatch(new RegExp('^' + BADGE_PREFIX));

    await page.evaluate(() => {
      (window as unknown as { __critTabBadge: { clear: () => void } }).__critTabBadge.clear();
    });
    await expect.poll(() => page.title(), { timeout: 2000 }).not.toMatch(new RegExp('^' + BADGE_PREFIX));
  });
});
