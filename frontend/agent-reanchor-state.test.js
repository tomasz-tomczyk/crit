'use strict';
const test = require('node:test');
const assert = require('node:assert');
const { ReanchorState } = require('./agent-reanchor-state.js');

test('arms with pin_id, single-shot, then disarms', () => {
  const s = new ReanchorState();
  assert.equal(s.armed, false);
  s.arm('p7');
  assert.equal(s.armed, true);
  assert.equal(s.consume(), 'p7');
  assert.equal(s.armed, false);
  assert.equal(s.consume(), null);
});
