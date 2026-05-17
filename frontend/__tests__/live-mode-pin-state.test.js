'use strict';
const test = require('node:test');
const assert = require('node:assert');
const { PinState } = require('../live-mode-pin-state.js');

test('PinState records resolution status per pin', () => {
  const s = new PinState();
  s.setComments([
    { id: 'a', body: 'x', dom_anchor: { pathname: '/x' } },
    { id: 'b', body: 'y', dom_anchor: { pathname: '/x' } },
  ]);
  s.applyResolution({ pin_id: 'a', status: 'resolved' });
  s.applyResolution({ pin_id: 'b', status: 'drifted-recoverable', recovered_via: 'role+name+landmark' });
  assert.equal(s.statusOf('a'), 'resolved');
  assert.equal(s.statusOf('b'), 'drifted-recoverable');
  assert.deepEqual(s.driftedRows().map(r => r.id), ['b']);
});

test('PinState ignores unknown pin_id', () => {
  const s = new PinState();
  s.setComments([{ id: 'a', body: 'x', dom_anchor: { pathname: '/x' } }]);
  s.applyResolution({ pin_id: 'zzz', status: 'drifted' });
  assert.equal(s.statusOf('a'), undefined);
});
