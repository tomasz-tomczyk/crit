'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { create } = require('../live-mode.comments-loader.js');

// Build a tiny shared.fetchJSON-equivalent that pulls from a route table.
// Each entry can be a function (called with the URL) so a single test can
// exercise multiple sequential responses for the same URL.
function makeShared(routes) {
  const log = [];
  return {
    log,
    fetchJSON: async function (url) {
      log.push(url);
      const handler = routes[url];
      if (handler == null) {
        const err = new Error('no route for ' + url);
        err.status = 404;
        throw err;
      }
      return typeof handler === 'function' ? handler(url) : handler;
    },
  };
}

test('loadAllComments refetches /api/session so freshly-added files are picked up', async () => {
  // Reproduces the user-visible bug: live daemon boots with no FileEntry
  // (files: []), so state.session captured at boot has files: []. The user
  // creates a pin, the agent posts a reply via crit comment --reply-to, the
  // user advances to round 2. The reply is in the daemon's data — visible
  // at GET /api/file/comments?path=/ — but state.comments stayed empty
  // because the old loadAllComments early-returned on
  // `if (!files.length) return;` without ever consulting the now-populated
  // server-side files list.
  //
  // The loader must refresh state.session before reading files so it sees
  // the daemon's current truth.
  const state = {
    // Simulates "session captured at boot, before any pin existed".
    session: { review_round: 1, files: [] },
    currentRound: 2,
  };
  const replyComment = {
    id: 'c_433d21',
    body: 'pin body',
    author: 'me',
    review_round: 1,
    replies: [{
      id: 'rp_4df4c7',
      body: 'Acknowledged. No changes.',
      author: 'Claude Code',
    }],
    dom_anchor: { pathname: '/' },
  };
  const shared = makeShared({
    // Daemon side: by the time the reload fires the FileEntry exists.
    '/api/session': { review_round: 2, files: [{ path: '/' }] },
    '/api/file/comments?path=%2F': [replyComment],
  });
  const loader = create({ state, shared });
  await loader.loadAllComments();

  assert.equal(state.comments && state.comments.length, 1,
    'state.comments must be populated from the freshly-fetched file list');
  const got = state.comments[0];
  assert.equal(got.id, 'c_433d21');
  assert.ok(Array.isArray(got.replies) && got.replies.length === 1,
    'reply must round-trip into state.comments');
  assert.equal(got.replies[0].body, 'Acknowledged. No changes.');
  // state.session should have been refreshed so other code sees the new
  // round number / files list.
  assert.deepEqual(state.session.files, [{ path: '/' }]);
  assert.equal(state.session.review_round, 2);
});

test('OLD inline logic (without session refresh) misses the reply — pins down the regression', async () => {
  // Mirrors the exact code that used to live inline in live-mode.js
  // (pre-fix). It uses state.session.files captured at boot. With files
  // empty, the function bails early — exactly the user-visible bug. This
  // test asserts the OLD shape would have failed; it stays in the file as
  // a guardrail so future refactors that re-inline path discovery don't
  // silently regress to the broken behaviour.
  const state = { session: { files: [] }, currentRound: 2 };
  const shared = makeShared({
    '/api/file/comments?path=%2F': [{
      id: 'c_433d21', body: 'pin body', replies: [{ id: 'rp_4df4c7', body: 'reply' }],
      dom_anchor: { pathname: '/' },
    }],
  });
  // Inline copy of the broken pre-fix loader (no session refetch).
  async function brokenLoader() {
    const s = state.session || {};
    const files = (s.files || []).map((f) => f.path);
    if (!files.length) return;
    const results = await Promise.all(files.map((p) =>
      shared.fetchJSON('/api/file/comments?path=' + encodeURIComponent(p))
        .then((list) => Array.isArray(list) ? list : [])));
    state.comments = results.reduce((a, b) => a.concat(b), []);
  }
  await brokenLoader();
  // Demonstrates the bug: nothing got loaded, even though the reply was on
  // disk. comments-changed SSE handlers were calling this no-op repeatedly.
  assert.equal(state.comments, undefined,
    'OLD logic short-circuits with no fetch — proves the bug existed');
  assert.equal(shared.log.length, 0,
    'OLD logic never fetched /api/file/comments because files was empty');
});

test('loadAllComments tolerates a transient /api/session failure (falls back to stale)', async () => {
  // 503 during a round transition is a real possibility (daemon's withReady
  // gate). The loader should not wipe state.comments; it should fall back
  // to whatever stale session is in memory rather than render an empty
  // panel during the brief outage.
  const state = {
    session: { review_round: 1, files: [{ path: '/' }] },
    comments: [{ id: 'old', body: 'stale' }],
  };
  const shared = makeShared({
    '/api/session': () => { const e = new Error('503'); e.status = 503; throw e; },
    '/api/file/comments?path=%2F': [{
      id: 'c1', body: 'fresh', replies: [],
      dom_anchor: { pathname: '/' },
    }],
  });
  await create({ state, shared }).loadAllComments();
  assert.equal(state.comments.length, 1);
  assert.equal(state.comments[0].id, 'c1', 'stale session still drives a fetch');
});

test('loadAllComments early-returns if even the refreshed session has no files', async () => {
  // A brand-new live daemon with zero pins: refresh succeeds, files is
  // still []. Don't fan out a phantom fetch and don't clobber any
  // optimistic state that other code paths put on state.comments.
  const state = { session: { files: [] }, comments: [{ id: 'optimistic' }] };
  const shared = makeShared({
    '/api/session': { files: [], review_round: 1 },
  });
  await create({ state, shared }).loadAllComments();
  assert.deepEqual(state.comments, [{ id: 'optimistic' }],
    'no-op when there are no files — preserves optimistic insertions');
  assert.equal(shared.log.length, 1, 'only /api/session is hit, no comment fetches');
});

test('loadAllComments stamps _createdInRound on every comment', async () => {
  // Round-faithful drift detection in handlePinResolutionResult relies on
  // _createdInRound being set on every reload — both the boot reload and
  // every SSE refresh. Pulled out of the original loadAllComments so this
  // contract is explicit.
  const state = { session: { files: [{ path: '/' }] }, currentRound: 3 };
  const shared = makeShared({
    '/api/session': { files: [{ path: '/' }], review_round: 3 },
    '/api/file/comments?path=%2F': [
      { id: 'a', body: 'one', review_round: 2, dom_anchor: { pathname: '/' } },
      { id: 'b', body: 'two', dom_anchor: { pathname: '/' } },
    ],
  });
  await create({ state, shared }).loadAllComments();
  assert.equal(state.comments[0]._createdInRound, 2, 'persisted review_round wins');
  assert.equal(state.comments[1]._createdInRound, 3, 'falls back to currentRound');
});
