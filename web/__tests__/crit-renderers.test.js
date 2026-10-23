'use strict';
// Pluggable renderers (issue #989): registry, file/fence matching, render
// target markup, the built-in mermaid registrations and anchorFor, and line-block
// emission for claimed fences.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');
const markdownit = require('markdown-it');

function freshRegistry() {
  delete require.cache[require.resolve('../crit-renderers.js')];
  return require('../crit-renderers.js');
}

const mermaid = require('../crit-renderer-mermaid.js');

function registerMermaid(r) {
  r.register(mermaid.fence);
  r.register(mermaid.file);
}

test('register validates name, kind and render', () => {
  const r = freshRegistry();
  assert.throws(() => r.register({ kind: 'fence', render() {} }), /name/);
  assert.throws(() => r.register({ name: 'x', render() {} }), /kind/);
  assert.throws(() => r.register({ name: 'x', kind: 'block', render() {} }), /kind/);
  assert.throws(() => r.register({ name: 'x', kind: 'fence' }), /render/);
});

test('forFence is case-insensitive and the last registration wins', () => {
  const r = freshRegistry();
  registerMermaid(r);
  assert.equal(r.forFence('mermaid').name, 'mermaid');
  assert.equal(r.forFence('Mermaid').name, 'mermaid');
  assert.equal(r.forFence('js'), null);
  assert.equal(r.forFence(''), null);
  r.register({ kind: 'fence', name: 'mine', langs: ['mermaid'], render() {} });
  assert.equal(r.forFence('mermaid').name, 'mine');
  r.unregister('fence', 'mine');
  assert.equal(r.forFence('mermaid').name, 'mermaid');
});

test('re-registering a kind + name replaces the old entry', () => {
  const r = freshRegistry();
  r.register({ kind: 'fence', name: 'a', langs: ['x'], render() {} });
  r.register({ kind: 'fence', name: 'a', langs: ['y'], render() {} });
  assert.equal(r.list().length, 1);
  assert.equal(r.forFence('x'), null);
  assert.equal(r.forFence('y').name, 'a');
});

test('fence and file registrations of one name are separate', () => {
  const r = freshRegistry();
  registerMermaid(r);
  assert.equal(r.list().length, 2);
  assert.equal(r.forFence('mermaid').kind, 'fence');
  assert.equal(r.forFile('flow.mmd').kind, 'file');
  assert.equal(r.byName('fence', 'mermaid').anchorFor, null, 'fence comments attach to the whole block');
  assert.equal(typeof r.byName('file', 'mermaid').anchorFor, 'function');
  r.unregister('file', 'mermaid');
  assert.equal(r.forFile('flow.mmd'), null);
  assert.equal(r.forFence('mermaid').name, 'mermaid');
});

test('a kind only reads its own selector', () => {
  const r = freshRegistry();
  r.register({ kind: 'fence', name: 'a', langs: ['x'], paths: ['*.x'], anchorFor() {}, render() {} });
  r.register({ kind: 'file', name: 'b', langs: ['y'], paths: ['*.y'], render() {} });
  assert.equal(r.forFile('a.x'), null);
  assert.equal(r.forFence('y'), null);
  assert.equal(r.byName('fence', 'a').anchorFor, null);
});

test('zoomable defaults to false; mermaid is zoomable', () => {
  const r = freshRegistry();
  assert.equal(r.register({ kind: 'fence', name: 'x', render() {} }).zoomable, false);
  assert.equal(r.register(mermaid.fence).zoomable, true);
  assert.equal(r.register(mermaid.file).zoomable, true);
});

test('mermaid claims .mermaid and .mmd files at any depth', () => {
  const r = freshRegistry();
  registerMermaid(r);
  for (const p of ['flow.mmd', 'docs/arch/flow.mmd', 'flow.mermaid', 'a/b.mermaid']) {
    assert.equal(r.forFile(p) && r.forFile(p).name, 'mermaid', p);
  }
  for (const p of ['flow.md', 'mmd', 'flow.mmd.bak', 'x.mermaid.txt']) {
    assert.equal(r.forFile(p), null, p);
  }
});

test('matchFile supports a leading **/ for any depth', () => {
  const r = freshRegistry();
  assert.ok(r.matchFile('specs/api/pay.arazzo.yaml', '**/*.arazzo.yaml'));
  assert.ok(r.matchFile('pay.arazzo.yaml', '**/*.arazzo.yaml'));
  assert.ok(r.matchFile('specs/api/pay.arazzo.yaml', '**/api/*.arazzo.yaml'));
  assert.ok(!r.matchFile('specs/pay.yaml', '**/*.arazzo.yaml'));
  assert.ok(r.matchFile('docs/a.mmd', 'docs/*.mmd'));
  assert.ok(!r.matchFile('docs/x/a.mmd', 'docs/*.mmd'));
});

test('label defaults to Rendered; mermaid uses Diagram', () => {
  const r = freshRegistry();
  assert.equal(r.register({ kind: 'file', name: 'x', render() {} }).label, 'Rendered');
  assert.equal(r.register(mermaid.file).label, 'Diagram');
});

test('targetHTML escapes the source and records its lines', () => {
  const r = freshRegistry();
  const html = r.targetHTML('mermaid', 'fence', 'A-->B["<b>&"]\n', { lang: 'mermaid', startLine: 3, endLine: 3 });
  assert.match(html, /^<div class="crit-render" data-crit-renderer="mermaid" data-crit-kind="fence" data-source-start="3" data-source-end="3">/);
  assert.match(html, /<pre class="crit-render-source"><code class="language-mermaid">A--&gt;B\[&quot;&lt;b&gt;&amp;&quot;\]\n<\/code><\/pre><\/div>$/);
  assert.doesNotMatch(r.targetHTML('x', 'fence', 'a'), /data-source-start/);
});

test('lineCount ignores one trailing newline', () => {
  const r = freshRegistry();
  assert.equal(r.lineCount(''), 0);
  assert.equal(r.lineCount('a'), 1);
  assert.equal(r.lineCount('a\n'), 1);
  assert.equal(r.lineCount('a\nb\n\n'), 3);
});

// --- mermaid file anchorFor -------------------------------------------------

// Minimal element stand-in: tag, attributes, text, parent chain.
function el(tag, attrs, text, parent) {
  return {
    nodeType: 1, tagName: tag, parentNode: parent || null,
    textContent: text || '',
    getAttribute(name) { return (attrs && attrs[name]) || null; },
  };
}

const flow = [
  'flowchart TD',
  '  Start[Begin here] --> Check{Valid?}',
  '  Check -->|yes| Done',
  '  Check -->|no| Start',
].join('\n');

test('mermaid anchorFor maps a node id to its line, else the first line naming it', () => {
  const svg = el('svg', { id: 'crit-mermaid-1' }, 'Begin here Valid? Done');
  const node = el('g', { id: 'crit-mermaid-1-flowchart-Check-1', class: 'node' }, 'Valid?', svg);
  const label = el('span', {}, 'Valid?', node);
  assert.deepEqual(mermaid.file.anchorFor(label, { source: flow, container: null }), { startLine: 2, endLine: 2 });
  const done = el('g', { 'data-id': 'Done' }, 'Done', svg);
  assert.deepEqual(mermaid.file.anchorFor(done, { source: flow, container: null }), { startLine: 3, endLine: 3 });
});

test('mermaid anchorFor prefers the line where the id opens a shape', () => {
  const src = [
    'flowchart TD',
    '  A --> B',
    '  B --> C',
    '  %% services',
    '  B[Payment service]',
  ].join('\n');
  const node = el('g', { id: 'flowchart-B-1' }, 'Payment service', el('svg', {}, ''));
  assert.deepEqual(mermaid.file.anchorFor(node, { source: src, container: null }), { startLine: 5, endLine: 5 });
  for (const def of ['B(Round)', 'B{Decide}', 'B>Flag]', 'B@{ shape: rect }', 'B [Spaced]']) {
    const s = 'flowchart TD\n  A --> B\n  ' + def;
    assert.deepEqual(mermaid.file.anchorFor(node, { source: s, container: null }), { startLine: 3, endLine: 3 }, def);
  }
  // An edge arrow is not a shape opener.
  const edgeOnly = 'flowchart TD\n  A --> B\n  B-->C';
  assert.deepEqual(mermaid.file.anchorFor(node, { source: edgeOnly, container: null }), { startLine: 2, endLine: 2 });
});

test('mermaid anchorFor falls back to label text', () => {
  const svg = el('svg', {}, '');
  const text = el('text', {}, 'yes', svg);
  assert.deepEqual(mermaid.file.anchorFor(text, { source: flow, container: null }), { startLine: 3, endLine: 3 });
});

test('mermaid anchorFor ids match whole words only', () => {
  const src = 'flowchart LR\n  AB --> C\n  A --> C';
  const node = el('g', { id: 'flowchart-A-0' }, '', el('svg', {}, ''));
  assert.deepEqual(mermaid.file.anchorFor(node, { source: src, container: null }), { startLine: 3, endLine: 3 });
});

test('mermaid anchorFor returns null when nothing matches', () => {
  const node = el('rect', {}, '', el('svg', {}, ''));
  assert.equal(mermaid.file.anchorFor(node, { source: flow, container: null }), null);
});

test('mermaid anchorFor ignores commented definitions, id references and labels without shifting lines', () => {
  const svg = el('svg', {}, '');
  const node = el('g', { id: 'flowchart-B-0' }, 'Real', svg);
  const label = el('text', {}, 'Real', svg);
  for (const live of ['  B[Real]', '  A --> B']) {
    const source = 'flowchart TD\n  %% B[Real]\n\t%% A --> B\n' + live + '\n';
    assert.deepEqual(mermaid.file.anchorFor(node, { source, container: null }), { startLine: 4, endLine: 4 });
  }
  const source = 'flowchart TD\n  %% Real\n  B[Real]\n';
  assert.deepEqual(mermaid.file.anchorFor(label, { source, container: null }), { startLine: 3, endLine: 3 });
  assert.equal(mermaid.file.anchorFor(node, { source: 'flowchart TD\n  %% B[Real]\n', container: null }), null);
});

// --- line blocks -------------------------------------------------------

function loadLineBlocks(renderers) {
  const src = fs.readFileSync(path.join(__dirname, '..', 'crit-line-blocks.js'), 'utf8');
  const window = { crit: { renderers: renderers } };
  new Function('window', 'document', src)(window, {});
  return window.crit.lineBlocks;
}

test('a claimed fence becomes one render block mapped to its content lines', () => {
  const r = freshRegistry();
  registerMermaid(r);
  const lb = loadLineBlocks(r);
  const md = markdownit();
  const content = '# Title\n\n```mermaid\ngraph TD\n  A --> B\n```\n\nafter';
  const blocks = lb.buildLineBlocks(md.parse(content, {}), md, content);
  const block = blocks.find(b => b.cssClass === 'render-block');
  assert.ok(block, 'render block emitted');
  assert.equal(block.startLine, 3);
  assert.equal(block.endLine, 6);
  assert.match(block.html, /data-crit-renderer="mermaid" data-crit-kind="fence" data-source-start="4" data-source-end="5"/);
  assert.ok(!blocks.some(b => b.cssClass && b.cssClass.indexOf('code-line') !== -1), 'not split per line');
});

test('an unclaimed fence still renders per line', () => {
  const lb = loadLineBlocks(freshRegistry());
  const md = markdownit();
  const content = '```mermaid\ngraph TD\n```';
  const blocks = lb.buildLineBlocks(md.parse(content, {}), md, content);
  assert.ok(blocks.every(b => b.cssClass !== 'render-block'));
  assert.ok(blocks.some(b => /code-line/.test(b.cssClass || '')));
});

// --- ctx.renderAs ----------------------------------------------------------

test('renderAs renders derived source with a fence renderer by name', async () => {
  const r = freshRegistry();
  const seen = [];
  r.register({ kind: 'fence', name: 'inner', langs: ['inner'], render(source, ctx) {
    seen.push(ctx);
    return '<b>' + source + '</b>';
  } });
  const container = {};
  const parent = { container, path: 'wf.arazzo.yaml', theme: 'light', startLine: 1, endLine: 9 };
  assert.equal(await r.renderAs('inner', 'graph', parent), '<b>graph</b>');
  assert.equal(seen[0].container, container, 'shares the caller container');
  assert.equal(seen[0].kind, 'fence');
  assert.equal(seen[0].renderer, 'inner');
  assert.equal(seen[0].path, 'wf.arazzo.yaml');
  assert.equal(seen[0].theme, 'light');
  assert.equal(seen[0].startLine, 0, 'derived source has no file lines');
  assert.equal(typeof seen[0].renderAs, 'function', 'renderers can chain');
});

test('renderAs rejects for an unknown or file-only name and passes render errors', async () => {
  const r = freshRegistry();
  r.register({ kind: 'file', name: 'fileonly', paths: ['*.x'], render() {} });
  r.register({ kind: 'fence', name: 'boom', langs: ['boom'], render() { throw new Error('bad input'); } });
  await assert.rejects(r.renderAs('nope', 'x', {}), /no fence renderer named nope/);
  await assert.rejects(r.renderAs('fileonly', 'x', {}), /no fence renderer named fileonly/);
  await assert.rejects(r.renderAs('boom', 'x', {}), /bad input/);
});

// --- anchorAt ----------------------------------------------------------------

function fakeTarget(source, start, end) {
  const attrs = { 'data-crit-renderer': 'steps', 'data-crit-kind': 'file', 'data-source-start': String(start), 'data-source-end': String(end) };
  return {
    _critSource: source,
    getAttribute(n) { return n in attrs ? attrs[n] : null; },
    querySelector() { return null; },
    closest() { return { getAttribute() { return 'a.steps'; } }; },
  };
}

test('anchorAt keeps a quote only when it is text of the chosen lines', () => {
  const r = freshRegistry();
  let result = null;
  r.register({ kind: 'file', name: 'steps', paths: ['*.steps'], render() {}, anchorFor() { return result; } });
  const target = fakeTarget('one\ntwo words\nthree', 1, 3);
  result = { startLine: 2, endLine: 2, quote: 'two' };
  assert.deepEqual(r.anchorAt(target, {}), { startLine: 2, endLine: 2, quote: 'two' });
  result = { startLine: 2, endLine: 2, quote: 'three' };
  assert.deepEqual(r.anchorAt(target, {}), { startLine: 2, endLine: 2 }, 'quote from another line is dropped');
  result = { startLine: 2, endLine: 9 };
  assert.deepEqual(r.anchorAt(target, {}), { startLine: 2, endLine: 3 }, 'clamped to the file');
  result = null;
  assert.equal(r.anchorAt(target, {}), null);
});

// --- stale targets (theme change while off-screen) --------------------------

function fakeRenderTarget(attrs) {
  return {
    hasAttribute(n) { return n in attrs; },
    getAttribute(n) { return n in attrs ? attrs[n] : null; },
  };
}

function fakeRoot(targets, codes = []) {
  return { querySelectorAll(selector) { return selector === '.crit-render' ? targets : codes; } };
}

test('needsRender detects claimed bare comment fences on first mount and skips source and compose forms', () => {
  const r = freshRegistry();
  registerMermaid(r);
  r.register({ kind: 'fence', name: 'custom', langs: ['custom'], render() {} });
  const code = (lang, ancestor) => ({
    className: 'language-' + lang,
    parentElement: { closest(selector) { return selector === ancestor ? {} : null; } },
  });
  withDocument('dark', null, () => {
    assert.equal(r.needsRender(fakeRoot([], [code('mermaid')])), true);
    assert.equal(r.needsRender(fakeRoot([], [code('custom')])), true);
    assert.equal(r.needsRender(fakeRoot([], [code('javascript')])), false);
    const rendered = fakeRenderTarget({ 'data-crit-state': 'rendered', 'data-crit-theme': 'dark' });
    assert.equal(r.needsRender(fakeRoot([rendered], [code('mermaid', '.crit-render')])), false);
    assert.equal(r.needsRender(fakeRoot([], [code('mermaid', '.comment-form')])), false);
  });
});

function withDocument(theme, palette, fn) {
  const had = Object.prototype.hasOwnProperty.call(global, 'document');
  const prev = global.document;
  global.document = {
    documentElement: {
      getAttribute(n) {
        if (n === 'data-theme') return theme;
        if (n === 'data-crit-palette') return palette;
        return null;
      },
    },
  };
  try { return fn(); } finally {
    if (had) global.document = prev; else delete global.document;
  }
}

test('needsRender: unrendered targets and targets rendered in another theme are stale', () => {
  const r = freshRegistry();
  const rendered = (theme) => fakeRenderTarget({ 'data-crit-state': 'rendered', 'data-crit-theme': theme });

  withDocument('dark', null, () => {
    assert.equal(r.needsRender(fakeRoot([])), false);
    assert.equal(r.needsRender(fakeRoot([fakeRenderTarget({})])), true, 'never rendered');
    assert.equal(r.needsRender(fakeRoot([rendered('dark')])), false, 'current theme');
  });
  // The theme switched while Pierre kept the target detached: on remount it
  // still carries the old theme, so onPierrePostRender re-renders it.
  withDocument('light', null, () => {
    assert.equal(r.needsRender(fakeRoot([rendered('dark')])), true, 'rendered in the old theme');
    assert.equal(r.needsRender(fakeRoot([rendered('light')])), false);
  });
  withDocument('light', 'tokyo-night', () => {
    assert.equal(r.needsRender(fakeRoot([rendered('light')])), true, 'palette changed');
    assert.equal(r.needsRender(fakeRoot([rendered('light/tokyo-night')])), false);
  });
});

// A small element stub for renderAll / rerenderAll: class selectors, with
// an optional `:scope > ` prefix, and the few DOM methods renderTarget uses.
class FakeEl {
  constructor(className = '') {
    this.className = className;
    this.attrs = {};
    this.children = [];
    this.parentElement = null;
    this.hidden = false;
    this.textContent = '';
    this._html = '';
  }
  get id() { return this.attrs.id || ''; }
  get innerHTML() { return this._html; }
  set innerHTML(v) { this._html = v; }
  hasAttribute(n) { return n in this.attrs; }
  getAttribute(n) { return n in this.attrs ? this.attrs[n] : null; }
  setAttribute(n, v) { this.attrs[n] = String(v); }
  dispatchEvent() { return true; }
  appendChild(c) { c.parentElement = this; this.children.push(c); return c; }
  insertBefore(c, ref) {
    c.parentElement = this;
    const i = this.children.indexOf(ref);
    this.children.splice(i < 0 ? this.children.length : i, 0, c);
    return c;
  }
  remove() {
    if (!this.parentElement) return;
    const sib = this.parentElement.children;
    sib.splice(sib.indexOf(this), 1);
    this.parentElement = null;
  }
  replaceWith(c) {
    const p = this.parentElement;
    p.children[p.children.indexOf(this)] = c;
    c.parentElement = p;
    this.parentElement = null;
  }
  cloneNode() {
    const c = new FakeEl(this.className);
    c.attrs = { ...this.attrs };
    c._html = this._html;
    c.textContent = this.textContent;
    c.children = this.children.map((ch) => { const cc = ch.cloneNode(); cc.parentElement = c; return cc; });
    return c;
  }
  addEventListener() {}
  closest() { return null; }
  matches(cls) { return this.className.split(/\s+/).includes(cls); }
  querySelectorAll(sel) {
    const direct = sel.startsWith(':scope > ');
    const m = /^(?:\.([\w-]+))?(?:\[([\w-]+)\])?$/.exec(direct ? sel.slice(9) : sel);
    if (!m || (!m[1] && !m[2])) return [];
    const [, cls, attr] = m;
    const out = [];
    const walk = (el) => {
      for (const ch of el.children) {
        if ((!cls || ch.matches(cls)) && (!attr || ch.hasAttribute(attr))) out.push(ch);
        if (!direct) walk(ch);
      }
    };
    walk(this);
    return out;
  }
  querySelector(sel) { return this.querySelectorAll(sel)[0] || null; }
  contains(el) {
    for (let n = el; n; n = n.parentElement) if (n === this) return true;
    return false;
  }
}

// A target holding `source` under a root, as targetHTML emits it.
function stubTarget(source) {
  const root = new FakeEl();
  const target = root.appendChild(new FakeEl('crit-render'));
  target.setAttribute('data-crit-renderer', 'slow');
  target.setAttribute('data-crit-kind', 'fence');
  const pre = target.appendChild(new FakeEl('crit-render-source'));
  pre.textContent = source;
  return { root, target, pre };
}

async function withFakeDom(fn) {
  const had = Object.prototype.hasOwnProperty.call(global, 'document');
  const prev = global.document;
  const hadWindow = Object.prototype.hasOwnProperty.call(global, 'window');
  const prevWindow = global.window;
  global.document = {
    documentElement: { getAttribute(n) { return n === 'data-theme' ? 'dark' : null; } },
    createElement() { return new FakeEl(); },
    addEventListener() {},
    // Ids the test marks as already in the page.
    ids: new Map(),
    getElementById(id) { return this.ids.get(id) || null; },
  };
  global.window = global.window || {};
  try { return await fn(); } finally {
    if (had) global.document = prev; else delete global.document;
    if (hadWindow) global.window = prevWindow; else delete global.window;
  }
}

// A renderer whose renders resolve when the test says so.
function deferredRenderer(r) {
  const calls = [];
  r.register({
    kind: 'fence', name: 'slow', langs: ['slow'],
    render(source) {
      return new Promise((resolve) => { calls.push({ source, resolve }); });
    },
  });
  return calls;
}

const tick = () => new Promise((resolve) => setImmediate(resolve));

test('renderAll: of two renders in flight for one target, the newest wins', async () => {
  const r = freshRegistry();
  const calls = deferredRenderer(r);
  await withFakeDom(async () => {
    const { root, target, pre } = stubTarget('graph');
    const first = r.renderAll(root);
    const second = r.rerenderAll(root); // e.g. a theme change mid-render
    await tick();
    assert.equal(calls.length, 2);
    calls[1].resolve('<svg>new</svg>');
    await second;
    calls[0].resolve('<svg>old</svg>');
    await first;
    const outputs = target.querySelectorAll(':scope > .crit-render-output');
    assert.equal(outputs.length, 1);
    assert.equal(outputs[0].innerHTML, '<svg>new</svg>');
    assert.equal(target.getAttribute('data-crit-state'), 'rendered');
    assert.equal(pre.hidden, true);
    // The stale result is dropped, not cached for the next rebuild.
    const rebuilt = stubTarget('graph');
    r.renderAll(rebuilt.root);
    assert.equal(rebuilt.target.querySelector(':scope > .crit-render-output').innerHTML, '<svg>new</svg>');
  });
});

test('renderAll: a stale render that fails does not replace the newer output', async () => {
  const r = freshRegistry();
  const calls = deferredRenderer(r);
  const warn = console.warn;
  console.warn = () => {};
  try {
    await withFakeDom(async () => {
      const { root, target, pre } = stubTarget('graph');
      const first = r.renderAll(root);
      const second = r.rerenderAll(root);
      await tick();
      calls[1].resolve('<svg>new</svg>');
      await second;
      calls[0].resolve(Promise.reject(new Error('late failure')));
      await first;
      assert.equal(target.getAttribute('data-crit-state'), 'rendered');
      assert.equal(target.querySelector(':scope > .crit-render-error'), null);
      assert.equal(target.querySelector(':scope > .crit-render-output').innerHTML, '<svg>new</svg>');
      assert.equal(pre.hidden, true);
    });
  } finally {
    console.warn = warn;
  }
});

test('renderAll: a rebuilt target with rendered source shows the cached output at once', async () => {
  const r = freshRegistry();
  const calls = deferredRenderer(r);
  await withFakeDom(async () => {
    const a = stubTarget('graph');
    const done = r.renderAll(a.root);
    await tick();
    calls[0].resolve('<svg>graph</svg>');
    await done;

    // A document rebuild emits a fresh target with the same source.
    const b = stubTarget('graph');
    r.renderAll(b.root);
    assert.equal(calls.length, 1, 'no second render');
    assert.equal(b.target.getAttribute('data-crit-state'), 'rendered', 'rendered synchronously');
    assert.equal(b.pre.hidden, true);
    const out = b.target.querySelector(':scope > .crit-render-output');
    assert.equal(out.innerHTML, '<svg>graph</svg>');
    assert.notEqual(out, a.target.querySelector(':scope > .crit-render-output'), 'a clone, not the node itself');

    // Other source misses; a theme re-render clears the cache.
    r.renderAll(stubTarget('other').root);
    await tick();
    assert.equal(calls.length, 2);
    r.rerenderAll(b.root);
    await tick();
    assert.equal(calls.length, 3);
  });
});

test('renderAll: crit:rendered fires after the clean output is cached', async () => {
  const r = freshRegistry();
  const calls = deferredRenderer(r);
  await withFakeDom(async () => {
    const a = stubTarget('graph');
    // A listener marks up the drawn output, as quote highlights do.
    a.target.dispatchEvent = function() {
      const out = this.querySelector(':scope > .crit-render-output');
      out.innerHTML = out.innerHTML.replace('graph', '<mark>graph</mark>');
      return true;
    };
    const done = r.renderAll(a.root);
    await tick();
    calls[0].resolve('<svg>graph</svg>');
    await done;
    assert.equal(a.target.querySelector(':scope > .crit-render-output').innerHTML, '<svg><mark>graph</mark></svg>');

    const b = stubTarget('graph');
    r.renderAll(b.root);
    assert.equal(calls.length, 1, 'served from the cache');
    assert.equal(b.target.querySelector(':scope > .crit-render-output').innerHTML, '<svg>graph</svg>', 'cache holds no markup from the listener');
  });
});

test('renderAll: a cached output whose ids are already in the page renders afresh', async () => {
  const r = freshRegistry();
  const calls = [];
  r.register({
    kind: 'fence', name: 'slow', langs: ['slow'],
    render(source, ctx) {
      calls.push(source);
      ctx.container.appendChild(new FakeEl('svg')).setAttribute('id', 'diagram-1');
    },
  });
  await withFakeDom(async () => {
    const a = stubTarget('graph');
    await r.renderAll(a.root);
    assert.equal(calls.length, 1);
    // The first copy is still in the page: a second target with the same
    // source must not get a clone with the same ids.
    const shown = a.target.querySelector(':scope > .crit-render-output').querySelector('[id]');
    global.document.ids.set('diagram-1', shown);
    const b = stubTarget('graph');
    await r.renderAll(b.root);
    assert.equal(calls.length, 2, 'rendered again');
    // Re-rendering the target that holds those ids may reuse the cache.
    global.document.ids.set('diagram-1', b.target.querySelector(':scope > .crit-render-output').querySelector('[id]'));
    b.target.attrs['data-crit-theme'] = 'stale';
    await r.renderAll(b.root);
    assert.equal(calls.length, 2, 'cache hit when replacing the only copy');
  });
});
