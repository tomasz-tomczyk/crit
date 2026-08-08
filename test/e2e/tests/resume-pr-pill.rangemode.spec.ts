import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments } from './helpers';
import { ensureRangeFocus, rangeFixture } from './range-helpers';

// The Resume PR pill appears in working-tree mode whenever the session
// remembers a recently-active range focus. Click restores that focus.

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
  await ensureRangeFocus(request);
});

test('pill is hidden in range mode and visible after switching to working tree', async ({ page, request }) => {
  await loadPage(page);
  // We're in range mode; the resume pill must NOT be visible yet (no last
  // range focus stashed because the session was booted into range, not
  // toggled out of one).
  await expect(page.locator('#resumePrPill')).toBeHidden();
  await expect(page.locator('#stackChipExit')).toBeVisible();

  // Toggle to working tree via the stack chip ✕ exit.
  await page.locator('#stackChipExit').click();

  // Wait for focus update + UI re-render.
  await expect.poll(async () => {
    const r = await request.get('/api/session');
    const s = await r.json();
    return s.focus && s.focus.kind;
  }, { timeout: 5_000 }).toBe('working_tree');

  // Resume pill should now appear.
  await expect(page.locator('#resumePrPill')).toBeVisible({ timeout: 5_000 });
});

test('clicking Resume restores the previous range focus', async ({ page, request }) => {
  await loadPage(page);
  // Capture the current range head SHA, switch to WT, then click Resume.
  const start = await (await request.get('/api/session')).json();
  expect(start.focus.kind).toBe('range');
  const startHead = start.focus.head_sha;

  await page.locator('#stackChipExit').click();
  await expect.poll(async () => {
    const r = await request.get('/api/session');
    return (await r.json()).focus.kind;
  }, { timeout: 5_000 }).toBe('working_tree');

  await page.locator('#resumePrPill').click();
  await expect.poll(async () => {
    const r = await request.get('/api/session');
    const s = await r.json();
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
