import { test, expect } from '@playwright/test';
import { clearAllLivePins, forgeUnauthedWrite, getIframe } from './livemode-helpers';

test.describe('security — two-port origin separation (Scenario 17)', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('upstream-origin fetch to API port is blocked by browser CORS', async ({ page, request }) => {
    await page.goto('/live');
    // Wait for iframe to be navigable so we can run script in its origin.
    await expect(getIframe(page).locator('#title')).toBeVisible();

    // The API origin is the chrome's origin; iframe is on the proxy origin.
    const apiOrigin = new URL(page.url()).origin;

    const result = await forgeUnauthedWrite(
      page,
      { start_line: 0, end_line: 0, body: 'forged', dom_anchor: { pathname: '/' } },
      apiOrigin,
    );
    // CORS preflight failure throws TypeError; status is undefined.
    expect(result.ok).toBe(false);
    expect(result.error ?? '').toMatch(/Failed to fetch|TypeError|NetworkError/);

    // Secondary: forged write did not land. Read all comments via API.
    const sessionResp = await request.get('/api/session');
    const session = await sessionResp.json();
    const totalComments =
      (session.review_comments?.length ?? 0) +
      (session.files ?? []).reduce(
        (acc: number, f: { comments?: unknown[] }) => acc + (f.comments?.length ?? 0),
        0,
      );
    expect(totalComments).toBe(0);
  });
});
