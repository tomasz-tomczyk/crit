'use strict';
const test = require('node:test');
const assert = require('node:assert');
const { resolveAllAndEmit } = require('../agent-resolution.js');

test('resolveAllAndEmit posts one pin-resolution-result per pin', () => {
  const messages = [];
  const ctx = {
    pins: [{ id: 'p1', dom_anchor: { css_selector: '#a', tag_chain: ['H1'] } }],
    document: {
      querySelector: () => ({
        tagName: 'H1', parentElement: null,
        getBoundingClientRect: () => ({ left: 0, top: 0, width: 10, height: 5 }),
      }),
    },
    post: (m) => messages.push(m),
  };
  resolveAllAndEmit(ctx);
  assert.equal(messages.length, 1);
  assert.equal(messages[0].type, 'pin-resolution-result');
  assert.equal(messages[0].pin_id, 'p1');
  assert.equal(messages[0].status, 'resolved');
  assert.deepEqual(messages[0].rect, { x: 0, y: 0, w: 10, h: 5 });
});

test('resolveAllAndEmit reads only — never writes to el.style', () => {
  const reads = [];
  const ctx = {
    pins: [{ id: 'p1', dom_anchor: { css_selector: '#a', tag_chain: ['H1'] } }],
    document: {
      querySelector: () => ({
        tagName: 'H1', parentElement: null,
        getBoundingClientRect: () => { reads.push('rect'); return { left: 1, top: 2, width: 3, height: 4 }; },
        style: new Proxy({}, { set: () => { throw new Error('resolver wrote to el.style'); } }),
      }),
    },
    post: () => {},
    onResolved: () => {},
  };
  resolveAllAndEmit(ctx);
  assert.deepEqual(reads, ['rect']);
});

test('resolveAllAndEmit skips pins on other pathnames', () => {
  const messages = [];
  const ctx = {
    pathname: '/here',
    pins: [
      { id: 'a', dom_anchor: { pathname: '/here', css_selector: 'h1', tag_chain: ['H1'] } },
      { id: 'b', dom_anchor: { pathname: '/elsewhere', css_selector: 'h1', tag_chain: ['H1'] } },
    ],
    document: {
      querySelector: () => ({
        tagName: 'H1', parentElement: null,
        getBoundingClientRect: () => ({ left: 0, top: 0, width: 1, height: 1 }),
      }),
    },
    post: m => messages.push(m),
  };
  resolveAllAndEmit(ctx);
  assert.equal(messages.length, 1);
  assert.equal(messages[0].pin_id, 'a');
});

test('resolveAllAndEmit emits drifted with no rect when element missing', () => {
  const messages = [];
  const ctx = {
    pins: [{ id: 'p2', dom_anchor: { css_selector: '#missing', tag_chain: ['H1'] } }],
    document: { querySelector: () => null, querySelectorAll: () => [] },
    post: (m) => messages.push(m),
  };
  resolveAllAndEmit(ctx);
  assert.equal(messages[0].status, 'drifted');
  assert.equal(messages[0].rect, undefined);
});

test('resolveAllAndEmit calls onResolved with id, element, status', () => {
  const calls = [];
  const el = {
    tagName: 'H1', parentElement: null,
    getBoundingClientRect: () => ({ left: 0, top: 0, width: 1, height: 1 }),
  };
  const ctx = {
    pins: [{ id: 'p1', dom_anchor: { css_selector: '#a', tag_chain: ['H1'] } }],
    document: { querySelector: () => el },
    post: () => {},
    onResolved: (id, e, s) => calls.push([id, e, s]),
  };
  resolveAllAndEmit(ctx);
  assert.deepEqual(calls, [['p1', el, 'resolved']]);
});
