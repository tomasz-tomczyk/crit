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

test('loaded files become full diffs; lazy files become stubs until rendered', async function() {
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
  assert.equal(view.isStub('lazy.go'), true);
  assert.equal(view.isStub('a.go'), false);

  // Scrolling the stub into the rendered window hydrates it in place.
  fake.setRendered(['lazy.go']);
  fake.scroll();
  await new Promise(r => setTimeout(r, 0));
  await new Promise(r => setTimeout(r, 0));
  assert.equal(loaded, 'lazy.go');
  assert.equal(view.isStub('lazy.go'), false);
  assert.ok(fake.log.some(e => e[0] === 'updateItem' && e[1] === 'lazy.go'));
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

test('switching a file between diff and document view removes and reinserts the item', function() {
  const fake = fakePierre();
  const md = file('plan.md', { fileType: 'markdown', viewMode: 'diff' });
  const view = makeView(fake, { isDocumentView: f => f.viewMode === 'document' });
  view.setFiles([file('a.go'), md]);
  md.viewMode = 'document';
  view.refreshFile(md, []);
  const i = fake.log.findIndex(e => e[0] === 'removeItem' && e[1] === 'plan.md');
  assert.ok(i >= 0, 'type change removes the old record');
  assert.deepEqual(fake.log[i + 1], ['setItems', ['a.go:diff', 'plan.md:file']], 'reinserted in the same position');
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
