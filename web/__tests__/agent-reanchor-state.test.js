'use strict';
const test = require('node:test');
const assert = require('node:assert');
const { ReanchorState } = require('../agent-reanchor-state.js');

test('arms with pin_id, single-shot, then disarms', () => {
  const s = new ReanchorState();
  assert.equal(s.armed, false);
  s.arm('p7');
  assert.equal(s.armed, true);
  assert.equal(s.consume(), 'p7');
  assert.equal(s.armed, false);
  assert.equal(s.consume(), null);
});

test('disarm clears armed state and is idempotent', () => {
  const s = new ReanchorState();
  s.arm('p1');
  assert.equal(s.armed, true);
  s.disarm();
  assert.equal(s.armed, false);
  assert.equal(s.pinId, null);
  s.disarm();
  assert.equal(s.armed, false);
});
