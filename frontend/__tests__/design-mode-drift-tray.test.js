'use strict';
const test = require('node:test');
const assert = require('node:assert');
const { renderDriftTrayHTML } = require('../design-mode-drift-tray.js');

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

test('renderDriftTrayHTML omits section heading regardless of round', () => {
  // Regression: the per-round partition with bright "Drifted on round N"
  // headings was removed — drifted pins are already enumerated in the main
  // panel with per-row badges, and the tray exists only for the Re-anchor
  // affordance. A heading here would duplicate that surface area.
  const rows = [
    { id: 'a', body: 'x', pathname: '/', status: 'drifted', drifted_on_round: 2 },
    { id: 'b', body: 'y', pathname: '/', status: 'drifted', drifted_on_round: 1 },
  ];
  const html = renderDriftTrayHTML(rows, 2);
  assert.doesNotMatch(html, /<h3[^>]*>/);
  assert.doesNotMatch(html, /Drifted on round/);
  assert.doesNotMatch(html, /Drifted earlier/);
  assert.doesNotMatch(html, /crit-design-drifted-tray-section/);
  assert.match(html, /data-pin-id="a"/);
  assert.match(html, /data-pin-id="b"/);
});

test('renderDriftTrayHTML truncates BEFORE escaping (no mid-entity chops)', () => {
  const body = '&'.repeat(122);
  const html = renderDriftTrayHTML([{ id: 'a', body, pathname: '/x', status: 'drifted' }]);
  const m = html.match(/&amp;/g) || [];
  assert.equal(m.length, 120);
  assert.doesNotMatch(html, /&am(?!p;)/);
});
