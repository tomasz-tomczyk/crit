'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const panel = require('../design-mode.panel.js');

test('countUnresolved on empty input returns 0', () => {
  assert.equal(panel.countUnresolved(null), 0);
  assert.equal(panel.countUnresolved(undefined), 0);
  assert.equal(panel.countUnresolved([]), 0);
  assert.equal(panel.countUnresolved({}), 0);
});

test('countUnresolved on flat list of comments', () => {
  const list = [
    { id: 'a', resolved: false },
    { id: 'b', resolved: true },
    { id: 'c', resolved: false },
    { id: 'd' }, // missing -> falsy -> unresolved
  ];
  assert.equal(panel.countUnresolved(list), 3);
});

test('countUnresolved on a route map (pinsByRoute shape)', () => {
  const map = {
    '/home': [
      { id: '1', resolved: false },
      { id: '2', resolved: true },
    ],
    '/about': [
      { id: '3', resolved: false },
      { id: '4', resolved: false },
    ],
    '/empty': [],
  };
  assert.equal(panel.countUnresolved(map), 3);
});

test('countUnresolved tolerates null entries inside arrays', () => {
  assert.equal(panel.countUnresolved({ '/x': [null, { resolved: false }, undefined] }), 1);
});

test('computeResizeWidth: dragging left grows the panel', () => {
  // startW=400, startX=1000, currentX=900 -> dx=-100 -> w = 400 - (-100) = 500
  assert.equal(panel.computeResizeWidth(400, 1000, 900, 200), 500);
});

test('computeResizeWidth: dragging right shrinks the panel', () => {
  // startW=400, dx=+100 -> w = 300
  assert.equal(panel.computeResizeWidth(400, 1000, 1100, 200), 300);
});

test('computeResizeWidth: clamps to min', () => {
  // dx=+1000 -> w would be -600, clamp to 200
  assert.equal(panel.computeResizeWidth(400, 1000, 2000, 200), 200);
});

test('computeResizeWidth: default min is 200', () => {
  assert.equal(panel.computeResizeWidth(100, 0, 1000), 200);
});

test('computeResizeWidth: NO upper clamp (user gets what they ask for)', () => {
  // dx=-2000 -> w=2400, no clamp
  assert.equal(panel.computeResizeWidth(400, 1000, -1000, 200), 2400);
});

test('computeResizeWidth: rounds fractional values', () => {
  assert.equal(panel.computeResizeWidth(400.5, 1000, 999.7, 200), 401);
});
