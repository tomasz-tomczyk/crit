'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { handleShortcut } = require('../design-mode.shortcut.js');

test('p toggles when not in input', () => {
  let mode = 'navigate';
  const ctx = {
    focusInInput: false,
    getMode: () => mode,
    setMode: (m) => { mode = m; },
  };
  handleShortcut({ key: 'p' }, ctx);
  assert.equal(mode, 'pin');
  handleShortcut({ key: 'p' }, ctx);
  assert.equal(mode, 'navigate');
});

test('Esc exits pin only', () => {
  let mode = 'pin';
  const ctx = {
    focusInInput: false,
    getMode: () => mode,
    setMode: (m) => { mode = m; },
  };
  handleShortcut({ key: 'Escape' }, ctx);
  assert.equal(mode, 'navigate');
  handleShortcut({ key: 'Escape' }, ctx);
  assert.equal(mode, 'navigate');
});

test('shortcuts suppressed while focusInInput', () => {
  let mode = 'navigate';
  const ctx = {
    focusInInput: true,
    getMode: () => mode,
    setMode: (m) => { mode = m; },
  };
  handleShortcut({ key: 'p' }, ctx);
  handleShortcut({ key: 'Escape' }, ctx);
  assert.equal(mode, 'navigate');
});
