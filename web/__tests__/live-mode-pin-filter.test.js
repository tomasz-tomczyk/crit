'use strict';
const test = require('node:test');
const assert = require('node:assert');
const { filterPinsForPath } = require('../live-mode-pin-filter.js');

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

test('filterPinsForPath matches when trailing slash differs (preview mode)', () => {
  const all = [
    { id: 'a', dom_anchor: { pathname: '/preview-content/' } },
    { id: 'b', dom_anchor: { pathname: '/preview-content' } },
    { id: 'c', dom_anchor: { pathname: '/other' } },
  ];
  const out = filterPinsForPath(all, '/preview-content');
  assert.deepEqual(out.map(p => p.id), ['a', 'b']);
  const out2 = filterPinsForPath(all, '/preview-content/');
  assert.deepEqual(out2.map(p => p.id), ['a', 'b']);
});

test('filterPinsForPath drops resolved pins so markers do not render for them', () => {
  // Resolved comments shouldn't paint a pin marker on the proxied page.
  // Hiding happens by omission from the set-pins payload — the agent
  // never gets told a marker exists, so nothing is rendered.
  const all = [
    { id: 'open', resolved: false, dom_anchor: { pathname: '/foo' } },
    { id: 'done', resolved: true, dom_anchor: { pathname: '/foo' } },
    { id: 'open2', dom_anchor: { pathname: '/foo' } }, // no resolved field == open
  ];
  const out = filterPinsForPath(all, '/foo');
  assert.deepEqual(out.map(p => p.id), ['open', 'open2']);
});
