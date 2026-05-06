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

test('cssSelectorFor builds :nth-of-type path from anchor root', () => {
  const h1 = n('h1');
  const h2a = n('h2');
  const h2b = n('h2');
  const sec = n('section', { children: [h1, h2a, h2b] });
  const main = n('main', { id: 'main', children: [sec] });
  const root = u.findAnchorRoot(h2b);
  assert.equal(root, main);
  assert.equal(
    u.cssSelectorFor(h2b, root),
    '#main > section:nth-of-type(1) > h2:nth-of-type(2)',
  );
});

test('cssSelectorFor uses body fallback when no id', () => {
  const span = n('span');
  const div = n('div', { children: [span] });
  const body = n('body', { children: [div] });
  const root = u.findAnchorRoot(span);
  assert.equal(root, body);
  assert.equal(u.cssSelectorFor(span, root), 'body > div:nth-of-type(1) > span:nth-of-type(1)');
});

test('tagChainFor returns uppercase tag names from root to element', () => {
  const h1 = n('h1');
  const sec = n('section', { children: [h1] });
  const main = n('main', { id: 'main', children: [sec] });
  assert.deepEqual(u.tagChainFor(h1, main), ['MAIN', 'SECTION', 'H1']);
});

test('accessibleNameFor prefers ariaLabel', () => {
  const el = n('button', { ariaLabel: 'Save changes', text: 'Save' });
  assert.equal(u.accessibleNameFor(el), 'Save changes');
});

test('accessibleNameFor falls back to trimmed textContent capped at 80', () => {
  const el = n('button', { text: '  Long button label  ' });
  assert.equal(u.accessibleNameFor(el), 'Long button label');
});

test('accessibleNameFor truncates to 80 chars', () => {
  const el = n('p', { text: 'x'.repeat(200) });
  assert.equal(u.accessibleNameFor(el).length, 80);
});

test('accessibleNameFor handles empty input', () => {
  const el = n('div');
  assert.equal(u.accessibleNameFor(el), '');
});

test('roleFor uses explicit role attribute first', () => {
  const el = n('div', { attrs: { role: 'tablist' } });
  assert.equal(u.roleFor(el), 'tablist');
});

test('roleFor falls back to implicit role', () => {
  const el = n('button');
  assert.equal(u.roleFor(el), 'button');
});

test('roleFor returns empty for unknown div', () => {
  const el = n('div');
  assert.equal(u.roleFor(el), '');
});

test('landmarkFor finds nearest landmark ancestor', () => {
  const span = n('span');
  const main = n('main', { ariaLabel: 'Dashboard', children: [span] });
  assert.equal(u.landmarkFor(span), 'Dashboard');
});

test('landmarkFor falls back to tagName.toLowerCase() when no aria-label', () => {
  const span = n('span');
  const nav = n('nav', { children: [span] });
  assert.equal(u.landmarkFor(span), 'nav');
});

test('landmarkFor returns empty when no landmark ancestor', () => {
  const span = n('span');
  const div = n('div', { children: [span] });
  assert.equal(u.landmarkFor(span), '');
});

test('truncateOuterHTML caps at given length', () => {
  assert.equal(u.truncateOuterHTML('<p>hi</p>', 2048).length, 9);
  assert.equal(u.truncateOuterHTML('x'.repeat(3000), 2048).length, 2048);
});

test('walkAncestors returns array from element to anchor root inclusive', () => {
  const span = n('span');
  const div = n('div', { children: [span] });
  const sec = n('section', { children: [div] });
  const main = n('main', { id: 'main', children: [sec] });
  const root = u.findAnchorRoot(span);
  const chain = u.walkAncestors(span, root);
  assert.equal(chain.length, 4);
  assert.equal(chain[0], span);
  assert.equal(chain[3], main);
});
