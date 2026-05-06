'use strict';
const test = require('node:test');
const assert = require('node:assert');
const { filterPinsForPath } = require('./design-mode-pin-filter.js');

test('filterPinsForPath keeps only same-pathname pins', () => {
  const all = [
    { id: 'a', dom_anchor: { pathname: '/foo' } },
    { id: 'b', dom_anchor: { pathname: '/bar' } },
    { id: 'c', dom_anchor: { pathname: '/foo' } },
    { id: 'd' },
    { id: 'e', dom_anchor: null },
  ];
  const out = filterPinsForPath(all, '/foo');
  assert.deepEqual(out.map(p => p.id), ['a', 'c']);
});

test('filterPinsForPath returns [] on empty pathname', () => {
  assert.deepEqual(filterPinsForPath([{ id: 'a', dom_anchor: { pathname: '/foo' } }], ''), []);
});

test('filterPinsForPath returns [] when pins is not array', () => {
  assert.deepEqual(filterPinsForPath(null, '/x'), []);
});
