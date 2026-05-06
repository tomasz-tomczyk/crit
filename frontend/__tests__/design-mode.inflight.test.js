'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { makeInFlightSet, makeInFlightFlag } = require('../design-mode.inflight.js');

// ---- per-id Set (resolve, reply, edit, drift PUT) ----

test('makeInFlightSet: has/add/delete track ids independently', () => {
  const g = makeInFlightSet();
  assert.equal(g.has('a'), false);
  g.add('a');
  assert.equal(g.has('a'), true);
  assert.equal(g.has('b'), false);
  assert.equal(g.size(), 1);
  g.delete('a');
  assert.equal(g.has('a'), false);
});

test('resolve double-click: second invocation of run() for same id is a no-op while first is in flight', async () => {
  const g = makeInFlightSet();
  let calls = 0;
  let release;
  const work = new Promise(r => { release = r; });
  // First click — kicks off the resolve PUT.
  const p1 = g.run('pin-1', async () => {
    calls++;
    await work;
    return 'first';
  });
  // Second click while first is still in flight — must be skipped.
  const p2 = g.run('pin-1', async () => {
    calls++;
    return 'second';
  });
  // Different id can run in parallel.
  const p3 = g.run('pin-2', async () => {
    calls++;
    return 'other';
  });
  // p2 returns immediately with undefined (the dedup signal).
  assert.equal(await p2, undefined);
  assert.equal(await p3, 'other');
  release();
  assert.equal(await p1, 'first');
  assert.equal(calls, 2); // only the first 'pin-1' run + the 'pin-2' run
  assert.equal(g.size(), 0);
});

test('makeInFlightSet.run clears id even when fn throws', async () => {
  const g = makeInFlightSet();
  await assert.rejects(g.run('x', async () => { throw new Error('boom'); }), /boom/);
  assert.equal(g.has('x'), false);
});

// ---- singleton flag (finish review, composer save) ----

test('finish double-submit: second run() while first in flight returns undefined and does not invoke fn', async () => {
  const g = makeInFlightFlag();
  let calls = 0;
  let release;
  const work = new Promise(r => { release = r; });
  const p1 = g.run(async () => {
    calls++;
    await work;
    return 'finished';
  });
  // Click again before the first finish completes.
  const p2 = g.run(async () => {
    calls++;
    return 'duplicate';
  });
  assert.equal(g.busy(), true);
  assert.equal(await p2, undefined);
  release();
  assert.equal(await p1, 'finished');
  assert.equal(calls, 1);
  assert.equal(g.busy(), false);
  // After completion, a fresh call works again.
  assert.equal(await g.run(async () => 'again'), 'again');
});

test('makeInFlightFlag.run clears flag even when fn throws', async () => {
  const g = makeInFlightFlag();
  await assert.rejects(g.run(async () => { throw new Error('nope'); }), /nope/);
  assert.equal(g.busy(), false);
});
