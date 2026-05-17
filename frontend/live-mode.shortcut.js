'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.live = root.crit.live || {};
    root.crit.live.shortcut = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  function handleShortcut(ev, ctx) {
    if (ctx.focusInInput) return;
    // Shift+F triggers Finish Review for parity with code-review mode.
    // Match against `key` (case-sensitive 'F' arrives when shift is held)
    // AND require shiftKey, so plain 'f' on layouts where shift produces a
    // different glyph still works. Ignore when other modifiers are pressed
    // — we don't want Cmd+Shift+F (browser find-in-page) to be hijacked.
    if (ev.shiftKey && !ev.ctrlKey && !ev.metaKey && !ev.altKey
        && (ev.key === 'F' || ev.key === 'f')) {
      if (typeof ctx.finishReview === 'function') {
        if (typeof ev.preventDefault === 'function') ev.preventDefault();
        ctx.finishReview();
      }
      return;
    }
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
