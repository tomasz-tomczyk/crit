'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const dom = require('../crit-pierre-dom.js');

test('range selectors keep old and new sides separate and do not invent rows', () => {
  const file = { path: 'a.go' };
  global.CSS = { escape: value => value };
  assert.deepEqual(dom.pierreLineSelectors(file, [{ start: 2, end: 3, old: true }], false, false, []), [
    ':host([data-crit-path="a.go"]) code[data-deletions] [data-content] > [data-line="2"]',
    ':host([data-crit-path="a.go"]) code[data-deletions] [data-content] > [data-line="3"]',
  ]);
  assert.match(dom.pierreLineSelectors(file, [{ start: 1, end: 1 }], false, true, [])[0], /code\[data-code\]/);
  delete global.CSS;
});

test('line lookup is contained in one renderer root and gracefully handles no host', () => {
  assert.equal(dom.pierreLineElement(null, 2, 'old'), null);
  const selectors = [];
  const root = { querySelector: selector => { selectors.push(selector); return null; } };
  dom.pierreLineElement({ querySelector: () => ({ shadowRoot: root }) }, 2, 'old');
  assert.equal(selectors[0], 'code[data-deletions] [data-line="2"]');
  assert.equal(selectors.length, 2);
});

test('compatibility styles use public spacing and separator variables', () => {
  assert.match(dom.unsafeCSS, /--diffs-gap-block: 0px/);
  assert.match(dom.unsafeCSS, /--diffs-bg-separator-override/);
  assert.match(dom.unsafeCSS, /:host\(\[data-crit-stub\]\)/);
});

test('documents and placeholders drop the empty code row; loading stubs keep theirs', () => {
  // The host only needs querySelector; answer with what the selector asks for.
  const host = classes => ({
    querySelector(selector) {
      const wantsDoc = selector.includes('.pierre-document') && classes.includes('pierre-document');
      const wantsPlaceholder = selector.includes('.diff-deleted-placeholder:not(.pierre-loading)') &&
        classes.includes('diff-deleted-placeholder') && !classes.includes('pierre-loading');
      return wantsDoc || wantsPlaceholder ? {} : null;
    },
  });
  assert.equal(dom.isFileLevelOnly(host(['pierre-document'])), true);
  assert.equal(dom.isFileLevelOnly(host(['diff-deleted-placeholder'])), true);
  assert.equal(dom.isFileLevelOnly(host(['diff-deleted-placeholder', 'pierre-loading'])), false);
  assert.equal(dom.isFileLevelOnly(host([])), false);
  // The rule that keeps Pierre from counting the empty row next to the content.
  assert.ok(dom.unsafeCSS.includes(':host([data-crit-document]) [data-content] > [data-line] { display: none; }'));
});
