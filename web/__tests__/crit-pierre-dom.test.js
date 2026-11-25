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
  // It shrinks the row to 1px: a 0px row stops Pierre measuring the document.
  assert.ok(dom.unsafeCSS.includes(':host([data-crit-document]) [data-content] > [data-line] { box-sizing: border-box; height: 1px;'));
  assert.ok(!dom.unsafeCSS.includes(':host([data-crit-document]) [data-content] > [data-line] { display: none; }'));
});

test('cached rendering retains quotes when the returned host attaches in the same task', async t => {
  const globals = ['CSSStyleSheet', 'CSS', 'Highlight', 'Range', 'document', 'NodeFilter'];
  const original = globals.map(key => Object.getOwnPropertyDescriptor(global, key));
  t.after(() => globals.forEach((key, index) => {
    if (original[index]) Object.defineProperty(global, key, original[index]);
    else delete global[key];
  }));
  global.CSSStyleSheet = class { replaceSync() {} };
  global.CSS = { highlights: new Map() };
  global.Highlight = class extends Set { constructor(...ranges) { super(ranges); } };
  global.Range = class {
    setStart(node, offset) { this.startContainer = node; this.startOffset = offset; }
    setEnd(node, offset) { this.endContainer = node; this.endOffset = offset; }
    toString() { return this.startContainer.textContent.slice(this.startOffset, this.endOffset); }
  };
  global.NodeFilter = { SHOW_TEXT: 4 };
  global.document = { createTreeWalker(line) {
    let visited = false;
    return { nextNode() { if (visited) return null; visited = true; return line.text; } };
  } };
  const line = { text: { textContent: 'const selectedWord = true;' } };
  const host = {
    dataset: {}, isConnected: false, querySelector: () => null,
    shadowRoot: { adoptedStyleSheets: [], querySelector: () => line, querySelectorAll: () => [] },
  };
  const quoted = [{ start: 1, end: 1, quote: 'selectedWord', offset: 6 }];
  const decorations = dom.createDecorations();

  // A cached FileDiff invokes onPostRender before its caller appends the
  // returned story chapter. Connection happens later in the same JS task.
  decorations.mount(host, 'example.js', quoted);
  host.isConnected = true;
  await Promise.resolve();
  assert.deepEqual([...(CSS.highlights.get('crit-quote') || [])].map(range => range.toString()), ['selectedWord']);

  // Closing/replacing the chapter releases its registered quote.
  decorations.unmount(host);
  host.isConnected = false;
  await Promise.resolve();
  assert.equal(CSS.highlights.has('crit-quote'), false);

  // A render abandoned before attachment must never leave a stale range.
  decorations.mount(host, 'example.js', quoted);
  decorations.unmount(host);
  await Promise.resolve();
  assert.equal(CSS.highlights.has('crit-quote'), false);

  // Replacing a whole story also removes hosts without individual unmounts.
  // Mounting its next unquoted file must clear the previous chapter's quote.
  host.isConnected = true;
  decorations.mount(host, 'example.js', quoted);
  await Promise.resolve();
  assert.equal(CSS.highlights.has('crit-quote'), true);
  host.isConnected = false;
  const replacement = { ...host, dataset: {}, isConnected: true };
  decorations.mount(replacement, 'unquoted.js', []);
  await Promise.resolve();
  assert.equal(CSS.highlights.has('crit-quote'), false);
});

test('a saved comment\'s quote can be found by point, looked up and flashed', async t => {
  const globals = ['CSSStyleSheet', 'CSS', 'Highlight', 'Range', 'document', 'NodeFilter'];
  const original = globals.map(key => Object.getOwnPropertyDescriptor(global, key));
  t.after(() => globals.forEach((key, index) => {
    if (original[index]) Object.defineProperty(global, key, original[index]);
    else delete global[key];
  }));
  t.mock.timers.enable({ apis: ['setTimeout'] });
  global.CSSStyleSheet = class { replaceSync() {} };
  global.CSS = { highlights: new Map() };
  global.Highlight = class extends Set { constructor(...ranges) { super(ranges); } };
  global.Range = class {
    setStart(node, offset) { this.startContainer = node; this.startOffset = offset; }
    setEnd(node, offset) { this.endContainer = node; this.endOffset = offset; }
    // Each quote sits on its own 20px-high row, 100px wide.
    getClientRects() { const top = this.startContainer.row * 20; return [{ left: 0, right: 100, top, bottom: top + 20 }]; }
  };
  global.NodeFilter = { SHOW_TEXT: 4 };
  global.document = { createTreeWalker(line) {
    let visited = false;
    return { nextNode() { if (visited) return null; visited = true; return line.text; } };
  } };
  const lines = [1, 2, 3].map(n => ({ text: { textContent: 'word' + n + ' here', row: n } }));
  const host = {
    dataset: {}, isConnected: true, querySelector: () => null,
    shadowRoot: {
      adoptedStyleSheets: [], querySelectorAll: () => [],
      querySelector: selector => { const m = selector.match(/data-line="(\d+)"/); return m ? lines[Number(m[1]) - 1] : null; },
    },
  };
  const decorations = dom.createDecorations();
  decorations.mount(host, 'a.go', [
    { id: 'c1', start: 1, end: 1, quote: 'word1', offset: 0 },
    { id: 'c2', start: 2, end: 2, quote: 'word2', offset: 0 },
    // An open form's quote has no comment id.
    { start: 3, end: 3, quote: 'word3', offset: 0 },
  ]);

  assert.equal(decorations.quoteAt(host, 50, 30), 'c1');
  assert.equal(decorations.quoteAt(host, 50, 50), 'c2');
  assert.equal(decorations.quoteAt(host, 50, 70), null, 'a form quote is not a link');
  assert.equal(decorations.quoteAt(host, 150, 30), null);
  assert.equal(decorations.quoteRange('c2').startContainer, lines[1].text);
  assert.equal(decorations.quoteRange('missing'), null);

  assert.equal(decorations.flashQuote('c1'), true);
  const flash = CSS.highlights.get('crit-quote-flash');
  assert.equal(flash.priority, 1);
  assert.deepEqual([...flash].map(r => r.startContainer), [lines[0].text]);
  t.mock.timers.tick(1200);
  assert.equal(CSS.highlights.has('crit-quote-flash'), false);
  assert.equal(decorations.flashQuote('missing'), false);

  // A disconnected host's quotes are no longer found.
  host.isConnected = false;
  assert.equal(decorations.quoteRange('c1'), null);
});

test('the flash highlight and quote-hover cursor are styled inside Pierre', () => {
  assert.match(dom.unsafeCSS, /::highlight\(crit-quote-flash\) \{ background-color: var\(--crit-comment-ref-flash-bg\); \}/);
  assert.match(dom.unsafeCSS, /:host\(\[data-crit-quote-hover\]\) \[data-content\] \{ cursor: pointer; \}/);
});
