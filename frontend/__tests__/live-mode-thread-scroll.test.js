'use strict';
const test = require('node:test');
const assert = require('node:assert');
const { scrollThreadToPin } = require('../live-mode-thread-scroll.js');

test('locating thread row by data-comment-id calls scroller', () => {
  const fake = {
    querySelector: (sel) => sel === '[data-comment-id="abc"]' ? { scrollIntoView: () => {} } : null,
  };
  let called = 0;
  scrollThreadToPin(fake, 'abc', { scroller: () => called++ });
  assert.equal(called, 1);
});

test('returns false when no element found', () => {
  const fake = { querySelector: () => null };
  assert.equal(scrollThreadToPin(fake, 'missing'), false);
});

test('escapes quotes in selector', () => {
  const queries = [];
  const fake = { querySelector: (sel) => { queries.push(sel); return null; } };
  scrollThreadToPin(fake, 'a"b');
  assert.equal(queries[0], '[data-comment-id="a\\"b"]');
});
