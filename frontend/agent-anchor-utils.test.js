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

// Minimal node-side stub: a "node" is { tagName, id, parent, children, attrs, text }.
function n(tagName, opts = {}) {
  const node = {
    tagName: tagName.toUpperCase(),
    id: opts.id || '',
    parentNode: null,
    children: [],
    attrs: opts.attrs || {},
    textContent: opts.text || '',
    ariaLabel: opts.ariaLabel || '',
    className: opts.className || '',
    outerHTML: opts.outerHTML || '',
    isContentEditable: !!opts.isContentEditable,
    getAttribute(name) { return this.attrs[name] || null; },
  };
  for (const c of opts.children || []) {
    c.parentNode = node;
    node.children.push(c);
  }
  return node;
}

test('findAnchorRoot returns nearest id ancestor', () => {
  const h1 = n('h1');
  const sec = n('section', { children: [h1] });
  const main = n('main', { id: 'main', children: [sec] });
  const body = n('body', { children: [main] });
  body.parentNode = null;
  assert.equal(u.findAnchorRoot(h1), main);
});

test('findAnchorRoot falls back to BODY when no ancestor has id', () => {
  const h1 = n('h1');
  const body = n('body', { children: [h1] });
  assert.equal(u.findAnchorRoot(h1), body);
});

test('findAnchorRoot returns the element itself if it has an id', () => {
  const el = n('div', { id: 'top' });
  assert.equal(u.findAnchorRoot(el), el);
});
