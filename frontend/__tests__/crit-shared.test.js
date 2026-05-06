const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');

// Load crit-shared.js in a fake-browser shim — only the pure helpers are exercised.
const src = fs.readFileSync(path.join(__dirname, '..', 'crit-shared.js'), 'utf8');
const sandbox = { window: {}, document: { cookie: '' } };
const fn = new Function('window', 'document', src + '\nreturn window;');
fn(sandbox.window, sandbox.document);
const shared = sandbox.window.crit.shared;

test('escapeHTML escapes <, >, &, "', () => {
  assert.equal(shared.escapeHTML('<a href="x">&</a>'),
    '&lt;a href=&quot;x&quot;&gt;&amp;&lt;/a&gt;');
});

test('escapeHTML returns empty string for null/undefined', () => {
  assert.equal(shared.escapeHTML(null), '');
  assert.equal(shared.escapeHTML(undefined), '');
});

test('getCookie reads document.cookie', () => {
  sandbox.document.cookie = 'crit-settings=' + encodeURIComponent('{"theme":"dark"}') + '; other=x';
  assert.equal(decodeURIComponent(shared.getCookie('crit-settings')), '{"theme":"dark"}');
  assert.equal(shared.getCookie('missing'), null);
});

test('setCookie writes document.cookie with path (2-arg)', () => {
  sandbox.document.cookie = '';
  shared.setCookie('foo', 'bar');
  assert.match(sandbox.document.cookie, /^foo=bar/);
});

test('readThemeFromSettings parses JSON crit-settings cookie', () => {
  sandbox.document.cookie = 'crit-settings=' + encodeURIComponent('{"theme":"light"}');
  assert.equal(shared.readThemeFromSettings(), 'light');
  sandbox.document.cookie = '';
  assert.equal(shared.readThemeFromSettings(), 'system');
  sandbox.document.cookie = 'crit-settings=' + encodeURIComponent('not json');
  assert.equal(shared.readThemeFromSettings(), 'system');
});

// updateCommentCountIndicator — navbar pill parity helper. Both code-review
// (app.js) and design-mode (design-mode.js) call this so the resolved-state
// class, count text, and tooltip stay in sync. Tests use a fresh sandbox
// because the helper reads document.getElementById, which the lightweight
// shim above (cookie-only) doesn't implement.
function makeIndicatorSandbox() {
  function makeEl() {
    return {
      textContent: '',
      title: '',
      style: { display: 'unset' },
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
    };
  }
  const els = {
    commentNavGroup: makeEl(),
    commentCount: makeEl(),
    commentCountNumber: makeEl(),
  };
  const win = {};
  const doc = { cookie: '', getElementById: (id) => els[id] || null };
  const fn = new Function('window', 'document', src + '\nreturn window;');
  fn(win, doc);
  return { shared: win.crit.shared, els };
}

test('updateCommentCountIndicator: 0/0 marks no-comments state', () => {
  const { shared: s, els } = makeIndicatorSandbox();
  s.updateCommentCountIndicator({ totalCount: 0, openCount: 0 });
  assert.equal(els.commentCountNumber.textContent, '');
  assert.equal(els.commentCount.title, 'Toggle comments panel');
  assert.equal(els.commentCount.classList.contains('comment-count-resolved'), true);
  assert.equal(els.commentNavGroup.classList.contains('has-comments'), false);
});

test('updateCommentCountIndicator: open > 0 sets unresolved title + class', () => {
  const { shared: s, els } = makeIndicatorSandbox();
  s.updateCommentCountIndicator({ totalCount: 5, openCount: 3 });
  assert.equal(els.commentCountNumber.textContent, '3');
  assert.equal(els.commentCount.title, '3 unresolved comments — toggle panel');
  assert.equal(els.commentCount.classList.contains('comment-count-resolved'), false);
  assert.equal(els.commentNavGroup.classList.contains('has-comments'), true);
});

test('updateCommentCountIndicator: 1 unresolved uses singular', () => {
  const { shared: s, els } = makeIndicatorSandbox();
  s.updateCommentCountIndicator({ totalCount: 1, openCount: 1 });
  assert.equal(els.commentCount.title, '1 unresolved comment — toggle panel');
});

test('updateCommentCountIndicator: all resolved sets resolved title + class', () => {
  const { shared: s, els } = makeIndicatorSandbox();
  s.updateCommentCountIndicator({ totalCount: 4, openCount: 0 });
  assert.equal(els.commentCountNumber.textContent, '4');
  assert.equal(els.commentCount.title, '4 resolved comments — toggle panel');
  assert.equal(els.commentCount.classList.contains('comment-count-resolved'), true);
  assert.equal(els.commentNavGroup.classList.contains('has-comments'), true);
});

test('fetchJSON throws on !response.ok', async () => {
  const origFetch = globalThis.fetch;
  globalThis.fetch = async () => ({ ok: false, status: 500, text: async () => 'boom', headers: { get: () => '' } });
  try {
    await assert.rejects(() => shared.fetchJSON('/x'), /500/);
  } finally {
    globalThis.fetch = origFetch;
  }
});

// ----- showToast -----
// Minimal DOM stub: supports the operations crit.shared.showToast uses
// (createElement, body.appendChild/removeChild, querySelector for the host,
// element classList + listeners). Lets us assert lifecycle without jsdom.
function makeToastSandbox() {
  function elClassList() {
    const set = new Set();
    return {
      _set: set,
      add(...c) { c.forEach((x) => set.add(x)); },
      remove(...c) { c.forEach((x) => set.delete(x)); },
      contains(c) { return set.has(c); },
    };
  }
  function makeNode(tag) {
    const node = {
      tagName: (tag || 'div').toUpperCase(),
      _children: [],
      _listeners: {},
      classList: elClassList(),
      textContent: '',
      _className: '',
      get className() { return this._className; },
      set className(v) {
        this._className = v;
        this.classList = elClassList();
        String(v).split(/\s+/).filter(Boolean).forEach((c) => this.classList.add(c));
      },
      parentNode: null,
      appendChild(child) {
        child.parentNode = this;
        this._children.push(child);
        return child;
      },
      removeChild(child) {
        const i = this._children.indexOf(child);
        if (i >= 0) this._children.splice(i, 1);
        child.parentNode = null;
        return child;
      },
      addEventListener(evt, cb /* , opts */) {
        (this._listeners[evt] = this._listeners[evt] || []).push(cb);
      },
      dispatchEvent(evt) {
        const arr = this._listeners[evt.type] || [];
        arr.slice().forEach((cb) => cb(evt));
      },
      querySelector(sel) {
        // crawl descendants for first node with matching class.
        if (!sel.startsWith('.')) return null;
        const want = sel.slice(1);
        const stack = this._children.slice();
        while (stack.length) {
          const n = stack.shift();
          if (n.classList && n.classList.contains(want)) return n;
          if (n._children) stack.push(...n._children);
        }
        return null;
      },
    };
    return node;
  }
  const body = makeNode('body');
  const doc = {
    cookie: '',
    body,
    createElement: (tag) => makeNode(tag),
    querySelector: (sel) => body.querySelector(sel),
  };
  // Run helpers under a faked timer/raf so tests are deterministic.
  const timers = [];
  let now = 0;
  const win = {};
  const sandboxGlobals = {
    requestAnimationFrame: (cb) => { timers.push({ at: now, cb }); return timers.length; },
    setTimeout: (cb, ms) => { timers.push({ at: now + (ms || 0), cb }); return timers.length; },
    clearTimeout: (id) => { const t = timers[id - 1]; if (t) t.cancelled = true; },
  };
  function flush(ms) {
    now += (ms || 0);
    // run any non-cancelled timers whose time has come, repeatedly to handle
    // chained scheduling.
    let progress = true;
    while (progress) {
      progress = false;
      for (const t of timers) {
        if (!t.cancelled && !t.fired && t.at <= now) {
          t.fired = true;
          progress = true;
          t.cb();
        }
      }
    }
  }
  const fn = new Function(
    'window', 'document', 'requestAnimationFrame', 'setTimeout', 'clearTimeout',
    src + '\nreturn window;',
  );
  fn(win, doc, sandboxGlobals.requestAnimationFrame, sandboxGlobals.setTimeout, sandboxGlobals.clearTimeout);
  return { shared: win.crit.shared, doc, body, flush };
}

test('showToast appends a .mini-toast inside a single .mini-toast-host', () => {
  const { shared: s, body } = makeToastSandbox();
  s.showToast('hello');
  s.showToast('world');
  const hosts = body._children.filter((n) => n.classList.contains('mini-toast-host'));
  assert.equal(hosts.length, 1, 'host is created once');
  assert.equal(hosts[0]._children.length, 2, 'both toasts mounted in the host');
  assert.equal(hosts[0]._children[0].textContent, 'hello');
  assert.equal(hosts[0]._children[1].textContent, 'world');
});

test('showToast applies kind modifier class', () => {
  const { shared: s, body } = makeToastSandbox();
  s.showToast('boom', { kind: 'error' });
  const host = body._children.find((n) => n.classList.contains('mini-toast-host'));
  const t = host._children[0];
  assert.equal(t.classList.contains('mini-toast'), true);
  assert.equal(t.classList.contains('mini-toast--error'), true);
});

test('showToast: rAF adds the visible class for the entry transition', () => {
  const { shared: s, body, flush } = makeToastSandbox();
  s.showToast('hi');
  const t = body.querySelector('.mini-toast-host')._children[0];
  assert.equal(t.classList.contains('mini-toast-visible'), false, 'not yet — rAF pending');
  flush(0); // run rAF
  assert.equal(t.classList.contains('mini-toast-visible'), true);
});

test('showToast auto-dismisses after timeout via transitionend cleanup', () => {
  const { shared: s, body, flush } = makeToastSandbox();
  s.showToast('bye', { timeout: 3000 });
  flush(0); // rAF -> visible
  const host = body.querySelector('.mini-toast-host');
  const t = host._children[0];
  assert.equal(host._children.length, 1);
  flush(3000); // timeout fires -> visible class removed
  assert.equal(t.classList.contains('mini-toast-visible'), false);
  assert.equal(host._children.length, 1, 'still mounted until transitionend');
  // Simulate the browser firing transitionend at the end of the exit transition.
  t.dispatchEvent({ type: 'transitionend' });
  assert.equal(host._children.length, 0, 'removed on transitionend');
});

test('showToast: returned dismiss() removes the toast early; idempotent', () => {
  const { shared: s, body, flush } = makeToastSandbox();
  const dismiss = s.showToast('x', { timeout: 10000 });
  flush(0);
  const host = body.querySelector('.mini-toast-host');
  const t = host._children[0];
  dismiss();
  t.dispatchEvent({ type: 'transitionend' });
  assert.equal(host._children.length, 0);
  // Calling again is a no-op (would otherwise throw on missing parent).
  assert.doesNotThrow(() => dismiss());
});

test('showToast: fallback timeout removes the toast if transitionend never fires', () => {
  const { shared: s, body, flush } = makeToastSandbox();
  s.showToast('z', { timeout: 3000 });
  flush(0);
  const host = body.querySelector('.mini-toast-host');
  flush(3000); // dismiss scheduled — visible removed
  flush(400);  // fallback fires, transitionend never dispatched
  assert.equal(host._children.length, 0, 'fallback cleanup removed the toast');
});

test('showToast: timeout=0 keeps the toast open until dismiss()', () => {
  const { shared: s, body, flush } = makeToastSandbox();
  const dismiss = s.showToast('sticky', { timeout: 0 });
  flush(0);
  flush(60000);
  const host = body.querySelector('.mini-toast-host');
  assert.equal(host._children.length, 1, 'still there');
  dismiss();
  host._children[0].dispatchEvent({ type: 'transitionend' });
  assert.equal(host._children.length, 0);
});
