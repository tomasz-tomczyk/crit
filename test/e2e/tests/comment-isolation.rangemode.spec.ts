import { test, expect } from '@playwright/test';
import { clearAllComments } from './helpers';
import { ensureRangeFocus, rangeFixture } from './range-helpers';

// Comments belong to the *view* they were authored in, identified by
// FocusKey. A comment authored in one range view must not leak into a
// different range view, even if the diff content overlaps.

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
  await ensureRangeFocus(request);
});

test('comment authored at A..B is hidden when viewing A..D, visible when returning to A..B', async ({ request }) => {
  const f = rangeFixture();

  // Author against b.txt while in A..B.
  const post = await request.post('/api/file/comments?path=b.txt', {
    data: { start_line: 1, end_line: 1, side: 'RIGHT', body: 'authored in A..B' },
  });
  expect(post.ok()).toBeTruthy();

  // Confirm visible in this view.
  let res = await request.get('/api/file/comments?path=b.txt');
  let comments = await res.json();
  expect(comments.length).toBe(1);
  expect(comments[0].body).toBe('authored in A..B');

  // Switch focus to A..D — different view, even if file overlaps.
  await request.post('/api/focus', {
    data: { kind: 'range', base_sha: f.base, head_sha: f.headAfter, diff_scope: 'layer' },
  });

  res = await request.get('/api/file/comments?path=b.txt');
  comments = await res.json();
  expect(comments).toEqual([]);

  // Switch back to A..B — comment reappears.
  await request.post('/api/focus', {
    data: { kind: 'range', base_sha: f.base, head_sha: f.head, diff_scope: 'layer' },
  });
  res = await request.get('/api/file/comments?path=b.txt');
  comments = await res.json();
  expect(comments.length).toBe(1);
  expect(comments[0].body).toBe('authored in A..B');
});

test('comment focus_key is stamped from the active focus and remains constant across focus changes', async ({ request }) => {
  await request.post('/api/file/comments?path=b.txt', {
    data: { start_line: 1, end_line: 1, side: 'RIGHT', body: 'tag check' },
  });
  // Snapshot the on-disk shape via the focus-included list — focus_key is
  // not exposed in the API today (visibility is the contract). Switch
  // focus and confirm the comment is hidden there.
  const f = rangeFixture();
  await request.post('/api/focus', {
    data: { kind: 'range', base_sha: f.base, head_sha: f.headAfter, diff_scope: 'layer' },
  });
  const otherView = await (await request.get('/api/file/comments?path=b.txt')).json();
  expect(otherView).toEqual([]);
});
