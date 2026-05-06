'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { reduceToggle } = require('../design-mode.toggle.js');

test('toggle flips navigate->pin', () => {
  assert.equal(reduceToggle('navigate'), 'pin');
});

test('toggle flips pin->navigate', () => {
  assert.equal(reduceToggle('pin'), 'navigate');
});
