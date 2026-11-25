import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { clearAllLivePins, getIframe } from './livemode-helpers';

// The previewed file is untrusted, and the daemon serves it on the same
// host:port as /api. /preview-content is sandboxed (CSP `sandbox` without
// allow-same-origin, plus the iframe's sandbox attribute), so its scripts get
// an opaque origin: they cannot write to or read from the API, and cannot
// reach into the review page. Preview counterpart of live mode's Scenario 17
// (security.livemode.spec.ts). Fixture: setup-fixtures-preview.sh (an
// Excalidraw scene's canvas page).

const PIN_PATH = '/preview-content/';
const PIN_BODY = {
  start_line: 0, end_line: 0, body: 'forged',
  dom_anchor: { pathname: PIN_PATH, css_selector: 'body', tag_chain: ['BODY'], outer_html: '<body/>' },
};

async function openPreview(page: Page): Promise<void> {
  await page.goto('/preview');
  await expect(page.locator('#critLiveIframe')).toBeVisible({ timeout: 15_000 });
  await expect.poll(
    () => page.evaluate(() =>
      ((window as unknown as { __critLiveMessages?: { type: string }[] }).__critLiveMessages || [])
        .some((m) => m.type === 'agent-ready')),
    { timeout: 15_000 },
  ).toBe(true);
}

async function pinCount(request: APIRequestContext): Promise<number> {
  const resp = await request.get(`/api/file/comments?path=${encodeURIComponent(PIN_PATH)}`);
  expect(resp.ok()).toBeTruthy();
  return ((await resp.json()) as unknown[]).length;
}

test.describe('security — preview content is sandboxed', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('preview iframe is sandboxed without allow-same-origin', async ({ page }) => {
    await openPreview(page);
    const frame = page.locator('#critLiveIframe');
    await expect(frame).toHaveAttribute('sandbox', /allow-scripts/);
    await expect(frame).not.toHaveAttribute('sandbox', /allow-same-origin/);
  });

  test('previewed page runs on an opaque origin and cannot reach the review page', async ({ page }) => {
    await openPreview(page);
    const result = await getIframe(page).locator('body').evaluate(() => {
      let parentDOM = 'blocked';
      try {
        parentDOM = String(window.parent.document.title);
      } catch (_) { /* cross-origin, as intended */ }
      return { origin: self.origin, parentDOM };
    });
    expect(result.origin).toBe('null');
    expect(result.parentDOM).toBe('blocked');
  });

  test('previewed page opened directly in a tab is still on an opaque origin', async ({ page }) => {
    await page.goto('/preview-content/');
    expect(await page.evaluate(() => self.origin)).toBe('null');
  });

  test('previewed page cannot read API data', async ({ page }) => {
    await openPreview(page);
    const apiOrigin = new URL(page.url()).origin;
    const result = await getIframe(page).locator('body').evaluate(async (_b, origin) => {
      try {
        const r = await fetch(`${origin}/api/session`);
        return { read: true, status: r.status };
      } catch (e) {
        return { read: false, error: String(e) };
      }
    }, apiOrigin);
    expect(result.read).toBe(false);
  });

  test('previewed page cannot write to the API, even without a preflight', async ({ page, request }) => {
    // Positive control: the same body, sent by a trusted client, is accepted.
    // Without this the test would also pass if the body were simply invalid.
    const ok = await request.post(`/api/file/comments?path=${encodeURIComponent(PIN_PATH)}`, { data: PIN_BODY });
    expect(ok.ok()).toBeTruthy();
    expect(await pinCount(request)).toBe(1);
    await clearAllLivePins(request);

    await openPreview(page);
    const apiOrigin = new URL(page.url()).origin;
    // no-cors skips the CORS preflight, so the request reaches the daemon;
    // its Sec-Fetch-Site check must reject it.
    await getIframe(page).locator('body').evaluate(async (_b, args) => {
      try {
        await fetch(`${args.origin}/api/file/comments?path=${encodeURIComponent(args.path)}`, {
          method: 'POST', mode: 'no-cors', headers: { 'Content-Type': 'text/plain' }, body: JSON.stringify(args.body),
        });
      } catch (_) { /* a network error also counts as blocked */ }
    }, { origin: apiOrigin, path: PIN_PATH, body: PIN_BODY });

    expect(await pinCount(request)).toBe(0);
  });
});
