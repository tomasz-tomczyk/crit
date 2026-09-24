'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const diffV = require('../crit-diff-virtualizer.js');
const fileList = require('../crit-file-list-virtualizer.js');

test('estimateFileSectionHeight is header-only when collapsed', function() {
  const collapsed = fileList.estimateFileSectionHeight({
    collapsed: true,
    bodyHeight: 2000,
  });
  const open = fileList.estimateFileSectionHeight({
    collapsed: false,
    bodyHeight: 2000,
  });
  assert.equal(collapsed, fileList.FILE_HEADER_ESTIMATE);
  assert.equal(open, fileList.FILE_HEADER_ESTIMATE + 2000);
});

test('getScrollAnchor / resolveAnchoredScrollTop pin a file across height refine', function() {
  const items = [
    { key: 'a', kind: 'file' },
    { key: 'b', kind: 'file' },
    { key: 'c', kind: 'file' },
  ];
  const index = new diffV.HeightIndex(items, function() { return 100; });
  // Viewport shows file b starting 20px into it at scrollTop 120.
  const anchor = fileList.getScrollAnchor(index, items, 120);
  assert.equal(anchor.key, 'b');
  assert.equal(anchor.index, 1);
  assert.equal(anchor.intraOffset, 20);

  // File a grows from 100 → 400 (mount refine). Recompute offsets.
  const changes = new Map();
  changes.set(0, 400);
  index.updateMany(changes);

  const fixed = fileList.resolveAnchoredScrollTop(index, items, anchor);
  // b still starts 20px into the viewport → scrollTop = offset(b) - 0 + 20... wait:
  // anchor means: item at index should appear with intraOffset at the top of viewport,
  // so scrollTop = offset(index) + intraOffset.
  assert.equal(fixed, 400 + 20);
});

test('FileListVirtualizer windows items with spacers and placeholders', function() {
  function makeNode(tag) {
    const node = {
      tagName: String(tag || 'div').toUpperCase(),
      nodeType: 1,
      className: '',
      style: {},
      dataset: {},
      children: [],
      parentNode: null,
      remove: function() {
        if (!this.parentNode) return;
        const kids = this.parentNode.children;
        const idx = kids.indexOf(this);
        if (idx >= 0) kids.splice(idx, 1);
        this.parentNode = null;
      },
      setAttribute: function(name, value) {
        if (name === 'data-file-key') this.dataset.fileKey = value;
        if (name === 'data-virtual-key') this.dataset.virtualKey = value;
        if (name === 'aria-hidden') this.setAttribute._aria = value;
      },
      getBoundingClientRect: function() {
        return { top: 0, bottom: 40, height: Number(this.style.height) || 40 };
      },
    };
    return node;
  }

  const surface = makeNode('div');
  surface._kids = [];
  Object.defineProperty(surface, 'children', {
    get: function() { return this._kids; },
  });
  surface.insertBefore = function(node, ref) {
    if (node.parentNode) node.remove();
    node.parentNode = surface;
    if (!ref) {
      surface._kids.push(node);
      return node;
    }
    const idx = surface._kids.indexOf(ref);
    surface._kids.splice(idx < 0 ? surface._kids.length : idx, 0, node);
    return node;
  };
  Object.defineProperty(surface, 'lastElementChild', {
    get: function() { return this._kids[this._kids.length - 1] || null; },
  });
  surface.getBoundingClientRect = function() {
    return { top: 0, bottom: 900, height: 900 };
  };
  surface.clientWidth = 800;

  globalThis.window = globalThis.window || {};
  globalThis.document = {
    createElement: makeNode,
    addEventListener: function() {},
    removeEventListener: function() {},
  };
  globalThis.ResizeObserver = undefined;
  globalThis.requestAnimationFrame = function(cb) { return setTimeout(cb, 0); };
  globalThis.cancelAnimationFrame = function(id) { clearTimeout(id); };

  const items = Array.from({ length: 50 }, function(_, i) {
    return {
      key: 'f' + i,
      kind: 'file',
      collapsed: false,
      bodyHeight: 500,
    };
  });

  const fl = new fileList.FileListVirtualizer({
    surface: surface,
    items: items,
    estimateHeight: function(item) {
      return fileList.estimateFileSectionHeight(item);
    },
    renderPlaceholder: function(item) {
      const el = makeNode('div');
      el.className = 'file-section-placeholder';
      el.dataset.fileKey = item.key;
      return el;
    },
    renderMounted: function(item) {
      const el = makeNode('details');
      el.className = 'file-section';
      el.dataset.fileKey = item.key;
      el.dataset.mounted = '1';
      return el;
    },
    // Force a tight window for the stub (no real viewport metrics).
    viewportHeight: 900,
    localTop: 0,
  });

  fl.reconcile(diffV.calculateWindow(fl.heightIndex, 0, 900));

  const mounted = surface._kids.filter(function(n) {
    return n.dataset && n.dataset.mounted === '1';
  });
  const spacers = surface._kids.filter(function(n) {
    return n.className === 'file-list-virtual-spacer';
  });
  assert.ok(mounted.length > 0, 'mounts near-viewport files');
  assert.ok(mounted.length < items.length, 'does not mount every file');
  assert.ok(spacers.length >= 1, 'uses spacers for off-screen range');

  // Pin a far file — must appear even when ordinary window is at top.
  fl.pinnedKeys.add('f40');
  fl.reconcile(diffV.mergeIntervals(
    [diffV.calculateWindow(fl.heightIndex, 0, 900), [40, 40]],
    items.length
  ));
  assert.ok(fl.nodes.get('f40'), 'pinned far file is mounted');
  assert.equal(fl.nodes.get('f40').dataset.mounted, '1');

  fl.dispose();
});

test('FileListVirtualizer.setItemHeight applies scroll fix via anchor', function() {
  const items = [
    { key: 'a', kind: 'file', bodyHeight: 100 },
    { key: 'b', kind: 'file', bodyHeight: 100 },
    { key: 'c', kind: 'file', bodyHeight: 100 },
  ];
  const index = new diffV.HeightIndex(items, function(item) {
    return fileList.estimateFileSectionHeight(item);
  });
  const header = fileList.FILE_HEADER_ESTIMATE;
  assert.equal(index.height(0), header + 100);

  const scrollTop = header + 100 + 10; // 10px into file b
  const anchor = fileList.getScrollAnchor(index, items, scrollTop);
  assert.equal(anchor.key, 'b');

  const changes = new Map();
  changes.set(0, header + 500);
  index.updateMany(changes);
  const next = fileList.resolveAnchoredScrollTop(index, items, anchor);
  assert.equal(next, header + 500 + 10);
});
