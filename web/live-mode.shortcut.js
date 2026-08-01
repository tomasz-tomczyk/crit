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
    if (ev.key === '?') {
      if (typeof ev.preventDefault === 'function') ev.preventDefault();
      if (typeof ev.stopImmediatePropagation === 'function') ev.stopImmediatePropagation();
      if (typeof ctx.toggleShortcuts === 'function') ctx.toggleShortcuts();
      return;
    }
    if (ctx.settingsOpen) return;
    var action = typeof ctx.actionForEvent === 'function' ? ctx.actionForEvent(ev) : '';
    // Shift+F triggers Finish Review for parity with code-review mode.
    // Match against `key` (case-sensitive 'F' arrives when shift is held)
    // AND require shiftKey, so plain 'f' on layouts where shift produces a
    // different glyph still works. Ignore when other modifiers are pressed
    // — we don't want Cmd+Shift+F (browser find-in-page) to be hijacked.
    if (action === 'finish_review' || (!ctx.actionForEvent && ev.shiftKey && !ev.ctrlKey && !ev.metaKey && !ev.altKey
        && (ev.key === 'F' || ev.key === 'f'))) {
      if (typeof ctx.finishReview === 'function') {
        if (typeof ev.preventDefault === 'function') ev.preventDefault();
        ctx.finishReview();
      }
      return;
    }
    if (action === 'toggle_pin_mode' || (!ctx.actionForEvent && (ev.key === 'p' || ev.key === 'P'))) {
      if (typeof ev.preventDefault === 'function') ev.preventDefault();
      if (typeof ev.stopPropagation === 'function') ev.stopPropagation();
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
