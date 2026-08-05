import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { addComment, clearAllComments, loadPage } from './helpers';

// ============================================================
// Share Transport — browser round trip against a stub crit-web
//
// Smoke coverage for the Share modal actions that talk to crit-web: Pull
// comments, Re-share, and Unpublish. With proxy_auth off these all go through
// the local Go server (/api/share/pull, /api/share/reshare,
// DELETE /api/share-url) so the browser never issues a cross-origin request.
// A regression to direct fetches would break every self-hosted instance with
// authentication enabled, since browsers omit Authorization on CORS preflight.
//
// The fixture (setup-fixtures-sharetransport.sh) points crit at a stub crit-web
// on a different origin and records the Origin header of every request it
// receives — empty for server-to-server calls, populated for browser fetches.
// ============================================================

const STUB_ORIGIN = `http://127.0.0.1:${process.env.CRIT_TEST_STUB_PORT || '3133'}`;

type StubRequest = { method: string; path: string; origin: string; has_auth: boolean };

async function resetStub(request: APIRequestContext) {
  const res = await request.post(`${STUB_ORIGIN}/__reset`);
  expect(res.ok()).toBeTruthy();
}

async function configureStub(request: APIRequestContext, config: { fail_delete: boolean }) {
  const res = await request.post(`${STUB_ORIGIN}/__config`, { data: config });
  expect(res.ok()).toBeTruthy();
}

async function stubRequests(request: APIRequestContext): Promise<StubRequest[]> {
  const res = await request.get(`${STUB_ORIGIN}/__log`);
  expect(res.ok()).toBeTruthy();
  return res.json();
}

/** Stand in for a viewer commenting on the hosted review page. */
async function seedRemoteComment(request: APIRequestContext, token: string, body: string, line: number) {
  const res = await request.post(`${STUB_ORIGIN}/__seed-comment?token=${token}`, {
    data: {
      body,
      file_path: 'main.go',
      start_line: line,
      end_line: line,
      review_round: 1,
      external_id: `web-${line}`,
      author_display_name: 'Web Reviewer',
      replies: [],
    },
  });
  expect(res.ok()).toBeTruthy();
}

async function hostedToken(request: APIRequestContext): Promise<string> {
  const config = await request.get('/api/config').then(r => r.json());
  expect(config.hosted_token).toBeTruthy();
  return config.hosted_token as string;
}

async function fileCommentBodies(request: APIRequestContext): Promise<string[]> {
  const comments = await request.get('/api/file/comments?path=main.go').then(r => r.json());
  return comments.map((c: { body: string }) => c.body);
}

/**
 * Newest share toast. showToast replaces a toast by appending the new node and
 * letting the superseded one play its exit animation, so two #toast-share
 * elements briefly coexist and a bare id locator is not strict-mode safe.
 */
function shareToast(page: Page) {
  return page.locator('#toast-share').last();
}

test.describe('Share Transport', () => {
  test.beforeEach(async ({ request }) => {
    await resetStub(request);
    // Drop any share recorded by a previous test without re-contacting the
    // stub (which has just been reset and no longer knows the review).
    await request.delete('/api/share-url?local_only=1');
    await clearAllComments(request);
  });

  test('share, pull, re-share and unpublish all stay on the local origin', async ({ page, request }) => {
    const browserCallsToCritWeb: string[] = [];
    page.on('request', req => {
      if (req.url().startsWith(STUB_ORIGIN)) {
        browserCallsToCritWeb.push(`${req.method()} ${req.url()}`);
      }
    });

    await addComment(request, 'main.go', 3, 'Local comment before sharing');
    await loadPage(page);

    await page.locator('#shareBtn').click();
    await expect(page.locator('.share-dialog')).toBeVisible();
    await expect(page.locator('.share-dialog-url')).toContainText(`${STUB_ORIGIN}/r/`);

    // Pull: a viewer's comment on the hosted page lands in the local review.
    await seedRemoteComment(request, await hostedToken(request), 'Remote reviewer comment', 7);
    await page.locator('#modalPullBtn').click();
    await expect(shareToast(page)).toContainText('Comments pulled');
    expect(await fileCommentBodies(request)).toContain('Remote reviewer comment');

    // Re-share: the local edit changes the content hash, so a PUT is required
    // (an unchanged review is a no-op upsert and would send nothing).
    await addComment(request, 'main.go', 5, 'Local comment before re-sharing');
    await page.locator('#modalReshareBtn').click();
    await expect(shareToast(page)).toContainText('Re-shared');
    expect(await stubRequests(request)).toContainEqual(
      expect.objectContaining({ method: 'PUT', path: expect.stringMatching(/^\/api\/reviews\/\w+$/) }),
    );

    // Unpublish: the review goes away remotely and the button resets locally.
    await page.locator('#modalUnpublishBtn').click();
    await page.locator('#confirmUnpublishBtn').click();
    await expect(page.locator('.share-dialog')).toBeHidden();
    await expect(page.locator('#shareBtn')).toHaveText('Share');
    expect(await stubRequests(request)).toContainEqual(
      expect.objectContaining({ method: 'DELETE', path: '/api/reviews' }),
    );
    const config = await request.get('/api/config').then(r => r.json());
    expect(config.hosted_url).toBe('');

    // The regression guard: crit-web was reached only by the local Go server.
    expect(browserCallsToCritWeb).toEqual([]);
    const withOrigin = (await stubRequests(request)).filter(r => r.origin !== '');
    expect(withOrigin).toEqual([]);
  });

  test('a rejected unpublish keeps the review shared and offers a retry', async ({ page, request }) => {
    await addComment(request, 'main.go', 3, 'Local comment before sharing');
    await loadPage(page);

    await page.locator('#shareBtn').click();
    await expect(page.locator('.share-dialog')).toBeVisible();

    await configureStub(request, { fail_delete: true });
    await page.locator('#modalUnpublishBtn').click();
    await page.locator('#confirmUnpublishBtn').click();

    await expect(shareToast(page)).toContainText('Unpublish failed');
    await expect(page.locator('#shareUnpublishRetryBtn')).toBeVisible();

    // Local state must survive a failed remote delete, otherwise the user is
    // left with a live review they can no longer unpublish from the UI.
    const config = await request.get('/api/config').then(r => r.json());
    expect(config.hosted_url).toContain(`${STUB_ORIGIN}/r/`);
  });

  test('pulling with nothing shared returns a readable error rather than a CORS failure', async ({ request }) => {
    const res = await request.post('/api/share/pull');
    expect(res.status()).toBe(400);
    expect((await res.json()).error).toContain('no shared review');
  });
});
