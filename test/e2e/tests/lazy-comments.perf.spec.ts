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
  const first = await addComment(request, TAIL_FILE, 1, 'Open on a lazy file');
  const second = await addComment(request, TAIL_FILE, 2, 'Second open one');
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

  // Resolving a comment after open reaches the lazy file through the
  // comments-changed refresh.
  const resolveRes = await request.put(
    `/api/comment/${second.id}/resolve?path=${encodeURIComponent(TAIL_FILE)}`,
    { data: { resolved: true } },
  );
  expect(resolveRes.ok()).toBeTruthy();
  await expect(page.locator('#commentCountNumber')).toHaveText('1');
  await expect(page.locator(`.tree-file[data-tree-path="${TAIL_FILE}"] .tree-comment-badge`)).toHaveText('1');

  // Clicking the open card loads the diff and shows the inline card.
  await page.locator('#commentsFilterPill .toggle-btn[data-filter="open"]').click();
  await expect(cards).toHaveCount(1);
  await cards.first().click();
  const inline = page.locator(`#filesContainer .comment-card[data-comment-id="${first.id}"]`);
  await expect(inline).toBeVisible();
  await expect(inline).toBeInViewport();
});

test('a comments refresh during lazy load is not overwritten', async ({ page, request }) => {
  const first = await addComment(request, TAIL_FILE, 1, 'Open on a lazy file');
  await addComment(request, TAIL_FILE, 2, 'Resolved while loading');
  await loadPage(page);
  const badge = page.locator(`.tree-file[data-tree-path="${TAIL_FILE}"] .tree-comment-badge`);
  await expect(badge).toHaveText('2');

  // Hold the per-file comments fetch that loads with the diff.
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  let fetchStarted!: () => void;
  const started = new Promise<void>((resolve) => { fetchStarted = resolve; });
  await page.route(`**/api/file/comments?path=${encodeURIComponent(TAIL_FILE)}`, async (route) => {
    const response = await route.fetch();
    fetchStarted();
    await held;
    await route.fulfill({ response });
  });

  await page.keyboard.press('Shift+C');
  await page.locator('.panel-comment-block .comment-card').filter({ hasText: 'Open on a lazy file' }).click();
  await started;

  // Resolve the second comment while the load holds the older list.
  const comments = await request.get(`/api/file/comments?path=${encodeURIComponent(TAIL_FILE)}`).then((r) => r.json());
  const second = comments.find((c: { id: string }) => c.id !== first.id);
  const res = await request.put(
    `/api/comment/${second.id}/resolve?path=${encodeURIComponent(TAIL_FILE)}`,
    { data: { resolved: true } },
  );
  expect(res.ok()).toBeTruthy();
  await expect(badge).toHaveText('1');

  release();
  await expect(page.locator(`#filesContainer .comment-card[data-comment-id="${first.id}"]`)).toBeVisible();
  await expect(badge).toHaveText('1');
  await expect(page.locator('#commentCountNumber')).toHaveText('1');
});
