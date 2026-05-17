'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const dl = require('../live-mode-deeplink.js');

test('parseDeepLink extracts pin id from #pin=<id>', () => {
  assert.equal(dl.parseDeepLink('#pin=abc-123'), 'abc-123');
  assert.equal(dl.parseDeepLink('#pin=xyz_9'), 'xyz_9');
});

test('parseDeepLink returns null for unrelated/empty fragments', () => {
  assert.equal(dl.parseDeepLink(''), null);
  assert.equal(dl.parseDeepLink('#section'), null);
  assert.equal(dl.parseDeepLink('#pin='), null);
  assert.equal(dl.parseDeepLink('#pin=ab/cd'), null);
});

test('serializePinFragment formats #pin=<id>', () => {
  assert.equal(dl.serializePinFragment('p1'), '#pin=p1');
});

test('shouldClearOnRouteChange clears fragment when path differs from open pin', () => {
  const state = { openPin: { id: 'p1', dom_anchor: { pathname: '/dashboard' } } };
  assert.equal(dl.shouldClearOnRouteChange(state, '/settings'), true);
  assert.equal(dl.shouldClearOnRouteChange(state, '/dashboard'), false);
});

test('shouldClearOnRouteChange returns false when no pin is open', () => {
  assert.equal(dl.shouldClearOnRouteChange({ openPin: null }, '/x'), false);
});
