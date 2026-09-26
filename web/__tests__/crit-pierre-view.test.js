'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { createPierreView } = require('../crit-pierre-view.js');

// Fake @pierre/diffs: a CodeView that records calls and lets tests drive
// scroll/render callbacks; processFile returns a tagged stand-in.
function fakePierre() {
  const log = [];
  let scrollListener = null;
  let rendered = [];
  let lastOptions = null;
  class CodeView {
    constructor(options) { lastOptions = options; this.options = options; this.items = new Map(); }
    setup(root) { log.push(['setup', root]); }
    setItems(items) { log.push(['setItems', items.map(i => i.id + ':' + i.type)]); items.forEach(i => this.items.set(i.id, i)); }
    updateItem(item) { log.push(['updateItem', item.id, item.type, item.version]); this.items.set(item.id, item); return true; }
    removeItem(id) { log.push(['removeItem', id]); this.items.delete(id); }
    render() {}
    setOptions(o) { lastOptions = o; log.push(['setOptions', o.diffStyle, o.themeType]); }
    getRenderedItems() { return rendered.map(id => ({ id, element: { getBoundingClientRect: () => ({ top: 100 }) } })); }
    subscribeToScroll(fn) { scrollListener = fn; return () => { scrollListener = null; }; }
    scrollTo(target) { log.push(['scrollTo', target]); }
    getScrollTop() { return 0; }
    setSelectedLines(sel) { log.push(['setSelectedLines', sel]); }
    clearSelectedLines() { log.push(['clearSelectedLines']); }
    cleanUp() { log.push(['cleanUp']); }
  }
  return {
    P: {
      CodeView,
      processFile(patch, opts) { return { patch, full: !!(opts && opts.oldFile), cacheKey: opts && opts.cacheKey }; },
    },
    log,
    options: () => lastOptions,
    setRendered: ids => { rendered = ids; },
    scroll: () => scrollListener && scrollListener(0),
  };
}

function file(path, extra) {
  return Object.assign({
    path,
    status: 'modified',
    content: 'a\nB\n',
    diffHunks: [{ OldStart: 1, OldCount: 2, NewStart: 1, NewCount: 2, Lines: [
      { Type: 'context', Content: 'a', OldNum: 1, NewNum: 1 },
      { Type: 'del', Content: 'b', OldNum: 2 },
      { Type: 'add', Content: 'B', NewNum: 2 },
    ] }],
    comments: [],
  }, extra);
}

function makeView(fake, overrides) {
  return createPierreView(Object.assign({
    pierre: fake.P,
    root: {},
    annotations: () => [],
    loadFile: () => Promise.resolve(null),
    buildHeader: () => ({}),
    buildAnnotation: () => ({}),
    onGutterUtilityClick: () => {},
  }, overrides));
}

test('loaded files become full diffs; lazy files retain line-based size estimates', async function() {
  const fake = fakePierre();
  const lazy = file('lazy.go', { lazy: true, additions: 3, deletions: 1, diffHunks: [], content: '' });
  let loaded = null;
  const view = makeView(fake, {
    loadFile: path => {
      loaded = path;
      Object.assign(lazy, file('lazy.go'), { lazy: false });
      return Promise.resolve(lazy);
    },
  });
  view.setFiles([file('a.go'), lazy]);
  const items = fake.log.find(e => e[0] === 'setItems');
  assert.deepEqual(items[1], ['a.go:diff', 'lazy.go:diff']);
  assert.match(view.viewer.items.get('lazy.go').fileDiff.patch, /@@/);
  assert.equal(view.viewer.items.get('lazy.go').annotations[0].metadata.kind, 'loading');
  assert.equal(view.isStub('lazy.go'), true);
  assert.equal(view.isStub('a.go'), false);

  // Scrolling the stub into the rendered window hydrates it in place.
  fake.setRendered(['lazy.go']);
  fake.scroll();
  await new Promise(r => setTimeout(r, 0));
  await new Promise(r => setTimeout(r, 0));
  assert.equal(loaded, 'lazy.go');
  assert.equal(view.isStub('lazy.go'), false);
  assert.equal(view.viewer.items.get('lazy.go').type, 'diff');
  assert.ok(fake.log.some(e => e[0] === 'updateItem' && e[1] === 'lazy.go'), 'hydrated item published in place');
});

test('a full setFiles during a load starts a new load instead of leaving the stub', async function() {
  const fake = fakePierre();
  const lazy = () => file('lazy.go', { lazy: true, additions: 1, deletions: 0, diffHunks: [], content: '' });
  const pending = [];
  const view = makeView(fake, { loadFile: () => new Promise(resolve => pending.push(resolve)) });
  fake.setRendered(['lazy.go']);
  view.setFiles([lazy()]);
  assert.equal(pending.length, 1);
  // A theme or settings change re-renders while the first load is in flight.
  view.setFiles([lazy()]);
  await new Promise(r => setTimeout(r, 0));
  assert.equal(pending.length, 2, 'the new generation requests the file again');
  pending[0](Object.assign(file('lazy.go'), { lazy: false }));
  await new Promise(r => setTimeout(r, 0));
  assert.equal(view.isStub('lazy.go'), true, 'the stale result is ignored');
  pending[1](Object.assign(file('lazy.go'), { lazy: false }));
  await new Promise(r => setTimeout(r, 0));
  assert.equal(view.isStub('lazy.go'), false);
});

test('annotation elements are cached by kind:id so drafts survive re-renders', function() {
  const fake = fakePierre();
  let builds = 0;
  makeView(fake, { buildAnnotation: () => { builds++; return { tag: 'el' + builds }; } });
  const render = fake.options().renderAnnotation;
  const ctx = { item: { id: 'a.go' } };
  const ann = { metadata: { kind: 'form', id: 'k1' } };
  const first = render(ann, ctx);
  const again = render(ann, ctx);
  assert.equal(first, again, 'same element returned for the same form');
  assert.equal(builds, 1);
});

test('refreshFile invalidates only the named annotation keys', function() {
  const fake = fakePierre();
  let builds = 0;
  const view = makeView(fake, { buildAnnotation: () => ({ n: ++builds }) });
  view.setFiles([file('a.go')]);
  const render = fake.options().renderAnnotation;
  const ctx = { item: { id: 'a.go' } };
  const thread = render({ metadata: { kind: 'thread', id: 't1' } }, ctx);
  const form = render({ metadata: { kind: 'form', id: 'f1' } }, ctx);
  view.refreshFile(file('a.go'), ['thread:t1']);
  assert.notEqual(render({ metadata: { kind: 'thread', id: 't1' } }, ctx), thread, 'thread rebuilt');
  assert.equal(render({ metadata: { kind: 'form', id: 'f1' } }, ctx), form, 'open form kept');
});

test('annotation metadata objects are stable across publishes', function() {
  const fake = fakePierre();
  const view = makeView(fake, {
    annotations: () => [{ side: 'additions', lineNumber: 2, metadata: { kind: 'thread', id: 't1' } }],
  });
  view.setFiles([file('a.go')]);
  const firstMeta = view.viewer.items.get('a.go').annotations[0].metadata;
  view.refreshFile(file('a.go'), []);
  assert.equal(view.viewer.items.get('a.go').annotations[0].metadata, firstMeta,
    'unchanged annotation keeps its metadata reference (Pierre does not re-create it)');
  view.refreshFile(file('a.go'), ['thread:t1']);
  assert.notEqual(view.viewer.items.get('a.go').annotations[0].metadata, firstMeta, 'invalidated one gets a new reference');
});

test('switching a file between diff and document view swaps the item type in place', function() {
  const fake = fakePierre();
  const md = file('plan.md', { fileType: 'markdown', viewMode: 'diff' });
  const view = makeView(fake, { isDocumentView: f => f.viewMode === 'document' });
  view.setFiles([file('a.go'), md]);
  fake.log.length = 0;
  md.viewMode = 'document';
  view.refreshFile(md, []);
  assert.deepEqual(fake.log.find(e => e[0] === 'setItems'), ['setItems', ['a.go:diff', 'plan.md:file']], 'same position, new type');
  assert.ok(!fake.log.some(e => e[0] === 'updateItem' && e[1] === 'plan.md'), 'updateItem cannot change a type');
});

test('scroll and selection map Crit sides to Pierre sides', async function() {
  const fake = fakePierre();
  const view = makeView(fake);
  view.setFiles([file('a.go')]);
  await view.scrollToLine('a.go', 2, 'old', 'center');
  await view.scrollToLine('a.go', 2, '', 'nearest');
  const scrolls = fake.log.filter(e => e[0] === 'scrollTo').map(e => e[1]);
  assert.deepEqual(scrolls, [
    { type: 'line', id: 'a.go', lineNumber: 2, side: 'deletions', align: 'center' },
    { type: 'line', id: 'a.go', lineNumber: 2, side: 'additions', align: 'nearest' },
  ]);
  view.setSelectedLine('a.go', 2, 'old');
  view.setSelectedLine(null);
  const sels = fake.log.filter(e => e[0] === 'setSelectedLines' || e[0] === 'clearSelectedLines');
  assert.deepEqual(sels[0][1], { id: 'a.go', range: { start: 2, end: 2, side: 'deletions', endSide: 'deletions' } });
  assert.deepEqual(sels[1], ['clearSelectedLines']);
});

test('scrolling to a lazy file hydrates it before jumping', async function() {
  const fake = fakePierre();
  const lazy = file('deep.go', { lazy: true, diffHunks: [], content: '' });
  const order = [];
  const view = makeView(fake, {
    loadFile: () => { order.push('load'); Object.assign(lazy, file('deep.go'), { lazy: false }); return Promise.resolve(lazy); },
  });
  view.setFiles([lazy]);
  await view.scrollToFile('deep.go');
  const jump = fake.log.find(e => e[0] === 'scrollTo');
  order.push('jump');
  assert.deepEqual(order, ['load', 'jump']);
  assert.deepEqual(jump[1], { type: 'item', id: 'deep.go', align: 'start' });
});

test('gutter clicks become Crit form ranges (file items are single-sided)', function() {
  const fake = fakePierre();
  const clicks = [];
  makeView(fake, { onGutterUtilityClick: (path, range) => clicks.push([path, range]) });
  const onClick = fake.options().onGutterUtilityClick;
  onClick({ start: 5, end: 3, side: 'deletions' }, { type: 'diff', item: { id: 'a.go' } });
  onClick({ start: 7, end: 7 }, { type: 'file', item: { id: 'plan.md' } });
  assert.deepEqual(clicks, [
    ['a.go', { startLine: 3, endLine: 5, side: 'old' }],
    ['plan.md', { startLine: 7, endLine: 7, side: '' }],
  ]);
});

test('diff style and theme go through setOptions without losing other options', function() {
  const fake = fakePierre();
  const view = makeView(fake, { diffStyle: 'split', themeType: 'system' });
  view.setDiffStyle('unified');
  view.setThemeType('dark');
  const opts = fake.options();
  assert.equal(opts.diffStyle, 'unified');
  assert.equal(opts.themeType, 'dark');
  assert.equal(typeof opts.renderAnnotation, 'function', 'callbacks kept after setOptions');
  assert.equal(opts.lineDiffType, 'word-alt');
});

test('destroy tears down the viewer and caches', function() {
  const fake = fakePierre();
  const view = makeView(fake);
  view.setFiles([file('a.go')]);
  view.destroy();
  assert.ok(fake.log.some(e => e[0] === 'cleanUp'));
});

test('a full setFiles rebuilds thread cards but keeps open forms', function() {
  const fake = fakePierre();
  let builds = 0;
  const view = makeView(fake, { buildAnnotation: () => ({ n: ++builds }) });
  view.setFiles([file('a.go')]);
  const render = fake.options().renderAnnotation;
  const ctx = { item: { id: 'a.go' } };
  const thread = render({ metadata: { kind: 'thread', id: 't1' } }, ctx);
  const form = render({ metadata: { kind: 'form', id: 'f1' } }, ctx);
  view.setFiles([file('a.go')]);
  assert.notEqual(render({ metadata: { kind: 'thread', id: 't1' } }, ctx), thread, 'thread rebuilt from the new model');
  assert.equal(render({ metadata: { kind: 'form', id: 'f1' } }, ctx), form, 'form element (and its typing) kept');
});

test('re-publishing a file reuses its parsed diff until the content changes', function() {
  const fake = fakePierre();
  let parses = 0;
  const processFile = fake.P.processFile;
  fake.P.processFile = function() { parses++; return processFile.apply(this, arguments); };
  const view = makeView(fake);
  const f = file('a.go', { fileHash: 'h1' });
  view.setFiles([f]);
  const first = view.viewer.items.get('a.go').fileDiff;
  view.refreshFile(f, []);          // comment/form/collapse change: same content
  view.setCollapsed(f, true);
  assert.equal(view.viewer.items.get('a.go').fileDiff, first, 'same object, no re-parse');
  assert.equal(parses, 1);
  f.diffHunks = f.diffHunks.concat([{ OldStart: 9, OldCount: 1, NewStart: 9, NewCount: 1, Lines: [
    { Type: 'del', Content: 'x', OldNum: 9 }, { Type: 'add', Content: 'y', NewNum: 9 },
  ] }]);
  view.refreshFile(f, []);
  assert.notEqual(view.viewer.items.get('a.go').fileDiff, first, 'new hunks parse again');
  assert.equal(parses, 2);
});

// Minimal DOM for annotation wrappers: Pierre wraps each annotation element
// in a slotted div and appends changed ones at the end of the host.
function fakeHost() {
  const doc = { activeElement: null };
  const host = {
    children: [],
    ownerDocument: doc,
    appendChild(el) { this.children = this.children.filter(c => c !== el).concat(el); el.parentNode = this; },
    insertBefore(el, ref) {
      const rest = this.children.filter(c => c !== el);
      rest.splice(rest.indexOf(ref), 0, el);
      this.children = rest;
      el.parentNode = this;
      if (doc.activeElement && el.contains(doc.activeElement)) doc.activeElement = null; // moving blurs
    },
  };
  function wrap(name, slot) {
    const inner = { name, focus() { doc.activeElement = inner; } };
    const wrapper = {
      slot, ownerDocument: doc, parentNode: null,
      contains: n => n === inner,
      compareDocumentPosition(other) { return host.children.indexOf(other) < host.children.indexOf(this) ? 2 : 4; },
    };
    inner.parentElement = wrapper;
    inner.ownerDocument = doc;
    return { inner, wrapper };
  }
  return { host, doc, wrap };
}

test('file-level annotations keep their order when Pierre appends a new one after the document', function() {
  const fake = fakePierre();
  const { host, doc, wrap } = fakeHost();
  const built = {};
  const annotations = [
    { side: 'additions', lineNumber: 0, metadata: { kind: 'thread', id: 't1' } },
    { side: 'additions', lineNumber: 0, metadata: { kind: 'form', id: 'f1' } },
    { side: 'additions', lineNumber: 0, metadata: { kind: 'document', id: 'a.md' } },
  ];
  makeView(fake, {
    annotations: () => annotations,
    buildAnnotation: (kind, _path, id) => {
      const key = kind + ':' + id;
      built[key] = wrap(key, 'annotation-0');
      return built[key].inner;
    },
  }).setFiles([file('a.md')]);
  const ctx = { item: { id: 'a.md' } };
  annotations.forEach(a => fake.options().renderAnnotation(a, ctx));
  // Pierre's DOM order: the document first, then the thread and the form appended after it.
  host.appendChild(built['document:a.md'].wrapper);
  host.appendChild(built['thread:t1'].wrapper);
  host.appendChild(built['form:f1'].wrapper);
  built['form:f1'].inner.focus();

  fake.options().onPostRender(host, null, 'update', ctx);
  assert.deepEqual(host.children.map(w => w === built['thread:t1'].wrapper ? 'thread'
    : w === built['form:f1'].wrapper ? 'form' : 'document'), ['thread', 'form', 'document']);
  assert.equal(doc.activeElement, built['form:f1'].inner, 'the composer keeps focus');
});

test('a new review round invalidates changed old text with identical new content and hunk geometry', function() {
  const fake = fakePierre();
  const view = makeView(fake);
  const original = file('a.go', { fileHash: 'same-new-content' });
  view.setFiles([original]);
  const first = view.viewer.items.get('a.go').fileDiff;
  const nextRound = file('a.go', { fileHash: 'same-new-content' });
  nextRound.diffHunks[0].Lines[1].Content = 'c';
  view.setFiles([nextRound]);
  const second = view.viewer.items.get('a.go').fileDiff;
  assert.notEqual(second, first);
  assert.notEqual(second.cacheKey, first.cacheKey, 'worker cache also invalidated');
  assert.match(second.patch, /\n-c\n/);
  view.refreshFile(nextRound);
  assert.equal(view.viewer.items.get('a.go').fileDiff, second, 'comment refresh reuses parsed result');
});

test('same-length file edits invalidate parsed contents even without a server hash', function() {
  const fake = fakePierre();
  const view = makeView(fake, { isFileView: () => true });
  const f = file('a.go');
  view.setFiles([f]);
  const first = view.viewer.items.get('a.go').file;
  f.content = 'a\nC\n';
  view.refreshFile(f);
  const second = view.viewer.items.get('a.go').file;
  assert.notEqual(second, first);
  assert.notEqual(second.cacheKey, first.cacheKey);
  assert.equal(second.contents, 'a\nC\n');
});

test('newly mounted stubs hydrate after the scroll callback sampled the previous viewport', async function(t) {
  const frames = [];
  const previousRAF = global.requestAnimationFrame;
  global.requestAnimationFrame = fn => frames.push(fn);
  t.after(() => {
    if (previousRAF) global.requestAnimationFrame = previousRAF;
    else delete global.requestAnimationFrame;
  });
  const fake = fakePierre();
  const lazy = file('lazy.go', { lazy: true, diffHunks: [], content: '' });
  const loaded = [];
  const view = makeView(fake, { loadFile: async path => {
    loaded.push(path);
    Object.assign(lazy, file(path), { lazy: false });
    return lazy;
  } });
  view.setFiles([file('a.go'), lazy]);
  fake.setRendered(['a.go']);
  fake.scroll();
  frames.shift()(); // CodeView has not mounted its next viewport yet.
  assert.deepEqual(loaded, []);
  fake.setRendered(['lazy.go']);
  fake.options().onPostRender({ dataset: {} }, null, 'mount', { item: { id: 'lazy.go' } });
  assert.equal(frames.length, 1, 'mount schedules hydration without another scroll');
  frames.shift()();
  await view.ensureLoaded('lazy.go');
  assert.deepEqual(loaded, ['lazy.go']);
  assert.equal(view.isStub('lazy.go'), false);
  view.destroy();
});
