'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { create } = require('../design-mode.sse.js');

test('applyRoundStart resets per-round flags and announces', () => {
  const calls = { announced: [], scheduled: [], uiState: [] };
  const state = {
    currentRound: 1,
    resolutionCache: { '/x': 'fresh' },
    userActedThisRound: true,
    currentRoute: '/foo',
  };
  const ctl = create({
    state,
    pinsByRoute: () => ({
      '/x': [{ id: 'a', _roundResolved: true }, { id: 'b', _roundResolved: true }],
    }),
    scheduleResolutionForPath: (p) => calls.scheduled.push(p),
    announceLive: (m) => calls.announced.push(m),
    setUIState: (s) => calls.uiState.push(s),
  });
  ctl.applyRoundStart(3);
  assert.equal(state.currentRound, 3);
  assert.deepEqual(state.resolutionCache, {});
  assert.equal(state.userActedThisRound, false);
  assert.deepEqual(calls.uiState, ['reviewing']);
  assert.deepEqual(calls.scheduled, ['/foo']);
  assert.deepEqual(calls.announced, ['Round 3 started.']);
});

test('applyRoundStart clears _roundResolved on every existing pin', () => {
  const pins = [
    { id: 'a', _roundResolved: true },
    { id: 'b', _roundResolved: true },
  ];
  const ctl = create({
    state: { currentRoute: '/' },
    pinsByRoute: () => ({ '/': pins }),
    scheduleResolutionForPath: () => {},
    announceLive: () => {},
    setUIState: () => {},
  });
  ctl.applyRoundStart(2);
  assert.equal(pins[0]._roundResolved, false);
  assert.equal(pins[1]._roundResolved, false);
});

test('applyRoundStart falls back to currentRoute then "/" for path', () => {
  const seen = [];
  const ctl = create({
    state: { currentRoute: '/r' },
    pinsByRoute: () => ({}),
    scheduleResolutionForPath: (p) => seen.push(p),
    announceLive: () => {},
    setUIState: () => {},
  });
  ctl.applyRoundStart(1);
  assert.deepEqual(seen, ['/r']);
});

test('applyRoundStart re-fetches comments so replies posted mid-round appear', async () => {
  // Regression for Bug D: replies posted during round N (e.g. by the agent
  // via `crit comment --reply-to`) didn't appear when round N+1 started.
  // Round-start re-rendered the panel from stale state; comments-changed
  // SSE listener exists but events emitted during the round transition
  // were lost (panel re-renders before the reload lands). Round-start
  // itself must trigger a canonical re-fetch.
  let reloads = 0;
  const ctl = create({
    state: {},
    pinsByRoute: () => ({}),
    scheduleResolutionForPath: () => {},
    announceLive: () => {},
    setUIState: () => {},
    reloadComments: () => { reloads++; return Promise.resolve(); },
  });
  ctl.applyRoundStart(2);
  // Allow the queued microtask (Promise chain in applyCommentsChanged) to settle.
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(reloads, 1, 'round-start must re-fetch comments to capture mid-round replies');
});

test('applyCommentsChanged invokes reloadComments', async () => {
  let reloads = 0;
  const ctl = create({
    state: {},
    pinsByRoute: () => ({}),
    scheduleResolutionForPath: () => {},
    announceLive: () => {},
    setUIState: () => {},
    reloadComments: () => { reloads++; return Promise.resolve(); },
  });
  await ctl.applyCommentsChanged();
  assert.equal(reloads, 1);
});

test('applyCommentsChanged coalesces overlapping reloads', async () => {
  // A burst of comments-changed events (e.g. agent posting many replies in
  // quick succession) must not trigger N parallel reloads. The dedup guard
  // collapses overlapping calls into a single trailing reload.
  let inFlight = 0;
  let maxConcurrent = 0;
  let reloads = 0;
  let resolveFirst;
  const firstResolved = new Promise(function (r) { resolveFirst = r; });
  const ctl = create({
    state: {},
    pinsByRoute: () => ({}),
    scheduleResolutionForPath: () => {},
    announceLive: () => {},
    setUIState: () => {},
    reloadComments: () => {
      reloads++;
      inFlight++;
      maxConcurrent = Math.max(maxConcurrent, inFlight);
      const p = reloads === 1 ? firstResolved : Promise.resolve();
      return p.then(function () { inFlight--; });
    },
  });
  const a = ctl.applyCommentsChanged();
  ctl.applyCommentsChanged();
  ctl.applyCommentsChanged();
  resolveFirst();
  await a;
  // Wait one more microtask tick so the trailing reload can settle.
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(maxConcurrent, 1, 'reloads must not run in parallel');
  assert.equal(reloads, 2, 'three events collapse to two reloads (initial + one trailing)');
});

test('applyCommentsChanged swallows reloadComments rejections', async () => {
  // SSE handlers must not let an exception break the connection. A
  // rejected reload should be logged and the in-flight guard reset so the
  // next event can still trigger a reload.
  let reloads = 0;
  const ctl = create({
    state: {},
    pinsByRoute: () => ({}),
    scheduleResolutionForPath: () => {},
    announceLive: () => {},
    setUIState: () => {},
    reloadComments: () => {
      reloads++;
      return Promise.reject(new Error('boom'));
    },
  });
  await ctl.applyCommentsChanged();
  await ctl.applyCommentsChanged();
  assert.equal(reloads, 2);
});

test('round transition surfaces a reply that landed between rounds (DOM-asserted)', async () => {
  // User-visible regression for Bug B: a reply posted via
  // `crit comment --reply-to` between round N's finish and round N+1's
  // start did NOT appear in the panel after the round flipped, even though
  // the reply was already on disk. The previous agent's fetch-count test
  // passed despite the user-visible bug — so this asserts the rendered
  // DOM, not internal call counts.
  //
  // Setup uses the real panel-render module + a stub renderDesignPinRow
  // that emits the comment body + each reply.body as text. The
  // reloadComments dep mimics the real one: pull the latest list from a
  // staged fetch queue, write into state.comments, then call panelRefresh.
  // Step 1 stages a comment with zero replies; step 2 stages the SAME
  // comment with one reply. Triggering applyRoundStart must end with the
  // reply's body text rendered in panelBody.
  const panelRender = require('../design-mode.panel-render.js');
  require('../design-route-utils.js');

  // Minimal DOM — same shape as design-mode.panel-render.test.js.
  function makeNode(tag) {
    const el = {
      nodeType: 1, tagName: String(tag || 'div').toUpperCase(),
      _className: '', textContent: '', innerHTML: '',
      dataset: {}, style: {}, attrs: {}, parentNode: null, childNodes: [],
      scrollTop: 0,
      classList: {
        _s: new Set(),
        add(c) { this._s.add(c); }, remove(c) { this._s.delete(c); },
        contains(c) { return this._s.has(c); },
        toggle(c, force) {
          if (typeof force === 'boolean') { force ? this._s.add(c) : this._s.delete(c); return force; }
          if (this._s.has(c)) { this._s.delete(c); return false; }
          this._s.add(c); return true;
        },
      },
      get className() { return this._className; },
      set className(v) {
        this._className = String(v || '');
        this.classList._s = new Set(this._className.split(/\s+/).filter(Boolean));
      },
      setAttribute(k, v) { this.attrs[k] = v; },
      getAttribute(k) { return this.attrs[k]; },
      addEventListener() {}, removeEventListener() {},
      appendChild(child) { return this.insertBefore(child, null); },
      insertBefore(child, ref) {
        if (child.parentNode) child.parentNode.removeChild(child);
        child.parentNode = this;
        if (ref == null) this.childNodes.push(child);
        else {
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
  function flatText(node) {
    let s = node.textContent || '';
    for (const c of (node.childNodes || [])) s += ' ' + flatText(c);
    return s;
  }

  const prevDoc = global.document;
  const prevWin = global.window;
  const doc = { createElement: (t) => makeNode(t) };
  const panelBody = makeNode('div');
  panelBody.className = 'comments-panel-body';
  const utils = require('../design-route-utils.js');
  // Stub row module that puts the comment body and each reply.body in
  // textContent so flatText() can find them.
  const win = {
    crit: {
      designUtils: utils,
      design: {
        row: {
          renderDesignPinRow(c) {
            const wrap = makeNode('div');
            wrap.className = 'comment-card';
            wrap.dataset.id = String(c.id || '');
            wrap.dataset.designRoute = (c.dom_anchor && c.dom_anchor.pathname) || '/';
            const body = makeNode('div');
            body.className = 'comment-card-body';
            body.textContent = c.body || '';
            wrap.appendChild(body);
            const replies = Array.isArray(c.replies) ? c.replies : [];
            for (const r of replies) {
              const rn = makeNode('div');
              rn.className = 'comment-reply';
              rn.dataset.replyId = r.id || '';
              const rb = makeNode('div');
              rb.className = 'reply-body';
              rb.textContent = r.body || '';
              rn.appendChild(rb);
              wrap.appendChild(rn);
            }
            return wrap;
          },
        },
      },
    },
  };
  global.document = doc;
  global.window = win;

  try {
    const state = {
      comments: [],
      designFilter: 'all',
      designExpandAll: false,
      designCollapseOverrides: new Map(),
      session: { review_round: 1 },
      currentRoute: '/',
    };
    const panelCtl = panelRender.create({
      state, els: { panelBody }, utils, shared: null,
    });

    // Initial render: one comment, zero replies.
    state.comments = [{
      id: 'c1', body: 'pin body', author: 'me', resolved: false,
      replies: [],
      dom_anchor: { pathname: '/dashboard', css_selector: 'button' },
    }];
    panelCtl.panelRefresh();
    assert.ok(flatText(panelBody).includes('pin body'), 'initial render must show pin body');
    assert.ok(!flatText(panelBody).includes('looks great'), 'reply text must not be present yet');

    // Stage what the next reload returns: the same comment, now with a
    // reply attached. This is exactly the data the server has on disk
    // when round N+1 starts (the reply was written during round N).
    const nextFetch = [{
      id: 'c1', body: 'pin body', author: 'me', resolved: false,
      replies: [{ id: 'r1', body: 'looks great after the fix', author: 'them' }],
      dom_anchor: { pathname: '/dashboard', css_selector: 'button' },
    }];

    const sseCtl = create({
      state,
      pinsByRoute: () => ({}),
      scheduleResolutionForPath: () => {},
      announceLive: () => {},
      setUIState: () => {},
      reloadComments: () => {
        // Mimic loadAllComments + refreshPanel: pull staged data, then render.
        state.comments = nextFetch.slice();
        return Promise.resolve().then(() => { panelCtl.panelRefresh(); });
      },
    });

    sseCtl.applyRoundStart(2);
    for (let i = 0; i < 6; i++) await Promise.resolve();

    const text = flatText(panelBody);
    assert.ok(
      text.includes('looks great after the fix'),
      'panel DOM must contain the new reply body after round transition; got: ' + text
    );
  } finally {
    global.document = prevDoc;
    global.window = prevWin;
  }
});

test('applyRoundStart triggers iframe reload so reviewers see freshly-rendered UI', () => {
  // Regression for Bug C: when round 2 starts the iframe (proxied target
  // page) wasn't reloaded, so reviewers saw stale UI even after the agent
  // shipped fixes between rounds. The round-start handler must signal the
  // iframe to refresh.
  let reloads = 0;
  const ctl = create({
    state: {},
    pinsByRoute: () => ({}),
    scheduleResolutionForPath: () => {},
    announceLive: () => {},
    setUIState: () => {},
    reloadIframe: () => { reloads++; },
  });
  ctl.applyRoundStart(3);
  assert.equal(reloads, 1, 'applyRoundStart must call reloadIframe once');
});

test('install does nothing when EventSource is unavailable', () => {
  // create() with no global EventSource — install() must not throw.
  const ctl = create({
    state: {},
    pinsByRoute: () => ({}),
    scheduleResolutionForPath: () => {},
    announceLive: () => {},
    setUIState: () => {},
  });
  // EventSource not defined in Node — install catches the construction
  // failure and returns undefined.
  const res = ctl.install();
  assert.equal(res, undefined);
});
