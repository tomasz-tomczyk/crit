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

test('fetchJSON throws on !response.ok', async () => {
  const origFetch = globalThis.fetch;
  globalThis.fetch = async () => ({ ok: false, status: 500, text: async () => 'boom', headers: { get: () => '' } });
  try {
    await assert.rejects(() => shared.fetchJSON('/x'), /500/);
  } finally {
    globalThis.fetch = origFetch;
  }
});
