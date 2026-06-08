'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { MESSAGE_TYPES, A2C, C2A, validateMessage } = require('../agent-protocol.js');

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

test('pin-resolution-result validates with status and optional rect', () => {
  assert.deepEqual(
    validateMessage({ type: 'pin-resolution-result', pin_id: 'p1', status: 'resolved', rect: { x: 0, y: 0, w: 10, h: 10 } }),
    { ok: true }
  );
  assert.deepEqual(
    validateMessage({ type: 'pin-resolution-result', pin_id: 'p1', status: 'drifted' }),
    { ok: true }
  );
  assert.deepEqual(
    validateMessage({ type: 'pin-resolution-result', pin_id: 'p1', status: 'drifted-recoverable', recovered_via: 'role+name+landmark', rect: { x: 1, y: 2, w: 3, h: 4 } }),
    { ok: true }
  );
});

test('pin-resolution-result rejects unknown status', () => {
  const r = validateMessage({ type: 'pin-resolution-result', pin_id: 'p1', status: 'lost' });
  assert.equal(r.ok, false);
  assert.equal(r.reason, 'pin-resolution-result.status');
});

test('pin-resolution-result rejects bad rect', () => {
  const r = validateMessage({ type: 'pin-resolution-result', pin_id: 'p1', status: 'resolved', rect: { x: 0, y: 0, w: 'wide', h: 10 } });
  assert.equal(r.ok, false);
  assert.equal(r.reason, 'pin-resolution-result.rect');
});

test('viewport-applied validates with width/height', () => {
  assert.deepEqual(validateMessage({ type: 'viewport-applied', width: 1280, height: 800 }), { ok: true });
  assert.equal(validateMessage({ type: 'viewport-applied', width: 'wide', height: 800 }).ok, false);
});

test('request-resolution validates with no payload', () => {
  assert.deepEqual(validateMessage({ type: 'request-resolution' }), { ok: true });
});

test('enter-reanchor-mode validates with pin_id', () => {
  assert.deepEqual(validateMessage({ type: 'enter-reanchor-mode', pin_id: 'p3' }), { ok: true });
  assert.equal(validateMessage({ type: 'enter-reanchor-mode' }).ok, false);
});

test('selection accepts optional reanchor_for', () => {
  const sel = {
    type: 'selection',
    dom_anchor: {
      pathname: '/x', css_selector: 'h1', tag_chain: ['H1'],
      outer_html: '<h1/>', screenshot: '',
      viewport_width: 1280, viewport_height: 800,
    },
    pointer: { x: 1, y: 2 },
    reanchor_for: 'p9',
  };
  assert.deepEqual(validateMessage(sel), { ok: true });

  sel.reanchor_for = 7;
  assert.equal(validateMessage(sel).ok, false);
});

test('set-viewport requires positive integer width and height', () => {
  assert.equal(validateMessage({ type: 'set-viewport', width: 1280, height: 800 }).ok, true);
  assert.equal(validateMessage({ type: 'set-viewport', width: 0, height: 800 }).ok, false);
  assert.equal(validateMessage({ type: 'set-viewport', width: 1280 }).ok, false);
  assert.equal(validateMessage({ type: 'set-viewport', width: '1280', height: 800 }).ok, false);
});

test('flash-marker requires pin_id string', () => {
  assert.equal(C2A.FLASH_MARKER, 'flash-marker');
  assert.deepEqual(validateMessage({ type: 'flash-marker', pin_id: 'p1' }), { ok: true });
  assert.equal(validateMessage({ type: 'flash-marker' }).ok, false);
  assert.equal(validateMessage({ type: 'flash-marker', pin_id: 9 }).ok, false);
});

test('cancel-reanchor requires no payload', () => {
  assert.equal(C2A.CANCEL_REANCHOR, 'cancel-reanchor');
  assert.deepEqual(validateMessage({ type: 'cancel-reanchor' }), { ok: true });
});

test('hovered-ancestor-level requires numeric level', () => {
  assert.equal(A2C.HOVERED_ANCESTOR_LEVEL, 'hovered-ancestor-level');
  assert.deepEqual(validateMessage({ type: 'hovered-ancestor-level', level: 0 }), { ok: true });
  assert.equal(validateMessage({ type: 'hovered-ancestor-level', level: '0' }).ok, false);
});

test('set-marker-tabindex requires numeric value', () => {
  assert.equal(C2A.SET_MARKER_TABINDEX, 'set-marker-tabindex');
  assert.deepEqual(validateMessage({ type: 'set-marker-tabindex', value: 0 }), { ok: true });
  assert.deepEqual(validateMessage({ type: 'set-marker-tabindex', value: -1 }), { ok: true });
  assert.equal(validateMessage({ type: 'set-marker-tabindex', value: '0' }).ok, false);
});

test('keep-highlight requires non-empty selector', () => {
  assert.equal(C2A.KEEP_HIGHLIGHT, 'keep-highlight');
  assert.deepEqual(
    validateMessage({ type: 'keep-highlight', selector: '#foo' }),
    { ok: true },
  );
  assert.equal(validateMessage({ type: 'keep-highlight' }).ok, false);
  assert.equal(validateMessage({ type: 'keep-highlight', selector: '' }).ok, false);
  assert.equal(validateMessage({ type: 'keep-highlight', selector: 42 }).ok, false);
});

test('clear-highlight validates without payload', () => {
  assert.equal(C2A.CLEAR_HIGHLIGHT, 'clear-highlight');
  assert.deepEqual(validateMessage({ type: 'clear-highlight' }), { ok: true });
});
