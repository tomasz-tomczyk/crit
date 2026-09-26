'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

// Fresh module per test: the highlight cache is module state.
function load() {
  delete require.cache[require.resolve('../crit-code-highlight.js')];
  return require('../crit-code-highlight.js');
}

// Fake Pierre worker pool: "tokenizes" every word into a coloured span and
// fails for languages it does not know, like the real pool.
function fakePool(known) {
  const results = new Map();
  const calls = [];
  return {
    calls,
    primeFileHighlightCache(file) {
      calls.push(file.lang);
      if (!known.includes(file.lang)) return Promise.reject(new Error('unknown language'));
      const lines = file.contents.split('\n').map(line => ({
        type: 'element', tagName: 'div', properties: {},
        children: line ? [{
          type: 'element', tagName: 'span',
          properties: { style: '--diffs-token-dark:#fff;--diffs-token-light:#000' },
          children: [{ type: 'text', value: line }],
        }] : [],
      }));
      results.set(file.cacheKey, { result: { code: lines } });
      return Promise.resolve();
    },
    getFileResultCache(file) { return results.get(file.cacheKey); },
    evictFileFromCache(key) { results.delete(key); },
  };
}

test('prime tokenizes in the pool; lines() then serves highlighted HTML per line', async () => {
  const ch = load();
  const pool = fakePool(['go']);
  ch.configure({ pool: () => pool });
  const code = 'func a() {}\nreturn <x>\n';
  assert.equal(ch.lines(code, 'go'), null, 'nothing before priming');
  await ch.prime([{ code, lang: 'go' }]);
  assert.deepEqual(ch.lines(code, 'go'), [
    '<span style="--diffs-token-dark:#fff;--diffs-token-light:#000">func a() {}</span>',
    '<span style="--diffs-token-dark:#fff;--diffs-token-light:#000">return &lt;x&gt;</span>',
  ]);
  assert.match(ch.html(code, 'go'), /return &lt;x&gt;<\/span>\n$/);
});

test('language names are normalized and each block is tokenized once', async () => {
  const ch = load();
  const pool = fakePool(['go']);
  ch.configure({ pool: () => Promise.resolve(pool) });
  await ch.prime([{ code: 'x\n', lang: 'Go' }, { code: 'x\n', lang: 'go {.numberLines}' }]);
  await ch.prime([{ code: 'x\n', lang: 'go' }]);
  assert.deepEqual(pool.calls, ['go']);
  assert.ok(ch.lines('x\n', 'GO'));
});

test('unknown languages and a missing pool fall back to plain text', async () => {
  const ch = load();
  ch.configure({ pool: () => fakePool([]) });
  await ch.prime([{ code: 'a\n', lang: 'klingon' }]);
  assert.equal(ch.lines('a\n', 'klingon'), null);
  assert.equal(ch.html('a\n', 'klingon'), '', 'markdown-it escapes it');

  const bare = load();
  await bare.prime([{ code: 'a\n', lang: 'go' }]);
  assert.equal(bare.lines('a\n', 'go'), null);
});

test('linesFromResult drops the empty line after a trailing newline and escapes text', () => {
  const ch = load();
  const code = [
    { type: 'element', tagName: 'div', properties: {}, children: [{ type: 'text', value: 'a & b' }] },
    { type: 'element', tagName: 'div', properties: {}, children: [] },
  ];
  assert.deepEqual(ch.linesFromResult({ code }, 'a & b\n'), ['a &amp; b']);
  assert.deepEqual(ch.linesFromResult({ code }, 'a & b'), ['a &amp; b', ''], 'no trailing newline: keep every line');
});

test('an old highlight cannot refill the cache after a theme or worker change', async () => {
  const ch = load();
  let finish;
  const oldPool = fakePool(['go']);
  const oldPrime = oldPool.primeFileHighlightCache;
  oldPool.primeFileHighlightCache = file => new Promise(resolve => {
    finish = () => { oldPrime(file); resolve(); };
  });
  ch.configure({ pool: () => oldPool });
  const pending = ch.prime([{ code: 'old\n', lang: 'go' }]);
  await Promise.resolve();
  ch.configure({ pool: () => fakePool(['go']) });
  finish();
  await pending;
  assert.equal(ch.lines('old\n', 'go'), null);
  await ch.prime([{ code: 'old\n', lang: 'go' }]);
  assert.ok(ch.lines('old\n', 'go'));
});

test('a preserved mounted fence follows the new pool while old tokenization is pending', async () => {
  const ch = load();
  const attrs = new Map();
  const code = {
    className: 'language-go', textContent: 'old\n', innerHTML: '',
    classList: { add() {} },
    setAttribute: (key, value) => attrs.set(key, value),
    removeAttribute: key => attrs.delete(key),
  };
  const root = { querySelectorAll: () => attrs.has('data-crit-code') ? [] : [code] };
  global.document = { body: root, querySelectorAll: () => attrs.has('data-crit-code') ? [code] : [] };
  try {
    let finish;
    const oldPool = fakePool(['go']);
    const oldPrime = oldPool.primeFileHighlightCache;
    oldPool.primeFileHighlightCache = file => new Promise(resolve => {
      finish = () => { oldPrime(file); resolve(); };
    });
    ch.configure({ pool: () => oldPool });
    await Promise.resolve();
    const currentPool = fakePool(['go']);
    currentPool.getFileResultCache = () => ({ result: { code: [{ type: 'text', value: 'current' }, { type: 'text', value: '' }] } });
    ch.configure({ pool: () => currentPool });
    await ch.prime([{ code: 'old\n', lang: 'go' }]);
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(code.innerHTML, 'current');
    finish();
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(code.innerHTML, 'current', 'late old-theme tokens cannot replace current tokens');
    assert.equal(attrs.get('data-crit-code'), 'highlighted');
  } finally { delete global.document; }
});
