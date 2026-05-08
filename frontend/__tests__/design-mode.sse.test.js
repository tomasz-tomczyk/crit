'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { create } = require('../design-mode.sse.js');

test('applyRoundStart resets per-round flags and announces', () => {
  const calls = { announced: [], scheduled: [], uiState: [] };
  const state = {
    currentRound: 1,
    resolutionCache: { '/x': 'fresh' },
    userActedThisRound: true,
    currentPathname: '/foo',
  };
  const ctl = create({
    state,
    pinsByRoute: () => ({
      '/x': [{ id: 'a', _roundResolved: true }, { id: 'b', _roundResolved: true }],
    }),
    scheduleResolutionForPath: (p) => calls.scheduled.push(p),
    announceLive: (m) => calls.announced.push(m),
    setUIState: (s) => calls.uiState.push(s),
  });
  ctl.applyRoundStart(3);
  assert.equal(state.currentRound, 3);
  assert.deepEqual(state.resolutionCache, {});
  assert.equal(state.userActedThisRound, false);
  assert.deepEqual(calls.uiState, ['reviewing']);
  assert.deepEqual(calls.scheduled, ['/foo']);
  assert.deepEqual(calls.announced, ['Round 3 started.']);
});

test('applyRoundStart clears _roundResolved on every existing pin', () => {
  const pins = [
    { id: 'a', _roundResolved: true },
    { id: 'b', _roundResolved: true },
  ];
  const ctl = create({
    state: { currentPathname: '/' },
    pinsByRoute: () => ({ '/': pins }),
    scheduleResolutionForPath: () => {},
    announceLive: () => {},
    setUIState: () => {},
  });
  ctl.applyRoundStart(2);
  assert.equal(pins[0]._roundResolved, false);
  assert.equal(pins[1]._roundResolved, false);
});

test('applyRoundStart falls back to currentRoute then "/" for path', () => {
  const seen = [];
  const ctl = create({
    state: { currentRoute: '/r' },
    pinsByRoute: () => ({}),
    scheduleResolutionForPath: (p) => seen.push(p),
    announceLive: () => {},
    setUIState: () => {},
  });
  ctl.applyRoundStart(1);
  assert.deepEqual(seen, ['/r']);
});

test('applyRoundStart re-fetches comments so replies posted mid-round appear', async () => {
  // Regression for Bug D: replies posted during round N (e.g. by the agent
  // via `crit comment --reply-to`) didn't appear when round N+1 started.
  // Round-start re-rendered the panel from stale state; comments-changed
  // SSE listener exists but events emitted during the round transition
  // were lost (panel re-renders before the reload lands). Round-start
  // itself must trigger a canonical re-fetch.
  let reloads = 0;
  const ctl = create({
    state: {},
    pinsByRoute: () => ({}),
    scheduleResolutionForPath: () => {},
    announceLive: () => {},
    setUIState: () => {},
    reloadComments: () => { reloads++; return Promise.resolve(); },
  });
  ctl.applyRoundStart(2);
  // Allow the queued microtask (Promise chain in applyCommentsChanged) to settle.
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(reloads, 1, 'round-start must re-fetch comments to capture mid-round replies');
});

test('applyCommentsChanged invokes reloadComments', async () => {
  let reloads = 0;
  const ctl = create({
    state: {},
    pinsByRoute: () => ({}),
    scheduleResolutionForPath: () => {},
    announceLive: () => {},
    setUIState: () => {},
    reloadComments: () => { reloads++; return Promise.resolve(); },
  });
  await ctl.applyCommentsChanged();
  assert.equal(reloads, 1);
});

test('applyCommentsChanged coalesces overlapping reloads', async () => {
  // A burst of comments-changed events (e.g. agent posting many replies in
  // quick succession) must not trigger N parallel reloads. The dedup guard
  // collapses overlapping calls into a single trailing reload.
  let inFlight = 0;
  let maxConcurrent = 0;
  let reloads = 0;
  let resolveFirst;
  const firstResolved = new Promise(function (r) { resolveFirst = r; });
  const ctl = create({
    state: {},
    pinsByRoute: () => ({}),
    scheduleResolutionForPath: () => {},
    announceLive: () => {},
    setUIState: () => {},
    reloadComments: () => {
      reloads++;
      inFlight++;
      maxConcurrent = Math.max(maxConcurrent, inFlight);
      const p = reloads === 1 ? firstResolved : Promise.resolve();
      return p.then(function () { inFlight--; });
    },
  });
  const a = ctl.applyCommentsChanged();
  ctl.applyCommentsChanged();
  ctl.applyCommentsChanged();
  resolveFirst();
  await a;
  // Wait one more microtask tick so the trailing reload can settle.
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(maxConcurrent, 1, 'reloads must not run in parallel');
  assert.equal(reloads, 2, 'three events collapse to two reloads (initial + one trailing)');
});

test('applyCommentsChanged swallows reloadComments rejections', async () => {
  // SSE handlers must not let an exception break the connection. A
  // rejected reload should be logged and the in-flight guard reset so the
  // next event can still trigger a reload.
  let reloads = 0;
  const ctl = create({
    state: {},
    pinsByRoute: () => ({}),
    scheduleResolutionForPath: () => {},
    announceLive: () => {},
    setUIState: () => {},
    reloadComments: () => {
      reloads++;
      return Promise.reject(new Error('boom'));
    },
  });
  await ctl.applyCommentsChanged();
  await ctl.applyCommentsChanged();
  assert.equal(reloads, 2);
});

test('install does nothing when EventSource is unavailable', () => {
  // create() with no global EventSource — install() must not throw.
  const ctl = create({
    state: {},
    pinsByRoute: () => ({}),
    scheduleResolutionForPath: () => {},
    announceLive: () => {},
    setUIState: () => {},
  });
  // EventSource not defined in Node — install catches the construction
  // failure and returns undefined.
  const res = ctl.install();
  assert.equal(res, undefined);
});
