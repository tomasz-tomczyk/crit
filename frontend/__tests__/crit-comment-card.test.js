'use strict';
// Contract tests for the shared crit-comment-card module. These exercise the
// option contract (callbacks, deps) without a real DOM by stubbing only what
// the renderer touches.
const { test } = require('node:test');
const assert = require('node:assert/strict');

// Minimal DOM shim — enough for buildCommentCard's createElement / classList /
// dataset / appendChild / addEventListener calls. We don't assert on rendered
// HTML here (that's covered by E2E + manual code-review checks); we only verify
// the option contract (callbacks fire, deps are consulted, return shape).
function makeEl() {
  const el = {
    children: [],
    classList: {
      _set: new Set(),
      add(...c) { c.forEach((x) => this._set.add(x)); },
      remove(...c) { c.forEach((x) => this._set.delete(x)); },
      contains(c) { return this._set.has(c); },
      toggle(c, force) {
        if (force === true) { this._set.add(c); return true; }
        if (force === false) { this._set.delete(c); return false; }
        if (this._set.has(c)) { this._set.delete(c); return false; }
        this._set.add(c); return true;
      },
    },
    dataset: {},
    style: {},
    listeners: {},
    appendChild(c) { this.children.push(c); return c; },
    prepend(c) { this.children.unshift(c); return c; },
    addEventListener(name, fn) { this.listeners[name] = fn; },
    setAttribute(k, v) { this.attrs = this.attrs || {}; this.attrs[k] = v; },
    set className(v) { this._cn = v; v.split(/\s+/).forEach((c) => this.classList._set.add(c)); },
    get className() { return this._cn; },
    set innerHTML(v) { this._html = v; },
    get innerHTML() { return this._html; },
    set textContent(v) { this._text = v; },
    get textContent() { return this._text; },
    set id(v) { this._id = v; },
    get id() { return this._id; },
  };
  return el;
}

global.document = {
  createElement: () => makeEl(),
};

const card = require('../crit-comment-card.js');

// Shared helper: recursively find elements matching a predicate.
function findByClass(root, className) {
  const hits = [];
  (function walk(el) {
    if (el.className && new RegExp('\\b' + className + '\\b').test(el.className)) hits.push(el);
    for (const c of (el.children || [])) walk(c);
  })(root);
  return hits;
}

function baseDeps() {
  return {
    commentMd: { render: (b) => '<p>' + (b || '') + '</p>' },
    formatTime: () => '12:00',
    authorColorIndex: () => 1,
    getReviewRound: () => 2,
    getAgentName: () => 'agent',
    buildCommentEnv: () => ({}),
    renderReplyList: () => makeEl(),
    createReplyInput: () => makeEl(),
    iconChevron: '<svg/>',
  };
}

test('buildCommentCard returns { wrapper, card, actions }', () => {
  const out = card.buildCommentCard(
    { id: 'a1', body: 'hi', created_at: '2024-01-01T00:00:00Z' },
    '',
    { deps: baseDeps() }
  );
  assert.ok(out.wrapper);
  assert.ok(out.card);
  assert.ok(out.actions);
  assert.equal(out.card.dataset.commentId, 'a1');
});

test('isPendingAgentRequest callback is consulted', () => {
  const calls = [];
  const out = card.buildCommentCard(
    { id: 'p1', body: 'x', created_at: '2024-01-01T00:00:00Z' },
    '',
    {
      deps: baseDeps(),
      isPendingAgentRequest: (id) => { calls.push(id); return true; },
    }
  );
  assert.ok(calls.includes('p1'));
  assert.equal(out.wrapper.classList.contains('agent-pending'), true);
  assert.equal(out.wrapper.classList.contains('live-thread'), true);
});

test('getCollapseOverride / setCollapseOverride callbacks are wired', () => {
  let stored;
  const out = card.buildCommentCard(
    { id: 'c1', body: 'x', resolved: true, created_at: '2024-01-01T00:00:00Z' },
    '',
    {
      deps: baseDeps(),
      collapseDefault: true,
      getCollapseOverride: () => undefined,
      setCollapseOverride: (id, v) => { stored = [id, v]; },
    }
  );
  // Resolved + collapseDefault + no override → collapsed.
  assert.equal(out.card.classList.contains('collapsed'), true);
  // Click the collapse button to trigger setCollapseOverride.
  const collapseBtn = out.card.children[0].children[0];
  // header.children[0] is collapseBtn (prepended onto headerLeft, then header
  // appended headerLeft). Walk via children path.
  // Easier: search recursively for an element with a 'click' listener.
  function findClick(el) {
    if (el.listeners && el.listeners.click) return el;
    for (const c of (el.children || [])) {
      const f = findClick(c);
      if (f) return f;
    }
    return null;
  }
  const btn = findClick(out.card);
  btn.listeners.click({ stopPropagation() {} });
  assert.equal(stored[0], 'c1');
  assert.equal(typeof stored[1], 'boolean');
});

test('isLiveThread callback adds live-thread class on unresolved comment', () => {
  const out = card.buildCommentCard(
    { id: 'l1', body: 'x', created_at: '2024-01-01T00:00:00Z' },
    '',
    {
      deps: baseDeps(),
      isLiveThread: () => true,
    }
  );
  assert.equal(out.wrapper.classList.contains('live-thread'), true);
});

test('showReplyInput appends reply input via deps.createReplyInput', () => {
  let called = 0;
  const deps = baseDeps();
  deps.createReplyInput = () => { called++; return makeEl(); };
  card.buildCommentCard(
    { id: 'r1', body: 'x', created_at: '2024-01-01T00:00:00Z' },
    '/some/path',
    { deps: deps, showReplyInput: true }
  );
  assert.equal(called, 1);
});

test('replies list rendered when comment.replies non-empty', () => {
  let called = 0;
  const deps = baseDeps();
  deps.renderReplyList = () => { called++; return makeEl(); };
  card.buildCommentCard(
    { id: 'rep1', body: 'x', created_at: '2024-01-01T00:00:00Z',
      replies: [{ id: 'r1', body: 'reply', author: 'a' }] },
    '',
    { deps: deps }
  );
  assert.equal(called, 1);
});

test('suppressDrift omits the Drifted badge and drifted-context block', () => {
  // Live mode never wants the drift UI: the daemon is no longer setting
  // the drifted bit on live pins, but legacy review files might still
  // carry `drifted: true`. The shared card must accept a flag to hide both
  // the header badge and the disclosure block, so live mode renders a
  // clean card while code-review keeps the existing drift affordance.
  const out = card.buildCommentCard(
    { id: 'd1', body: 'x', drifted: true, anchor: 'old line\nstill old',
      created_at: '2024-01-01T00:00:00Z' },
    '',
    { deps: baseDeps(), suppressDrift: true }
  );
  const badges = findByClass(out.card, 'outdated-badge');
  assert.equal(badges.length, 0, 'no Drifted badge should render under suppressDrift');
  const ctx = findByClass(out.card, 'drifted-context');
  assert.equal(ctx.length, 0, 'no drifted-context disclosure should render under suppressDrift');
  assert.equal(out.wrapper.classList.contains('outdated-comment'), false,
    'wrapper should not get outdated-comment class');
});

test('GitHub badge renders when comment.github_id is set', () => {
  // Comments synced from a GitHub PR carry a non-zero github_id from the
  // Go side. The shared card paints a small pill so reviewers can tell
  // imported comments apart from native crit comments (#370).
  const out = card.buildCommentCard(
    { id: 'gh1', body: 'x', github_id: 12345,
      created_at: '2024-01-01T00:00:00Z' },
    '',
    { deps: baseDeps() }
  );
  const badges = findByClass(out.card, 'github-badge');
  assert.equal(badges.length, 1, 'one GitHub badge should render when github_id is set');
  assert.equal(badges[0].textContent, 'GitHub');
  assert.equal(badges[0].attrs['aria-label'], 'Synced from GitHub');
});

test('GitHub badge omitted when comment.github_id is missing or zero', () => {
  const noField = card.buildCommentCard(
    { id: 'n1', body: 'x', created_at: '2024-01-01T00:00:00Z' },
    '',
    { deps: baseDeps() }
  );
  assert.equal(findByClass(noField.card, 'github-badge').length, 0, 'no badge when github_id is absent');
  const zero = card.buildCommentCard(
    { id: 'n2', body: 'x', github_id: 0, created_at: '2024-01-01T00:00:00Z' },
    '',
    { deps: baseDeps() }
  );
  assert.equal(findByClass(zero.card, 'github-badge').length, 0, 'no badge when github_id is 0');
});
