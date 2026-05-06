'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { MESSAGE_TYPES, A2C, C2A, validateMessage } = require('./agent-protocol.js');

test('A2C and C2A constants are disjoint', () => {
  const a2c = new Set(Object.values(A2C));
  for (const v of Object.values(C2A)) assert.ok(!a2c.has(v), `dup ${v}`);
});

test('agent-ready validates with no payload', () => {
  assert.deepEqual(validateMessage({ type: 'agent-ready' }), { ok: true });
});

test('agent-error requires kind and message', () => {
  assert.equal(validateMessage({ type: 'agent-error' }).ok, false);
  assert.equal(
    validateMessage({ type: 'agent-error', kind: 'shadow-dom', message: 'no' }).ok,
    true,
  );
});

test('selection requires DOMAnchor with all required fields', () => {
  const good = {
    type: 'selection',
    dom_anchor: {
      pathname: '/x',
      css_selector: 'body > h1',
      tag_chain: ['BODY', 'H1'],
      outer_html: '<h1></h1>',
      screenshot: '',
      viewport_width: 1280,
      viewport_height: 800,
    },
  };
  assert.equal(validateMessage(good).ok, true);
  const missing = JSON.parse(JSON.stringify(good));
  delete missing.dom_anchor.tag_chain;
  assert.equal(validateMessage(missing).ok, false);
});

test('set-mode rejects unknown values', () => {
  assert.equal(validateMessage({ type: 'set-mode', value: 'pin' }).ok, true);
  assert.equal(validateMessage({ type: 'set-mode', value: 'flying' }).ok, false);
});

test('focus-state requires boolean in_input', () => {
  assert.equal(validateMessage({ type: 'focus-state', in_input: true }).ok, true);
  assert.equal(validateMessage({ type: 'focus-state', in_input: 'yes' }).ok, false);
});

test('request-ancestor-menu requires options array and pointer', () => {
  assert.equal(
    validateMessage({
      type: 'request-ancestor-menu',
      options: [{ level: 0, label: 'span' }],
      pointer: { x: 10, y: 20 },
    }).ok,
    true,
  );
  assert.equal(validateMessage({ type: 'request-ancestor-menu', options: [] }).ok, false);
});

test('commit-ancestor-selection requires non-negative integer level', () => {
  assert.equal(validateMessage({ type: 'commit-ancestor-selection', level: 0 }).ok, true);
  assert.equal(validateMessage({ type: 'commit-ancestor-selection', level: -1 }).ok, false);
  assert.equal(validateMessage({ type: 'commit-ancestor-selection', level: 1.5 }).ok, false);
});

test('unknown type rejected', () => {
  assert.equal(validateMessage({ type: 'nope' }).ok, false);
});

test('MESSAGE_TYPES is frozen', () => {
  assert.throws(() => { MESSAGE_TYPES.AGENT_READY = 'x'; });
});
