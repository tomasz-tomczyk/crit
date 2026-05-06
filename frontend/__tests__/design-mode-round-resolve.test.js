'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const r = require('../design-mode-round-resolve.js');

test('pinsToResolveAtRoundStart returns only pins on currentPath', () => {
  const pins = [
    { id: 'a', dom_anchor: { pathname: '/' } },
    { id: 'b', dom_anchor: { pathname: '/x' } },
    { id: 'c', dom_anchor: { pathname: '/' } },
  ];
  assert.deepEqual(r.pinsToResolveAtRoundStart(pins, '/'), ['a', 'c']);
});

test('pinsToResolveAtRoundStart skips already-resolved this round', () => {
  const pins = [
    { id: 'a', dom_anchor: { pathname: '/' }, _roundResolved: true },
    { id: 'b', dom_anchor: { pathname: '/' } },
  ];
  assert.deepEqual(r.pinsToResolveAtRoundStart(pins, '/'), ['b']);
});

test('classifyPinForRound: resolved → not drifted', () => {
  const c = r.classifyPinForRound({ drifted: false }, { status: 'resolved' }, 3);
  assert.deepEqual(c, { driftedOnRound: null, drifted: false });
});

test('classifyPinForRound: newly drifted this round stamps driftedOnRound', () => {
  const c = r.classifyPinForRound({ drifted: false }, { status: 'drifted' }, 3);
  assert.deepEqual(c, { driftedOnRound: 3, drifted: true });
});

test('classifyPinForRound: drifted-recoverable does not stamp round', () => {
  const c = r.classifyPinForRound(
    { drifted: false }, { status: 'drifted-recoverable' }, 3,
  );
  assert.deepEqual(c, { driftedOnRound: null, drifted: false });
});

test('classifyPinForRound: previously drifted stays drifted, no round overwrite', () => {
  const c = r.classifyPinForRound(
    { drifted: true, drifted_on_round: 1 }, { status: 'drifted' }, 3,
  );
  assert.deepEqual(c, { driftedOnRound: null, drifted: true });
});
