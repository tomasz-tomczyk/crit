'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { makeMessageDispatcher } = require('../live-mode.dispatch.js');

test('dispatcher routes selection to onSelection', () => {
  const events = [];
  const d = makeMessageDispatcher({
    onAgentReady: () => events.push('ready'),
    onSelection: (a) => events.push(['sel', a.css_selector]),
    onAgentError: (e) => events.push(['err', e.kind]),
    onRequestAncestorMenu: () => events.push('menu'),
    onFocusState: (s) => events.push(['focus', s]),
  });
  d({ type: 'agent-ready' });
  d({ type: 'selection', dom_anchor: {
    pathname: '/x', css_selector: 'body', tag_chain: ['BODY'],
    outer_html: '<body></body>', screenshot: '', viewport_width: 1, viewport_height: 1,
  }});
  d({ type: 'agent-error', kind: 'shadow-dom', message: 'no' });
  d({ type: 'request-ancestor-menu', options: [{ level: 0, label: 'span' }], pointer: { x: 1, y: 2 } });
  d({ type: 'focus-state', in_input: true });
  assert.deepEqual(events, ['ready', ['sel', 'body'], ['err', 'shadow-dom'], 'menu', ['focus', true]]);
});

test('dispatcher ignores invalid messages silently', () => {
  let count = 0;
  const d = makeMessageDispatcher({ onAgentReady: () => count++ });
  d({ type: 'agent-ready' });
  d(null);
  d({ type: 'unknown' });
  d({ type: 'selection' /* no dom_anchor */ });
  assert.equal(count, 1);
});
