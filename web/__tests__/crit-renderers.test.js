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

function fakeRoot(targets) {
  return { querySelectorAll() { return targets; } };
}

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
