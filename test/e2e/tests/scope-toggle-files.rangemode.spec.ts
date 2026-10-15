import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments } from './helpers';
import { ensureRangeFocus, ensureStackedFocus, rangeFixture } from './range-helpers';

// File-list rebuild on layer ↔ full-stack toggle.
//
// Fixture: main → A (adds a.txt) → B (adds b.txt) → C (adds c.txt). Daemon
// boots with --range A..B so layer = {b.txt}. Synthesizing is_stacked=true +
// default_sha=main means full-stack = main..B = {a.txt, b.txt}.
//
// scope-toggle.rangemode.spec.ts covers the disabled full-stack option without
// default_sha. This spec covers what changes when the scope flips.

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
  await ensureRangeFocus(request);
});

test('layer scope file list = files changed in A..B', async ({ page, request }) => {
  await ensureStackedFocus(request, 'layer');
  await loadPage(page);
  // Layer scope shows only b.txt (added in B).
  await expect(page.locator('.tree-file', { hasText: 'b.txt' })).toBeVisible();
  await expect(page.locator('.tree-file', { hasText: 'a.txt' })).toHaveCount(0);
});

test('full-stack scope expands file list to include MAIN..A changes', async ({ page, request }) => {
  await ensureStackedFocus(request, 'full_stack');
  await loadPage(page);
  // Full-stack = main..B = a.txt (from A) + b.txt (from B).
  await expect(page.locator('.tree-file', { hasText: 'a.txt' })).toBeVisible({ timeout: 5_000 });
  await expect(page.locator('.tree-file', { hasText: 'b.txt' })).toBeVisible();
});

test('toggling layer→full-stack→layer restores the layer file list', async ({ page, request }) => {
  await ensureStackedFocus(request, 'layer');
  await loadPage(page);

  await expect(page.locator('.tree-file', { hasText: 'b.txt' })).toBeVisible();
  await expect(page.locator('.tree-file', { hasText: 'a.txt' })).toHaveCount(0);

  // Flip via the in-popover scope toggle (the legacy diffScopeToggle bar
  // was retired; the layer/full-stack radio now lives inside the stack
  // popover with one-line subcopy).
  await page.locator('#stackChipBtn').click();
  const fullStackBtn = page.locator('#stackPopover [data-action="scope"][data-diff-scope="full_stack"]');
  await expect(fullStackBtn).toBeVisible();
  await fullStackBtn.click();

  // After SSE focus-changed, file list expands.
  await expect(page.locator('.tree-file', { hasText: 'a.txt' })).toBeVisible({ timeout: 5_000 });
  await expect(page.locator('.tree-file', { hasText: 'b.txt' })).toBeVisible();

  // Flip back to layer.
  await page.locator('#stackChipBtn').click();
  const layerBtn = page.locator('#stackPopover [data-action="scope"][data-diff-scope="layer"]');
  await layerBtn.click();
  await expect(page.locator('.tree-file', { hasText: 'a.txt' })).toHaveCount(0, { timeout: 5_000 });
  await expect(page.locator('.tree-file', { hasText: 'b.txt' })).toBeVisible();
});

test('a new file added in MAIN..A is visible in full-stack but absent in layer', async ({ request }) => {
  // Read both views via the API so we don't depend on UI rendering timing.
  await ensureStackedFocus(request, 'layer');
  let sess = await (await request.get('/api/session')).json();
  const layerPaths = (sess.files as Array<{ path: string }>).map((f) => f.path);
  expect(layerPaths).toContain('b.txt');
  expect(layerPaths).not.toContain('a.txt');

  await ensureStackedFocus(request, 'full_stack');
  sess = await (await request.get('/api/session')).json();
  const fullStackPaths = (sess.files as Array<{ path: string }>).map((f) => f.path);
  expect(fullStackPaths).toContain('a.txt');
  expect(fullStackPaths).toContain('b.txt');
});

test('focus.diff_scope on /api/session reflects the current scope', async ({ request }) => {
  await ensureStackedFocus(request, 'layer');
  let sess = await (await request.get('/api/session')).json();
  expect(sess.focus.diff_scope).toBe('layer');
  expect(sess.focus.is_stacked).toBe(true);
  expect(sess.focus.default_sha).toBe(rangeFixture().defaultSHA);

  await ensureStackedFocus(request, 'full_stack');
  sess = await (await request.get('/api/session')).json();
  expect(sess.focus.diff_scope).toBe('full_stack');
});

test('comment authored in layer scope is hidden in full-stack and reappears on flip back', async ({ request }) => {
  // Author against the layer scope — DiffScope is stamped from the running
  // focus, so this comment becomes layer-only.
  await ensureStackedFocus(request, 'layer');
  await request.post('/api/file/comments?path=b.txt', {
    data: { start_line: 1, end_line: 1, side: 'RIGHT', body: 'layer-only on b.txt' },
  });
  let res = await request.get('/api/file/comments?path=b.txt');
  let comments = await res.json();
  expect(comments.length).toBe(1);

  // Flip to full-stack: the layer-scoped comment should be filtered out.
  await ensureStackedFocus(request, 'full_stack');
  res = await request.get('/api/file/comments?path=b.txt');
  comments = await res.json();
  expect(comments.length).toBe(0);

  // Flip back to layer — comment is still on disk and reappears.
  await ensureStackedFocus(request, 'layer');
  res = await request.get('/api/file/comments?path=b.txt');
  comments = await res.json();
  expect(comments.length).toBe(1);
  expect(comments[0].body).toBe('layer-only on b.txt');
});
