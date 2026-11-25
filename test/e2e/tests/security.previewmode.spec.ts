import { test, expect, type Page } from '@playwright/test';
import { clearAllLivePins, getIframe } from './livemode-helpers';

// The previewed file is untrusted, and the daemon serves it on the same
// host:port as /api. /preview-content is sandboxed (CSP `sandbox` without
// allow-same-origin), so its scripts get an opaque origin: they cannot write
// to or read from the API, and cannot reach into the review page. Preview
// counterpart of live mode's Scenario 17 (security.livemode.spec.ts).
// Fixture: setup-fixtures-preview.sh (an Excalidraw scene's canvas page).

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

test.describe('security — preview content is sandboxed', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
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
    await openPreview(page);
    const apiOrigin = new URL(page.url()).origin;
    // no-cors with a text/plain body skips the CORS preflight, so the request
    // reaches the daemon; its Sec-Fetch-Site check must reject it.
    await getIframe(page).locator('body').evaluate(async (_b, origin) => {
      const body = JSON.stringify({ start_line: 0, end_line: 0, body: 'forged', dom_anchor: { pathname: '/preview-content/' } });
      try {
        await fetch(`${origin}/api/file/comments?path=${encodeURIComponent('/preview-content/')}`, {
          method: 'POST', mode: 'no-cors', headers: { 'Content-Type': 'text/plain' }, body,
        });
        await fetch(`${origin}/api/project-prompts/trust`, {
          method: 'POST', mode: 'no-cors', headers: { 'Content-Type': 'text/plain' }, body: '{"mode":"always"}',
        });
      } catch (_) { /* a network error also counts as blocked */ }
    }, apiOrigin);

    const resp = await request.get(`/api/file/comments?path=${encodeURIComponent('/preview-content/')}`);
    expect(resp.ok()).toBeTruthy();
    expect(await resp.json()).toHaveLength(0);
  });
});
