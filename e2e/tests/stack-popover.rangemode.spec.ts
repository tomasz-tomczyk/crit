import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments } from './helpers';
import { ensureStackedFocus } from './range-helpers';

// Replaces the old breadcrumb-* suite. The stack chip + popover is the new
// in-stack navigator: a single chip in the header that toggles a vertical
// tree popover with all stack entries plus a default-branch entry that
// flips diff_scope to full_stack.

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
  await ensureStackedFocus(request);
});

test('chip is visible in stacked focus and shows current entry label', async ({ page }) => {
  await loadPage(page);
  const chip = page.locator('#stackChip');
  await expect(chip).toBeVisible({ timeout: 5_000 });
  // Label is non-empty (real entry label, not the placeholder "Stack").
  await expect(page.locator('#stackChipLabel')).not.toHaveText(/^$/);
});

test('clicking the chip toggles the popover open and shut', async ({ page }) => {
  await loadPage(page);
  const chipBtn = page.locator('#stackChipBtn');
  const popover = page.locator('#stackPopover');
  await expect(chipBtn).toBeVisible({ timeout: 5_000 });
  await expect(popover).toBeHidden();
  await chipBtn.click();
  await expect(popover).toBeVisible();
  await chipBtn.click();
  await expect(popover).toBeHidden();
});

test('Escape closes the popover', async ({ page }) => {
  await loadPage(page);
  await page.locator('#stackChipBtn').click();
  await expect(page.locator('#stackPopover')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.locator('#stackPopover')).toBeHidden();
});

test('outside click closes the popover', async ({ page }) => {
  await loadPage(page);
  await page.locator('#stackChipBtn').click();
  await expect(page.locator('#stackPopover')).toBeVisible();
  await page.locator('body').click({ position: { x: 5, y: 5 } });
  await expect(page.locator('#stackPopover')).toBeHidden();
});

test('current entry is a non-clickable span marked aria-current', async ({ page }) => {
  await loadPage(page);
  await page.locator('#stackChipBtn').click();
  // The current row is a <span> (not a <button>) so it can't be clicked
  // through to itself. The brand-tinted background + aria-current="page"
  // are the visual + a11y signals; there is no separate "(reviewing)"
  // text marker (intentionally removed — the styling already conveys
  // current state without crowding the row).
  const current = page.locator('#stackPopover .stack-popover-current').filter({ hasNotText: /full stack/i });
  await expect(current).toBeVisible();
  const tag = await current.evaluate((el) => el.tagName.toLowerCase());
  expect(tag).toBe('span');
  await expect(current).toHaveAttribute('aria-current', 'page');
});

test('default-branch entry is a non-interactive root marker (no click action)', async ({ page, request }) => {
  await loadPage(page);
  await page.locator('#stackChipBtn').click();
  const db = page.locator('#stackPopover .stack-popover-default');
  await expect(db).toBeVisible();
  // It must NOT be a button — scope switching is the diff-area toggle's job.
  const tag = await db.evaluate((el) => el.tagName.toLowerCase());
  expect(tag).toBe('span');
  // Capture diff_scope before, click, confirm scope did NOT change.
  const before = await (await request.get('/api/session')).json();
  const beforeScope = before.focus.diff_scope;
  await db.click();
  // Give the no-op a beat — but assert by polling that scope is unchanged.
  await expect.poll(async () => {
    const r = await request.get('/api/session');
    return (await r.json()).focus.diff_scope;
  }, { timeout: 1_500 }).toBe(beforeScope);
});

test('default branch is rendered ONCE (no ghost duplicate stack entry)', async ({ page }) => {
  // Regression: the picker walks first-parent ancestors; when the default
  // branch tip sits on the chain it produced a stack entry whose label was
  // the default-branch name — the popover then rendered the default branch
  // both as the root marker AND as a regular tree entry. The frontend now
  // filters out any stack entry whose head_sha matches the entry-stamped
  // default_sha so each branch appears at most once.
  await loadPage(page);
  await page.locator('#stackChipBtn').click();
  // Popover initially renders "Loading…"; wait for real items so allTextContents()
  // doesn't snapshot the empty pre-fetch DOM.
  await expect(page.locator('#stackPopover .stack-popover-item').first()).toBeVisible();
  const allLabels = await page.locator('#stackPopover .stack-popover-label').allTextContents();
  const mainCount = allLabels.filter((s) => s.trim() === 'main').length;
  expect(mainCount).toBe(1);
});

test('non-current entry click switches focus', async ({ page, request }) => {
  await loadPage(page);
  await page.locator('#stackChipBtn').click();
  // Wait for popover to finish loading before snapshotting button count.
  await expect(page.locator('#stackPopover .stack-popover-item').first()).toBeVisible();
  // Pick the first non-default, non-current entry button.
  const buttons = page.locator('#stackPopover button.stack-popover-item:not(.stack-popover-default)');
  const count = await buttons.count();
  if (count === 0) test.skip(true, 'fixture has no other clickable stack entries');
  const before = await (await request.get('/api/session')).json();
  await buttons.first().click();
  await expect.poll(async () => {
    const r = await request.get('/api/session');
    const s = await r.json();
    return s.focus.head_sha;
  }, { timeout: 5_000 }).not.toBe(before.focus.head_sha);
});
