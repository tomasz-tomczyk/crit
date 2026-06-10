import { test, expect } from '@playwright/test';
import { clearAllLivePins } from './livemode-helpers';

// Suite-wide cleanup. Server persists comments across tests; some tests in
// this file (e.g. 'comment panel lists comments grouped by route') seed
// pins via /api/file/comments, and stale pins from prior tests would skew
// counts and selectors. Clear in beforeEach so each test starts on bare
// state.
test.beforeEach(async ({ request }) => {
  await clearAllLivePins(request);
});

// All Phase B tests target the future `live-mode` Playwright project
// (Phase F infra). Until then, `npx playwright test --list ...` confirms
// they parse and `--project live-mode` is recognised. Tests are
// `test.fixme`'d so they are marked deferred and not executed.

test.describe('live-mode shell — bootstrap', () => {
  test('loads live-mode.js when pathname is /live', async ({ page }) => {
    const loaded: string[] = [];
    page.on('response', r => loaded.push(r.url()));
    await page.goto('/live');
    await expect.poll(() => loaded.some(u => u.endsWith('/live-mode.js'))).toBe(true);
  });

  test('falls back to app.js when pathname is /', async ({ page }) => {
    const loaded: string[] = [];
    page.on('response', r => loaded.push(r.url()));
    await page.goto('/');
    await expect.poll(() => loaded.some(u => u.endsWith('/app.js'))).toBe(true);
    await expect.poll(() => loaded.some(u => u.endsWith('/live-mode.js'))).toBe(false);
  });

  test('loads style-live.css only in live mode', async ({ page }) => {
    const loaded: string[] = [];
    page.on('response', r => loaded.push(r.url()));
    await page.goto('/live');
    await expect.poll(() => loaded.some(u => u.endsWith('/style-live.css'))).toBe(true);
  });

  test('html has crit-mode-live marker class in live mode', async ({ page }) => {
    await page.goto('/live');
    await expect(page.locator('html')).toHaveClass(/crit-mode-live/);
  });

  test('header (existing) and live iframe pane both visible in DOM', async ({ page }) => {
    await page.goto('/live');
    await expect(page.locator('.header')).toBeVisible();
    await expect(page.locator('.crit-live-iframe-pane')).toBeVisible();
  });
});

test.describe('live-mode shell — boot + session', () => {
  test('boot fetches /api/session and renders iframe pane', async ({ page }) => {
    let sessionRequested = false;
    page.on('request', r => { if (r.url().endsWith('/api/session')) sessionRequested = true; });
    await page.goto('/live');
    await expect(page.locator('.crit-live-iframe-pane')).toBeVisible();
    expect(sessionRequested).toBe(true);
  });
});

test.describe('live-mode shell — viewport selector', () => {
  // R3/R4: viewport selector reuses .scope-toggle + .toggle-btn
  test('viewport selector renders 4 buttons inside .scope-toggle', async ({ page }) => {
    await page.goto('/live');
    const group = page.locator('#liveViewportToggle');
    await expect(group).toHaveClass(/scope-toggle/);
    await expect(group.locator('.toggle-btn')).toHaveCount(4);
    await expect(group.locator('.toggle-btn.active')).toHaveCount(1);
  });

  test('clicking Mobile changes iframe frame width to 390px', async ({ page }) => {
    await page.goto('/live');
    await page.locator('button[data-viewport="mobile"]').click();
    const frame = page.locator('.crit-live-iframe-frame');
    await expect.poll(async () => (await frame.boundingBox())?.width).toBe(390);
  });

  test('Fit fills the available iframe pane width', async ({ page }) => {
    await page.goto('/live');
    await page.locator('button[data-viewport="fit"]').click();
    const frame = await page.locator('.crit-live-iframe-frame').boundingBox();
    const pane = await page.locator('.crit-live-iframe-pane').boundingBox();
    expect(Math.abs((pane!.width - 32) - frame!.width)).toBeLessThan(33);
  });
});

test.describe('live-mode shell — Pin/Navigate toggle', () => {
  // R4: uses .diff-mode-toggle + .toggle-btn; pin uses native disabled attr
  // Phase B asserted Pin disabled-by-default. Post Phase C the agent-ready
  // handshake enables it. Keep the structural shape but drop the disabled
  // assertion — it doesn't reflect current chrome behavior.
  test('mode toggle is present and pin button is enabled after agent-ready', async ({ page }) => {
    await page.goto('/live');
    const group = page.locator('#liveModeToggle');
    await expect(group).toHaveClass(/diff-mode-toggle/);
    await expect(group.locator('.toggle-btn')).toHaveCount(2);
    const pinBtn = group.locator('button[data-mode="pin"]');
    await expect(pinBtn).toBeEnabled();
    await expect(group.locator('button[data-mode="navigate"]')).toHaveClass(/active/);
  });
});

test.describe('live-mode shell — iframe + route detection', () => {
  test('iframe src points at proxy_port from /api/session', async ({ page }) => {
    await page.goto('/live');
    const src = await page.locator('#critLiveIframe').getAttribute('src');
    expect(src).toMatch(/^http:\/\/(localhost|127\.0\.0\.1):\d+\/$/);
  });

  // Removed two fixme'd specs that drove postMessage from the chrome's own
  // window. The agent + chrome message dispatcher validates ev.source ===
  // expectedSource (iframe.contentWindow) and drops messages from any other
  // source by design (see agent.livemode.spec.ts 'agent rejects inbound
  // messages from a foreign origin'). The real behaviors — breadcrumb on
  // genuine iframe route change, "(unsaved)" badge — are exercised by
  // navigation.livemode.spec.ts (link/pushState specs) and tests that
  // navigate the iframe via setIframeRoute().
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
  test('round counter renders from session.review_round (empty on round 1, "Round #N" on N>1)', async ({ page }) => {
    // Per live-mode.js#updateRoundCounter (commit 73877e9): the counter is
    // populated from session.review_round and intentionally rendered as
    // empty text on round 1, "Round #N" once N > 1. The fixture starts at
    // round 1, so the element exists but holds no copy — assert the contract.
    await page.goto('/live');
    const counter = page.locator('#liveRoundCounter');
    await expect(counter).toBeAttached();
    await expect(counter).toHaveText('');
  });

  test('empty comment list shows placeholder copy', async ({ page }) => {
    await page.goto('/live');
    const empty = page.locator('#commentsPanelBody');
    await expect(empty).toContainText(/No pins yet/);
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
  test('live mode honours crit-settings theme=dark cookie', async ({ page, context }) => {
    await context.addCookies([{ name: 'crit-settings', value: encodeURIComponent('{"theme":"dark"}'), url: 'http://localhost:3129' }]);
    await page.goto('/live');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  });

  test('live mode honours crit-settings theme=light cookie', async ({ page, context }) => {
    await context.addCookies([{ name: 'crit-settings', value: encodeURIComponent('{"theme":"light"}'), url: 'http://localhost:3129' }]);
    await page.goto('/live');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  });
});

test.describe('live-mode shell — deep-link / a11y / errors', () => {
  test('#pin=<id> is accepted without error', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', e => errors.push(e.message));
    await page.goto('/live#pin=abc123');
    await expect(page.locator('.crit-live-iframe-pane')).toBeVisible();
    expect(errors).toEqual([]);
  });

  test('aria-live announcement region exists', async ({ page }) => {
    await page.goto('/live');
    await expect(page.locator('#critLiveAnnouncer')).toHaveAttribute('aria-live', 'polite');
  });

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

  test('window.crit.live exposes the Phase B state contract', async ({ page }) => {
    await page.goto('/live');
    const shape = await page.evaluate(() => {
      const d = (window as any).crit.live;
      return {
        hasSession: 'session' in d,
        hasRoutes: Array.isArray(d.routes),
        hasCurrentRoute: typeof d.currentRoute === 'string',
        hasViewport: typeof d.viewport === 'object',
        hasMode: d.mode === 'navigate',
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
  });
});
