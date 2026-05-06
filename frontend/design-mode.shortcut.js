'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.design = root.crit.design || {};
    root.crit.design.shortcut = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  function handleShortcut(ev, ctx) {
    if (ctx.focusInInput) return;
    if (ev.key === 'p' || ev.key === 'P') {
      ctx.setMode(ctx.getMode() === 'pin' ? 'navigate' : 'pin');
      return;
    }
    if (ev.key === 'Escape') {
      if (ctx.getMode() === 'pin') ctx.setMode('navigate');
      return;
    }
  }
  return { handleShortcut };
});
