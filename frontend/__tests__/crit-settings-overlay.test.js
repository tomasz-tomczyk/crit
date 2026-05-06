// crit-settings-overlay.test.js — exercise the shared Settings overlay
// shell extracted from app.js. The shell owns open/close/Esc/?/focus-trap/
// sliding-underline/tab click + arrow-key nav. Pane rendering is the
// caller's responsibility (delegated via onOpen / onTabSwitch hooks).
//
// Tests run against a hand-rolled DOM stub (no jsdom available in this
// repo) and so verify behaviour at the API boundary: classList toggles,
// aria-selected updates, focus calls, listener invocation paths.
'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');

// ---------- minimal DOM ----------------------------------------------------

function makeClassList() {
  var s = new Set();
  return {
    _s: s,
    add: function () { for (var i = 0; i < arguments.length; i++) s.add(arguments[i]); },
    remove: function () { for (var i = 0; i < arguments.length; i++) s.delete(arguments[i]); },
    contains: function (c) { return s.has(c); },
    toggle: function (c, force) {
      if (typeof force === 'boolean') { force ? s.add(c) : s.delete(c); return force; }
      if (s.has(c)) { s.delete(c); return false; }
      s.add(c); return true;
    },
  };
}

function makeNode(tag, opts) {
  opts = opts || {};
  var node = {
    tagName: (tag || 'div').toUpperCase(),
    classList: makeClassList(),
    dataset: opts.dataset || {},
    attrs: opts.attrs || {},
    style: {},
    _children: [],
    _listeners: {},
    parentElement: null,
    focusCount: 0,
    _rect: opts._rect || { left: 0, top: 0, width: 0 },
    addEventListener: function (t, fn) { (this._listeners[t] = this._listeners[t] || []).push(fn); },
    removeEventListener: function (t, fn) {
      var arr = this._listeners[t]; if (!arr) return;
      var i = arr.indexOf(fn); if (i !== -1) arr.splice(i, 1);
    },
    setAttribute: function (k, v) { this.attrs[k] = v; },
    getAttribute: function (k) { return this.attrs[k]; },
    appendChild: function (n) { n.parentElement = this; this._children.push(n); return n; },
    getBoundingClientRect: function () { return this._rect; },
    set className(v) { this._classes = String(v).split(/\s+/).filter(Boolean); },
    get className() { return (this._classes || []).join(' '); },
    focus: function () { this.focusCount++; doc.activeElement = this; },
    closest: function (sel) {
      // very small subset: '#id', '.cls[data-tab]', '.cls'
      var n = this;
      while (n) {
        if (sel.charAt(0) === '#' && '#' + n.attrs.id === sel) return n;
        if (sel === '.settings-tab[data-tab]') {
          if (n._classes && n._classes.indexOf('settings-tab') !== -1 && n.dataset && n.dataset.tab) return n;
        }
        if (sel === '.settings-tab[role="tab"]') {
          if (n._classes && n._classes.indexOf('settings-tab') !== -1 && n.attrs.role === 'tab') return n;
        }
        n = n.parentElement;
      }
      return null;
    },
    querySelector: function (sel) {
      var hits = this.querySelectorAll(sel);
      return hits.length ? hits[0] : null;
    },
    querySelectorAll: function (sel) {
      // recursive walk; supports a small selector subset used by the installer.
      var out = [];
      function matches(n) {
        var classes = n._classes || [];
        if (sel === '.settings-tabs') return classes.indexOf('settings-tabs') !== -1;
        if (sel === '.settings-tabs[role="tablist"]') {
          return classes.indexOf('settings-tabs') !== -1 && n.attrs.role === 'tablist';
        }
        if (sel === '.settings-tab[data-tab]') {
          return classes.indexOf('settings-tab') !== -1 && n.dataset && n.dataset.tab;
        }
        if (sel === '.settings-tab[role="tab"]') {
          return classes.indexOf('settings-tab') !== -1 && n.attrs.role === 'tab';
        }
        if (sel === '.settings-tab-underline') return classes.indexOf('settings-tab-underline') !== -1;
        if (sel === '.settings-pane') return classes.indexOf('settings-pane') !== -1;
        if (sel.indexOf('button:not([disabled])') === 0) {
          // FOCUSABLE selector — match buttons (and inputs) that are not disabled
          return (n.tagName === 'BUTTON' || n.tagName === 'INPUT') && !n.attrs.disabled;
        }
        return false;
      }
      function walk(n) {
        if (!n) return;
        if (matches(n)) out.push(n);
        for (var i = 0; i < n._children.length; i++) walk(n._children[i]);
      }
      for (var i = 0; i < this._children.length; i++) walk(this._children[i]);
      return out;
    },
  };
  if (opts.classes) {
    node._classes = opts.classes.slice();
    node.classList.add.apply(node.classList, opts.classes);
  } else {
    node._classes = [];
  }
  if (opts.id) node.attrs.id = opts.id;
  if (opts.role) node.attrs.role = opts.role;
  return node;
}

var doc;
function makeDoc() {
  return {
    activeElement: null,
    _listeners: {},
    addEventListener: function (t, fn) { (this._listeners[t] = this._listeners[t] || []).push(fn); },
    removeEventListener: function (t, fn) {
      var arr = this._listeners[t]; if (!arr) return;
      var i = arr.indexOf(fn); if (i !== -1) arr.splice(i, 1);
    },
    createElement: function (tag) { return makeNode(tag); },
    fire: function (t, ev) {
      (this._listeners[t] || []).slice().forEach(function (fn) { fn(ev); });
    },
  };
}

function makeOverlay() {
  // Build a structure that mirrors index.html:
  //  .settings-overlay
  //    .settings-tabs[role=tablist]
  //      button.settings-tab[role=tab][data-tab=settings]
  //      button.settings-tab[role=tab][data-tab=shortcuts]
  //      button.settings-tab[role=tab][data-tab=about]
  //      button.settings-tab-close#settingsClose
  //    button.settings-pane[data-pane=settings] (.active)
  //    button.settings-pane[data-pane=shortcuts]
  //    button.settings-pane[data-pane=about]
  var overlay = makeNode('div', { id: 'settingsOverlay', classes: ['settings-overlay'] });
  var tabsBar = makeNode('div', { classes: ['settings-tabs'], role: 'tablist' });
  overlay.appendChild(tabsBar);
  function tab(name, active, x) {
    var t = makeNode('button', {
      classes: ['settings-tab'],
      role: 'tab',
      dataset: { tab: name },
      _rect: { left: x, top: 0, width: 50 },
    });
    if (active) t.classList.add('active');
    t.attrs['aria-selected'] = active ? 'true' : 'false';
    tabsBar.appendChild(t);
    return t;
  }
  var t1 = tab('settings', true, 0);
  var t2 = tab('shortcuts', false, 50);
  var t3 = tab('about', false, 100);
  tabsBar._rect = { left: 0, top: 0, width: 200 };

  var closeBtn = makeNode('button', { id: 'settingsClose', classes: ['settings-tab-close'] });
  tabsBar.appendChild(closeBtn);

  function pane(name, active) {
    var p = makeNode('div', { classes: ['settings-pane'], dataset: { pane: name } });
    if (active) p.classList.add('active');
    overlay.appendChild(p);
    return p;
  }
  var p1 = pane('settings', true);
  pane('shortcuts', false);
  pane('about', false);

  // Plus a focusable button inside the active pane (so focus-trap has 2+ targets)
  var focusable1 = makeNode('button', { classes: [] });
  p1.appendChild(focusable1);

  return { overlay: overlay, t1: t1, t2: t2, t3: t3, tabsBar: tabsBar, closeBtn: closeBtn };
}

function loadOverlay() {
  var src = fs.readFileSync(path.join(__dirname, '..', 'crit-settings-overlay.js'), 'utf8');
  var sandbox = { window: {} };
  new Function('window', 'module', src)(sandbox.window, undefined);
  return sandbox.window.crit.settingsOverlay;
}

// ---------- tests ----------------------------------------------------------

test('install: open() activates overlay, switches tab, ensures underline, fires onOpen', () => {
  doc = makeDoc();
  var api = loadOverlay();
  var dom = makeOverlay();

  var openTab = null;
  var ctl = api.install({
    overlay: dom.overlay,
    document: doc,
    onOpen: function (tab) { openTab = tab; },
  });

  ctl.open('shortcuts');

  assert.equal(dom.overlay.classList.contains('active'), true);
  assert.equal(openTab, 'shortcuts');
  // sliding underline created and parented under .settings-tabs
  var underline = dom.tabsBar._children.filter(function (c) {
    return c._classes && c._classes.indexOf('settings-tab-underline') !== -1;
  });
  assert.equal(underline.length, 1, 'underline element appended');
  // sliding underline positioned over the active tab (shortcuts: x=50, w=50)
  assert.equal(underline[0].style.left, '50px');
  assert.equal(underline[0].style.width, '50px');
  // shortcuts tab is aria-selected=true; settings is false
  assert.equal(dom.t1.attrs['aria-selected'], 'false');
  assert.equal(dom.t2.attrs['aria-selected'], 'true');
});

test('Esc closes overlay (document-level handler) and fires onClose', () => {
  doc = makeDoc();
  var api = loadOverlay();
  var dom = makeOverlay();
  var closed = false;
  var ctl = api.install({
    overlay: dom.overlay, document: doc,
    onClose: function () { closed = true; },
  });
  ctl.open();
  assert.equal(ctl.isOpen(), true);

  doc.fire('keydown', { key: 'Escape', preventDefault: function () {} });
  assert.equal(ctl.isOpen(), false);
  assert.equal(closed, true);
  assert.equal(dom.overlay.classList.contains('active'), false);
});

test('focus trap: Tab from last focusable wraps to first', () => {
  doc = makeDoc();
  var api = loadOverlay();
  var dom = makeOverlay();
  var ctl = api.install({ overlay: dom.overlay, document: doc });

  // Add a second focusable so first/last differ
  var second = makeNode('button');
  dom.overlay._children[1].appendChild(second); // append into pane

  ctl.open();

  // The querySelectorAll(FOCUSABLE) walks all buttons; first is t1 (tab),
  // last is `second`. Simulate Tab while on the last:
  doc.activeElement = second;
  var prevented = false;
  // overlay-level keydown handler is what enforces the trap
  var handlers = dom.overlay._listeners.keydown || [];
  assert.ok(handlers.length > 0, 'trap handler attached');
  handlers[0]({
    key: 'Tab',
    shiftKey: false,
    preventDefault: function () { prevented = true; },
  });
  assert.equal(prevented, true, 'wrap prevents default');
  // first focusable is the first tab button (t1)
  assert.equal(dom.t1.focusCount >= 1, true, 'first focusable received focus');
});

test('focus trap: Shift+Tab from first focusable wraps to last', () => {
  doc = makeDoc();
  var api = loadOverlay();
  var dom = makeOverlay();
  var ctl = api.install({ overlay: dom.overlay, document: doc });
  var second = makeNode('button');
  dom.overlay._children[1].appendChild(second);

  ctl.open();
  doc.activeElement = dom.t1; // first
  var handlers = dom.overlay._listeners.keydown || [];
  var prevented = false;
  handlers[0]({
    key: 'Tab', shiftKey: true,
    preventDefault: function () { prevented = true; },
  });
  assert.equal(prevented, true);
  assert.equal(second.focusCount >= 1, true, 'last focusable received focus');
});

test('Arrow keys cycle through tabs (ARIA tabs pattern)', () => {
  doc = makeDoc();
  var api = loadOverlay();
  var dom = makeOverlay();
  var ctl = api.install({ overlay: dom.overlay, document: doc });
  ctl.open('settings');

  // Fire keydown on the tablist
  var handlers = dom.tabsBar._listeners.keydown || [];
  assert.ok(handlers.length > 0, 'tablist keydown attached');
  handlers[0].call(dom.tabsBar, {
    key: 'ArrowRight', preventDefault: function () {},
  });
  assert.equal(ctl.getActiveTab(), 'shortcuts');
  assert.equal(dom.t2.focusCount >= 1, true);

  // ArrowLeft from shortcuts -> settings
  handlers[0].call(dom.tabsBar, {
    key: 'ArrowLeft', preventDefault: function () {},
  });
  assert.equal(ctl.getActiveTab(), 'settings');
});

test('switchTab updates classList + aria-selected + sliding underline position', () => {
  doc = makeDoc();
  var api = loadOverlay();
  var dom = makeOverlay();
  var ctl = api.install({ overlay: dom.overlay, document: doc });
  ctl.open('settings');

  ctl.switchTab('about');
  assert.equal(dom.t3.classList.contains('active'), true);
  assert.equal(dom.t3.attrs['aria-selected'], 'true');
  assert.equal(dom.t1.attrs['aria-selected'], 'false');
  var underline = dom.tabsBar._children.filter(function (c) {
    return c._classes && c._classes.indexOf('settings-tab-underline') !== -1;
  })[0];
  // about tab: x=100, w=50
  assert.equal(underline.style.left, '100px');
  assert.equal(underline.style.width, '50px');
});

test('? toggles shortcuts tab when overlay is open; closes when already on shortcuts', () => {
  doc = makeDoc();
  var api = loadOverlay();
  var dom = makeOverlay();
  var ctl = api.install({ overlay: dom.overlay, document: doc });
  ctl.open('settings');

  doc.fire('keydown', { key: '?', preventDefault: function () {} });
  assert.equal(ctl.getActiveTab(), 'shortcuts');

  doc.fire('keydown', { key: '?', preventDefault: function () {} });
  assert.equal(ctl.isOpen(), false, 'second ? closes overlay when on shortcuts');
});

test('toggle button click opens then closes', () => {
  doc = makeDoc();
  var api = loadOverlay();
  var dom = makeOverlay();
  var toggle = makeNode('button', { id: 'settingsToggle' });
  var ctl = api.install({ overlay: dom.overlay, toggle: toggle, document: doc });

  // simulate click
  (toggle._listeners.click || []).forEach(function (fn) { fn({}); });
  assert.equal(ctl.isOpen(), true);
  (toggle._listeners.click || []).forEach(function (fn) { fn({}); });
  assert.equal(ctl.isOpen(), false);
});

test('clicking a tab inside overlay switches to it (delegated handler)', () => {
  doc = makeDoc();
  var api = loadOverlay();
  var dom = makeOverlay();
  var ctl = api.install({ overlay: dom.overlay, document: doc });
  ctl.open('settings');

  // Wire dom.t2's parent chain so closest('.settings-tab[data-tab]') hits t2
  dom.t2.parentElement = dom.tabsBar;

  (dom.overlay._listeners.click || []).forEach(function (fn) {
    fn({ target: dom.t2 });
  });
  assert.equal(ctl.getActiveTab(), 'shortcuts');
});

test('clicking the overlay backdrop (target===overlay) closes', () => {
  doc = makeDoc();
  var api = loadOverlay();
  var dom = makeOverlay();
  var ctl = api.install({ overlay: dom.overlay, document: doc });
  ctl.open();

  (dom.overlay._listeners.click || []).forEach(function (fn) {
    fn({ target: dom.overlay });
  });
  assert.equal(ctl.isOpen(), false);
});

test('destroy() unbinds toggle, overlay click, and document keydown handlers', () => {
  doc = makeDoc();
  var api = loadOverlay();
  var dom = makeOverlay();
  var toggle = makeNode('button');
  var ctl = api.install({ overlay: dom.overlay, toggle: toggle, document: doc });
  ctl.destroy();

  // After destroy, firing Escape should not flip state (open_ is false anyway,
  // but listeners should also be gone).
  assert.equal((doc._listeners.keydown || []).length, 0);
  assert.equal((toggle._listeners.click || []).length, 0);
  assert.equal((dom.overlay._listeners.click || []).length, 0);
});
