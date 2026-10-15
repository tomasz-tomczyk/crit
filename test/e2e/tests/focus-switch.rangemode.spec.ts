import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments } from './helpers';
import { ensureRangeFocus } from './range-helpers';

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
  await ensureRangeFocus(request);
});

test('switch to working tree refreshes file list (via API)', async ({ page, request }) => {
  await loadPage(page);
  // Sanity: range mode shows only b.txt.
  await expect(page.locator('.tree-file', { hasText: 'b.txt' })).toBeVisible();

  // Switch directly via API (the stack chip ✕ exit UI is also wired but the
  // API call exercises the same endpoint and avoids depending on dynamic
  // stack data).
  const post = await request.post('/api/focus', {
    data: { kind: 'working_tree' },
  });
  expect(post.ok()).toBeTruthy();

  // After SSE focus-changed fires, the file list should include c.txt
  // (the tip of feat-c on the running fixture) — but the fixture's HEAD
  // is feat-c so working-tree changes vs main include all 3 files.
  await expect(page.locator('.tree-file', { hasText: 'a.txt' })).toBeVisible({ timeout: 5_000 });
  await expect(page.locator('.tree-file', { hasText: 'c.txt' })).toBeVisible();
});
