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
