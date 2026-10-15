import { test, expect, type Page } from '@playwright/test';
import { clearAllComments } from './helpers';

// The badge logic itself (hidden tab badges, visible tab is a no-op, clear
// restores the title, idempotent set/clear) is unit-tested in
// web/__tests__/crit-tab-ready.test.js. This covers the wiring: a real
// round-complete SSE badges a hidden tab, and visibility clears it.

// The bullet prefix the app prepends to document.title when the badge is active.
const BADGE_PREFIX = '● ';

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

test.describe('Tab-Ready Indicator', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('round-complete badges a hidden tab, the badge clears when the tab becomes visible, and set/clear are idempotent', async ({ page, request }) => {
    // ?test exposes the tab-badge test hook; wait for it so listeners are wired.
    await page.goto('/?test');
    await expect(page.locator('.loading')).toBeHidden();
    await page.waitForFunction(() => !!(window as unknown as { __critTabBadge?: unknown }).__critTabBadge);
    expect((await page.title()).startsWith(BADGE_PREFIX)).toBe(false);

    // Simulate user tabbing away; the server emits file-changed via SSE.
    await setVisibility(page, false);
    await request.post('/api/round-complete');
    await expect.poll(() => page.title()).toMatch(new RegExp('^' + BADGE_PREFIX));

    await setVisibility(page, true);
    await expect.poll(() => page.title()).not.toMatch(new RegExp('^' + BADGE_PREFIX));

    // Repeated set/clear through the page hook is idempotent: the prefix is
    // never doubled and a final clear removes it.
    const title = await page.title();
    const prefixed = await page.evaluate(() => {
      const api = (window as unknown as { __critTabBadge: { set: () => void; clear: () => void } }).__critTabBadge;
      api.set();
      api.set();
      const afterSet = document.title;
      api.clear();
      api.clear();
      api.set();
      api.clear();
      return { afterSet, afterClear: document.title };
    });
    expect(prefixed.afterSet).toBe(BADGE_PREFIX + title);
    expect(prefixed.afterClear).toBe(title);
  });
});
