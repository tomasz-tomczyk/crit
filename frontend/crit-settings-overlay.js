// crit-settings-overlay.js — shared "shell" for the Settings overlay used
// by both code review (app.js) and live mode (live-mode.js).
//
// The shell owns the cross-cutting concerns that are hard to keep in sync
// across two callers: open/close, Esc handling, focus trap, sliding-tab
// underline, click-outside-to-close, click-on-tab-to-switch, and arrow-key
// tab navigation (ARIA tabs pattern).
//
// Rendering of the panes themselves is NOT owned here — callers wire that
// up via `onTabSwitch(tab)` and `onOpen(tab)` hooks (typically delegating
// to crit-settings-panes.js).
//
// Exports:
//   window.crit.settingsOverlay.install(opts) -> controller
//
//   opts = {
//     overlay: HTMLElement,         // #settingsOverlay (required)
//     toggle: HTMLElement,          // #settingsToggle (optional — caller may
//                                   //   bind its own opener)
//     closeBtn: HTMLElement,        // #settingsClose (optional)
//     document: Document,           // defaults to window.document
//     initialTab: string,           // default 'settings'
//     onOpen: function(tab),        // fired AFTER overlay is shown + tab activated
//     onClose: function(),          // fired AFTER overlay is hidden
//     onTabSwitch: function(tab),   // fired AFTER a tab becomes active
//     enableQuestionMarkShortcut: boolean,  // '?' toggles shortcuts tab when open
//                                           //   (default true)
//   }
//
//   controller = {
//     open(tab),  close(),  switchTab(tab),
//     isOpen(),
//     destroy(),  // unbind all listeners
//   }

(function () {
  'use strict';

  var FOCUSABLE = 'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

  function install(opts) {
    opts = opts || {};
    var overlay = opts.overlay;
    if (!overlay) throw new Error('settingsOverlay.install: overlay required');
    var doc = opts.document || (typeof document !== 'undefined' ? document : null);
    if (!doc) throw new Error('settingsOverlay.install: document required');

    var initialTab = opts.initialTab || 'settings';
    var enableQ = opts.enableQuestionMarkShortcut !== false;

    var open_ = false;
    var currentTab = initialTab;
    var trapHandler = null;

    // --- sliding underline -------------------------------------------------
    function ensureUnderline() {
      var tabsEl = overlay.querySelector('.settings-tabs');
      if (!tabsEl) return null;
      var u = tabsEl.querySelector('.settings-tab-underline');
      if (!u) {
        u = doc.createElement('div');
        u.className = 'settings-tab-underline';
        tabsEl.appendChild(u);
      }
      return u;
    }
    function positionUnderline(activeBtn) {
      var u = ensureUnderline();
      if (!u || !activeBtn || !activeBtn.parentElement) return;
      // Some test stubs do not implement getBoundingClientRect — guard so the
      // installer remains usable in jsdom-less unit tests.
      if (typeof activeBtn.getBoundingClientRect !== 'function') return;
      if (typeof activeBtn.parentElement.getBoundingClientRect !== 'function') return;
      var tabsRect = activeBtn.parentElement.getBoundingClientRect();
      var btnRect = activeBtn.getBoundingClientRect();
      u.style.left = (btnRect.left - tabsRect.left) + 'px';
      u.style.width = btnRect.width + 'px';
    }

    // --- tab switching -----------------------------------------------------
    function switchTab(tab) {
      currentTab = tab;
      var activeBtn = null;
      var tabBtns = overlay.querySelectorAll('.settings-tab[role="tab"]');
      for (var i = 0; i < tabBtns.length; i++) {
        var t = tabBtns[i];
        var isActive = t.dataset && t.dataset.tab === tab;
        if (t.classList) t.classList.toggle('active', isActive);
        if (t.setAttribute) t.setAttribute('aria-selected', String(isActive));
        if (isActive) activeBtn = t;
      }
      var panes = overlay.querySelectorAll('.settings-pane');
      for (var j = 0; j < panes.length; j++) {
        var p = panes[j];
        if (p.classList) p.classList.toggle('active', p.dataset && p.dataset.pane === tab);
      }
      positionUnderline(activeBtn);
      if (typeof opts.onTabSwitch === 'function') opts.onTabSwitch(tab);
    }

    // --- focus trap --------------------------------------------------------
    function trapFocus() {
      releaseTrap();
      trapHandler = function (e) {
        if (e.key !== 'Tab') return;
        var nodes = overlay.querySelectorAll(FOCUSABLE);
        if (!nodes || nodes.length === 0) return;
        var first = nodes[0];
        var last = nodes[nodes.length - 1];
        var active = doc.activeElement;
        if (e.shiftKey) {
          if (active === first) { e.preventDefault(); if (last && last.focus) last.focus(); }
        } else {
          if (active === last) { e.preventDefault(); if (first && first.focus) first.focus(); }
        }
      };
      overlay.addEventListener('keydown', trapHandler);
      var firstFocusable = overlay.querySelector(FOCUSABLE);
      if (firstFocusable && firstFocusable.focus) {
        var raf = (typeof requestAnimationFrame === 'function')
          ? requestAnimationFrame
          : function (fn) { return setTimeout(fn, 0); };
        raf(function () { firstFocusable.focus(); });
      }
    }
    function releaseTrap() {
      if (trapHandler) {
        overlay.removeEventListener('keydown', trapHandler);
        trapHandler = null;
      }
    }

    // --- open / close ------------------------------------------------------
    function open(tab) {
      open_ = true;
      if (overlay.classList) overlay.classList.add('active');
      ensureUnderline();
      switchTab(tab || currentTab || initialTab);
      trapFocus();
      if (typeof opts.onOpen === 'function') opts.onOpen(currentTab);
    }
    function close() {
      open_ = false;
      releaseTrap();
      if (overlay.classList) overlay.classList.remove('active');
      if (typeof opts.onClose === 'function') opts.onClose();
    }

    // --- listeners ---------------------------------------------------------
    var listeners = [];
    function on(target, event, fn) {
      if (!target || !target.addEventListener) return;
      target.addEventListener(event, fn);
      listeners.push(function () { target.removeEventListener(event, fn); });
    }

    // Toggle button
    on(opts.toggle, 'click', function () {
      if (open_) close(); else open(currentTab);
    });
    // Close button
    on(opts.closeBtn, 'click', function () { close(); });
    // Click outside (delegated on overlay root)
    on(overlay, 'click', function (e) {
      if (e.target === overlay) { close(); return; }
      var closeHit = e.target && e.target.closest && e.target.closest('#settingsClose');
      if (closeHit) { close(); return; }
      var tabBtn = e.target && e.target.closest && e.target.closest('.settings-tab[data-tab]');
      if (tabBtn && tabBtn.dataset && tabBtn.dataset.tab) {
        switchTab(tabBtn.dataset.tab);
      }
    });
    // Arrow-key nav between tabs (ARIA tabs pattern)
    var tabsEl = overlay.querySelector('.settings-tabs[role="tablist"]');
    on(tabsEl, 'keydown', function (e) {
      if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
      var tabBtns = Array.prototype.slice.call(this.querySelectorAll('.settings-tab[data-tab]'));
      if (tabBtns.length === 0) return;
      var current = -1;
      for (var i = 0; i < tabBtns.length; i++) {
        if (tabBtns[i].getAttribute && tabBtns[i].getAttribute('aria-selected') === 'true') {
          current = i; break;
        }
      }
      if (current === -1) return;
      var next = e.key === 'ArrowRight' ? current + 1 : current - 1;
      if (next < 0) next = tabBtns.length - 1;
      if (next >= tabBtns.length) next = 0;
      e.preventDefault();
      switchTab(tabBtns[next].dataset.tab);
      if (tabBtns[next].focus) tabBtns[next].focus();
    });
    // Document-level Esc + '?' (only fires when overlay is open)
    on(doc, 'keydown', function (e) {
      if (!open_) return;
      if (e.key === 'Escape') {
        e.preventDefault();
        close();
        return;
      }
      if (enableQ && e.key === '?') {
        e.preventDefault();
        e.stopImmediatePropagation();
        if (currentTab === 'shortcuts') close();
        else switchTab('shortcuts');
      }
    });

    function destroy() {
      releaseTrap();
      while (listeners.length) {
        try { listeners.pop()(); } catch (_) { /* ignore */ }
      }
    }

    return {
      open: open,
      close: close,
      switchTab: switchTab,
      isOpen: function () { return open_; },
      getActiveTab: function () { return currentTab; },
      destroy: destroy,
    };
  }

  var root = (typeof window !== 'undefined') ? window : globalThis;
  root.crit = root.crit || {};
  root.crit.settingsOverlay = { install: install };

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = { install: install };
  }
})();
