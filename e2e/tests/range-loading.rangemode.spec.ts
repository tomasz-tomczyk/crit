import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments } from './helpers';
import { ensureRangeFocus } from './range-helpers';

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
  await ensureRangeFocus(request);
});

test('range mode shows only files between A and B', async ({ page }) => {
  await loadPage(page);
  // The fixture builds main -> A (a.txt) -> B (b.txt) -> C (c.txt) and
  // boots --range A..B. Only b.txt is in the diff.
  await expect(page.locator('.tree-file', { hasText: 'b.txt' })).toBeVisible();
  await expect(page.locator('.tree-file', { hasText: 'a.txt' })).toHaveCount(0);
  await expect(page.locator('.tree-file', { hasText: 'c.txt' })).toHaveCount(0);
});

test('chip popover renders the reviewing entry for the booted range', async ({ page }) => {
  await loadPage(page);
  // The fixture boots `--range A..B` where B is feat-b. The chip's
  // current entry should be feat-b (marked aria-current; visual styling
  // conveys "reviewing" without a separate text marker).
  await expect(page.locator('#stackChip')).toBeVisible({ timeout: 5_000 });
  await page.locator('#stackChipBtn').click();
  const current = page.locator('#stackPopover .stack-popover-current').filter({ hasNotText: /full stack/i });
  await expect(current).toBeVisible();
  await expect(current).toContainText('feat-b');
  await expect(current).toHaveAttribute('aria-current', 'page');
});

test('session API exposes range focus', async ({ request }) => {
  const res = await request.get('/api/session');
  expect(res.ok()).toBeTruthy();
  const body = await res.json();
  expect(body.focus.kind).toBe('range');
  expect(body.focus.base_sha).toBeTruthy();
  expect(body.focus.head_sha).toBeTruthy();
});
