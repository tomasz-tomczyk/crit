import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import fs from 'fs';
import path from 'path';
import { addComment, clearAllComments, loadPage } from './helpers';

// ============================================================
// Share Transport — browser round trip against stub crit-web backends
//
// Smoke coverage for the Share modal actions that talk to crit-web: Pull
// comments, Re-share, and Unpublish. With proxy_auth off these all go through
// the local Go server (/api/share/pull, /api/share/reshare,
// DELETE /api/share-url) so the browser never issues a cross-origin request.
// A regression to direct fetches would break every self-hosted instance with
// authentication enabled, since browsers omit Authorization on CORS preflight.
//
// The fixture (setup-fixtures-sharetransport.sh) configures two live stubs plus
// https://crit.md (consent-only) via share_targets, and records the Origin
// header of every request each stub receives — empty for server-to-server
// calls, populated for browser fetches.
// ============================================================

const STUB_ORIGIN = `http://127.0.0.1:${process.env.CRIT_TEST_STUB_PORT || '3133'}`;
const STUB2_ORIGIN = `http://127.0.0.1:${process.env.CRIT_TEST_STUB2_PORT || '3135'}`;

type StubRequest = { method: string; path: string; origin: string; has_auth: boolean };

function fixtureEnv(): { STUB_ORIGIN: string; STUB2_ORIGIN: string; CRIT_HOME: string } {
  const envPath = path.join(__dirname, '..', '.state', 'sharetransport.env');
  const raw = fs.readFileSync(envPath, 'utf8');
  const out: Record<string, string> = {};
  for (const line of raw.split('\n')) {
    const i = line.indexOf('=');
    if (i > 0) out[line.slice(0, i)] = line.slice(i + 1);
  }
  return out as { STUB_ORIGIN: string; STUB2_ORIGIN: string; CRIT_HOME: string };
}

async function resetStub(request: APIRequestContext, origin = STUB_ORIGIN) {
  const res = await request.post(`${origin}/__reset`);
  expect(res.ok()).toBeTruthy();
}

async function configureStub(request: APIRequestContext, config: { fail_delete: boolean }, origin = STUB_ORIGIN) {
  const res = await request.post(`${origin}/__config`, { data: config });
  expect(res.ok()).toBeTruthy();
}

async function stubRequests(request: APIRequestContext, origin = STUB_ORIGIN): Promise<StubRequest[]> {
  const res = await request.get(`${origin}/__log`);
  expect(res.ok()).toBeTruthy();
  return res.json();
}

/** Stand in for a viewer commenting on the hosted review page. */
async function seedRemoteComment(
  request: APIRequestContext,
  token: string,
  body: string,
  line: number,
  origin = STUB_ORIGIN,
) {
  const res = await request.post(`${origin}/__seed-comment?token=${token}`, {
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

/** Multi-target fixtures always open the destination picker before sharing. */
async function shareToDestination(page: Page, name: string) {
  await page.locator('#shareBtn').click();
  const option = page.locator('.sd-target-option', { hasText: name });
  await expect(option).toBeVisible();
  await option.click();
  await expect(page.locator('.share-dialog')).toBeVisible();
}

test.describe('Share Transport', () => {
  test.beforeEach(async ({ request }) => {
    await resetStub(request, STUB_ORIGIN);
    await resetStub(request, STUB2_ORIGIN);
    // Drop any share recorded by a previous test without re-contacting the
    // stub (which has just been reset and no longer knows the review).
    await request.delete('/api/share-url?local_only=1');
    await clearAllComments(request);
  });

  test('share, pull, re-share and unpublish all stay on the local origin', async ({ page, request }) => {
    const browserCallsToCritWeb: string[] = [];
    page.on('request', req => {
      if (req.url().startsWith(STUB_ORIGIN) || req.url().startsWith(STUB2_ORIGIN)) {
        browserCallsToCritWeb.push(`${req.method()} ${req.url()}`);
      }
    });

    await addComment(request, 'main.go', 3, 'Local comment before sharing');
    await loadPage(page);

    await shareToDestination(page, 'Stub A');
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

    await shareToDestination(page, 'Stub A');

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

  test('config exposes every share target and marks crit.md as needing consent', async ({ request }) => {
    const config = await request.get('/api/config').then(r => r.json());
    const urls = (config.share_targets as { url: string; needs_share_consent?: boolean; name?: string }[])
      .map(t => t.url);
    expect(urls).toEqual(expect.arrayContaining([STUB_ORIGIN, STUB2_ORIGIN, 'https://crit.md']));

    const critMd = (config.share_targets as { url: string; needs_share_consent?: boolean }[])
      .find(t => t.url === 'https://crit.md');
    expect(critMd?.needs_share_consent).toBe(true);

    const stubA = (config.share_targets as { url: string; needs_share_consent?: boolean }[])
      .find(t => t.url === STUB_ORIGIN);
    expect(stubA?.needs_share_consent).toBeFalsy();
  });

  test('destination picker lists both stubs and the crit.md external warning', async ({ page }) => {
    await loadPage(page);
    await page.locator('#shareBtn').click();

    await expect(page.locator('.sd-target-option', { hasText: 'Stub A' })).toBeVisible();
    await expect(page.locator('.sd-target-option', { hasText: 'Stub B' })).toBeVisible();
    const critMd = page.locator('.sd-target-option', { hasText: 'crit.md' });
    await expect(critMd).toBeVisible();
    await expect(critMd).toContainText('External to your organization');
  });

  test('sharing to Stub B binds the review and never touches Stub A', async ({ page, request }) => {
    await addComment(request, 'main.go', 3, 'Local comment before sharing');
    await loadPage(page);

    await shareToDestination(page, 'Stub B');
    await expect(page.locator('.share-dialog-url')).toContainText(`${STUB2_ORIGIN}/r/`);

    const config = await request.get('/api/config').then(r => r.json());
    expect(config.share_base_url).toBe(STUB2_ORIGIN);
    expect(config.hosted_url).toContain(`${STUB2_ORIGIN}/r/`);

    expect(await stubRequests(request, STUB_ORIGIN)).toEqual([]);
    expect(await stubRequests(request, STUB2_ORIGIN)).toContainEqual(
      expect.objectContaining({ method: 'POST', path: '/api/reviews' }),
    );

    // Bound reviews reject a switch to the other configured target.
    const wrong = await request.post('/api/share', {
      data: { target_url: STUB_ORIGIN },
    });
    expect(wrong.status()).toBe(400);
    expect(await wrong.text()).toMatch(/bound/i);

    // Pull/re-share stay on B.
    await seedRemoteComment(request, await hostedToken(request), 'Remote on B', 8, STUB2_ORIGIN);
    await page.locator('#modalPullBtn').click();
    await expect(shareToast(page)).toContainText('Comments pulled');
    expect(await fileCommentBodies(request)).toContain('Remote on B');
    expect(await stubRequests(request, STUB_ORIGIN)).toEqual([]);
  });

  test('removing the bound target keeps the link and explains the missing instance', async ({ page, request }) => {
    const env = fixtureEnv();
    await addComment(request, 'main.go', 3, 'Local comment before sharing');
    await loadPage(page);
    await shareToDestination(page, 'Stub A');
    const hostedURL = (await request.get('/api/config').then(r => r.json())).hosted_url as string;
    expect(hostedURL).toContain(`${STUB_ORIGIN}/r/`);

    // Drop Stub A from global config; freshShareConfig reloads on the next API hit.
    fs.writeFileSync(
      path.join(env.CRIT_HOME, '.crit.config.json'),
      JSON.stringify({
        share_targets: [
          { name: 'Stub B', url: STUB2_ORIGIN, default: true },
          { name: 'crit.md', url: 'https://crit.md' },
        ],
      }, null, 2) + '\n',
    );

    await page.reload();
    await loadPage(page);
    await page.locator('#shareBtn').click();
    await expect(page.locator('.share-dialog')).toBeVisible();
    await expect(page.locator('.share-dialog')).toContainText('no longer configured');
    await expect(page.locator('.share-dialog-url')).toContainText(hostedURL);

    // Restore multi-target config for later tests in this worker.
    fs.writeFileSync(
      path.join(env.CRIT_HOME, '.crit.config.json'),
      JSON.stringify({
        share_targets: [
          { name: 'Stub A', url: STUB_ORIGIN, default: true },
          { name: 'Stub B', url: STUB2_ORIGIN },
          { name: 'crit.md', url: 'https://crit.md' },
        ],
      }, null, 2) + '\n',
    );
  });
});
