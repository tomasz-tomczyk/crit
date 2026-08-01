'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');

function loadShortcuts(initial) {
  const modulePath = require.resolve('../crit-shortcuts.js');
  delete require.cache[modulePath];
  const settings = Object.assign({}, initial || {});
  global.crit = {
    shared: {
      getSetting(key, fallback) { return Object.hasOwn(settings, key) ? settings[key] : fallback; },
      setSetting(key, value) { settings[key] = value; },
    },
  };
  const shortcuts = require(modulePath);
  return { shortcuts, settings };
}

test.afterEach(() => { delete global.crit; });

test('defaults resolve code-review actions from normalized key events', () => {
  const { shortcuts } = loadShortcuts();
  assert.equal(shortcuts.actionForEvent({ key: 'j', code: 'KeyJ' }, 'code-review'), 'next_block');
  assert.equal(shortcuts.actionForEvent({ key: 'F', code: 'KeyF', shiftKey: true }, 'code-review'), 'finish_review');
  assert.equal(shortcuts.actionForEvent({ key: 'f', shiftKey: true }, 'code-review'), 'finish_review');
  assert.equal(shortcuts.actionForEvent({ key: '!', code: 'Digit1', shiftKey: true }, 'code-review'), 'scope_all');
  assert.equal(shortcuts.actionForEvent({ key: '!', code: 'Digit1' }, 'code-review'), 'scope_all');
  assert.equal(shortcuts.actionForEvent({ key: 'p', code: 'KeyP' }, 'code-review'), '');
});

test('overrides persist, replace defaults, and can disable actions', () => {
  const { shortcuts, settings } = loadShortcuts();
  assert.equal(shortcuts.setBinding('next_block', 'ArrowDown'), true);
  assert.deepEqual(settings.shortcuts, { next_block: 'ArrowDown' });
  assert.equal(shortcuts.actionForEvent({ key: 'j', code: 'KeyJ' }, 'code-review'), '');
  assert.equal(shortcuts.actionForEvent({ key: 'ArrowDown', code: 'ArrowDown' }, 'code-review'), 'next_block');

  shortcuts.setBinding('next_block', '');
  assert.equal(shortcuts.getBinding('next_block'), '');
  assert.equal(shortcuts.actionForEvent({ key: 'ArrowDown', code: 'ArrowDown' }, 'code-review'), '');
});

test('saving the default removes an override and resetAll clears all overrides', () => {
  const { shortcuts, settings } = loadShortcuts({ shortcuts: { next_block: 'ArrowDown', comment: 'x' } });
  shortcuts.setBinding('next_block', 'j');
  assert.deepEqual(settings.shortcuts, { comment: 'x' });
  shortcuts.resetAll();
  assert.deepEqual(settings.shortcuts, {});
});

test('findConflict considers every mode shared by the edited action', () => {
  const { shortcuts } = loadShortcuts();
  assert.equal(shortcuts.findConflict('previous_block', 'j').id, 'next_block');
  assert.equal(shortcuts.findConflict('finish_review', 'p').id, 'toggle_pin_mode');
  assert.equal(shortcuts.findConflict('next_block', 'p'), null);
});

test('eventToBinding supports modifier chords and readable special keys', () => {
  const { shortcuts } = loadShortcuts();
  assert.equal(shortcuts.eventToBinding({ key: 'K', code: 'KeyK', ctrlKey: true, shiftKey: true }), 'Ctrl+Shift+K');
  assert.equal(shortcuts.eventToBinding({ key: '?', code: 'Slash', shiftKey: true }), 'Shift+/');
  assert.equal(shortcuts.eventToBinding({ key: ' ', code: 'Space' }), 'Space');
  assert.equal(shortcuts.eventToBinding({ key: 'Shift', code: 'ShiftLeft', shiftKey: true }), '');
});

test('malformed and unknown persisted overrides are ignored', () => {
  const { shortcuts } = loadShortcuts({ shortcuts: { next_block: 7, unknown: 'x' } });
  assert.equal(shortcuts.getBinding('next_block'), 'j');
});

test('reserved and conflicting persisted overrides are ignored', () => {
  const { shortcuts } = loadShortcuts({ shortcuts: { next_block: 'Ctrl+Enter', previous_block: 'j' } });
  assert.equal(shortcuts.getBinding('next_block'), 'j');
  assert.equal(shortcuts.getBinding('previous_block'), 'k');
});

test('setBinding rejects reserved and conflicting bindings', () => {
  const { shortcuts, settings } = loadShortcuts();
  assert.equal(shortcuts.setBinding('next_block', 'Ctrl+Enter'), false);
  assert.equal(shortcuts.setBinding('next_block', 'k'), false);
  assert.deepEqual(settings, {});
});

test('all modified question-mark chords are reserved', () => {
  const { shortcuts } = loadShortcuts();
  assert.equal(shortcuts.isReservedBinding('Shift+/'), true);
  assert.equal(shortcuts.isReservedBinding('Ctrl+Shift+/'), true);
  assert.equal(shortcuts.isReservedBinding('Meta+Alt+Shift+/'), true);
});
