import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments } from './helpers';
import { ensureRangeFocus } from './range-helpers';

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
  await ensureRangeFocus(request);
});

test('layer/full-stack toggle visible for synthesized stacked focus', async ({ page, request }) => {
  // Read SHAs from the running session so we can preserve range identity.
  const sess = await (await request.get('/api/session')).json();
  const baseSHA = sess.focus.base_sha;
  const headSHA = sess.focus.head_sha;
  // Synthesize stacked metadata. default_sha is required for full-stack toggle
  // to be enabled; baseSHA is fine as a stand-in (server only checks "non-empty").
  const post = await request.post('/api/focus', {
    data: {
      kind: 'range',
      base_sha: baseSHA,
      head_sha: headSHA,
      default_sha: baseSHA,
      diff_scope: 'layer',
      is_stacked: true,
    },
  });
  expect(post.ok()).toBeTruthy();

  await loadPage(page);
  await expect(page.locator('#diffScopeToggle')).toBeVisible();
});

test('toggle hidden when not stacked', async ({ page, request }) => {
  const sess = await (await request.get('/api/session')).json();
  const baseSHA = sess.focus.base_sha;
  const headSHA = sess.focus.head_sha;
  const post = await request.post('/api/focus', {
    data: {
      kind: 'range',
      base_sha: baseSHA,
      head_sha: headSHA,
      diff_scope: 'layer',
      is_stacked: false,
    },
  });
  expect(post.ok()).toBeTruthy();

  await loadPage(page);
  await expect(page.locator('#diffScopeToggle')).toBeHidden();
});

test('full-stack rejected without default_sha', async ({ request }) => {
  const sess = await (await request.get('/api/session')).json();
  const baseSHA = sess.focus.base_sha;
  const headSHA = sess.focus.head_sha;
  const post = await request.post('/api/focus', {
    data: {
      kind: 'range',
      base_sha: baseSHA,
      head_sha: headSHA,
      diff_scope: 'full_stack',
    },
  });
  expect(post.status()).toBe(400);
});
