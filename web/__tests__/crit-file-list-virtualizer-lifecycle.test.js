'use strict';
// Behavioral tests for FileListVirtualizer lifecycle: pins, holds, stick
// settle, teardown and user-intent handling. Runs against a tiny fake layout
// (block stacking + a scroll container) instead of asserting on source text.

const test = require('node:test');
const assert = require('node:assert/strict');

// Object.assign would evaluate getters; copy property descriptors instead.
function extend(target, src) {
  return Object.defineProperties(target, Object.getOwnPropertyDescriptors(src));
}

// ---- Fake browser globals (must exist before the modules load) ----------

function makeTarget() {
  const listeners = new Map();
  return {
    addEventListener(type, fn) {
      if (!listeners.has(type)) listeners.set(type, new Set());
      listeners.get(type).add(fn);
    },
    removeEventListener(type, fn) {
      if (listeners.has(type)) listeners.get(type).delete(fn);
    },
    dispatch(type, event) {
      (listeners.get(type) || new Set()).forEach(function(fn) { fn(event || {}); });
    },
    listenerCount(type) {
      return listeners.has(type) ? listeners.get(type).size : 0;
    },
  };
}

global.window = extend(makeTarget(), { devicePixelRatio: 1, innerHeight: 800 });
global.document = {
  documentElement: { style: {} },
  createElement: function() { return makeNode(); },
  querySelector: function() { return null; },
};
global.getComputedStyle = function() {
  return { getPropertyValue: function() { return ''; }, scrollMarginTop: '67px' };
};
let rafQueue = [];
global.requestAnimationFrame = function(fn) { rafQueue.push(fn); return rafQueue.length; };
global.cancelAnimationFrame = function() {};
function flushFrames() {
  for (let i = 0; i < 10 && rafQueue.length; i++) {
    const q = rafQueue;
    rafQueue = [];
    q.forEach(function(fn) { fn(); });
  }
}

const observed = new Set();
let observerCallback = null;
global.ResizeObserver = function(cb) {
  observerCallback = cb;
  this.observe = function(n) { observed.add(n); };
  this.unobserve = function(n) { observed.delete(n); };
  this.disconnect = function() { observed.clear(); };
};

const fileList = require('../crit-file-list-virtualizer.js');

// ---- Fake layout ------------------------------------------------------------

const VIEWPORT = 800;
const TARGET_TOP = 67; // scrollMarginTop above

// Layout truth for mounted sections, by key.
let realHeights = new Map();
let scroller;
let surface;

function makeNode() {
  const node = {
    nodeType: 1,
    tagName: 'DIV',
    className: '',
    style: {},
    dataset: {},
    parent: null,
    setAttribute: function() {},
    classList: {
      contains: function(c) { return (' ' + node.className + ' ').indexOf(' ' + c + ' ') !== -1; },
    },
    get isConnected() { return !!node.parent; },
    remove: function() {
      if (!node.parent) return;
      const kids = node.parent.children;
      kids.splice(kids.indexOf(node), 1);
      node.parent = null;
    },
    getBoundingClientRect: function() {
      if (!node.parent) return { top: 0, bottom: 0, height: 0 };
      let top = surface.getBoundingClientRect().top;
      for (const kid of surface.children) {
        if (kid === node) break;
        top += heightOf(kid);
      }
      const h = heightOf(node);
      return { top: top, bottom: top + h, height: h };
    },
  };
  return node;
}

function heightOf(node) {
  if (node.style.height) return parseFloat(node.style.height);
  return realHeights.get(node.dataset.virtualKey) || 0;
}

function contentHeight() {
  let h = 0;
  surface.children.forEach(function(kid) { h += heightOf(kid); });
  return h + (parseFloat(surface.style.paddingBottom) || 0);
}

function setup(opts) {
  opts = opts || {};
  const count = opts.count || 50;
  realHeights = new Map();
  observed.clear();
  rafQueue = [];
  const unmounted = [];
  const items = [];
  for (let i = 0; i < count; i++) {
    const key = 'f' + i;
    items.push({ key: key, bodyHeight: 400 });
    realHeights.set(key, opts.realHeight || 444);
  }
  let scrollTop = 0;
  scroller = extend(makeTarget(), {
    clientHeight: VIEWPORT,
    getBoundingClientRect: function() { return { top: 0, bottom: VIEWPORT, height: VIEWPORT }; },
    get scrollTop() { return scrollTop; },
    set scrollTop(v) {
      const max = Math.max(0, contentHeight() - VIEWPORT);
      const clamped = Math.max(0, Math.min(v, max));
      // Scrollers snap to whole pixels; element offsets need not.
      scrollTop = opts.quantize ? Math.round(clamped) : clamped;
    },
  });
  surface = extend(makeNode(), {
    children: [],
    get firstChild() { return surface.children[0] || null; },
    get lastElementChild() { return surface.children[surface.children.length - 1] || null; },
    insertBefore: function(n, ref) {
      if (n.parent) n.remove();
      const idx = ref ? surface.children.indexOf(ref) : -1;
      if (idx < 0) surface.children.push(n); else surface.children.splice(idx, 0, n);
      n.parent = surface;
      return n;
    },
    removeChild: function(n) { n.remove(); return n; },
    getBoundingClientRect: function() {
      return { top: (opts.surfaceTop || 0) - scroller.scrollTop, bottom: 0, height: contentHeight() };
    },
  });
  const fl = new fileList.FileListVirtualizer({
    surface: surface,
    scrollParent: scroller,
    items: items,
    estimateHeight: function() { return 444; },
    renderMounted: function(item) {
      const n = makeNode();
      n.dataset.filePath = item.key;
      return n;
    },
    onUnmount: function(key) { unmounted.push(key); },
  });
  fl.start();
  return { fl: fl, unmounted: unmounted };
}

function topOf(fl, key) {
  return fl.nodes.get(key).getBoundingClientRect().top;
}

// ---- Tests ------------------------------------------------------------------

test('tree jumps do not accumulate pins: only the latest target stays mounted', function() {
  const { fl } = setup();
  for (const key of ['f10', 'f30', 'f45']) {
    fl.stickToKey(key);
    fl.scrollToItem(key, 'start');
    flushFrames();
    fl.update();
  }
  assert.equal(fl.pinnedKeys.size, 0, 'jumps must not add caller-owned pins');
  assert.ok(fl.mountedKeys.has('f45'));
  assert.ok(!fl.mountedKeys.has('f10'), 'earlier jump target unmounted');
  assert.ok(!fl.mountedKeys.has('f30'), 'earlier jump target unmounted');
  fl.dispose();
});

test('stickToKey pins the target under the header and settles', function() {
  const { fl } = setup();
  fl.stickToKey('f30');
  fl.scrollToItem('f30', 'start');
  fl.update();
  fl.update();
  assert.equal(topOf(fl, 'f30'), TARGET_TOP);
  assert.equal(fl.stickKey(), null, 'settled stick is released');
  assert.ok(fl.isScrollLocked(), 'lock survives the settle');
  fl.dispose();
});

// Seen in Chromium at DPR 2: the header lands at 66.5 against a 67 target,
// the 0.5px correction snaps back, and the stick never released.
function withDpr(dpr, fn) {
  const prev = window.devicePixelRatio;
  window.devicePixelRatio = dpr;
  try { fn(); } finally { window.devicePixelRatio = prev; }
}

test('an unreachable sub-pixel stick target still releases', function() {
  withDpr(2, function() {
    const { fl } = setup({ quantize: true, surfaceTop: 0.3 });
    fl.stickToKey('f20');
    fl.scrollToItem('f20', 'start');
    for (let i = 0; i < 3; i++) fl.update();
    assert.ok(Math.abs(topOf(fl, 'f20') - TARGET_TOP) <= 0.5);
    assert.equal(fl.stickKey(), null);
    fl.dispose();
  });
});

test('a stick clamped at scroll top releases instead of waiting', function() {
  // First file starts above the header target at scrollTop 0 — unreachable.
  const { fl } = setup({ surfaceTop: 20 });
  fl.stickToKey('f0');
  fl.scrollToItem('f0', 'start');
  for (let i = 0; i < 3; i++) fl.update();
  assert.equal(scroller.scrollTop, 0);
  assert.equal(fl.stickKey(), null);
  fl.dispose();
});

test('a released stick does not yank a later programmatic scroll', function() {
  withDpr(2, function() {
    const { fl } = setup({ quantize: true, surfaceTop: 0.3 });
    fl.stickToKey('f20');
    fl.scrollToItem('f20', 'start');
    for (let i = 0; i < 3; i++) fl.update();
    scroller.scrollTop = scroller.scrollTop + 300; // e.g. j/k scrollIntoView
    const after = scroller.scrollTop;
    fl.update();
    assert.equal(scroller.scrollTop, after);
    fl.dispose();
  });
});

test('ensureMounted holds a slot temporarily, not forever', function() {
  const { fl } = setup();
  fl.ensureMounted('f40', 1000);
  assert.ok(fl.mountedKeys.has('f40'));
  assert.equal(fl.pinnedKeys.size, 0);
  fl._holdUntil.set('f40', Date.now() - 1); // expire
  fl.update();
  assert.ok(!fl.mountedKeys.has('f40'), 'expired hold is unmounted');
  fl.dispose();
});

test('setItems tears down mounted sections (onUnmount + unobserve)', function() {
  const { fl, unmounted } = setup();
  const before = Array.from(fl.mountedKeys);
  const oldNodes = before.map(function(k) { return fl.nodes.get(k); });
  assert.ok(before.length > 0);
  fl.setItems(fl.items.map(function(item) { return Object.assign({}, item, { collapsed: true }); }));
  before.forEach(function(key) { assert.ok(unmounted.includes(key), 'onUnmount for ' + key); });
  oldNodes.forEach(function(node) { assert.ok(!observed.has(node), 'detached node unobserved'); });
  fl.dispose();
});

test('ResizeObserver entries from detached nodes do not zero heights', function() {
  const { fl } = setup();
  const node = fl.nodes.get('f0');
  const index = fl.keyToIndex.get('f0');
  node.remove();
  observerCallback([{ target: node, borderBoxSize: [{ blockSize: 0 }] }]);
  flushFrames();
  assert.ok(fl.heightIndex.height(index) > 0);
  fl.dispose();
});

test('keydown: scroll keys fully clear, other keys clear pending only, typing is ignored', function() {
  const { fl } = setup();
  const textarea = extend(makeNode(), { tagName: 'TEXTAREA' });

  fl.stickToKey('f10');
  window.dispatch('keydown', { key: ' ', target: textarea });
  assert.equal(fl.stickKey(), 'f10', 'space typed in a form is not a scroll intent');

  window.dispatch('keydown', { key: 'j', target: {} });
  assert.equal(fl.stickKey(), null, 'j clears the pending stick');
  assert.ok(fl.isScrollLocked(), 'j keeps the lock');

  window.dispatch('keydown', { key: 'PageDown', target: {} });
  assert.ok(!fl.isScrollLocked(), 'PageDown releases the lock');
  fl.dispose();
});

test('pointerdown clears pending only; wheel clears lock and padding', function() {
  const { fl } = setup();
  fl.stickToKey('f10');
  scroller.dispatch('pointerdown');
  assert.equal(fl.stickKey(), null);
  assert.ok(fl.isScrollLocked());
  assert.ok(fl._pinPaddingPx > 0);
  scroller.dispatch('wheel');
  assert.ok(!fl.isScrollLocked());
  assert.equal(fl._pinPaddingPx, 0);
  fl.dispose();
});

test('dispose removes every listener it added', function() {
  const keydownBefore = window.listenerCount('keydown');
  const resizeBefore = window.listenerCount('resize');
  const { fl } = setup();
  fl.dispose();
  ['scroll', 'wheel', 'touchstart', 'touchmove', 'pointerdown'].forEach(function(type) {
    assert.equal(scroller.listenerCount(type), 0, type);
  });
  assert.equal(window.listenerCount('keydown'), keydownBefore);
  assert.equal(window.listenerCount('resize'), resizeBefore);
});
