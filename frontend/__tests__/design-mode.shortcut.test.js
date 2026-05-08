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
  let finished = 0;
  const ctx = {
    focusInInput: true,
    getMode: () => mode,
    setMode: (m) => { mode = m; },
    finishReview: () => { finished++; },
  };
  handleShortcut({ key: 'p' }, ctx);
  handleShortcut({ key: 'Escape' }, ctx);
  handleShortcut({ key: 'F', shiftKey: true }, ctx);
  assert.equal(mode, 'navigate');
  assert.equal(finished, 0);
});

function shortcutCtx(overrides) {
  return Object.assign({
    focusInInput: false,
    getMode: () => 'navigate',
    setMode: () => {},
    finishReview: () => {},
  }, overrides || {});
}

test('Shift+F invokes finishReview', () => {
  let finished = 0;
  let prevented = 0;
  const ev = {
    key: 'F',
    shiftKey: true,
    preventDefault: () => { prevented++; },
  };
  handleShortcut(ev, shortcutCtx({ finishReview: () => { finished++; } }));
  assert.equal(finished, 1);
  assert.equal(prevented, 1);
});

test('Shift+f (lowercase) also invokes finishReview', () => {
  // Some keyboard layouts deliver lowercase `key` even with shift held.
  let finished = 0;
  handleShortcut(
    { key: 'f', shiftKey: true, preventDefault: () => {} },
    shortcutCtx({ finishReview: () => { finished++; } })
  );
  assert.equal(finished, 1);
});

test('plain f without shift does not finish', () => {
  let finished = 0;
  handleShortcut(
    { key: 'f', shiftKey: false, preventDefault: () => {} },
    shortcutCtx({ finishReview: () => { finished++; } })
  );
  assert.equal(finished, 0);
});

test('Cmd/Ctrl+Shift+F does not hijack browser find-in-page', () => {
  let finished = 0;
  handleShortcut(
    { key: 'F', shiftKey: true, metaKey: true, preventDefault: () => {} },
    shortcutCtx({ finishReview: () => { finished++; } })
  );
  handleShortcut(
    { key: 'F', shiftKey: true, ctrlKey: true, preventDefault: () => {} },
    shortcutCtx({ finishReview: () => { finished++; } })
  );
  assert.equal(finished, 0);
});

test('Shift+F suppressed while focusInInput', () => {
  let finished = 0;
  handleShortcut(
    { key: 'F', shiftKey: true, preventDefault: () => {} },
    shortcutCtx({ focusInInput: true, finishReview: () => { finished++; } })
  );
  assert.equal(finished, 0);
});
