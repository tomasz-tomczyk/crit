'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { createMenuController } = require('../design-mode.menu-controller.js');

function fakeKey(key) { return { key, preventDefault() { this.prevented = true; } }; }

test('arrow down moves index forward and wraps', () => {
  const ctl = createMenuController({
    options: [{ level: 0 }, { level: 1 }, { level: 2 }],
    onCommit: () => {}, onCancel: () => {}, onHighlight: () => {},
  });
  assert.equal(ctl.index, 0);
  ctl.keydown(fakeKey('ArrowDown')); assert.equal(ctl.index, 1);
  ctl.keydown(fakeKey('ArrowDown')); assert.equal(ctl.index, 2);
  ctl.keydown(fakeKey('ArrowDown')); assert.equal(ctl.index, 0);
});

test('arrow up wraps to last', () => {
  const ctl = createMenuController({ options: [{ level: 0 }, { level: 1 }] });
  ctl.keydown(fakeKey('ArrowUp')); assert.equal(ctl.index, 1);
});

test('Enter commits the current option', () => {
  let committed = null;
  const ctl = createMenuController({
    options: [{ level: 0 }, { level: 1 }],
    onCommit: o => { committed = o; },
  });
  ctl.keydown(fakeKey('ArrowDown'));
  ctl.keydown(fakeKey('Enter'));
  assert.deepEqual(committed, { level: 1 });
});

test('Escape cancels', () => {
  let cancelled = false;
  const ctl = createMenuController({
    options: [{ level: 0 }],
    onCancel: () => { cancelled = true; },
  });
  ctl.keydown(fakeKey('Escape'));
  assert.equal(cancelled, true);
});

test('setHoveredLevel updates index when level matches an option', () => {
  let highlighted = null;
  const ctl = createMenuController({
    options: [{ level: 0 }, { level: 1 }, { level: 2 }],
    onHighlight: i => { highlighted = i; },
  });
  ctl.setHoveredLevel(2);
  assert.equal(ctl.index, 2);
  assert.equal(highlighted, 2);
});

test('setHoveredLevel ignores unknown levels', () => {
  const ctl = createMenuController({ options: [{ level: 0 }] });
  ctl.setHoveredLevel(99);
  assert.equal(ctl.index, 0);
});

test('Home and End jump to extremes', () => {
  const ctl = createMenuController({ options: [{ level: 0 }, { level: 1 }, { level: 2 }] });
  ctl.keydown(fakeKey('End'));
  assert.equal(ctl.index, 2);
  ctl.keydown(fakeKey('Home'));
  assert.equal(ctl.index, 0);
});
