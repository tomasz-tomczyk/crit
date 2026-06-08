'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { renderAncestorMenuHTML, clampMenuPosition } = require('../live-mode.menu.js');

test('renderAncestorMenuHTML escapes labels and stamps level data attribute', () => {
  const html = renderAncestorMenuHTML([
    { level: 0, label: 'span<' }, { level: 1, label: 'div.card' },
  ]);
  assert.ok(html.includes('span&lt;'));
  assert.ok(html.includes('data-level="0"'));
  assert.ok(html.includes('data-level="1"'));
});

test('clampMenuPosition keeps menu inside viewport (right/bottom)', () => {
  const p = clampMenuPosition({ x: 1000, y: 700, width: 200, height: 150, vw: 1024, vh: 768, pad: 8 });
  assert.equal(p.x, 1024 - 200 - 8);
  assert.equal(p.y, 768 - 150 - 8);
});

test('clampMenuPosition pins to top-left padding when negative', () => {
  const p = clampMenuPosition({ x: -50, y: -10, width: 200, height: 150, vw: 1024, vh: 768, pad: 8 });
  assert.equal(p.x, 8);
  assert.equal(p.y, 8);
});

test('clampMenuPosition leaves an in-bounds position untouched', () => {
  const p = clampMenuPosition({ x: 100, y: 100, width: 200, height: 150, vw: 1024, vh: 768, pad: 8 });
  assert.equal(p.x, 100);
  assert.equal(p.y, 100);
});
