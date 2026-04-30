import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments } from './helpers';
import { ensureRangeFocus } from './range-helpers';

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
  await ensureRangeFocus(request);
});

test('layer/full-stack scope rows render in stack popover for stacked focus', async ({ page, request }) => {
  // Synthesize stacked metadata. default_sha is required for full-stack
  // option to be enabled; baseSHA stands in (server only checks "non-empty").
  const sess = await (await request.get('/api/session')).json();
  const baseSHA = sess.focus.base_sha;
  const headSHA = sess.focus.head_sha;
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
  // Open the stack chip popover. The scope rows live at the bottom.
  await page.locator('#stackChipBtn').click();
  await expect(page.locator('#stackPopover [data-action="scope"][data-diff-scope="layer"]')).toBeVisible();
  const fullStackBtn = page.locator('#stackPopover [data-action="scope"][data-diff-scope="full_stack"]');
  await expect(fullStackBtn).toBeVisible();
  await expect(fullStackBtn).toBeEnabled();
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

test('legacy diff-scope-toggle bar stays hidden (toggle moved into popover)', async ({ page, request }) => {
  // Sanity-check: after moving the layer/full-stack toggle into the popover,
  // the old top-of-page #diffScopeToggle bar must not render — otherwise we'd
  // have two competing controls for the same setting.
  const sess = await (await request.get('/api/session')).json();
  const baseSHA = sess.focus.base_sha;
  const headSHA = sess.focus.head_sha;
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
  await expect(page.locator('#diffScopeToggle')).toBeHidden();
});

test('working-tree scope toggle is hidden in range mode', async ({ page }) => {
  // The All / Branch / Staged / Unstaged toggle filters by working-tree
  // state vs baseRef — meaningless in range mode where the diff is pinned
  // to a fixed BaseSHA..HeadSHA. Hiding it prevents the half-baked
  // interaction where the file list gets working-tree-filtered but file
  // diffs stay pinned to the range.
  await loadPage(page);
  await expect(page.locator('#scopeToggle')).toBeHidden();
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
