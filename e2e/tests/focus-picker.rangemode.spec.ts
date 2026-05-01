import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments } from './helpers';
import { ensureStackedFocus, rangeFixture } from './range-helpers';

// The stack chip + popover replace the old multi-section focus-picker
// popover. The CLI (`crit --pr <N>` / `crit --range A..B`) is still the
// only entry point into range mode from working tree — the popover only
// navigates *within* the active stack.

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
  // ensureStackedFocus stamps default_sha + is_stacked=true so the chip
  // and layer/full-stack toggle both show. The stack itself comes from
  // the fixture's per-commit branches (feat-a/b/c).
  await ensureStackedFocus(request);
});

test('chip is visible in stacked range focus', async ({ page }) => {
  await loadPage(page);
  await expect(page.locator('#stackChip')).toBeVisible();
});

test('stack chip ✕ exit is visible in range focus', async ({ page }) => {
  await loadPage(page);
  await expect(page.locator('#stackChipExit')).toBeVisible();
  await expect(page.locator('#stackChipExit')).toHaveAttribute('aria-label', /working tree/i);
});

test('popover lists feat-a, feat-b, feat-c', async ({ page }) => {
  await loadPage(page);
  await page.locator('#stackChipBtn').click();
  // Popover renders "Loading…" first, then re-renders with stack items
  // after /api/picker resolves. Wait for an actual stack item before reading text.
  await expect(page.locator('#stackPopover .stack-popover-item').first()).toBeVisible();
  await expect(page.locator('#stackPopover')).toContainText('feat-a');
  await expect(page.locator('#stackPopover')).toContainText('feat-b');
  await expect(page.locator('#stackPopover')).toContainText('feat-c');
});

test('popover renders the default branch as the first entry', async ({ page }) => {
  await loadPage(page);
  await page.locator('#stackChipBtn').click();
  const dbItem = page.locator('#stackPopover .stack-popover-default');
  await expect(dbItem).toBeVisible();
  await expect(dbItem).toContainText(/main/);
});

test('popover order is base->head (main, feat-a, feat-b, feat-c)', async ({ page }) => {
  await loadPage(page);
  await page.locator('#stackChipBtn').click();
  // Wait for the post-/api/picker render — the initial "Loading…" view has no
  // .stack-popover-item nodes, so reading evaluateAll too soon yields [].
  const items = page.locator('#stackPopover .stack-popover-item');
  await expect(items.first()).toBeVisible();
  // 4 entries expected: main + feat-a/b/c.
  await expect(items).toHaveCount(4);
  const labels = await items.evaluateAll((els) =>
    els.map((el) => (el as HTMLElement).innerText.replace(/\s*\(reviewing\)\s*/i, '').replace(/\s*\(full stack\)\s*/i, '').trim())
  );
  // First entry is the default branch label.
  expect(labels[0]).toMatch(/main/);
  const feats = labels.filter((s) => /^feat-[abc]$/.test(s.split('\n').pop() || ''));
  expect(feats.map((s) => (s.split('\n').pop() || '').trim())).toEqual(['feat-a', 'feat-b', 'feat-c']);
});

test('clicking a different stack entry switches focus and rebuilds file list', async ({ page, request }) => {
  await loadPage(page);
  // Sanity: focused on B -> file list = b.txt only.
  await expect(page.locator('.tree-file', { hasText: 'b.txt' })).toBeVisible();
  await expect(page.locator('.tree-file', { hasText: 'c.txt' })).toHaveCount(0);

  await page.locator('#stackChipBtn').click();
  const featC = page.locator('#stackPopover button.stack-popover-item', { hasText: /^.*feat-c.*$/ });
  await expect(featC).toBeVisible();
  await featC.click();

  await expect(async () => {
    const sess = await (await request.get('/api/session')).json();
    expect(sess.focus.kind).toBe('range');
    expect(sess.focus.head_sha).toBeTruthy();
  }).toPass({ timeout: 5_000 });

  await expect(page.locator('.tree-file', { hasText: 'c.txt' })).toBeVisible({ timeout: 5_000 });
  await expect(page.locator('.tree-file', { hasText: 'b.txt' })).toHaveCount(0);
});

test('chip stays visible in range mode; popover shows no-stack placeholder', async ({ page, request }) => {
  // The stack chip is always rendered in range mode — the ✕ exit must
  // stay reachable, and the chip's label paints from focus data without
  // waiting for /api/picker. When the picker resolves with an empty
  // stack, the popover content transitions from "Loading…" to a
  // "No surrounding stack" placeholder rather than hiding the chip.
  await page.route('**/api/picker', async (route) => {
    const real = await (await request.get('/api/picker')).json();
    real.stack = [];
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(real),
    });
  });
  await loadPage(page);
  await expect(page.locator('#stackChip')).toBeVisible();
  await page.locator('#stackChipBtn').click();
  await expect(page.locator('#stackPopover')).toContainText(/no surrounding stack/i, { timeout: 5_000 });
});

test('chip body opens popover; ✕ exit does NOT open popover (clicks independent)', async ({ page, request }) => {
  await loadPage(page);
  const popover = page.locator('#stackPopover');
  const exitBtn = page.locator('#stackChipExit');
  await expect(popover).toBeHidden();
  // Clicking the ✕ exit must not bubble into the chip's open-popover handler.
  // Use Promise to ensure no popover ever appears between the click and the
  // focus switch resolving.
  await exitBtn.click();
  await expect(popover).toBeHidden();
  await expect.poll(async () => {
    const r = await request.get('/api/session');
    return (await r.json()).focus.kind;
  }, { timeout: 5_000 }).toBe('working_tree');
});

test('focus-changed SSE re-renders the chip and popover (current marker moves)', async ({ page, request }) => {
  await loadPage(page);
  await page.locator('#stackChipBtn').click();
  const current = page.locator('#stackPopover .stack-popover-current').filter({ hasNotText: /full stack/i });
  await expect(current).toBeVisible({ timeout: 5_000 });
  await expect(current).toContainText('feat-b');

  // Close popover, switch focus, then re-open and re-check.
  await page.keyboard.press('Escape');
  const pickerData = await (await request.get('/api/picker')).json();
  const f = rangeFixture();
  const featA = (pickerData.stack as Array<{ label: string; head_sha: string; base_sha?: string }>).find((e) => e.label === 'feat-a');
  expect(featA).toBeTruthy();
  if (!featA) return;
  await request.post('/api/focus', {
    data: {
      kind: 'range',
      base_sha: featA.base_sha || f.defaultSHA,
      head_sha: featA.head_sha,
      default_sha: f.defaultSHA,
      diff_scope: 'layer',
      is_stacked: true,
    },
  });
  await page.locator('#stackChipBtn').click();
  await expect(
    page.locator('#stackPopover .stack-popover-current').filter({ hasNotText: /full stack/i })
  ).toContainText('feat-a', { timeout: 5_000 });
});
