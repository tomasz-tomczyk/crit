import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';
import { ensureRangeFocus } from './range-helpers';

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
  await ensureRangeFocus(request);
});

test('comment in range mode stamps head_sha and diff_scope', async ({ request }) => {
  // Author a comment via API on b.txt line 1.
  const post = await request.post('/api/file/comments?path=b.txt', {
    data: { start_line: 1, end_line: 1, side: 'RIGHT', body: 'hello' },
  });
  expect(post.ok()).toBeTruthy();

  // Verify the daemon returns it stamped.
  const res = await request.get('/api/file/comments?path=b.txt');
  expect(res.ok()).toBeTruthy();
  const comments = await res.json();
  expect(comments.length).toBeGreaterThan(0);
  expect(comments[0].head_sha).toBeTruthy();
  expect(comments[0].diff_scope).toBe('layer');
});

test('range-mode comment seeded with side=RIGHT does not render as outdated', async ({ page, request }) => {
  // Regression: comments POSTed with side="RIGHT" (GitHub-style, used by the
  // test-diff seed and external tools) were stored verbatim and the frontend's
  // diff renderer keyed them as "1:RIGHT" while diff hunk add lines keyed as
  // "1:". The mismatch caused the comment to be appended to the
  // appendOutdatedDiffComments section with the "Outdated" badge on first
  // load, before any user interaction.
  await request.post('/api/file/comments?path=b.txt', {
    data: { start_line: 1, end_line: 1, side: 'RIGHT', body: 'should render inline, not outdated' },
  });

  await loadPage(page);

  // The comment should be rendered inline within the diff, not in the
  // outdated section, and should not have the "Outdated" badge.
  const outdatedSection = page.locator('.outdated-diff-comments');
  await expect(outdatedSection).toHaveCount(0);
  await expect(page.locator('.outdated-badge')).toHaveCount(0);

  // And the comment itself must be visible somewhere on the page.
  await expect(page.getByText('should render inline, not outdated')).toBeVisible();
});
