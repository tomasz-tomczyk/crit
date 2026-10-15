import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments } from './helpers';
import { ensureRangeFocus } from './range-helpers';

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
  await ensureRangeFocus(request);
});

test('full-stack option disabled when default_sha is missing', async ({ page, request }) => {
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
  await page.locator('#stackChipBtn').click();
  // Layer is always available; full-stack requires default_sha.
  await expect(page.locator('#stackPopover [data-action="scope"][data-diff-scope="layer"]')).toBeVisible();
  await expect(page.locator('#stackPopover [data-action="scope"][data-diff-scope="full_stack"]')).toBeDisabled();
});
