import { test, expect } from '@playwright/test';
import { currentRound } from './helpers';
import { clearAllLivePins, getIframe, setViewportPreset } from './livemode-helpers';

// Suite-wide cleanup. Server persists comments across tests; some tests in
// this file (e.g. 'comment panel lists comments grouped by route') seed
// pins via /api/file/comments, and stale pins from prior tests would skew
// counts and selectors. Clear in beforeEach so each test starts on bare
// state.
test.beforeEach(async ({ request }) => {
  await clearAllLivePins(request);
});

// The `deferred error surfaces` block at the end holds chrome features that
// are not built yet; those tests are `test.fixme`'d (skipped, never run).

test.describe('live-mode shell — bootstrap', () => {
  // One load covers the whole /live boot contract: the live script fork,
  // its stylesheet, the session fetch, the chrome + iframe pane, the
  // proxied upstream root, the a11y announcer, the window.crit.live state
  // shape, and a #pin=<id> deep link that must not throw.
  test('boots live mode: scripts, session, chrome, iframe and state contract', async ({ page }) => {
    const loaded: string[] = [];
    const errors: string[] = [];
    let sessionRequested = false;
    page.on('response', r => loaded.push(r.url()));
    page.on('request', r => { if (r.url().endsWith('/api/session')) sessionRequested = true; });
    page.on('pageerror', e => errors.push(e.message));
    await page.goto('/live#pin=abc123');

    await expect.poll(() => loaded.some(u => u.endsWith('/live-mode.js'))).toBe(true);
    await expect.poll(() => loaded.some(u => u.endsWith('/style-live.css'))).toBe(true);
    await expect(page.locator('html')).toHaveClass(/crit-mode-live/);
    await expect(page.locator('.header')).toBeVisible();
    await expect(page.locator('.crit-live-iframe-pane')).toBeVisible();
    expect(sessionRequested).toBe(true);
    await expect(page.locator('#critLiveAnnouncer')).toHaveAttribute('aria-live', 'polite');

    // Iframe points at the proxy port from /api/session and renders the
    // upstream root; the chrome reflects its pathname.
    const src = await page.locator('#critLiveIframe').getAttribute('src');
    expect(src).toMatch(/^http:\/\/(localhost|127\.0\.0\.1):\d+\/$/);
    const frame = getIframe(page);
    await expect(frame.locator('#title')).toHaveText('Upstream');
    await expect(frame.locator('.card')).toHaveCount(3);
    await expect(page.locator('#liveRouteName')).toHaveText('/');

    const shape = await page.evaluate(() => {
      const d = (window as unknown as { crit: { live: Record<string, unknown> } }).crit.live;
      return {
        hasSession: 'session' in d,
        hasRoutes: Array.isArray(d.routes),
        hasCurrentRoute: typeof d.currentRoute === 'string',
        hasViewport: typeof d.viewport === 'object',
        hasMode: d.mode === 'pin',
        hasComments: Array.isArray(d.comments),
        // State.pinModeEnabled defaults to false; the chrome flips the Pin
        // button's `disabled` attr via agent-ready but never mutates this
        // boolean. Asserting the field exists with its default is enough
        // for a state-contract check.
        hasPinModeEnabled: typeof d.pinModeEnabled === 'boolean',
      };
    });
    expect(shape).toEqual({
      hasSession: true, hasRoutes: true, hasCurrentRoute: true, hasViewport: true,
      hasMode: true, hasComments: true, hasPinModeEnabled: true,
    });
    expect(errors).toEqual([]);
  });

  test('falls back to app.js when pathname is /', async ({ page }) => {
    const loaded: string[] = [];
    page.on('response', r => loaded.push(r.url()));
    await page.goto('/');
    await expect.poll(() => loaded.some(u => u.endsWith('/app.js'))).toBe(true);
    await expect.poll(() => loaded.some(u => u.endsWith('/live-mode.js'))).toBe(false);
  });
});

test.describe('live-mode shell — viewport selector', () => {
  // R3/R4: viewport selector reuses .scope-toggle + .toggle-btn
  test('viewport selector: 4 presets, Mobile is 390px, Fit fills the pane', async ({ page }) => {
    await page.goto('/live');
    const group = page.locator('#liveViewportToggle');
    await expect(group).toHaveClass(/scope-toggle/);
    await expect(group.locator('.toggle-btn')).toHaveCount(4);
    await expect(group.locator('.toggle-btn.active')).toHaveCount(1);

    const frame = page.locator('.crit-live-iframe-frame');
    await setViewportPreset(page, 'mobile');
    await expect.poll(async () => Math.round((await frame.boundingBox())?.width ?? 0)).toBe(390);

    await setViewportPreset(page, 'fit');
    await expect.poll(async () => {
      const f = await frame.boundingBox();
      const pane = await page.locator('.crit-live-iframe-pane').boundingBox();
      return Math.abs((pane!.width - 32) - f!.width);
    }).toBeLessThan(33);
  });
});

test.describe('live-mode shell — Pin/Navigate toggle', () => {
  // R4: uses .diff-mode-toggle + .toggle-btn; pin uses native disabled attr.
  // The agent-ready handshake enables Pin, and live mode launches in
  // Comment (pin) mode by default.
  test('mode toggle enables Comment after agent-ready and describes both modes', async ({ page }) => {
    await page.goto('/live');
    const group = page.locator('#liveModeToggle');
    await expect(group).toHaveClass(/diff-mode-toggle/);
    await expect(group.locator('.toggle-btn')).toHaveCount(2);
    const pinBtn = group.locator('button[data-mode="pin"]');
    await expect(pinBtn).toBeEnabled();
    await expect(pinBtn).toHaveClass(/active/);
    await expect(group.locator('button[data-mode="navigate"]')).toHaveText('Browse');
    await expect(pinBtn).toContainText('P');
    await expect(pinBtn).toContainText('Comment');
    await expect(page.locator('#liveModeHint')).toBeVisible();
    await expect(page.locator('#liveModeHint')).toContainText('Commenting');
    await expect(page.locator('#liveModeHint')).toContainText('leave feedback');

    await group.locator('button[data-mode="navigate"]').click();
    await expect(page.locator('#liveModeHint')).toBeVisible();
    await expect(page.locator('#liveModeHint')).toContainText('Browsing');
    await expect(page.locator('#liveModeHint')).toHaveAttribute('data-mode', 'navigate');
  });
});

test.describe('live-mode shell — drag resize', () => {
  test('dragging the resizer changes iframe frame width and clears active button', async ({ page }) => {
    await page.goto('/live');
    await page.locator('button[data-viewport="desktop"]').click();
    const before = (await page.locator('.crit-live-iframe-frame').boundingBox())!;
    const handle = page.locator('#critLiveResizer');
    const handleBox = (await handle.boundingBox())!;
    const sx = handleBox.x + handleBox.width / 2;
    const sy = handleBox.y + handleBox.height / 2;
    // Drive PointerEvents directly — page.mouse.* doesn't synthesise the
    // pointer events the production handler subscribes to.
    await handle.dispatchEvent('pointerdown', { pointerId: 1, clientX: sx, clientY: sy, button: 0, isPrimary: true });
    await handle.dispatchEvent('pointermove', { pointerId: 1, clientX: sx + 200, clientY: sy, button: 0, isPrimary: true });
    await handle.dispatchEvent('pointerup', { pointerId: 1, clientX: sx + 200, clientY: sy, button: 0, isPrimary: true });
    await expect.poll(async () =>
      (await page.locator('.crit-live-iframe-frame').boundingBox())!.width,
    ).toBeGreaterThan(before.width + 100);
    await expect(page.locator('#liveViewportToggle .toggle-btn.active')).toHaveCount(0);
  });
});

test.describe('live-mode shell — round counter + comments', () => {
  test('round counter and empty placeholder render from the session', async ({ page, request }) => {
    // Per live-mode.js#updateRoundCounter the counter is empty on round 1 and
    // "Round #N" once N > 1. Other specs bump the shared daemon's round, so
    // derive the expectation from the session rather than assuming round 1.
    const round = await currentRound(request);
    await page.goto('/live');
    const counter = page.locator('#liveRoundCounter');
    await expect(counter).toBeAttached();
    await expect(counter).toHaveText(round > 1 ? `Round #${round}` : '');
    await expect(page.locator('#commentsPanelBody')).toContainText(/No pins yet/);
  });

  test('comment panel lists comments grouped by route in .comments-panel-body', async ({ page, request }) => {
    await request.post('/api/file/comments?path=/dashboard', {
      data: {
        start_line: 0, end_line: 0, body: 'Header looks tight on mobile.',
        dom_anchor: { pathname: '/dashboard', css_selector: 'h1', tag_chain: ['H1'], outer_html: '<h1/>', viewport_width: 390, viewport_height: 844 }
      }
    });
    await page.goto('/live');
    const card = page.locator('.comments-panel-body .comments-panel-file-group .comment-card').first();
    await expect(card).toBeVisible();
    await expect(card).toContainText('Header looks tight');
  });

  test('clicking a comment-card sets iframe src to that route', async ({ page, request }) => {
    await request.post('/api/file/comments?path=/billing', {
      data: { start_line: 0, end_line: 0, body: 'check copy',
        dom_anchor: { pathname: '/billing', css_selector: 'p', tag_chain: ['P'], outer_html: '<p/>', viewport_width: 1280, viewport_height: 800 } }
    });
    await page.goto('/live');
    await page.locator('.comment-card[data-live-route="/billing"]').first().click();
    await expect.poll(async () => {
      const src = await page.locator('#critLiveIframe').getAttribute('src');
      return new URL(src!).pathname;
    }).toBe('/billing');
    await expect(page.locator('#liveRouteName')).toHaveText('/billing');
  });
});

test.describe('live-mode shell — theme', () => {
  test('live mode honours crit-settings theme=light cookie', async ({ page, context, baseURL }) => {
    await context.addCookies([{ name: 'crit-settings', value: encodeURIComponent('{"theme":"light"}'), url: baseURL! }]);
    await page.goto('/live');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  });
});

test.describe('live-mode shell — deferred error surfaces', () => {
  test.fixme('iframe surfaces load failure as a chrome banner with retry', async ({ page }) => {
    await page.goto('/live');
    await page.evaluate(() => {
      const f = document.getElementById('critLiveIframe') as HTMLIFrameElement;
      f.dispatchEvent(new Event('error'));
    });
    await expect(page.locator('.crit-live-iframe-error')).toBeVisible();
  });

  test.fixme('chrome surfaces a cross-origin-redirect message from iframe', async ({ page }) => {
    await page.goto('/live');
    await page.evaluate(() => {
      const iframe = document.getElementById('critLiveIframe') as HTMLIFrameElement;
      window.dispatchEvent(new MessageEvent('message', {
        source: iframe.contentWindow,
        data: { type: 'cross-origin-redirect', url: 'https://accounts.google.com/foo' }
      }));
    });
    await expect(page.locator('.crit-live-redirect-notice')).toBeVisible();
  });

  test.fixme('Esc dismisses the cross-origin redirect notice', async ({ page }) => {
    await page.goto('/live');
    await page.evaluate(() => {
      const iframe = document.getElementById('critLiveIframe') as HTMLIFrameElement;
      window.dispatchEvent(new MessageEvent('message', {
        source: iframe.contentWindow,
        data: { type: 'cross-origin-redirect', url: 'https://accounts.google.com/foo' }
      }));
    });
    await expect(page.locator('.crit-live-redirect-notice')).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.locator('.crit-live-redirect-notice')).toHaveCount(0);
  });

  test.fixme('iframe pane scrolls horizontally when iframe wider than pane', async ({ page }) => {
    await page.setViewportSize({ width: 800, height: 700 });
    await page.goto('/live');
    await page.locator('button[data-viewport="desktop"]').click();
    const pane = page.locator('.crit-live-iframe-pane');
    const scrollW = await pane.evaluate((el: HTMLElement) => el.scrollWidth);
    const clientW = await pane.evaluate((el: HTMLElement) => el.clientWidth);
    expect(scrollW).toBeGreaterThan(clientW);
  });

  test.fixme('resizing window in Fit mode resizes iframe frame to match', async ({ page }) => {
    await page.setViewportSize({ width: 1200, height: 800 });
    await page.goto('/live');
    await page.locator('button[data-viewport="fit"]').click();
    const beforeW = (await page.locator('.crit-live-iframe-frame').boundingBox())!.width;
    await page.setViewportSize({ width: 700, height: 800 });
    await expect.poll(async () =>
      (await page.locator('.crit-live-iframe-frame').boundingBox())!.width
    ).toBeLessThan(beforeW);
  });
});
