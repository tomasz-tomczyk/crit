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
