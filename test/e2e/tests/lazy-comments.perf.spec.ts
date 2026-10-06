import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments, addComment } from './helpers';

// Comments on files past the eager load limit (25) must show on open, before
// the file's diff loads (#1032). plan-big.md sorts last in the 301-file perf
// fixture, so it stays a lazy stub until the reader scrolls to it.
const TAIL_FILE = 'plan-big.md';

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
});

test('comments on lazy files count and list on open', async ({ page, request }) => {
  await addComment(request, TAIL_FILE, 1, 'Open on a lazy file');
  await addComment(request, TAIL_FILE, 2, 'Second open one');
  const resolved = await addComment(request, TAIL_FILE, 3, 'Resolved on a lazy file');
  const res = await request.put(
    `/api/comment/${resolved.id}/resolve?path=${encodeURIComponent(TAIL_FILE)}`,
    { data: { resolved: true } },
  );
  expect(res.ok()).toBeTruthy();

  const diffLoads: string[] = [];
  page.on('request', (r) => {
    const url = new URL(r.url());
    if (url.pathname === '/api/file/diff' && url.searchParams.get('path') === TAIL_FILE) diffLoads.push(TAIL_FILE);
  });

  await loadPage(page);

  // Header counts unresolved comments.
  await expect(page.locator('#commentCountNumber')).toHaveText('2');
  // Tree badge shows the unresolved count on the lazy file.
  await expect(page.locator(`.tree-file[data-tree-path="${TAIL_FILE}"] .tree-comment-badge`)).toHaveText('2');

  // Comments panel lists them, and the filters split open from resolved.
  await page.keyboard.press('Shift+C');
  const cards = page.locator('.panel-comment-block .comment-card');
  await page.locator('#commentsFilterPill .toggle-btn[data-filter="open"]').click();
  await expect(cards).toHaveCount(2);
  await expect(cards.filter({ hasText: 'Open on a lazy file' })).toHaveCount(1);
  await page.locator('#commentsFilterPill .toggle-btn[data-filter="resolved"]').click();
  await expect(cards).toHaveCount(1);
  await expect(cards.first()).toContainText('Resolved on a lazy file');

  // The diff itself is still deferred.
  expect(diffLoads).toHaveLength(0);
});
