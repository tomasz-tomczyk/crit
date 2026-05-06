'use strict';
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.design = root.crit.design || {};
    root.crit.design.menuController = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  function createMenuController({ options, onCommit, onCancel, onHighlight } = {}) {
    onCommit = onCommit || function () {};
    onCancel = onCancel || function () {};
    onHighlight = onHighlight || function () {};
    const ctl = { options: options || [], index: 0 };
    function highlight(i) { ctl.index = i; onHighlight(i); }
    ctl.keydown = function (ev) {
      const n = ctl.options.length;
      if (!n) return;
      const prevent = ev && typeof ev.preventDefault === 'function'
        ? function () { ev.preventDefault(); } : function () {};
      if (ev.key === 'ArrowDown') { prevent(); highlight((ctl.index + 1) % n); }
      else if (ev.key === 'ArrowUp') { prevent(); highlight((ctl.index - 1 + n) % n); }
      else if (ev.key === 'Home') { prevent(); highlight(0); }
      else if (ev.key === 'End') { prevent(); highlight(n - 1); }
      else if (ev.key === 'Enter' || ev.key === ' ') { prevent(); onCommit(ctl.options[ctl.index]); }
      else if (ev.key === 'Escape') { prevent(); onCancel(); }
    };
    ctl.setHoveredLevel = function (level) {
      const i = ctl.options.findIndex(function (o) { return o && o.level === level; });
      if (i < 0) return;
      highlight(i);
    };
    return ctl;
  }
  return { createMenuController };
});
