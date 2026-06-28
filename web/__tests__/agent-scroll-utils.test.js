'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const scroll = require('../agent-scroll-utils.js');

function mockWin(overrides) {
  var styleMap = (overrides && overrides.styles) || {};
  return {
    innerHeight: overrides && overrides.innerHeight != null ? overrides.innerHeight : 800,
    innerWidth: overrides && overrides.innerWidth != null ? overrides.innerWidth : 600,
    scrollY: overrides && overrides.scrollY != null ? overrides.scrollY : 0,
    scrollX: overrides && overrides.scrollX != null ? overrides.scrollX : 0,
    pageYOffset: overrides && overrides.scrollY != null ? overrides.scrollY : 0,
    pageXOffset: overrides && overrides.scrollX != null ? overrides.scrollX : 0,
    fullpage_api: overrides && overrides.fullpage_api,
    Lenis: overrides && overrides.Lenis,
    locomotiveScroll: overrides && overrides.locomotiveScroll,
    getComputedStyle: function (el) {
      var key = el._styleKey || el.tagName;
      return styleMap[key] || {
        overflow: 'visible',
        overflowY: 'visible',
        overflowX: 'visible',
        scrollSnapType: 'none',
      };
    },
    scrollTo: overrides && overrides.scrollTo ? overrides.scrollTo : function () {},
    document: overrides && overrides.document,
  };
}

function mockEl(rect, extra) {
  return Object.assign({
    getBoundingClientRect: function () { return rect; },
    parentElement: null,
    tagName: extra && extra.tagName || 'DIV',
    style: {},
    scrollHeight: extra && extra.scrollHeight != null ? extra.scrollHeight : 100,
    clientHeight: extra && extra.clientHeight != null ? extra.clientHeight : 100,
    scrollWidth: extra && extra.scrollWidth != null ? extra.scrollWidth : 100,
    clientWidth: extra && extra.clientWidth != null ? extra.clientWidth : 100,
    scrollTop: extra && extra.scrollTop != null ? extra.scrollTop : 0,
    scrollLeft: extra && extra.scrollLeft != null ? extra.scrollLeft : 0,
    _styleKey: extra && extra._styleKey,
  }, extra || {});
}

test('isMostlyVisible returns true when half or more of element is on screen', () => {
  var win = mockWin({ innerHeight: 800, innerWidth: 600 });
  var el = mockEl({ top: 100, bottom: 200, left: 10, right: 110, height: 100, width: 100 });
  var doc = { documentElement: {}, scrollingElement: {} };
  assert.equal(scroll.isMostlyVisible(el, win, doc.documentElement, doc), true);
});

test('isMostlyVisible returns false when element is mostly off screen', () => {
  var win = mockWin({ innerHeight: 800, innerWidth: 600 });
  var el = mockEl({ top: 900, bottom: 1000, left: 10, right: 110, height: 100, width: 100 });
  var doc = { documentElement: {}, scrollingElement: {} };
  assert.equal(scroll.isMostlyVisible(el, win, doc.documentElement, doc), false);
});

test('findScrollContainer prefers nearest scrollable ancestor', () => {
  var doc = {
    documentElement: { tagName: 'HTML', parentElement: null },
    body: { tagName: 'BODY' },
    scrollingElement: null,
    defaultView: null,
  };
  var panel = mockEl({ top: 0, bottom: 400, left: 0, right: 300, height: 400, width: 300 }, {
    tagName: 'DIV',
    _styleKey: 'PANEL',
    scrollHeight: 2000,
    clientHeight: 400,
  });
  var target = mockEl({ top: 500, bottom: 550, left: 0, right: 100, height: 50, width: 100 });
  target.parentElement = panel;
  panel.parentElement = doc.body;

  var win = mockWin({
    styles: {
      PANEL: { overflowY: 'auto', overflowX: 'visible' },
      HTML: { overflowY: 'visible', overflowX: 'visible' },
      BODY: { overflowY: 'visible', overflowX: 'visible' },
    },
  });
  doc.defaultView = win;
  doc.scrollingElement = doc.documentElement;

  assert.equal(scroll.findScrollContainer(target, doc), panel);
});

test('scrollIntoNearestContainer skips when mostly visible', () => {
  var win = mockWin();
  var calls = 0;
  win.scrollTo = function () { calls++; };
  var el = mockEl({ top: 100, bottom: 200, left: 10, right: 110, height: 100, width: 100 });
  var doc = { documentElement: {}, body: {}, scrollingElement: {} };
  assert.equal(scroll.scrollIntoNearestContainer(el, { win: win, doc: doc }), false);
  assert.equal(calls, 0);
});

test('scrollIntoNearestContainer skips document scroll when unsafe', () => {
  var calls = 0;
  var win = mockWin({
    innerHeight: 800,
    scrollTo: function () { calls++; },
    styles: {
      HTML: { overflowY: 'visible', overflowX: 'visible' },
      BODY: { overflowY: 'visible', overflowX: 'visible' },
    },
  });
  var doc = {
    documentElement: { tagName: 'HTML', _styleKey: 'HTML' },
    body: { tagName: 'BODY', _styleKey: 'BODY' },
    scrollingElement: null,
    defaultView: win,
  };
  doc.scrollingElement = doc.documentElement;
  var el = mockEl({ top: 900, bottom: 950, left: 0, right: 100, height: 50, width: 100 });
  el.parentElement = doc.body;

  assert.equal(scroll.scrollIntoNearestContainer(el, {
    win: win,
    doc: doc,
    unsafeDocumentScroll: true,
  }), false);
  assert.equal(calls, 0);
});

test('scrollIntoNearestContainer scrolls inner panel when document scroll is unsafe', () => {
  var panel = mockEl({ top: 0, bottom: 400, left: 0, right: 300, height: 400, width: 300 }, {
    tagName: 'DIV',
    _styleKey: 'PANEL',
    scrollHeight: 2000,
    clientHeight: 400,
    scrollTop: 0,
  });
  var target = mockEl({ top: 500, bottom: 550, left: 0, right: 100, height: 50, width: 100 });
  target.parentElement = panel;

  var win = mockWin({
    innerHeight: 800,
    styles: {
      PANEL: { overflowY: 'auto', overflowX: 'visible' },
      HTML: { overflowY: 'visible', overflowX: 'visible' },
      BODY: { overflowY: 'visible', overflowX: 'visible' },
    },
  });
  var doc = {
    documentElement: { tagName: 'HTML', _styleKey: 'HTML' },
    body: { tagName: 'BODY', _styleKey: 'BODY', parentElement: null },
    scrollingElement: null,
    defaultView: win,
  };
  panel.parentElement = doc.body;
  doc.scrollingElement = doc.documentElement;

  assert.equal(scroll.scrollIntoNearestContainer(target, {
    win: win,
    doc: doc,
    unsafeDocumentScroll: true,
  }), true);
  assert.ok(panel.scrollTop > 0);
});

test('findScrollContainerChain collects every scrollable ancestor inner-to-outer', () => {
  var win = mockWin({
    styles: {
      OUTER: { overflowY: 'auto', overflowX: 'visible' },
      INNER: { overflowY: 'auto', overflowX: 'visible' },
      HTML: { overflowY: 'visible', overflowX: 'visible' },
      BODY: { overflowY: 'visible', overflowX: 'visible' },
    },
  });
  var doc = {
    documentElement: { tagName: 'HTML', _styleKey: 'HTML', parentElement: null },
    body: { tagName: 'BODY', _styleKey: 'BODY' },
    scrollingElement: null,
    defaultView: win,
  };
  doc.scrollingElement = doc.documentElement;
  var outer = mockEl({ top: 0, bottom: 400, left: 0, right: 300, height: 400, width: 300 }, {
    _styleKey: 'OUTER', scrollHeight: 2000, clientHeight: 400,
  });
  var inner = mockEl({ top: 0, bottom: 120, left: 0, right: 300, height: 120, width: 300 }, {
    _styleKey: 'INNER', scrollHeight: 900, clientHeight: 120,
  });
  var target = mockEl({ top: 500, bottom: 550, left: 0, right: 100, height: 50, width: 100 });
  target.parentElement = inner;
  inner.parentElement = outer;
  outer.parentElement = doc.body;

  var chain = scroll.findScrollContainerChain(target, doc);
  assert.equal(chain.length, 3);
  assert.equal(chain[0], inner);
  assert.equal(chain[1], outer);
  assert.equal(chain[2], doc.documentElement);
});

test('isMostlyVisibleInChain treats element inside a scrolled-away container as hidden', () => {
  var win = mockWin({ innerHeight: 800, innerWidth: 600 });
  var doc = { documentElement: {}, body: {}, scrollingElement: {} };
  // Element appears fully inside its container rect, but the container itself is
  // pushed off the bottom of the viewport.
  var container = mockEl({ top: 900, bottom: 1100, left: 0, right: 300, height: 200, width: 300 }, {
    _styleKey: 'C',
  });
  var el = mockEl({ top: 920, bottom: 970, left: 0, right: 100, height: 50, width: 100 });
  var chain = [container, doc.documentElement];
  assert.equal(scroll.isMostlyVisibleInChain(el, win, chain, doc), false);
});

test('scrollIntoNearestContainer adjusts every level of a nested scroll', () => {
  var win = mockWin({
    innerHeight: 800,
    scrollTo: function () { win._docScrolled = true; },
    styles: {
      OUTER: { overflowY: 'auto', overflowX: 'visible' },
      INNER: { overflowY: 'auto', overflowX: 'visible' },
      HTML: { overflowY: 'visible', overflowX: 'visible' },
      BODY: { overflowY: 'visible', overflowX: 'visible' },
    },
  });
  var doc = {
    documentElement: { tagName: 'HTML', _styleKey: 'HTML', parentElement: null },
    body: { tagName: 'BODY', _styleKey: 'BODY' },
    scrollingElement: null,
    defaultView: win,
  };
  doc.scrollingElement = doc.documentElement;
  // Nested section sits below the fold; both inner containers are scrolled to 0.
  var outer = mockEl({ top: 1000, bottom: 1400, left: 0, right: 300, height: 400, width: 300 }, {
    _styleKey: 'OUTER', scrollHeight: 2000, clientHeight: 400, scrollTop: 0,
  });
  var inner = mockEl({ top: 1000, bottom: 1120, left: 0, right: 300, height: 120, width: 300 }, {
    _styleKey: 'INNER', scrollHeight: 900, clientHeight: 120, scrollTop: 0,
  });
  var target = mockEl({ top: 1500, bottom: 1550, left: 0, right: 100, height: 50, width: 100 });
  target.parentElement = inner;
  inner.parentElement = outer;
  outer.parentElement = doc.body;

  var ret = scroll.scrollIntoNearestContainer(target, { win: win, doc: doc });
  assert.equal(ret, true);
  assert.ok(inner.scrollTop > 0, 'inner container scrolled');
  assert.ok(outer.scrollTop > 0, 'outer container scrolled');
  assert.ok(win._docScrolled, 'document centered as final step');
});

test('detectUnsafeDocumentScroll flags locked root overflow', () => {
  var win = mockWin({
    innerHeight: 800,
    styles: {
      HTML: { overflow: 'hidden', overflowY: 'hidden' },
      BODY: { overflow: 'hidden', overflowY: 'hidden' },
    },
  });
  var body = {
    tagName: 'BODY',
    _styleKey: 'BODY',
    scrollHeight: 800,
    clientHeight: 800,
  };
  var doc = {
    documentElement: { tagName: 'HTML', _styleKey: 'HTML', clientHeight: 800 },
    body: body,
    querySelector: function () { return null; },
  };
  assert.equal(scroll.detectUnsafeDocumentScroll(doc, win), true);
});

test('detectUnsafeDocumentScroll flags scroll-snap on root', () => {
  var win = mockWin({
    styles: {
      HTML: { scrollSnapType: 'y mandatory', overflowY: 'visible' },
      BODY: { scrollSnapType: 'none', overflowY: 'visible' },
    },
  });
  var doc = {
    documentElement: { tagName: 'HTML', _styleKey: 'HTML' },
    body: { tagName: 'BODY', _styleKey: 'BODY', scrollHeight: 5000, clientHeight: 800 },
    querySelector: function () { return null; },
  };
  assert.equal(scroll.detectUnsafeDocumentScroll(doc, win), true);
});
