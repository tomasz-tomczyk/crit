'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { selectionTooLarge } = require('../design-mode.size.js');

test('selection within budget passes', () => {
  assert.equal(selectionTooLarge({
    pathname: '/x', css_selector: 'body', tag_chain: ['BODY'],
    outer_html: 'x'.repeat(2048),
    viewport_width: 1, viewport_height: 1,
  }), false);
});

test('selection over 5MB rejected', () => {
  const big = 'x'.repeat(6 * 1024 * 1024);
  assert.equal(selectionTooLarge({
    pathname: '/x', css_selector: 'body', tag_chain: ['BODY'],
    outer_html: big,
    viewport_width: 1, viewport_height: 1,
  }), true);
});
