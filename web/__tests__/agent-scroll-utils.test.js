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
