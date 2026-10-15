import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments } from './helpers';
import { ensureRangeFocus, rangeFixture } from './range-helpers';

// The Resume PR pill appears in working-tree mode whenever the session
// remembers a recently-active range focus. Click restores that focus.

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
  await ensureRangeFocus(request);
});

// One round trip through the stack chip ✕ exit and the Resume pill.
test('✕ exit leaves range mode without opening the popover; Resume restores the previous range', async ({ page, request }) => {
  await loadPage(page);
  const start = await (await request.get('/api/session')).json();
  expect(start.focus.kind).toBe('range');
  const startHead = start.focus.head_sha;

  // Range mode: only b.txt; the ✕ exit is visible; no resume pill yet (the
  // session was booted into range, not toggled out of one).
  await expect(page.locator('.tree-file', { hasText: 'b.txt' })).toBeVisible();
  const exitBtn = page.locator('#stackChipExit');
  await expect(exitBtn).toBeVisible();
  await expect(exitBtn).toHaveAttribute('aria-label', /working tree/i);
  await expect(page.locator('#resumePrPill')).toBeHidden();
  const popover = page.locator('#stackPopover');
  await expect(popover).toBeHidden();

  // Clicking ✕ must not bubble into the chip's open-popover handler.
  await exitBtn.click();
  await expect(popover).toBeHidden();
  await expect.poll(async () => {
    const s = await (await request.get('/api/session')).json();
    return s.focus && s.focus.kind;
  }, { timeout: 5_000 }).toBe('working_tree');

  // File list rebuilds for working tree (all three files vs main), and the
  // Resume pill appears.
  await expect(page.locator('.tree-file', { hasText: 'a.txt' })).toBeVisible({ timeout: 5_000 });
  await expect(page.locator('.tree-file', { hasText: 'c.txt' })).toBeVisible();
  await expect(page.locator('#resumePrPill')).toBeVisible({ timeout: 5_000 });

  // Resume restores the previous range focus.
  await page.locator('#resumePrPill').click();
  await expect.poll(async () => {
    const s = await (await request.get('/api/session')).json();
    return s.focus.kind === 'range' ? s.focus.head_sha : '';
  }, { timeout: 5_000 }).toBe(startHead);
});

test('GitLab focus resumes from canonical forge and change_number fields', async ({ page, request }) => {
  const fixture = rangeFixture();
  const post = await request.post('/api/focus', {
    data: {
      kind: 'range',
      forge: 'gitlab',
      change_number: 17,
      base_sha: fixture.base,
      head_sha: fixture.head,
      diff_scope: 'layer',
    },
  });
  expect(post.ok()).toBeTruthy();

  await loadPage(page);
  await page.locator('#stackChipExit').click();
  await expect(page.locator('#resumePrPill')).toHaveText('Resume MR !17');

  await page.locator('#resumePrPill').click();
  await expect.poll(async () => {
    const response = await request.get('/api/session');
    const focus = (await response.json()).focus;
    if (focus.kind !== 'range') return '';
    return `${focus.forge}:${focus.change_number}:${Object.prototype.hasOwnProperty.call(focus, 'mr_number')}`;
  }, { timeout: 5_000 }).toBe('gitlab:17:false');
});
