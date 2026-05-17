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

test('escapeHTML escapes <, >, &, ", and single quotes', () => {
  assert.equal(shared.escapeHTML('<a href="x">&</a>'),
    '&lt;a href=&quot;x&quot;&gt;&amp;&lt;/a&gt;');
  assert.equal(shared.escapeHTML("it's"), 'it&#39;s');
});

test('escapeHTML returns empty string for null/undefined', () => {
  assert.equal(shared.escapeHTML(null), '');
  assert.equal(shared.escapeHTML(undefined), '');
});

test('getCookie reads document.cookie and URL-decodes the value', () => {
  sandbox.document.cookie = 'crit-settings=' + encodeURIComponent('{"theme":"dark"}') + '; other=x';
  assert.equal(shared.getCookie('crit-settings'), '{"theme":"dark"}');
  assert.equal(shared.getCookie('missing'), null);
});

test('setCookie writes 1-year max-age, SameSite=Strict, URL-encoded value', () => {
  // Persistence policy must match app.js: design-mode prefs (theme,
  // commentsPanelOpen, hideResolved, etc.) survive browser restarts. A
  // session cookie here would silently reset those across the close/open.
  sandbox.document.cookie = '';
  shared.setCookie('foo', 'bar baz');
  assert.match(sandbox.document.cookie, /^foo=bar%20baz/);
  assert.match(sandbox.document.cookie, /max-age=31536000/);
  assert.match(sandbox.document.cookie, /SameSite=Strict/);
  assert.match(sandbox.document.cookie, /path=\//);
});

test('setCookie / getCookie round-trip preserves JSON with special chars', () => {
  sandbox.document.cookie = '';
  const payload = '{"a":"x;y=z","b":"é"}';
  shared.setCookie('crit-settings', payload);
  // Simulate a browser presenting only the name=value pair (no attributes
  // like max-age/SameSite are echoed back via document.cookie).
  sandbox.document.cookie = 'crit-settings=' + encodeURIComponent(payload);
  assert.equal(shared.getCookie('crit-settings'), payload);
});

test('setSetting / getSetting round-trip via the consolidated cookie', () => {
  sandbox.document.cookie = '';
  shared.setSetting('design_commentsPanelOpen', false);
  shared.setSetting('theme', 'dark');
  // The browser would echo back only the last write (one cookie name);
  // model that by extracting it from the assigned string.
  const m = sandbox.document.cookie.match(/^crit-settings=([^;]*)/);
  assert.ok(m, 'cookie was written');
  sandbox.document.cookie = 'crit-settings=' + m[1];
  assert.equal(shared.getSetting('design_commentsPanelOpen', true), false);
  assert.equal(shared.getSetting('theme', 'system'), 'dark');
  assert.equal(shared.getSetting('missing', 'fallback'), 'fallback');
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

// ----- runFinishReview -----
// Sandbox: stub document.getElementById for the five waitingDialog ids,
// stub window.crit.shared (loaded via the same Function shim used above),
// provide a fake fetch + clipboard, and exercise the approved/non-approved
// branches plus the dedup contract.
function makeFinishSandbox(fetchImpl, clipboardImpl) {
  function makeEl() {
    return {
      textContent: '', innerHTML: '', offsetWidth: 0,
      style: {},
      classList: {
        _set: new Set(),
        add(...c) { c.forEach((x) => this._set.add(x)); },
        remove(...c) { c.forEach((x) => this._set.delete(x)); },
        contains(c) { return this._set.has(c); },
      },
    };
  }
  const els = {
    waitingDialog: makeEl(),
    waitingHeading: makeEl(),
    waitingMessage: makeEl(),
    waitingClipboard: makeEl(),
    waitingPrompt: makeEl(),
  };
  const win = {};
  const doc = { cookie: '', getElementById: (id) => els[id] || null };
  const fn = new Function('window', 'document', src + '\nreturn window;');
  fn(win, doc);
  // Wire fetch/navigator into the sandbox window AND globalThis (helper uses bare fetch).
  win.fetch = fetchImpl;
  win.navigator = { clipboard: clipboardImpl };
  globalThis.fetch = fetchImpl;
  globalThis.navigator = win.navigator;
  globalThis.document = doc;
  return { shared: win.crit.shared, els };
}

test('runFinishReview approved path: sets approved class + Approved heading + onApproved fires', async () => {
  // Note: navigator.clipboard.writeText is wrapped in try/catch in the helper.
  // Node's built-in navigator is non-writable, so the call no-ops here — same
  // shape as a browser without clipboard permission, which is the contract
  // we want.
  const fetch = async () => ({ ok: true, json: async () => ({ approved: true, prompt: 'ok-prompt' }) });
  const { shared: s, els } = makeFinishSandbox(fetch, { writeText: async () => {} });
  let approvedArg = null;
  let waitingCalled = false;
  const result = await s.runFinishReview({
    onApproved: (p) => { approvedArg = p; },
    onWaiting: () => { waitingCalled = true; },
  });
  assert.deepEqual(result, { approved: true, prompt: 'ok-prompt' });
  assert.equal(els.waitingHeading.textContent, 'Approved');
  assert.equal(els.waitingPrompt.textContent, 'ok-prompt');
  assert.equal(els.waitingDialog.classList.contains('approved'), true);
  assert.equal(els.waitingClipboard.textContent, 'Copy prompt');
  assert.equal(approvedArg, 'ok-prompt');
  assert.equal(waitingCalled, false);
});

test('runFinishReview not-approved path: leaves approved class off + uses default prompt fallback', async () => {
  const fetch = async () => ({ ok: true, json: async () => ({ approved: false }) });
  const { shared: s, els } = makeFinishSandbox(fetch, { writeText: async () => {} });
  let waitingCalled = false;
  const result = await s.runFinishReview({ onWaiting: () => { waitingCalled = true; } });
  assert.equal(result.approved, false);
  assert.equal(result.prompt, 'I reviewed the changes, no feedback, good to go!');
  assert.equal(els.waitingHeading.textContent, 'Review Complete');
  assert.equal(els.waitingDialog.classList.contains('approved'), false);
  assert.equal(waitingCalled, true);
});

test('runFinishReview dedup blocks the second concurrent call', async () => {
  let calls = 0;
  let release;
  const gate = new Promise((r) => { release = r; });
  const fetch = async () => {
    calls++;
    await gate;
    return { ok: true, json: async () => ({ approved: false }) };
  };
  const { shared: s } = makeFinishSandbox(fetch, { writeText: async () => {} });
  const dedup = (function () {
    let busy = false;
    return { busy: () => busy, set: () => { busy = true; }, clear: () => { busy = false; } };
  })();
  const p1 = s.runFinishReview({ dedup });
  const p2 = s.runFinishReview({ dedup });
  release();
  const [r1, r2] = await Promise.all([p1, p2]);
  assert.equal(calls, 1);
  assert.equal(r1.approved, false);
  assert.equal(r2, null, 'second call short-circuits to null');
});

test('runFinishReview onError catches and returns null', async () => {
  const fetch = async () => ({ ok: false, status: 500, json: async () => ({}) });
  const { shared: s } = makeFinishSandbox(fetch, { writeText: async () => {} });
  let captured = null;
  const result = await s.runFinishReview({ onError: (e) => { captured = e; } });
  assert.equal(result, null);
  assert.match(String(captured), /HTTP 500/);
});

// ----- waitForSession -----
test('waitForSession: 503 then 200 resolves with payload, fires onProgress', async () => {
  let n = 0;
  globalThis.fetch = async () => {
    n++;
    if (n < 3) return { status: 503 };
    return { status: 200, ok: true, json: async () => ({ ready: true, n }) };
  };
  const win = {};
  const doc = { cookie: '' };
  new Function('window', 'document', src)(win, doc);
  const progress = [];
  const payload = await win.crit.shared.waitForSession({
    intervalMs: 1,
    onProgress: (e) => { progress.push(e); },
  });
  assert.deepEqual(payload, { ready: true, n: 3 });
  assert.equal(progress.length, 2, 'onProgress fires once per 503');
});

test('waitForSession: maxWaitMs cap rejects with timeout', async () => {
  globalThis.fetch = async () => ({ status: 503 });
  const win = {};
  const doc = { cookie: '' };
  new Function('window', 'document', src)(win, doc);
  await assert.rejects(
    () => win.crit.shared.waitForSession({ intervalMs: 5, maxWaitMs: 20 }),
    /timed out/,
  );
});

test('waitForSession: AbortSignal aborts mid-poll', async () => {
  globalThis.fetch = async (_url, opts) => {
    if (opts && opts.signal && opts.signal.aborted) {
      const e = new Error('aborted'); e.name = 'AbortError'; throw e;
    }
    return { status: 503 };
  };
  const win = {};
  const doc = { cookie: '' };
  new Function('window', 'document', src)(win, doc);
  const ac = new AbortController();
  const p = win.crit.shared.waitForSession({ intervalMs: 50, signal: ac.signal });
  setTimeout(() => ac.abort(), 5);
  await assert.rejects(() => p, (e) => e && e.name === 'AbortError');
});

// ===== installSidebarResize =====
// Pure-math + behavioural tests for the shared sidebar/panel resize helper.
// The helper owns pointer capture, the body.sidebar-resizing class (cursor
// lock — design-mode used to flicker without it), persistence on pointerup,
// and min clamping. All four are pinned below.
//
// Pure math via computeResizeDelta first, then DOM-level behaviour through
// a minimal element/event stub (jsdom-free for speed and parity with the
// other tests in this file).
test('computeResizeDelta: right-edge handle, drag right grows the panel', () => {
  // edge=right, dir=+1; dx=+100 -> w = 400 + 100 = 500
  assert.equal(shared.computeResizeDelta(400, 1000, 1100, 'right', 200), 500);
});

test('computeResizeDelta: left-edge handle, drag left grows the panel', () => {
  // edge=left, dir=-1; dx=-100 -> delta=+100 -> w=500
  assert.equal(shared.computeResizeDelta(400, 1000, 900, 'left', 200), 500);
});

test('computeResizeDelta: clamps to min', () => {
  // left edge, dragging right shrinks; w would be -600, clamps at 200
  assert.equal(shared.computeResizeDelta(400, 1000, 2000, 'left', 200), 200);
});

test('computeResizeDelta: NO upper clamp', () => {
  assert.equal(shared.computeResizeDelta(400, 1000, -1000, 'left', 200), 2400);
});

test('computeResizeDelta: default min is 200', () => {
  assert.equal(shared.computeResizeDelta(100, 0, 1000, 'left'), 200);
});

// ----- DOM-level behaviour -----
function makeResizeSandbox(panelWidth) {
  function classList() {
    const set = new Set();
    return {
      _set: set,
      add(...c) { c.forEach((x) => set.add(x)); },
      remove(...c) { c.forEach((x) => set.delete(x)); },
      contains(c) { return set.has(c); },
    };
  }
  function makeEl() {
    return {
      _listeners: {},
      classList: classList(),
      style: {},
      addEventListener(evt, cb) {
        (this._listeners[evt] = this._listeners[evt] || []).push(cb);
      },
      removeEventListener(evt, cb) {
        const arr = this._listeners[evt] || [];
        const i = arr.indexOf(cb);
        if (i >= 0) arr.splice(i, 1);
      },
      dispatch(type, props) {
        const ev = Object.assign({ type, preventDefault() {} }, props || {});
        (this._listeners[type] || []).slice().forEach((cb) => cb(ev));
      },
      setPointerCapture() {},
      releasePointerCapture() {},
      getBoundingClientRect() {
        // Panel bounding rect: width comes from style if set; default to seed.
        const w = parseFloat(this.style.width);
        return { width: Number.isFinite(w) ? w : panelWidth };
      },
    };
  }
  const handle = makeEl();
  const panel = makeEl();
  const body = makeEl();
  const win = {};
  const doc = { cookie: '', body };
  new Function('window', 'document', src)(win, doc);
  return { shared: win.crit.shared, handle, panel, body, doc };
}

test('installSidebarResize: pointerdown adds body.sidebar-resizing + handle.dragging', () => {
  const { shared: s, handle, panel, body } = makeResizeSandbox(400);
  s.installSidebarResize(handle, panel, { settingKey: 'k', min: 200, edge: 'left' });
  handle.dispatch('pointerdown', { button: 0, pointerId: 1, clientX: 1000 });
  assert.equal(body.classList.contains('sidebar-resizing'), true);
  assert.equal(handle.classList.contains('dragging'), true);
});

test('installSidebarResize: pointerup removes body.sidebar-resizing', () => {
  const { shared: s, handle, panel, body } = makeResizeSandbox(400);
  s.installSidebarResize(handle, panel, { settingKey: 'k', min: 200, edge: 'left' });
  handle.dispatch('pointerdown', { button: 0, pointerId: 1, clientX: 1000 });
  handle.dispatch('pointerup', { pointerId: 1, clientX: 900 });
  assert.equal(body.classList.contains('sidebar-resizing'), false);
  assert.equal(handle.classList.contains('dragging'), false);
});

test('installSidebarResize: pointermove updates panel.style.width (left edge grows on drag-left)', () => {
  const { shared: s, handle, panel } = makeResizeSandbox(400);
  s.installSidebarResize(handle, panel, { settingKey: 'k', min: 200, edge: 'left' });
  handle.dispatch('pointerdown', { button: 0, pointerId: 1, clientX: 1000 });
  handle.dispatch('pointermove', { pointerId: 1, clientX: 900 });
  assert.equal(panel.style.width, '500px');
});

test('installSidebarResize: min is respected during drag', () => {
  const { shared: s, handle, panel } = makeResizeSandbox(400);
  s.installSidebarResize(handle, panel, { settingKey: 'k', min: 200, edge: 'left' });
  handle.dispatch('pointerdown', { button: 0, pointerId: 1, clientX: 1000 });
  handle.dispatch('pointermove', { pointerId: 1, clientX: 5000 }); // would be -3600
  assert.equal(panel.style.width, '200px');
});

test('installSidebarResize: width persisted via setSetting on pointerup', () => {
  const { shared: s, handle, panel, doc } = makeResizeSandbox(400);
  s.installSidebarResize(handle, panel, { settingKey: 'design_commentsPanelWidth', min: 200, edge: 'left' });
  handle.dispatch('pointerdown', { button: 0, pointerId: 1, clientX: 1000 });
  handle.dispatch('pointermove', { pointerId: 1, clientX: 850 }); // w=550
  handle.dispatch('pointerup', { pointerId: 1, clientX: 850 });
  // The cookie write should contain the rounded width.
  const m = doc.cookie.match(/^crit-settings=([^;]*)/);
  assert.ok(m, 'crit-settings cookie was written');
  const parsed = JSON.parse(decodeURIComponent(m[1]));
  assert.equal(parsed.design_commentsPanelWidth, 550);
});

test('installSidebarResize: applies persisted width on install', () => {
  const { shared: s, handle, panel, doc } = makeResizeSandbox(400);
  doc.cookie = 'crit-settings=' + encodeURIComponent(JSON.stringify({ k: 612 }));
  s.installSidebarResize(handle, panel, { settingKey: 'k', min: 200, edge: 'left' });
  assert.equal(panel.style.width, '612px');
});

test('installSidebarResize: ignores non-primary mouse buttons', () => {
  const { shared: s, handle, panel, body } = makeResizeSandbox(400);
  s.installSidebarResize(handle, panel, { settingKey: 'k', min: 200, edge: 'left' });
  handle.dispatch('pointerdown', { button: 2, pointerId: 1, clientX: 1000 });
  assert.equal(body.classList.contains('sidebar-resizing'), false);
  assert.equal(panel.style.width, undefined);
});

test('installSidebarResize: teardown clears listeners and class state', () => {
  const { shared: s, handle, panel, body } = makeResizeSandbox(400);
  const off = s.installSidebarResize(handle, panel, { settingKey: 'k', min: 200, edge: 'left' });
  handle.dispatch('pointerdown', { button: 0, pointerId: 1, clientX: 1000 });
  off();
  assert.equal(body.classList.contains('sidebar-resizing'), false);
  // Subsequent pointerdown is a no-op.
  handle.dispatch('pointerdown', { button: 0, pointerId: 1, clientX: 1000 });
  assert.equal(body.classList.contains('sidebar-resizing'), false);
});
