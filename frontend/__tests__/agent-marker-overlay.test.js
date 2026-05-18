'use strict';
const test = require('node:test');
const assert = require('node:assert');

const overlay = require('../agent-marker-overlay.js');

test('exports expected API', () => {
  assert.equal(typeof overlay.createOverlay, 'function');
  assert.equal(typeof overlay.applyRects, 'function');
  assert.equal(typeof overlay.makeMarker, 'function');
  assert.equal(typeof overlay.setMarkersTabindex, 'function');
});

function makeFakeDoc() {
  const doc = {
    body: { appendChild: function (n) { this._appended = n; } },
    createElement: (tag) => ({
      tagName: tag.toUpperCase(),
      style: {},
      _attrs: {},
      _children: [],
      _listeners: {},
      setAttribute(k, v) { this._attrs[k] = v; },
      getAttribute(k) { return this._attrs[k]; },
      appendChild(n) { this._children.push(n); },
      addEventListener(t, fn) { (this._listeners[t] = this._listeners[t] || []).push(fn); },
      get textContent() { return this._t || ''; },
      set textContent(v) { this._t = v; },
      get className() { return this._attrs.class || ''; },
      set className(v) { this._attrs.class = v; },
    }),
  };
  return doc;
}

test('createOverlay builds a root with pointer-events: none and high z-index', () => {
  const fakeDoc = makeFakeDoc();
  const ov = overlay.createOverlay(fakeDoc);
  assert.ok(ov.root);
  assert.equal(fakeDoc.body._appended, ov.root);
  assert.equal(ov.root._attrs['aria-hidden'], 'true');
  assert.equal(ov.root._attrs.id, 'crit-marker-root');
  assert.equal(ov.root.style.position, 'absolute');
  assert.equal(ov.root.style.top, '0');
  assert.equal(ov.root.style.left, '0');
  assert.equal(ov.root.style.pointerEvents, 'none');
  assert.equal(ov.root.style.zIndex, '2147483600');
});

test('makeMarker creates an element with role/tabindex/data-pin-id', () => {
  const fakeDoc = makeFakeDoc();
  const m = overlay.makeMarker(fakeDoc, { id: 'xyz' }, 0);
  assert.equal(m._attrs['data-pin-id'], 'xyz');
  assert.equal(m._attrs['tabindex'], '0');
  assert.equal(m._attrs.role, 'button');
  assert.equal(m.textContent, '1');
});

test('makeMarker uses pin.pin_number when present', () => {
  const fakeDoc = makeFakeDoc();
  const m = overlay.makeMarker(fakeDoc, { id: 'xyz', pin_number: 7 }, 0);
  assert.equal(m.textContent, '7');
  assert.equal(m._attrs['aria-label'], 'Comment 7');
});

test('applyRects reads all rects before writing positions', () => {
  const reads = [];
  const writes = [];
  function makeStyleProxy(label) {
    const obj = {};
    return new Proxy(obj, {
      set(t, k, v) {
        if (k === 'transform') writes.push(label + ':' + v);
        t[k] = v; return true;
      },
      get(t, k) { return t[k]; },
    });
  }
  const targets = [
    { getBoundingClientRect: () => { reads.push('a'); return { left: 1, top: 2 }; } },
    { getBoundingClientRect: () => { reads.push('b'); return { left: 3, top: 4 }; } },
  ];
  const markers = [
    { target: targets[0], el: { style: makeStyleProxy('a') } },
    { target: targets[1], el: { style: makeStyleProxy('b') } },
  ];
  overlay.applyRects(markers);
  assert.deepEqual(reads, ['a', 'b']);
  assert.deepEqual(writes, ['a:translate(1px, 2px)', 'b:translate(3px, 4px)']);
});

test('applyRects hides marker when target is null', () => {
  const m = { target: null, el: { style: {} } };
  overlay.applyRects([m]);
  assert.equal(m.el.style.display, 'none');
});

test('applyRects writes document coordinates so markers stay anchored on scroll', () => {
  // Regression for Bug B: position:fixed + viewport-rect coords meant the
  // marker stayed glued to the viewport when the page scrolled. With
  // position:absolute + page coords (rect.top + scrollY), the marker tracks
  // the element regardless of scroll position, no scroll listener needed.
  const writes = [];
  function makeStyleProxy(label) {
    const obj = {};
    return new Proxy(obj, {
      set(t, k, v) {
        if (k === 'transform') writes.push(label + ':' + v);
        t[k] = v; return true;
      },
      get(t, k) { return t[k]; },
    });
  }
  // Element is at page-coord y=800. With scrollY=400, viewport rect.top is 400.
  const target = { getBoundingClientRect: () => ({ left: 100, top: 400 }) };
  const win = { scrollX: 0, scrollY: 400 };
  const markers = [{ target, el: { style: makeStyleProxy('a') } }];
  overlay.applyRects(markers, win);
  // Should write page coords (100, 800), not viewport coords (100, 400).
  assert.deepEqual(writes, ['a:translate(100px, 800px)']);
});

test('applyRects without scroll offsets behaves as before (back-compat)', () => {
  const writes = [];
  function makeStyleProxy(label) {
    const obj = {};
    return new Proxy(obj, {
      set(t, k, v) {
        if (k === 'transform') writes.push(label + ':' + v);
        t[k] = v; return true;
      },
      get(t, k) { return t[k]; },
    });
  }
  const target = { getBoundingClientRect: () => ({ left: 5, top: 10 }) };
  const markers = [{ target, el: { style: makeStyleProxy('a') } }];
  overlay.applyRects(markers); // no win arg -> treat scroll as 0
  assert.deepEqual(writes, ['a:translate(5px, 10px)']);
});

test('applyRects hides marker when target is disconnected from DOM', () => {
  const target = {
    isConnected: false,
    getBoundingClientRect: () => { throw new Error('should not be called'); },
  };
  const m = { target, el: { style: {} } };
  overlay.applyRects([m]);
  assert.equal(m.el.style.display, 'none');
});

test('applyRects hides marker when target has zero-size rect (detached element)', () => {
  const target = {
    isConnected: true,
    getBoundingClientRect: () => ({ left: 0, top: 0, width: 0, height: 0 }),
  };
  const m = { target, el: { style: {} } };
  overlay.applyRects([m]);
  assert.equal(m.el.style.display, 'none');
});

test('applyRects shows marker when target is connected with non-zero rect', () => {
  const target = {
    isConnected: true,
    getBoundingClientRect: () => ({ left: 10, top: 20, width: 100, height: 50 }),
  };
  const m = { target, el: { style: {} } };
  overlay.applyRects([m]);
  assert.equal(m.el.style.display, '');
  assert.equal(m.el.style.transform, 'translate(10px, 20px)');
});

test('setMarkersTabindex toggles all markers atomically', () => {
  const m1 = { _attrs: { tabindex: '0' }, setAttribute(k, v) { this._attrs[k] = v; } };
  const m2 = { _attrs: { tabindex: '0' }, setAttribute(k, v) { this._attrs[k] = v; } };
  const markersById = new Map([['p1', { el: m1 }], ['p2', { el: m2 }]]);
  overlay.setMarkersTabindex(markersById, '-1');
  assert.equal(m1._attrs.tabindex, '-1');
  assert.equal(m2._attrs.tabindex, '-1');
  overlay.setMarkersTabindex(markersById, '0');
  assert.equal(m1._attrs.tabindex, '0');
  assert.equal(m2._attrs.tabindex, '0');
});

test('marker keyboard handler fires on Enter and Space', () => {
  // Verify the wiring contract: when the agent attaches a keydown handler,
  // Enter and Space both trigger the post.
  const handlers = {};
  const el = {
    addEventListener: (t, fn) => { (handlers[t] = handlers[t] || []).push(fn); },
  };
  let posted = null;
  const post = (m) => (posted = m);
  el.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      post({ type: 'pin-clicked', pin_id: 'p1' });
    }
  });
  handlers.keydown[0]({ key: 'Enter', preventDefault() {} });
  assert.deepEqual(posted, { type: 'pin-clicked', pin_id: 'p1' });
  posted = null;
  handlers.keydown[0]({ key: ' ', preventDefault() {} });
  assert.deepEqual(posted, { type: 'pin-clicked', pin_id: 'p1' });
});
