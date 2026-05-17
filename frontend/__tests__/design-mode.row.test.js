'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');

// chipLabel now delegates to crit-comment-card-helpers.js at runtime.
// Set up the globals before requiring design-mode.row.js.
const helpers = require('../crit-comment-card-helpers.js');
globalThis.window = globalThis.window || globalThis;
globalThis.window.crit = globalThis.window.crit || {};
globalThis.window.crit.commentCardHelpers = helpers;

// Clear require cache so the module picks up the globals.
delete require.cache[require.resolve('../design-mode.row.js')];
const { chipLabel, renderDesignPinRow } = require('../design-mode.row.js');

test('chipLabel handles missing fields', () => {
  assert.equal(chipLabel({ tag_chain: ['DIV'] }), '<div>');
  assert.equal(chipLabel({ accessible_name: 'X', tag_chain: ['DIV'] }), 'X');
});

test('chipLabel prefers accessible_name over outer_html', () => {
  assert.equal(
    chipLabel({ accessible_name: 'Submit', outer_html: '<b>noise</b>', tag_chain: ['BUTTON'] }),
    'Submit',
  );
});

test('chipLabel falls back to outer_html text when no accessible_name', () => {
  assert.equal(
    chipLabel({ accessible_name: '', outer_html: '<b>hello</b>', tag_chain: ['B'] }),
    'hello',
  );
});

test('chipLabel falls back to tag name as a last resort', () => {
  assert.equal(chipLabel({ tag_chain: ['SECTION'] }), '<section>');
  assert.equal(chipLabel({}), 'pin');
});

test('chipLabel truncates long values to 60 chars + ellipsis', () => {
  const long = 'x'.repeat(80);
  const out = chipLabel({ accessible_name: long, tag_chain: ['DIV'] });
  assert.equal(out.length, 61); // 60 + ellipsis
  assert.ok(out.endsWith('…'));
});

// Minimal DOM stub for renderDesignPinRow tests that need to exercise the
// real card-mounting path (with a stubbed shared buildCommentCard).
function makeStubEl(tag) {
  const el = {
    tagName: (tag || 'DIV').toUpperCase(),
    children: [],
    dataset: {},
    style: {},
    _attrs: {},
    _cls: new Set(),
    appendChild(c) { this.children.push(c); c.parentNode = this; return c; },
    insertBefore(n, ref) { this.children.unshift(n); return n; },
    setAttribute(k, v) { this._attrs[k] = v; },
    getAttribute(k) { return this._attrs[k]; },
    addEventListener() {},
    set className(v) { this._cn = v; if (v) v.split(/\s+/).forEach((c) => this._cls.add(c)); },
    get className() { return this._cn || ''; },
    set innerHTML(v) { this._html = v; },
    get innerHTML() { return this._html || ''; },
    set textContent(v) { this._text = v; },
    get textContent() { return this._text || ''; },
    querySelector() { return null; },
  };
  return el;
}

test('renderDesignPinRow includes a Delete button on top-level comment cards', () => {
  // Regression for Bug C: design-mode comment cards rendered Resolve, Edit,
  // Reply but no Delete affordance. Reply rows already had one; the parent
  // card did not. The button must be in parts.actions and carry the
  // commentId/pathname so the existing dispatch (matching code-review's
  // .delete-btn click flow) can route the click.
  const origDocument = global.document;
  const origWindow = global.window;
  global.document = {
    createElement: (tag) => makeStubEl(tag),
  };
  // Stub shared card so renderDesignPinRow takes the real (non-fallback) path.
  const fakeActions = makeStubEl('div');
  const fakeCard = makeStubEl('div');
  const fakeWrapper = makeStubEl('div');
  global.window = {
    crit: {
      commentCard: {
        buildCommentCard: () => ({ wrapper: fakeWrapper, card: fakeCard, actions: fakeActions }),
      },
    },
  };
  try {
    renderDesignPinRow(
      { id: 'c-del-1', body: 'wrong colour', dom_anchor: { pathname: '/p' } },
      { iconDelete: '<svg/>' },
    );
    const deleteBtns = fakeActions.children.filter(
      (b) => b._cls && b._cls.has('crit-design-comment-delete'),
    );
    assert.equal(deleteBtns.length, 1, 'expected one Delete button in actions');
    const del = deleteBtns[0];
    assert.equal(del.dataset.commentId, 'c-del-1');
    assert.equal(del.dataset.pathname, '/p');
    assert.equal(del._attrs['aria-label'], 'Delete comment');
    assert.equal(del._cls.has('delete-btn'), true, 'should reuse shared .delete-btn class');
  } finally {
    if (origDocument === undefined) delete global.document; else global.document = origDocument;
    if (origWindow === undefined) delete global.window; else global.window = origWindow;
  }
});

test('renderDesignPinRow returns a fallback element when buildCommentCard is unavailable', () => {
  // Simulate a Node-side environment where window.crit.commentCard is not
  // wired. The row falls back to a minimal div so design mode still renders
  // even if the shared module is missing.
  const origDocument = global.document;
  const origWindow = global.window;
  global.document = {
    createElement(tag) {
      const el = {
        tagName: tag.toUpperCase(), className: '', textContent: '',
        dataset: {}, children: [],
        appendChild(c) { this.children.push(c); return c; },
      };
      return el;
    },
  };
  global.window = { crit: {} };
  try {
    const out = renderDesignPinRow(
      { id: 'c1', body: 'looks off', dom_anchor: { pathname: '/x' } },
      {},
    );
    assert.equal(out.dataset.id, 'c1');
    assert.equal(out.dataset.commentId, 'c1');
    assert.equal(out.dataset.designRoute, '/x');
    assert.equal(out.textContent, 'looks off');
  } finally {
    if (origDocument === undefined) delete global.document; else global.document = origDocument;
    if (origWindow === undefined) delete global.window; else global.window = origWindow;
  }
});
