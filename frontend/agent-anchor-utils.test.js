'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const u = require('./agent-anchor-utils.js');

test('implicitRole maps common tags', () => {
  assert.equal(u.implicitRole('BUTTON'), 'button');
  assert.equal(u.implicitRole('A'), 'link');
  assert.equal(u.implicitRole('NAV'), 'navigation');
  assert.equal(u.implicitRole('MAIN'), 'main');
  assert.equal(u.implicitRole('HEADER'), 'banner');
  assert.equal(u.implicitRole('FOOTER'), 'contentinfo');
  assert.equal(u.implicitRole('H1'), 'heading');
  assert.equal(u.implicitRole('UL'), 'list');
  assert.equal(u.implicitRole('LI'), 'listitem');
  assert.equal(u.implicitRole('IMG'), 'img');
});

test('implicitRole returns empty string for unknown tags', () => {
  assert.equal(u.implicitRole('DIV'), '');
  assert.equal(u.implicitRole('SPAN'), '');
});

test('implicitRole is case-insensitive', () => {
  assert.equal(u.implicitRole('button'), 'button');
});
