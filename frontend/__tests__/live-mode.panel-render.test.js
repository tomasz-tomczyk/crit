// live-mode.panel-render.test.js — granular update path for the
// comments-side panel.
//
// Until the granular fix, renderCommentsPanel() did `panelBody.innerHTML = ''`
// on every refresh — visible flicker, scroll position lost, and (worst) any
// open composer's textarea got torn out and rebuilt the moment an unrelated
// pin's reply arrived over SSE. The tests below pin the new behaviour:
//
//   1. Scroll position preserved across updates.
//   2. Reused card wrappers are the SAME node instance (so a focused
//      textarea inside an unrelated card survives an update).
//   3. Filter changes hide/show without rebuilding unaffected cards.
//   4. Empty state ↔ populated transitions still work.
//   5. Group-by-route insertion + removal as comments shift between routes.
'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

// We require the real route-utils so groupCommentsByRoute behaves exactly as
// it does in the browser; only the DOM is stubbed.
require('../live-route-utils.js');
const { create } = require('../live-mode.panel-render.js');

// --------------------------- minimal DOM stub ------------------------------
// Just enough to satisfy parent/sibling traversal, insertBefore, removeChild,
// scrollTop assignment, and the render module's reads of dataset / classList.
function makeNode(tag) {
  const el = {
    nodeType: 1,
    tagName: String(tag || 'div').toUpperCase(),
    _className: '',
    get className() { return this._className; },
    set className(v) {
      this._className = String(v || '');
      // Keep classList in sync so tests can use either API.
      this.classList._s = new Set(this._className.split(/\s+/).filter(Boolean));
    },
    textContent: '',
    innerHTML: '',
    dataset: {},
    style: {},
    attrs: {},
    parentNode: null,
    childNodes: [],
    scrollTop: 0,
    classList: {
      _s: new Set(),
      add(c) { this._s.add(c); },
      remove(c) { this._s.delete(c); },
      contains(c) { return this._s.has(c); },
      toggle(c, force) {
        if (typeof force === 'boolean') { force ? this._s.add(c) : this._s.delete(c); return force; }
        if (this._s.has(c)) { this._s.delete(c); return false; }
        this._s.add(c); return true;
      },
    },
    setAttribute(k, v) { this.attrs[k] = v; },
    getAttribute(k) { return this.attrs[k]; },
    addEventListener() {},
    removeEventListener() {},
    appendChild(child) { return this.insertBefore(child, null); },
    insertBefore(child, ref) {
      if (child.parentNode) child.parentNode.removeChild(child);
      child.parentNode = this;
      if (ref == null) {
        this.childNodes.push(child);
      } else {
        const idx = this.childNodes.indexOf(ref);
        if (idx === -1) this.childNodes.push(child);
        else this.childNodes.splice(idx, 0, child);
      }
      return child;
    },
    removeChild(child) {
      const idx = this.childNodes.indexOf(child);
      if (idx !== -1) this.childNodes.splice(idx, 1);
      child.parentNode = null;
      return child;
    },
    querySelector() { return null; },
    querySelectorAll() { return []; },
    get firstChild() { return this.childNodes[0] || null; },
    get nextSibling() {
      const p = this.parentNode;
      if (!p) return null;
      const idx = p.childNodes.indexOf(this);
      return idx >= 0 ? (p.childNodes[idx + 1] || null) : null;
    },
    focus() {},
  };
  return el;
}

function makeDocument() {
  return {
    createElement(tag) { return makeNode(tag); },
    createDocumentFragment() {
      const frag = makeNode('#document-fragment');
      frag.nodeType = 11;
      return frag;
    },
  };
}

// Render module reads window.crit.live.row for renderLivePinRow. The
// fallback path (no row module) builds plain comment-card divs — perfect for
// asserting against without hauling in the full row module.
function setupDom() {
  const doc = makeDocument();
  const panelBody = doc.createElement('div');
  panelBody.className = 'comments-panel-body';
  const win = { crit: { liveUtils: require('../live-route-utils.js'), live: {} } };
  global.document = doc;
  global.window = win;
  return { doc, panelBody, win };
}

function teardownDom(prev) {
  global.document = prev.document;
  global.window = prev.window;
}

function snapshotIds(panelBody) {
  // Flatten panelBody → for each comments-panel-file-group, return the ids
  // of its comment-card descendants in order.
  const out = [];
  for (const group of panelBody.childNodes) {
    if (!group.classList.contains('comments-panel-file-group')) continue;
    const cards = group.childNodes.find(n => n.classList && n.classList.contains('comments-panel-file-cards'));
    if (!cards) continue;
    out.push({
      route: group.dataset.liveRoute,
      ids: cards.childNodes.map(n => n.dataset.id),
    });
  }
  return out;
}

function makeCtl(panelBody) {
  const state = {
    comments: [],
    liveFilter: 'all',
    liveExpandAll: false,
    liveCollapseOverrides: new Map(),
    session: { review_round: 0 },
  };
  const utils = require('../live-route-utils.js');
  const ctl = create({
    state, els: { panelBody }, utils, shared: null,
  });
  return { ctl, state };
}

// ---------------------------------- tests ----------------------------------

test('renderCommentsPanel: empty comments -> "No pins yet" placeholder', () => {
  const prev = { document: global.document, window: global.window };
  const { panelBody } = setupDom();
  try {
    const { ctl, state } = makeCtl(panelBody);
    state.comments = [];
    ctl.renderCommentsPanel();
    assert.equal(panelBody.childNodes.length, 1);
    assert.ok(panelBody.firstChild.classList.contains('comments-panel-empty'));
  } finally {
    teardownDom(prev);
  }
});

test('renderCommentsPanel: all-filtered-out shows filter-specific message and clears bookkeeping', () => {
  const prev = { document: global.document, window: global.window };
  const { panelBody } = setupDom();
  try {
    const { ctl, state } = makeCtl(panelBody);
    state.comments = [{ id: '1', body: 'x', resolved: false, path: '/a' }];
    ctl.renderCommentsPanel();
    assert.equal(snapshotIds(panelBody).length, 1);

    state.liveFilter = 'resolved';
    ctl.renderCommentsPanel();
    assert.equal(panelBody.childNodes.length, 1);
    assert.ok(panelBody.firstChild.classList.contains('comments-panel-empty'));
    assert.equal(panelBody.firstChild.textContent, 'No resolved pins.');

    // Switching back populates without losing data.
    state.liveFilter = 'all';
    ctl.renderCommentsPanel();
    assert.deepEqual(snapshotIds(panelBody), [{ route: '/a', ids: ['1'] }]);
  } finally {
    teardownDom(prev);
  }
});

test('renderCommentsPanel: scroll position preserved across updates', () => {
  const prev = { document: global.document, window: global.window };
  const { panelBody } = setupDom();
  try {
    const { ctl, state } = makeCtl(panelBody);
    state.comments = [
      { id: '1', body: 'a', path: '/x' },
      { id: '2', body: 'b', path: '/x' },
    ];
    ctl.renderCommentsPanel();
    panelBody.scrollTop = 173;

    // Add a third comment (typical SSE comments-changed flow).
    state.comments.push({ id: '3', body: 'c', path: '/x' });
    ctl.renderCommentsPanel();

    assert.equal(panelBody.scrollTop, 173, 'scrollTop must survive granular update');
    assert.deepEqual(snapshotIds(panelBody), [{ route: '/x', ids: ['1', '2', '3'] }]);
  } finally {
    teardownDom(prev);
  }
});

test('renderCommentsPanel: unchanged cards keep the SAME wrapper instance (focus survives)', () => {
  const prev = { document: global.document, window: global.window };
  const { panelBody } = setupDom();
  try {
    const { ctl, state } = makeCtl(panelBody);
    state.comments = [
      { id: '1', body: 'a', path: '/x' },
      { id: '2', body: 'b', path: '/x' },
    ];
    ctl.renderCommentsPanel();

    const ids = snapshotIds(panelBody);
    const cards1 = panelBody.firstChild.childNodes.find(n => n.classList.contains('comments-panel-file-cards'));
    const wrapperBefore_1 = cards1.childNodes[0];
    const wrapperBefore_2 = cards1.childNodes[1];

    // An "unrelated pin's reply submits in parallel": comment #2 mutates,
    // #1 is untouched. The bug: wholesale rebuild rebuilds #1's wrapper too,
    // killing focus on a textarea inside it. The fix: #1's wrapper is the
    // exact same node instance afterwards.
    state.comments[1] = Object.assign({}, state.comments[1], { body: 'b updated' });
    ctl.renderCommentsPanel();

    const cards2 = panelBody.firstChild.childNodes.find(n => n.classList.contains('comments-panel-file-cards'));
    assert.strictEqual(cards2.childNodes[0], wrapperBefore_1, 'unrelated card wrapper must be identity-preserved');
    assert.notStrictEqual(cards2.childNodes[1], wrapperBefore_2, 'mutated card wrapper must be replaced');
    assert.deepEqual(snapshotIds(panelBody), ids); // ordering identical
  } finally {
    teardownDom(prev);
  }
});

test('renderCommentsPanel: route group appears/disappears as last pin moves out', () => {
  const prev = { document: global.document, window: global.window };
  const { panelBody } = setupDom();
  try {
    const { ctl, state } = makeCtl(panelBody);
    state.comments = [
      { id: '1', body: 'a', path: '/x' },
      { id: '2', body: 'b', path: '/y' },
    ];
    ctl.renderCommentsPanel();
    assert.deepEqual(snapshotIds(panelBody), [
      { route: '/x', ids: ['1'] },
      { route: '/y', ids: ['2'] },
    ]);

    // Comment 2 deleted → /y group must disappear.
    state.comments = [{ id: '1', body: 'a', path: '/x' }];
    ctl.renderCommentsPanel();
    assert.deepEqual(snapshotIds(panelBody), [{ route: '/x', ids: ['1'] }]);
  } finally {
    teardownDom(prev);
  }
});

test('renderCommentsPanel: filter hides cards without touching unaffected wrappers', () => {
  const prev = { document: global.document, window: global.window };
  const { panelBody } = setupDom();
  try {
    const { ctl, state } = makeCtl(panelBody);
    state.comments = [
      { id: '1', body: 'a', path: '/x', resolved: false },
      { id: '2', body: 'b', path: '/x', resolved: true },
    ];
    ctl.renderCommentsPanel();
    const cards = panelBody.firstChild.childNodes.find(n => n.classList.contains('comments-panel-file-cards'));
    const wrapper1Before = cards.childNodes[0];

    state.liveFilter = 'open';
    ctl.renderCommentsPanel();

    const cards2 = panelBody.firstChild.childNodes.find(n => n.classList.contains('comments-panel-file-cards'));
    assert.deepEqual(cards2.childNodes.map(n => n.dataset.id), ['1']);
    assert.strictEqual(cards2.childNodes[0], wrapper1Before, 'filter must not rebuild unaffected card');
  } finally {
    teardownDom(prev);
  }
});
