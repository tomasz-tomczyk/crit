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

test('pathCompare matches GitHub PR byte order (foo.go before foo_test.go)', () => {
  assert.ok(shared.pathCompare('foo.go', 'foo_test.go') < 0);
  assert.ok(shared.pathCompare('foo_test.go', 'foo.go') > 0);
  assert.equal(shared.pathCompare('pkg/foo.go', 'pkg/foo_test.go'), -49);
  assert.equal(shared.pathCompare('a/b', 'a/b'), 0);
});

test('escapeHTML escapes <, >, &, ", and single quotes', () => {
  assert.equal(shared.escapeHTML('<a href="x">&</a>'),
    '&lt;a href=&quot;x&quot;&gt;&amp;&lt;/a&gt;');
  assert.equal(shared.escapeHTML("it's"), 'it&#39;s');
});

test('escapeHTML returns empty string for null/undefined', () => {
  assert.equal(shared.escapeHTML(null), '');
  assert.equal(shared.escapeHTML(undefined), '');
});

test('applyProjectPromptTrustUI keeps finish button enabled so trust dialog can open', () => {
  const btn = { disabled: false, textContent: 'Approve', title: '' };
  shared.applyProjectPromptTrustUI({ project_prompts_untrusted: true }, btn);
  assert.equal(btn.disabled, false);
  assert.equal(btn.title, 'Review project prompts before finishing');
  shared.applyProjectPromptTrustUI({ project_prompts_untrusted: false }, btn);
  assert.equal(btn.disabled, false);
  assert.equal(btn.title, '');
});

test('applyProjectPromptTrustUI does not re-enable while waiting', () => {
  const btn = { disabled: true, textContent: 'Waiting...', title: 'Trust project prompts before finishing' };
  shared.applyProjectPromptTrustUI({ project_prompts_untrusted: false }, btn);
  assert.equal(btn.disabled, true);
});

test('applyProjectPromptTrustUI keeps waiting button disabled when prompts become untrusted', () => {
  const btn = { disabled: true, textContent: 'Waiting...', title: '' };
  shared.applyProjectPromptTrustUI({ project_prompts_untrusted: true }, btn);
  assert.equal(btn.disabled, true);
  assert.equal(btn.title, 'Review project prompts before finishing');
});

test('getCookie reads document.cookie and URL-decodes the value', () => {
  sandbox.document.cookie = 'crit-settings=' + encodeURIComponent('{"theme":"dark"}') + '; other=x';
  assert.equal(shared.getCookie('crit-settings'), '{"theme":"dark"}');
  assert.equal(shared.getCookie('missing'), null);
});

test('setCookie writes 1-year max-age, SameSite=Strict, URL-encoded value', () => {
  // Persistence policy must match app.js: live-mode prefs (theme,
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
  shared.setSetting('live_commentsPanelOpen', false);
  shared.setSetting('theme', 'dark');
  // The browser would echo back only the last write (one cookie name);
  // model that by extracting it from the assigned string.
  const m = sandbox.document.cookie.match(/^crit-settings=([^;]*)/);
  assert.ok(m, 'cookie was written');
  sandbox.document.cookie = 'crit-settings=' + m[1];
  assert.equal(shared.getSetting('live_commentsPanelOpen', true), false);
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
// (app.js) and live-mode (live-mode.js) call this so the resolved-state
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
      _attrs: {},
      setAttribute(k, v) { this._attrs[k] = v; },
      getAttribute(k) { return this._attrs[k] || null; },
      classList: {
        _set: new Set(),
        add(...c) { c.forEach((x) => this._set.add(x)); },
        remove(...c) { c.forEach((x) => this._set.delete(x)); },
        contains(c) { return this._set.has(c); },
      },
      querySelector() { return null; },
      querySelectorAll() { return []; },
    };
  }
  const copyLabel = makeEl();
  copyLabel.textContent = 'Copy';
  const clipEl = makeEl();
  clipEl._attrs['aria-label'] = 'Copy prompt to clipboard';
  clipEl.querySelector = function (sel) {
    if (sel === '.copy-label') return copyLabel;
    return null;
  };
  const els = {
    waitingDialog: makeEl(),
    waitingHeading: makeEl(),
    waitingMessage: makeEl(),
    waitingClipboard: clipEl,
    waitingPrompt: makeEl(),
    promptPreview: makeEl(),
    _copyLabel: copyLabel,
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
  assert.equal(els._copyLabel.textContent, 'Copy');
  assert.equal(els.waitingClipboard.classList.contains('copied'), false);
  assert.equal(els.waitingClipboard.getAttribute('aria-label'), 'Copy prompt to clipboard');
  assert.equal(approvedArg, 'ok-prompt');
  assert.equal(waitingCalled, false);
});

test('runFinishReview resets copy button via .copy-label span, not textContent (regression)', async () => {
  const fetch = async () => ({ ok: true, json: async () => ({ approved: true, prompt: 'p' }) });
  const { shared: s, els } = makeFinishSandbox(fetch, { writeText: async () => {} });
  // Simulate a prior "Copied" state
  els._copyLabel.textContent = 'Copied';
  els.waitingClipboard.classList.add('copied');
  els.waitingClipboard.setAttribute('aria-label', 'Copied');

  await s.runFinishReview({});

  // The reset must target the .copy-label span, not clobber the parent's textContent
  assert.equal(els._copyLabel.textContent, 'Copy');
  assert.equal(els.waitingClipboard.classList.contains('copied'), false);
  assert.equal(els.waitingClipboard.getAttribute('aria-label'), 'Copy prompt to clipboard');
  // querySelector must still work (DOM structure preserved)
  assert.ok(els.waitingClipboard.querySelector('.copy-label'), '.copy-label span still accessible');
});

test('runFinishReview not-approved path: leaves approved class off + uses prompt', async () => {
  const fetch = async () => ({
    ok: true,
    json: async () => ({
      approved: false,
      prompt: 'The review finished with 2 unresolved comments.\n\n[{"body":"fix"}]\n\nAddress each comment.\n\nWhen you\'re done, run:\n\n  crit test.md',
    }),
  });
  const { shared: s, els } = makeFinishSandbox(fetch, { writeText: async () => {} });
  let waitingCalled = false;
  const result = await s.runFinishReview({ onWaiting: () => { waitingCalled = true; } });
  assert.equal(result.approved, false);
  assert.match(result.prompt, /Address each comment/);
  assert.equal(els.waitingHeading.textContent, 'Review Complete');
  assert.match(els.waitingMessage.textContent, /wasn't listening/);
  assert.equal(els.waitingDialog.classList.contains('approved'), false);
  assert.equal(waitingCalled, true);
});

test('runFinishReview not-approved path: falls back when prompt missing', async () => {
  const fetch = async () => ({ ok: true, json: async () => ({ approved: false }) });
  const { shared: s, els } = makeFinishSandbox(fetch, { writeText: async () => {} });
  const result = await s.runFinishReview({ onWaiting: () => {} });
  assert.equal(result.prompt, 'I reviewed the changes, no feedback, good to go!');
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

// ----- close_on_approve_after_ms (auto-close after approve) -----
// A richer DOM stub than makeFinishSandbox: elements form a real parent/child
// tree (so ensureCloseCancelBtn's `messageEl.parentNode.insertBefore(...)`
// works, and a dynamically-inserted #waitingCloseCancel can be found by a
// later document.getElementById lookup) plus fake setTimeout/clearTimeout so
// the countdown can be driven deterministically without real delays.
function makeAutoCloseSandbox(fetchImpl) {
  const focusLog = [];
  function makeNode(tag) {
    return {
      tagName: (tag || 'div').toUpperCase(),
      id: '',
      style: {},
      _attrs: {},
      _children: [],
      textContent: '',
      innerHTML: '',
      offsetWidth: 0,
      onclick: null,
      parentNode: null,
      nextSibling: null,
      classList: {
        _set: new Set(),
        add(...c) { c.forEach((x) => this._set.add(x)); },
        remove(...c) { c.forEach((x) => this._set.delete(x)); },
        contains(c) { return this._set.has(c); },
      },
      setAttribute(k, v) { this._attrs[k] = v; },
      getAttribute(k) { return Object.prototype.hasOwnProperty.call(this._attrs, k) ? this._attrs[k] : null; },
      removeAttribute(k) { delete this._attrs[k]; },
      focus() { focusLog.push(this); },
      querySelector() { return null; },
      querySelectorAll() { return []; },
      appendChild(child) {
        child.parentNode = this;
        this._children.push(child);
        relink(this);
        return child;
      },
      insertBefore(newNode, referenceNode) {
        let idx = this._children.indexOf(referenceNode);
        if (idx === -1) idx = this._children.length;
        this._children.splice(idx, 0, newNode);
        newNode.parentNode = this;
        relink(this);
        return newNode;
      },
    };
  }
  function relink(parent) {
    const kids = parent._children;
    for (let i = 0; i < kids.length; i++) kids[i].nextSibling = kids[i + 1] || null;
  }
  function findById(node, id) {
    if (!node) return null;
    if (node.id === id) return node;
    for (const child of node._children || []) {
      const found = findById(child, id);
      if (found) return found;
    }
    return null;
  }

  const messageEl = makeNode('p'); messageEl.id = 'waitingMessage';
  const headingEl = makeNode('h3'); headingEl.id = 'waitingHeading';
  const header = makeNode('div'); header.id = 'waitingHeader';
  header.appendChild(headingEl);
  header.appendChild(messageEl);

  const copyLabel = makeNode('span'); copyLabel.textContent = 'Copy';
  const clipEl = makeNode('button'); clipEl.id = 'waitingClipboard';
  clipEl._attrs['aria-label'] = 'Copy prompt to clipboard';
  clipEl.querySelector = (sel) => (sel === '.copy-label' ? copyLabel : null);

  const dialog = makeNode('div'); dialog.id = 'waitingDialog';
  dialog.appendChild(header);

  const promptEl = makeNode('div'); promptEl.id = 'waitingPrompt';
  const previewEl = makeNode('span'); previewEl.id = 'promptPreview';
  const copyStatusEl = makeNode('div'); copyStatusEl.id = 'copyStatus';

  const root = makeNode('body');
  root.appendChild(dialog);
  root.appendChild(promptEl);
  root.appendChild(previewEl);
  root.appendChild(clipEl);
  root.appendChild(copyStatusEl);

  const doc = {
    cookie: '',
    getElementById: (id) => findById(root, id),
    createElement: (tag) => makeNode(tag),
  };

  // Deterministic fake clock: setTimeout/clearTimeout only (matches
  // scheduleAutoClose, which never uses setInterval).
  const timers = [];
  let now = 0;
  const fakeSetTimeout = (cb, ms) => { const t = { at: now + (ms || 0), cb, fired: false, cancelled: false }; timers.push(t); return t; };
  const fakeClearTimeout = (t) => { if (t) t.cancelled = true; };
  function flush(ms) {
    now += (ms || 0);
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

  const win = {
    closed: false,
    close: () => { win.closeCalls = (win.closeCalls || 0) + 1; },
    closeCalls: 0,
  };
  win.navigator = { clipboard: { writeText: async () => {} } };
  const fn = new Function(
    'window', 'document', 'setTimeout', 'clearTimeout',
    src + '\nreturn window;',
  );
  fn(win, doc, fakeSetTimeout, fakeClearTimeout);
  globalThis.fetch = fetchImpl;
  globalThis.navigator = win.navigator;
  return {
    shared: win.crit.shared,
    win,
    doc,
    els: { messageEl, headingEl, dialog, promptEl, previewEl, clipEl, copyStatusEl },
    focusLog,
    flush,
  };
}

test('scheduleAutoClose: counts down then calls window.close() after the configured delay', () => {
  const { shared: s, win, els, flush } = makeAutoCloseSandbox(async () => ({ ok: true, json: async () => ({}) }));
  s.scheduleAutoClose(3000, els.messageEl);
  assert.equal(els.messageEl.textContent, 'Closing in 3s…');
  assert.equal(win.closeCalls, 0);
  flush(1000);
  assert.equal(els.messageEl.textContent, 'Closing in 2s…');
  flush(1000);
  assert.equal(els.messageEl.textContent, 'Closing in 1s…');
  assert.equal(win.closeCalls, 0);
  flush(1000);
  assert.equal(win.closeCalls, 1, 'window.close() called once the countdown reaches zero');
});

test('scheduleAutoClose: negative or non-numeric ms is a no-op (disabled)', () => {
  const { shared: s, win, els, flush } = makeAutoCloseSandbox(async () => ({ ok: true, json: async () => ({}) }));
  s.scheduleAutoClose(-1, els.messageEl);
  s.scheduleAutoClose(undefined, els.messageEl);
  s.scheduleAutoClose('not-a-number', els.messageEl);
  flush(60000);
  assert.equal(win.closeCalls, 0);
  assert.equal(els.messageEl.textContent, '');
});

test('scheduleAutoClose: Cancel button stops the countdown, hides itself, and no close happens', () => {
  const { shared: s, win, els, doc, flush } = makeAutoCloseSandbox(async () => ({ ok: true, json: async () => ({}) }));
  s.scheduleAutoClose(3000, els.messageEl);
  const cancelBtn = doc.getElementById('waitingCloseCancel');
  assert.ok(cancelBtn, 'Cancel button was created');
  assert.equal(cancelBtn.style.display, '');

  flush(1000); // one tick in, then cancel
  cancelBtn.onclick();

  assert.equal(cancelBtn.style.display, 'none');
  assert.equal(els.messageEl.style.display, 'none');
  assert.equal(els.messageEl.textContent, '');

  flush(60000); // no further scheduled close, even well past the original delay
  assert.equal(win.closeCalls, 0);
});

test('scheduleAutoClose: reuses the same Cancel button across approvals instead of duplicating it', () => {
  const { shared: s, doc } = makeAutoCloseSandbox(async () => ({ ok: true, json: async () => ({}) }));
  s.scheduleAutoClose(5000, doc.getElementById('waitingMessage'));
  const first = doc.getElementById('waitingCloseCancel');
  s.scheduleAutoClose(5000, doc.getElementById('waitingMessage'));
  const second = doc.getElementById('waitingCloseCancel');
  assert.equal(first, second, 'Cancel button element is reused, not recreated');
});

test('scheduleAutoClose: after window.close(), shows "you can close this tab" if the tab is still open', () => {
  const { shared: s, els, doc, flush } = makeAutoCloseSandbox(async () => ({ ok: true, json: async () => ({}) }));
  s.scheduleAutoClose(1000, els.messageEl);
  flush(1000); // triggers window.close() (a no-op in this stub — tab "stays open")
  flush(50);   // the post-close fallback message tick
  assert.equal(els.messageEl.textContent, 'Approved — you can close this tab');
  // The spent countdown must not leave an idle timer or a dead Cancel behind,
  // and role="timer" is aria-live "off" so the new text needs a real announce.
  assert.equal(els.messageEl.getAttribute('role'), null);
  assert.equal(doc.getElementById('waitingCloseCancel').style.display, 'none');
  assert.equal(els.copyStatusEl.textContent, 'Approved — you can close this tab.');
});

test('scheduleAutoClose: after window.close(), skips fallback message when the tab actually closed', () => {
  const { shared: s, win, els, flush } = makeAutoCloseSandbox(async () => ({ ok: true, json: async () => ({}) }));
  win.close = () => { win.closeCalls = (win.closeCalls || 0) + 1; win.closed = true; };
  s.scheduleAutoClose(1000, els.messageEl);
  flush(1000);
  flush(50);
  assert.equal(win.closeCalls, 1);
  assert.notEqual(els.messageEl.textContent, 'Approved — you can close this tab');
  assert.notEqual(els.copyStatusEl.textContent, 'Approved — you can close this tab.');
});

test('scheduleAutoClose: announcement says "1 second", not "1 seconds"', () => {
  const { shared: s, els } = makeAutoCloseSandbox(async () => ({ ok: true, json: async () => ({}) }));
  s.scheduleAutoClose(1000, els.messageEl);
  assert.equal(
    els.copyStatusEl.textContent,
    'Approved. This tab closes in 1 second. Press Cancel to stay.',
  );
});

test('runFinishReview: approved + close_on_approve_after_ms set schedules the countdown and eventual close', async () => {
  const fetch = async (url) => {
    if (url === '/api/finish') return { ok: true, json: async () => ({ approved: true, prompt: 'ok' }) };
    if (url === '/api/config') return { ok: true, json: async () => ({ close_on_approve_after_ms: 2000 }) };
    throw new Error('unexpected fetch ' + url);
  };
  const { shared: s, win, els, flush } = makeAutoCloseSandbox(fetch);
  const result = await s.runFinishReview({});
  assert.equal(result.approved, true);
  assert.equal(els.messageEl.textContent, 'Closing in 2s…');
  // Ticks chain one setTimeout at a time (each scheduled relative to "now"
  // at fire time), so advance the fake clock one tick at a time rather than
  // jumping straight to the total delay.
  flush(1000);
  assert.equal(els.messageEl.textContent, 'Closing in 1s…');
  flush(1000);
  assert.equal(win.closeCalls, 1);
});

test('runFinishReview: approved + no close_on_approve_after_ms in config skips auto-close entirely', async () => {
  const fetch = async (url) => {
    if (url === '/api/finish') return { ok: true, json: async () => ({ approved: true, prompt: 'ok' }) };
    if (url === '/api/config') return { ok: true, json: async () => ({}) }; // key absent
    throw new Error('unexpected fetch ' + url);
  };
  const { shared: s, win, els, flush } = makeAutoCloseSandbox(fetch);
  await s.runFinishReview({});
  // approved path already hides the message when auto-close is disabled.
  assert.equal(els.messageEl.style.display, 'none');
  flush(60000);
  assert.equal(win.closeCalls, 0);
});

test('runFinishReview: not-approved path never schedules an auto-close, regardless of config', async () => {
  const fetch = async (url) => {
    if (url === '/api/finish') return { ok: true, json: async () => ({ approved: false, prompt: 'p' }) };
    if (url === '/api/config') return { ok: true, json: async () => ({ close_on_approve_after_ms: 500 }) };
    throw new Error('unexpected fetch ' + url);
  };
  const { shared: s, win, flush } = makeAutoCloseSandbox(fetch);
  await s.runFinishReview({ onWaiting: () => {} });
  flush(60000);
  assert.equal(win.closeCalls, 0, '/api/config is never even consulted on the not-approved path');
});

// ----- clearAutoCloseTimers (dismissing the waiting overlay) -----
// Regression: "Back to editing" / backdrop click only removed .active from the
// overlay, so a running countdown kept ticking behind the dismissed dialog and
// eventually called window.close() out from under the reviewer.

test('clearAutoCloseTimers: stops a running countdown so window.close() never fires', () => {
  const { shared: s, win, els, flush } = makeAutoCloseSandbox(async () => ({ ok: true, json: async () => ({}) }));
  s.scheduleAutoClose(3000, els.messageEl);
  flush(1000);
  assert.equal(els.messageEl.textContent, 'Closing in 2s…');

  s.clearAutoCloseTimers();

  flush(60000);
  assert.equal(win.closeCalls, 0, 'no close after the countdown was cleared');
});

test('clearAutoCloseTimers: hides the Cancel button and drops role="timer"', () => {
  const { shared: s, els, doc } = makeAutoCloseSandbox(async () => ({ ok: true, json: async () => ({}) }));
  s.scheduleAutoClose(3000, els.messageEl);
  assert.equal(els.messageEl.getAttribute('role'), 'timer');
  const cancelBtn = doc.getElementById('waitingCloseCancel');
  assert.equal(cancelBtn.style.display, '');

  s.clearAutoCloseTimers();

  assert.equal(cancelBtn.style.display, 'none');
  assert.equal(els.messageEl.getAttribute('role'), null);
});

test('clearAutoCloseTimers: is an idempotent no-op when no countdown is running', () => {
  const { shared: s, win, els, flush } = makeAutoCloseSandbox(async () => ({ ok: true, json: async () => ({}) }));
  s.clearAutoCloseTimers();
  s.clearAutoCloseTimers();
  assert.equal(els.messageEl.getAttribute('role'), null);
  flush(60000);
  assert.equal(win.closeCalls, 0);
});

test('clearAutoCloseTimers: a cleared countdown does not resume when a later one is scheduled', () => {
  const { shared: s, win, els, flush } = makeAutoCloseSandbox(async () => ({ ok: true, json: async () => ({}) }));
  s.scheduleAutoClose(3000, els.messageEl);
  s.clearAutoCloseTimers();
  s.scheduleAutoClose(2000, els.messageEl);
  flush(1000);
  assert.equal(els.messageEl.textContent, 'Closing in 1s…');
  flush(1000);
  assert.equal(win.closeCalls, 1, 'only the newest countdown closes the tab, and only once');
  flush(60000);
  assert.equal(win.closeCalls, 1);
});

test('runFinishReview: a not-approved finish clears a countdown left over from a prior approval', async () => {
  let approved = true;
  const fetch = async (url) => {
    if (url === '/api/finish') return { ok: true, json: async () => ({ approved, prompt: 'p' }) };
    if (url === '/api/config') return { ok: true, json: async () => ({ close_on_approve_after_ms: 3000 }) };
    throw new Error('unexpected fetch ' + url);
  };
  const { shared: s, win, els, doc, flush } = makeAutoCloseSandbox(fetch);
  await s.runFinishReview({});
  assert.equal(els.messageEl.textContent, 'Closing in 3s…');

  approved = false;
  await s.runFinishReview({ onWaiting: () => {} });
  assert.match(els.messageEl.textContent, /^Agent notified\./);
  assert.equal(els.messageEl.getAttribute('role'), null);
  assert.equal(doc.getElementById('waitingCloseCancel').style.display, 'none');

  flush(60000);
  assert.equal(win.closeCalls, 0, 'the inherited countdown never closes the tab');
});

// ----- auto-close accessibility -----

test('scheduleAutoClose: focuses Cancel and announces the countdown once, not per tick', () => {
  const { shared: s, els, doc, focusLog, flush } = makeAutoCloseSandbox(async () => ({ ok: true, json: async () => ({}) }));
  s.scheduleAutoClose(3000, els.messageEl);

  assert.equal(focusLog[focusLog.length - 1], doc.getElementById('waitingCloseCancel'));
  assert.equal(
    els.copyStatusEl.textContent,
    'Approved. This tab closes in 3 seconds. Press Cancel to stay.',
  );

  // The per-second text lives on a role="timer" element (implicit aria-live
  // "off") and the live region is left untouched by subsequent ticks.
  assert.equal(els.messageEl.getAttribute('role'), 'timer');
  assert.equal(els.messageEl.getAttribute('aria-live'), null);
  flush(1000);
  assert.equal(els.messageEl.textContent, 'Closing in 2s…');
  assert.equal(
    els.copyStatusEl.textContent,
    'Approved. This tab closes in 3 seconds. Press Cancel to stay.',
  );
});

test('scheduleAutoClose: Cancel announces and moves focus off the button it hides', () => {
  const { shared: s, els, doc, focusLog } = makeAutoCloseSandbox(async () => ({ ok: true, json: async () => ({}) }));
  s.scheduleAutoClose(3000, els.messageEl);
  doc.getElementById('waitingCloseCancel').onclick();

  assert.equal(focusLog[focusLog.length - 1], els.clipEl, 'focus lands on the Copy prompt button');
  assert.equal(els.copyStatusEl.textContent, 'Auto-close cancelled. This tab will stay open.');
  assert.equal(els.messageEl.getAttribute('role'), null);
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
// lock — live-mode used to flicker without it), persistence on pointerup,
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
  s.installSidebarResize(handle, panel, { settingKey: 'live_commentsPanelWidth', min: 200, edge: 'left' });
  handle.dispatch('pointerdown', { button: 0, pointerId: 1, clientX: 1000 });
  handle.dispatch('pointermove', { pointerId: 1, clientX: 850 }); // w=550
  handle.dispatch('pointerup', { pointerId: 1, clientX: 850 });
  // The cookie write should contain the rounded width.
  const m = doc.cookie.match(/^crit-settings=([^;]*)/);
  assert.ok(m, 'crit-settings cookie was written');
  const parsed = JSON.parse(decodeURIComponent(m[1]));
  assert.equal(parsed.live_commentsPanelWidth, 550);
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

// ---- Code font (--crit-font-mono override) ----

function makeCodeFontSandbox() {
  const props = {};
  const doc = {
    cookie: '',
    documentElement: {
      style: {
        setProperty(k, v) { props[k] = v; },
        removeProperty(k) { delete props[k]; },
      },
    },
  };
  const win = {};
  new Function('window', 'document', src + '\nreturn window;')(win, doc);
  return { shared: win.crit.shared, props, doc };
}

test('sanitizeCodeFont accepts a plain font-family list and trims it', () => {
  assert.equal(shared.sanitizeCodeFont("  'Fira Code', monospace  "), "'Fira Code', monospace");
  assert.equal(shared.sanitizeCodeFont('"Name \\"quoted\\"", monospace'), '"Name \\"quoted\\"", monospace');
  assert.equal(shared.sanitizeCodeFont(''), '');
  assert.equal(shared.sanitizeCodeFont(null), '');
});

test('sanitizeCodeFont rejects values that could escape the declaration', () => {
  // A `;` would end the declaration, `}` the rule, `@` start an at-rule, and
  // url()/expression() pull in external resources.
  assert.equal(shared.sanitizeCodeFont('monospace; background: red'), '');
  assert.equal(shared.sanitizeCodeFont('monospace} body {display:none'), '');
  assert.equal(shared.sanitizeCodeFont('@import "x"'), '');
  assert.equal(shared.sanitizeCodeFont('url(https://evil.example/f.woff)'), '');
  assert.equal(shared.sanitizeCodeFont('expression(alert(1))'), '');
  assert.equal(shared.sanitizeCodeFont('<script>'), '');
  assert.equal(shared.sanitizeCodeFont('A'.repeat(257)), '');
});

test('applyCodeFont sets --crit-font-code, and clears it for the default', () => {
  const { shared: s, props } = makeCodeFontSandbox();
  s.applyCodeFont("'Fira Code', monospace");
  assert.equal(props['--crit-font-code'], "'Fira Code', monospace");
  // Empty means "use theme.css's stack" — the override is removed, not
  // replaced with a copy of the default.
  s.applyCodeFont('');
  assert.equal('--crit-font-code' in props, false);
});

test('setCodeFont persists the sanitized stack and returns what was stored', () => {
  const { shared: s, props, doc } = makeCodeFontSandbox();
  assert.equal(s.setCodeFont("'IBM Plex Mono', monospace"), "'IBM Plex Mono', monospace");
  assert.match(doc.cookie, /crit-settings=/);
  assert.equal(s.getSetting('codeFont', ''), "'IBM Plex Mono', monospace");
  assert.equal(props['--crit-font-code'], "'IBM Plex Mono', monospace");

  // A rejected value falls back to the default rather than being stored raw.
  assert.equal(s.setCodeFont('monospace; color: red'), '');
  assert.equal(s.getSetting('codeFont', ''), '');
  assert.equal('--crit-font-code' in props, false);
});

test('applyCodeFontFromCookie restores the stored stack', () => {
  const { shared: s, props } = makeCodeFontSandbox();
  s.setSetting('codeFont', "'Cascadia Code', Consolas, monospace");
  s.applyCodeFontFromCookie();
  assert.equal(props['--crit-font-code'], "'Cascadia Code', Consolas, monospace");
});

test('CODE_FONT_PRESETS only includes always-available entries', () => {
  assert.equal(shared.CODE_FONT_PRESETS[0].id, 'default');
  assert.equal(shared.CODE_FONT_PRESETS[0].stack, '');
  assert.equal(shared.CODE_FONT_PRESETS.length, 2);
  shared.CODE_FONT_PRESETS.slice(1).forEach((p) => {
    assert.equal(shared.sanitizeCodeFont(p.stack), p.stack, p.id + ' must survive sanitization');
  });
});
