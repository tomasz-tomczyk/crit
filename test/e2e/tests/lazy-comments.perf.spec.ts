import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments, addComment, waitForScrollStable } from './helpers';

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

  // Hold the lazy file's diff load. The load fetches no comments, so the
  // list the file already has must survive it.
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  let fetchStarted!: () => void;
  const started = new Promise<void>((resolve) => { fetchStarted = resolve; });
  await page.route(`**/api/file/diff?path=${encodeURIComponent(TAIL_FILE)}*`, async (route) => {
    const response = await route.fetch();
    fetchStarted();
    await held;
    await route.fulfill({ response });
  });

  await page.keyboard.press('Shift+C');
  await page.locator('.panel-comment-block .comment-card').filter({ hasText: 'Open on a lazy file' }).click();
  await started;

  // Resolve the second comment while the load is held.
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

// The review conversation heads the list. Coming back to it from a comment
// at the far end must not stop partway up while files mount on the way.
test('a review comment card in the panel scrolls back from the bottom', async ({ page, request }) => {
  await expect(await request.post('/api/comments', { data: { body: 'Review-level note' } })).toBeOK();
  const tail = await addComment(request, TAIL_FILE, 1, 'At the bottom');
  await loadPage(page);

  await page.keyboard.press('Shift+C');
  const cards = page.locator('.panel-comment-block .comment-card');
  await cards.filter({ hasText: 'At the bottom' }).click();
  await expect(page.locator(`#filesContainer .comment-card[data-comment-id="${tail.id}"]`)).toBeInViewport();

  await cards.filter({ hasText: 'Review-level note' }).click();
  await expect(page.locator('#reviewConversation .comment-card')).toBeInViewport();
});

test('a file jump still loading does not pull a review comment away', async ({ page, request }) => {
  await expect(await request.post('/api/comments', { data: { body: 'Review-level note' } })).toBeOK();
  await addComment(request, TAIL_FILE, 1, 'At the bottom');
  await loadPage(page);

  // Hold the lazy file's diff so its jump is still in flight.
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  let fetchStarted!: () => void;
  const started = new Promise<void>((resolve) => { fetchStarted = resolve; });
  await page.route((url) => url.pathname === '/api/file/diff' && url.searchParams.get('path') === TAIL_FILE, async (route) => {
    fetchStarted();
    await held;
    await route.continue();
  });

  await page.keyboard.press('Shift+C');
  const cards = page.locator('.panel-comment-block .comment-card');
  await cards.filter({ hasText: 'At the bottom' }).click();
  await started;
  await cards.filter({ hasText: 'Review-level note' }).click();
  const reviewCard = page.locator('#reviewConversation .comment-card');
  await expect(reviewCard).toBeInViewport();

  const loaded = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/file/diff' && new URL(r.url()).searchParams.get('path') === TAIL_FILE);
  release();
  await loaded;
  await waitForScrollStable(page);
  await expect(reviewCard).toBeInViewport();
});

test('a jump right after a review comment is not pulled back to the top', async ({ page, request }) => {
  await expect(await request.post('/api/comments', { data: { body: 'Review-level note' } })).toBeOK();
  // An eager file a few screens down: its jump scrolls at once, while the
  // review comment's alignment frames are still running.
  const session = await (await request.get('/api/session')).json();
  const eager = session.files[12].path;
  const target = await addComment(request, eager, 1, 'Further down');
  await loadPage(page);

  await page.keyboard.press('Shift+C');
  const cards = page.locator('.panel-comment-block .comment-card');
  await cards.filter({ hasText: 'Review-level note' }).click();
  await cards.filter({ hasText: 'Further down' }).click();
  await waitForScrollStable(page);
  await expect(page.locator(`#filesContainer .comment-card[data-comment-id="${target.id}"]`)).toBeInViewport();
});
