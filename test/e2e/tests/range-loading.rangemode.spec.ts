import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments } from './helpers';
import { ensureRangeFocus } from './range-helpers';

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
  await ensureRangeFocus(request);
});

test('range mode shows only files between A and B, without the working-tree scope toggle', async ({ page, request }) => {
  // The session API exposes the range focus.
  const body = await (await request.get('/api/session')).json();
  expect(body.focus.kind).toBe('range');
  expect(body.focus.base_sha).toBeTruthy();
  expect(body.focus.head_sha).toBeTruthy();

  await loadPage(page);
  // The fixture builds main -> A (a.txt) -> B (b.txt) -> C (c.txt) and
  // boots --range A..B. Only b.txt is in the diff.
  await expect(page.locator('.tree-file', { hasText: 'b.txt' })).toBeVisible();
  await expect(page.locator('.tree-file', { hasText: 'a.txt' })).toHaveCount(0);
  await expect(page.locator('.tree-file', { hasText: 'c.txt' })).toHaveCount(0);

  // The All / Branch / Staged / Unstaged toggle filters by working-tree state
  // vs baseRef — meaningless when the diff is pinned to BaseSHA..HeadSHA.
  await expect(page.locator('#scopeToggle')).toBeHidden();
});
