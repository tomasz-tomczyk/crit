'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { makeAgentSender } = require('../live-mode.queue.js');

test('commands queue until ready, then drain in order', () => {
  const sent = [];
  const s = makeAgentSender({ post: (m) => sent.push(m) });
  s.send({ type: 'set-mode', value: 'pin' });
  s.send({ type: 'set-mode', value: 'navigate' });
  assert.deepEqual(sent, []);
  s.markReady();
  assert.deepEqual(sent.map(m => m.value), ['pin', 'navigate']);
});

test('post-ready commands send immediately', () => {
  const sent = [];
  const s = makeAgentSender({ post: (m) => sent.push(m) });
  s.markReady();
  s.send({ type: 'set-mode', value: 'pin' });
  assert.equal(sent.length, 1);
});
