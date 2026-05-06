'use strict';
const test = require('node:test');
const assert = require('node:assert');
const { renderDriftTrayHTML } = require('./design-mode-drift-tray.js');

test('renderDriftTrayHTML is empty when no drifted pins', () => {
  assert.equal(renderDriftTrayHTML([]), '');
});

test('renderDriftTrayHTML groups recoverable and lost', () => {
  const rows = [
    { id: 'a', body: 'recover me', pathname: '/x', status: 'drifted-recoverable' },
    { id: 'b', body: 'lost', pathname: '/y', status: 'drifted' },
  ];
  const html = renderDriftTrayHTML(rows);
  assert.match(html, /crit-design-drifted-tray/);
  assert.match(html, /Re-anchor here\?/);
  assert.match(html, /data-pin-id="a"/);
  assert.match(html, /data-pin-id="b"/);
  const aSection = html.split('data-pin-id="b"')[0];
  const bSection = html.split('data-pin-id="b"').slice(1).join('');
  assert.match(aSection, /Re-anchor here\?/);
  assert.doesNotMatch(bSection, /Re-anchor here\?/);
});

test('renderDriftTrayHTML escapes pin body', () => {
  const html = renderDriftTrayHTML([{ id: 'a', body: '<script>alert(1)</script>', pathname: '/x', status: 'drifted' }]);
  assert.doesNotMatch(html, /<script>/);
  assert.match(html, /&lt;script&gt;/);
});

test('renderDriftTrayHTML truncates BEFORE escaping (no mid-entity chops)', () => {
  const body = '&'.repeat(122);
  const html = renderDriftTrayHTML([{ id: 'a', body, pathname: '/x', status: 'drifted' }]);
  const m = html.match(/&amp;/g) || [];
  assert.equal(m.length, 120);
  assert.doesNotMatch(html, /&am(?!p;)/);
});
