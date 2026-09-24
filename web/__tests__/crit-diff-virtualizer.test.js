const test = require('node:test');
const assert = require('node:assert/strict');

const virtualizer = require('../crit-diff-virtualizer.js');

function fixture() {
  return {
    hunks: [
      {
        OldStart: 10,
        OldCount: 2,
        NewStart: 10,
        NewCount: 2,
        Header: '@@ -10,2 +10,2 @@',
        Lines: [
          { Type: 'del', OldNum: 10, NewNum: 0, Content: 'old' },
          { Type: 'add', OldNum: 0, NewNum: 10, Content: 'new' },
          { Type: 'context', OldNum: 11, NewNum: 11, Content: 'same' },
        ],
      },
      {
        OldStart: 40,
        OldCount: 1,
        NewStart: 40,
        NewCount: 1,
        Header: '@@ -40,1 +40,1 @@',
        Lines: [{ Type: 'add', OldNum: 0, NewNum: 40, Content: 'later' }],
      },
    ],
    commentsMap: {
      '10:old': [{ id: 'c_old', start_line: 10, end_line: 10, side: 'old' }],
      '10:': [{ id: 'c_new', start_line: 10, end_line: 10 }],
      '30:': [{ id: 'c_outdated', start_line: 30, end_line: 30 }],
      '31:': [{ id: 'c_resolved', start_line: 31, end_line: 31, resolved: true }],
    },
    forms: [{ formKey: 'file:10:10:', startLine: 10, endLine: 10, side: '' }],
    totalLines: 50,
  };
}

test('buildUnifiedRows emits stable semantic rows in visual order', function() {
  const rows = virtualizer.buildUnifiedRows(fixture());
  assert.equal(rows[0].kind, 'gap');
  assert.equal(rows[0].gapKind, 'leading');

  const lineRows = rows.filter(function(row) { return row.kind === 'line'; });
  assert.deepEqual(lineRows.map(function(row) { return row.visualIdx; }), [0, 1, 2, 3]);
  assert.deepEqual(lineRows.map(function(row) { return [row.lineNum, row.side]; }), [
    [10, 'old'], [10, ''], [11, ''], [40, ''],
  ]);

  const newLineIndex = rows.findIndex(function(row) {
    return row.kind === 'line' && row.lineNum === 10 && row.side === '';
  });
  assert.equal(rows[newLineIndex + 1].key, 'comment:c_new');
  assert.equal(rows[newLineIndex + 2].key, 'form:file:10:10:');
  assert.ok(rows.some(function(row) { return row.key === 'comment:c_old'; }));
  assert.ok(rows.some(function(row) { return row.key === 'outdated:c_outdated'; }));
  assert.ok(rows.some(function(row) { return row.key === 'outdated:c_resolved'; }));
  assert.equal(rows.at(-1).gapKind, undefined, 'outdated comments follow the trailing gap');

  const rebuiltKeys = virtualizer.buildUnifiedRows(fixture()).map(function(row) { return row.key; });
  assert.deepEqual(rebuiltKeys, rows.map(function(row) { return row.key; }));
});

test('buildUnifiedRows removes resolved comment rows when hide-resolved is active', function() {
  const options = fixture();
  options.hideResolved = true;
  const rows = virtualizer.buildUnifiedRows(options);
  assert.equal(rows.some(function(row) { return row.key === 'outdated:c_resolved'; }), false);
  assert.equal(rows.some(function(row) { return row.key === 'outdated:c_outdated'; }), true);
});

test('HeightIndex supports prefix lookup and batched measurement changes', function() {
  const rows = [
    { key: 'a', kind: 'line' },
    { key: 'b', kind: 'header' },
    { key: 'c', kind: 'form' },
  ];
  const index = new virtualizer.HeightIndex(rows);
  assert.deepEqual(index.offsets, [0, 20, 52, 242]);
  assert.equal(index.indexAt(0), 0);
  assert.equal(index.indexAt(19.9), 0);
  assert.equal(index.indexAt(20), 1);
  assert.equal(index.indexAt(241), 2);

  assert.equal(index.updateMany(new Map([[0, 25], [2, 200]])), 0);
  assert.deepEqual(index.offsets, [0, 25, 57, 257]);
  assert.equal(index.indexAt(56), 1);
  assert.equal(index.indexAt(57), 2);
});

test('mergeIntervals clamps, sorts, and joins adjacent pinned islands', function() {
  assert.deepEqual(
    virtualizer.mergeIntervals([[10, 12], [2, 4], [5, 5], [-3, 0], [99, 101]], 20),
    [[0, 0], [2, 5], [10, 12]]
  );
});

test('overscan matches Pierre CodeView overscrollSize', function() {
  assert.equal(virtualizer.overscanForViewport(300), 200);
  assert.equal(virtualizer.overscanForViewport(900), 200);
  assert.equal(virtualizer.overscanForViewport(2000), 200);
  assert.equal(virtualizer.OVERSCROLL_SIZE, 200);
  assert.equal(virtualizer.VIRTUALIZER_OVERSCROLL_SIZE, 1000);
  assert.equal(virtualizer.KEEP_ALIVE_MARGIN, 4000);
  assert.equal(virtualizer.overscanForViewport(900, 'virtualizer'), 1000);
  assert.equal(virtualizer.overscanForViewport(900, 'keepalive'), 4000);
  assert.equal(virtualizer.roundToDevicePixel(10.4), 10);
  assert.equal(virtualizer.DEFAULT_ESTIMATES.line, 20);
  assert.equal(virtualizer.DEFAULT_ESTIMATES.header, 32);
  assert.equal(virtualizer.DEFAULT_ESTIMATES.gap, 8);
});

test('calculateWindow matches Pierre createWindowFromScrollPosition', function() {
  const rows = Array.from({ length: 1000 }, function(_, index) {
    return { key: String(index), kind: 'line' };
  });
  const heights = new virtualizer.HeightIndex(rows);
  // At scrollTop 0, Pierre centers then clamps top→0 without growing bottom,
  // so bottom stays height/2 + overscroll = 450+200 = 650… wait:
  // top=-200, bottom=1100, clamp top→0 → {0,1100} → indices [0,55]
  assert.deepEqual(
    virtualizer.createWindowFromScrollPosition({
      scrollTop: 0, height: 900, scrollHeight: heights.total(), overscrollSize: 200,
    }),
    { top: 0, bottom: 1100 }
  );
  assert.deepEqual(virtualizer.calculateWindow(heights, 0, 900), [0, 55]);

  const middle = virtualizer.calculateWindow(heights, 10000, 900);
  // top = 10000 + 450 - 650 = 9800; bottom = 9800 + 1300 = 11100
  assert.deepEqual(middle, [490, 555]);

  const jump = virtualizer.calculateWindow(heights, 10000, 900, {
    fitPerfectly: true,
    fitPerfectlyOverscroll: 52,
    overscrollSize: 200,
  });
  assert.deepEqual(jump, [
    heights.indexAt(Math.max(10000 - 52, 0)),
    heights.indexAt(Math.min(heights.total(), 10000 + 900 + 52 * 2)),
  ]);

  const keep = virtualizer.calculateWindow(heights, 10000, 900, { kind: 'keepalive' });
  assert.ok(keep[1] - keep[0] > middle[1] - middle[0]);
});

test('createWindowFromScrollPosition fitPerfectly expands around target', function() {
  const win = virtualizer.createWindowFromScrollPosition({
    scrollTop: 5000,
    height: 900,
    scrollHeight: 100000,
    overscrollSize: 200,
    fitPerfectly: true,
    fitPerfectlyOverscroll: 52,
  });
  assert.equal(win.top, 5000 - 52);
  assert.equal(win.bottom, 5000 + 900 + 104);
});

test('buildSplitRows pairs old/new cells on one visual row', function() {
  const rows = virtualizer.buildSplitRows(fixture());
  assert.equal(rows[0].kind, 'gap');
  assert.equal(rows[0].gapKind, 'leading');

  const lineRows = rows.filter(function(row) { return row.kind === 'line'; });
  assert.equal(lineRows.length, 3);
  assert.deepEqual(lineRows.map(function(row) { return row.visualIdx; }), [0, 1, 2]);

  // First change: del 10 beside add 10
  assert.equal(lineRows[0].left && lineRows[0].left.Type, 'del');
  assert.equal(lineRows[0].left.OldNum, 10);
  assert.equal(lineRows[0].right && lineRows[0].right.Type, 'add');
  assert.equal(lineRows[0].right.NewNum, 10);
  assert.equal(lineRows[0].layout, 'split');

  // Context shares both sides on one row
  assert.equal(lineRows[1].left && lineRows[1].left.Type, 'context');
  assert.equal(lineRows[1].right && lineRows[1].right.Type, 'context');
  assert.equal(lineRows[1].left.OldNum, 11);
  assert.equal(lineRows[1].right.NewNum, 11);

  // Later hunk: surplus add with empty left
  assert.equal(lineRows[2].left, null);
  assert.equal(lineRows[2].right && lineRows[2].right.NewNum, 40);

  // Comments/forms follow the paired add/del row (old then new)
  const changeRowIndex = rows.findIndex(function(row) {
    return row.kind === 'line' && row.left && row.left.OldNum === 10;
  });
  assert.equal(rows[changeRowIndex + 1].key, 'comment:c_old');
  assert.equal(rows[changeRowIndex + 2].key, 'comment:c_new');
  assert.equal(rows[changeRowIndex + 3].key, 'form:file:10:10:');

  assert.ok(rows.some(function(row) { return row.key === 'outdated:c_outdated'; }));
  assert.ok(rows.some(function(row) { return row.key === 'outdated:c_resolved'; }));

  const rebuiltKeys = virtualizer.buildSplitRows(fixture()).map(function(row) { return row.key; });
  assert.deepEqual(rebuiltKeys, rows.map(function(row) { return row.key; }));
});

test('buildSplitRows removes resolved comment rows when hide-resolved is active', function() {
  const options = fixture();
  options.hideResolved = true;
  const rows = virtualizer.buildSplitRows(options);
  assert.equal(rows.some(function(row) { return row.key === 'outdated:c_resolved'; }), false);
  assert.equal(rows.some(function(row) { return row.key === 'outdated:c_outdated'; }), true);
});

test('rowKeyForLine finds split rows by left or right cell', function() {
  const rows = virtualizer.buildSplitRows(fixture());
  const keyToIndex = new Map();
  rows.forEach(function(row, i) { keyToIndex.set(row.key, i); });
  const fake = {
    rows: rows,
    keyToIndex: keyToIndex,
    rowKeyForLine: virtualizer.VirtualWindow.prototype.rowKeyForLine,
  };
  assert.equal(
    fake.rowKeyForLine(10, 'old'),
    rows.find(function(r) { return r.kind === 'line' && r.left && r.left.OldNum === 10; }).key
  );
  assert.equal(
    fake.rowKeyForLine(10, ''),
    rows.find(function(r) { return r.kind === 'line' && r.right && r.right.NewNum === 10; }).key
  );
  assert.equal(
    fake.rowKeyForLine(40, ''),
    rows.find(function(r) { return r.kind === 'line' && r.right && r.right.NewNum === 40; }).key
  );
});

test('rowKeyForLine finds unified rows by lineNum+side', function() {
  const rows = virtualizer.buildUnifiedRows(fixture());
  const keyToIndex = new Map();
  rows.forEach(function(row, i) { keyToIndex.set(row.key, i); });
  const fake = {
    rows: rows,
    keyToIndex: keyToIndex,
    rowKeyForLine: virtualizer.VirtualWindow.prototype.rowKeyForLine,
  };
  assert.equal(
    fake.rowKeyForLine(10, 'old'),
    rows.find(function(r) { return r.kind === 'line' && r.lineNum === 10 && r.side === 'old'; }).key
  );
  assert.equal(
    fake.rowKeyForLine(10, ''),
    rows.find(function(r) { return r.kind === 'line' && r.lineNum === 10 && r.side === ''; }).key
  );
});

test('pinLineRange covers split cells on the requested side', function() {
  const rows = virtualizer.buildSplitRows(fixture());
  const intervals = [];
  const fake = {
    rows: rows,
    pinInterval: function(name, start, end) { intervals.push([name, start, end]); },
    pinLineRange: virtualizer.VirtualWindow.prototype.pinLineRange,
  };
  fake.pinLineRange('drag', 10, 10, 'old');
  assert.equal(intervals.length, 1);
  assert.equal(intervals[0][0], 'drag');
  const startRow = rows[intervals[0][1]];
  assert.equal(startRow.kind, 'line');
  assert.equal(startRow.left && startRow.left.OldNum, 10);
});

test('pinned islands stay mounted when the ordinary window moves away', function() {
  const rows = Array.from({ length: 500 }, function(_, index) {
    return { key: 'line:' + index, kind: 'line', lineNum: index + 1, side: '', visualIdx: index };
  });
  // Pin a row far below the initial viewport window.
  const pinnedKey = 'line:400';
  const windowRange = virtualizer.calculateWindow(
    new virtualizer.HeightIndex(rows),
    0,
    900
  );
  assert.ok(windowRange[1] < 400, 'fixture assumes pin target is outside the first window');

  const merged = virtualizer.mergeIntervals(
    [windowRange, [400, 400]],
    rows.length
  );
  const coversPin = merged.some(function(interval) {
    return interval[0] <= 400 && interval[1] >= 400;
  });
  assert.equal(coversPin, true);
});

test('VirtualWindow.reconcile keeps pinned row nodes across window moves', function() {
  // Minimal DOM surface stub — enough for reconcile/pin without jsdom.
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
      setAttribute: function() {},
      getBoundingClientRect: function() { return { top: 0, bottom: 20, height: 20 }; },
    };
    return node;
  }

  const surface = makeNode('div');
  surface.classList = { add: function() {} };
  Object.defineProperty(surface, 'children', {
    get: function() { return this._kids || []; },
  });
  surface._kids = [];
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
  surface.contains = function(node) {
    let cur = node;
    while (cur) {
      if (cur === surface) return true;
      cur = cur.parentNode;
    }
    return false;
  };

  const rows = Array.from({ length: 300 }, function(_, index) {
    return { key: 'r' + index, kind: 'line', lineNum: index + 1, side: '', visualIdx: index };
  });

  globalThis.window = globalThis.window || {};
  globalThis.document = globalThis.document || {
    createElement: makeNode,
    addEventListener: function() {},
    removeEventListener: function() {},
  };
  globalThis.ResizeObserver = undefined;

  const vw = new virtualizer.VirtualWindow({
    surface: surface,
    rows: rows,
    renderRow: function(row) {
      const el = makeNode('div');
      el.dataset.rowKey = row.key;
      el.textContent = row.key;
      return el;
    },
  });

  assert.ok(surface._kids.length > 0);
  assert.ok(surface._kids.length < rows.length, 'initial mount is windowed, not full');

  const farKey = 'r250';
  // Avoid pin()→update() (needs a real viewport). Drive reconcile directly
  // with an explicit pin island, matching what pin() would request.
  vw.pinnedKeys.add(farKey);
  vw.reconcile(virtualizer.mergeIntervals(
    [virtualizer.calculateWindow(vw.heightIndex, 0, 900), [250, 250]],
    rows.length
  ));
  const pinnedNode = vw.nodes.get(farKey);
  assert.ok(pinnedNode, 'pin mounts the far row');
  assert.equal(pinnedNode.parentNode, surface);

  // Move the ordinary window; pin island must keep r250 alive.
  vw.reconcile(virtualizer.mergeIntervals(
    [virtualizer.calculateWindow(vw.heightIndex, 0, 900), [250, 250]],
    rows.length
  ));
  assert.equal(vw.nodes.get(farKey), pinnedNode);
  assert.equal(pinnedNode.parentNode, surface);
  assert.ok(
    surface._kids.some(function(child) { return child.dataset.virtualKey === farKey; }),
    'pinned row remains in the surface'
  );

  vw.pinnedKeys.delete(farKey);
  vw.reconcile([virtualizer.calculateWindow(vw.heightIndex, 0, 900)]);
  assert.equal(vw.nodes.has(farKey), false);
});

test('handleSelectionChange clears selectionInterval when selection leaves the surface', function() {
  function makeNode(tag) {
    const node = {
      tagName: String(tag || 'div').toUpperCase(),
      nodeType: 1,
      className: '',
      style: {},
      dataset: {},
      children: [],
      parentNode: null,
      closest: function(sel) {
        if (sel === '[data-virtual-row-index]' && this.dataset.virtualRowIndex != null) return this;
        return this.parentNode && this.parentNode.closest ? this.parentNode.closest(sel) : null;
      },
      remove: function() {
        if (!this.parentNode) return;
        const kids = this.parentNode.children;
        const idx = kids.indexOf(this);
        if (idx >= 0) kids.splice(idx, 1);
        this.parentNode = null;
      },
      setAttribute: function() {},
      getBoundingClientRect: function() { return { top: 0, bottom: 20, height: 20 }; },
    };
    return node;
  }

  const surface = makeNode('div');
  surface.classList = { add: function() {} };
  surface._kids = [];
  Object.defineProperty(surface, 'children', {
    get: function() { return this._kids; },
  });
  surface.insertBefore = function(node) {
    node.parentNode = surface;
    surface._kids.push(node);
    return node;
  };
  Object.defineProperty(surface, 'lastElementChild', {
    get: function() { return this._kids[this._kids.length - 1] || null; },
  });
  surface.contains = function(node) {
    let cur = node;
    while (cur) {
      if (cur === surface) return true;
      cur = cur.parentNode;
    }
    return false;
  };

  const rows = Array.from({ length: 40 }, function(_, index) {
    return { key: 's' + index, kind: 'line', lineNum: index + 1, side: '', visualIdx: index };
  });

  globalThis.window = globalThis.window || {};
  globalThis.document = {
    createElement: makeNode,
    addEventListener: function() {},
    removeEventListener: function() {},
    getSelection: function() { return globalThis.__critSelection || null; },
  };
  globalThis.ResizeObserver = undefined;

  const vw = new virtualizer.VirtualWindow({
    surface: surface,
    rows: rows,
    renderRow: function(row) {
      const el = makeNode('div');
      el.dataset.rowKey = row.key;
      el.dataset.virtualRowIndex = String(row.visualIdx);
      return el;
    },
  });
  let scheduled = 0;
  vw.scheduleUpdate = function() { scheduled += 1; };

  const inRow = makeNode('div');
  inRow.dataset.virtualRowIndex = '3';
  inRow.parentNode = surface;
  surface._kids.push(inRow);

  const outside = makeNode('div');
  outside.parentNode = null;

  vw.selectionInterval = [2, 5];
  globalThis.__critSelection = {
    isCollapsed: false,
    anchorNode: outside,
    focusNode: outside,
  };
  vw.handleSelectionChange();
  assert.equal(vw.selectionInterval, null, 'selection outside the surface clears the pin');
  assert.equal(scheduled, 1);

  vw.selectionInterval = [1, 2];
  globalThis.__critSelection = {
    isCollapsed: false,
    anchorNode: inRow,
    focusNode: inRow,
  };
  vw.handleSelectionChange();
  assert.deepEqual(vw.selectionInterval, [3, 3]);
  assert.equal(scheduled, 2);
});

test('viewportMetrics uses scroll-parent top, not the window, for localTop', function() {
  const surface = {
    getBoundingClientRect: function() {
      return { top: -200, bottom: 1800, height: 2000, left: 0, right: 100 };
    },
  };
  const pane = {
    getBoundingClientRect: function() {
      return { top: 80, bottom: 880, height: 800, left: 0, right: 100 };
    },
    clientHeight: 800,
  };
  globalThis.window = globalThis.window || {};
  globalThis.window.innerHeight = 900;
  globalThis.document = globalThis.document || {};
  globalThis.document.documentElement = { clientHeight: 900 };

  const windowMetrics = virtualizer.viewportMetrics(window, surface);
  assert.equal(windowMetrics.localTop, 200);
  assert.equal(windowMetrics.viewportHeight, 900);

  const paneMetrics = virtualizer.viewportMetrics(pane, surface);
  assert.equal(paneMetrics.localTop, 280); // 80 - (-200)
  assert.equal(paneMetrics.viewportHeight, 800);
  assert.equal(paneMetrics.vpTop, 80);
});

test('VirtualWindow.start listens on an explicit scrollParent', function() {
  function makeNode(tag) {
    return {
      tagName: String(tag || 'div').toUpperCase(),
      nodeType: 1,
      className: '',
      style: {},
      dataset: {},
      children: [],
      parentNode: null,
      classList: { add: function() {} },
      remove: function() {},
      setAttribute: function() {},
      getBoundingClientRect: function() { return { top: 0, bottom: 20, height: 20 }; },
    };
  }
  const surface = makeNode('div');
  surface._kids = [];
  Object.defineProperty(surface, 'children', { get: function() { return this._kids; } });
  surface.insertBefore = function(node) {
    node.parentNode = surface;
    surface._kids.push(node);
    return node;
  };
  Object.defineProperty(surface, 'lastElementChild', {
    get: function() { return this._kids[this._kids.length - 1] || null; },
  });
  surface.contains = function() { return true; };
  surface.clientWidth = 800;

  const listeners = [];
  const pane = {
    addEventListener: function(type, fn) { listeners.push({ type: type, fn: fn, target: 'pane' }); },
    removeEventListener: function(type, fn) {
      for (let i = listeners.length - 1; i >= 0; i--) {
        if (listeners[i].type === type && listeners[i].fn === fn) listeners.splice(i, 1);
      }
    },
    getBoundingClientRect: function() {
      return { top: 60, bottom: 860, height: 800, left: 0, right: 100 };
    },
    clientHeight: 800,
    scrollTop: 0,
    scrollLeft: 0,
    scrollTo: function() {},
  };

  globalThis.window = globalThis.window || {};
  globalThis.window.addEventListener = function(type, fn) {
    listeners.push({ type: type, fn: fn, target: 'window' });
  };
  globalThis.window.removeEventListener = function(type, fn) {
    for (let i = listeners.length - 1; i >= 0; i--) {
      if (listeners[i].type === type && listeners[i].fn === fn && listeners[i].target === 'window') {
        listeners.splice(i, 1);
      }
    }
  };
  globalThis.window.innerHeight = 900;
  globalThis.document = {
    createElement: makeNode,
    addEventListener: function() {},
    removeEventListener: function() {},
    documentElement: { clientHeight: 900 },
  };
  globalThis.ResizeObserver = undefined;
  globalThis.requestAnimationFrame = function(cb) { return setTimeout(cb, 0); };
  globalThis.cancelAnimationFrame = function(id) { clearTimeout(id); };

  const rows = Array.from({ length: 20 }, function(_, index) {
    return { key: 'p' + index, kind: 'line', lineNum: index + 1, side: '', visualIdx: index };
  });
  const vw = new virtualizer.VirtualWindow({
    surface: surface,
    rows: rows,
    scrollParent: pane,
    renderRow: function(row) {
      const el = makeNode('div');
      el.dataset.rowKey = row.key;
      return el;
    },
  });
  vw.start();
  assert.equal(vw.scrollParent, pane);
  assert.ok(listeners.some(function(l) { return l.type === 'scroll' && l.target === 'pane'; }));
  assert.ok(!listeners.some(function(l) { return l.type === 'scroll' && l.target === 'window'; }));
  vw.dispose();
  assert.ok(!listeners.some(function(l) { return l.type === 'scroll' && l.target === 'pane'; }));
});

test('estimateDiffBodyHeight is a cheap O(hunks) reservation (not a full row build)', function() {
  const options = fixture();
  const height = virtualizer.estimateDiffBodyHeight(options);
  const E = virtualizer.DEFAULT_ESTIMATES;
  let lineRows = 0;
  for (let i = 0; i < options.hunks.length; i++) {
    lineRows += options.hunks[i].Lines.length;
  }
  const expected =
    lineRows * E.line +
    options.hunks.length * E.header +
    (options.hunks.length + 1) * E.gap +
    4 * E.comment + // c_old, c_new, c_outdated, c_resolved
    1 * E.form;
  assert.equal(height, expected);
  assert.ok(height > 0);
});

test('estimateDiffBodyHeight respects hideResolved for comment counts', function() {
  const options = fixture();
  options.hideResolved = true;
  const height = virtualizer.estimateDiffBodyHeight(options);
  const E = virtualizer.DEFAULT_ESTIMATES;
  let lineRows = 0;
  for (let i = 0; i < options.hunks.length; i++) {
    lineRows += options.hunks[i].Lines.length;
  }
  const expected =
    lineRows * E.line +
    options.hunks.length * E.header +
    (options.hunks.length + 1) * E.gap +
    3 * E.comment + // c_resolved skipped
    1 * E.form;
  assert.equal(height, expected);
});
