// crit-comment-card-helpers.js — pure rendering helpers shared between the
// code-review comment card (app.js) and design-mode rows (design-mode.row.js).
//
// Rules for adding helpers here:
//   1. Pure: no closures over module state, no DOM mutation, no fetch.
//   2. Returns a string, primitive, or plain object.
//   3. Behaviour must be byte-identical to the original app.js implementation
//      so code review keeps rendering exactly the same HTML.
//
// Exports onto window.crit.commentCardHelpers. Loaded before app.js and
// design-mode.js via index.html script order.
'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.commentCardHelpers = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {

  // escapeHtml — byte-identical to app.js's escapeHtml (escapes &, <, >, ").
  // Does NOT escape single quotes; design rows that need that should use the
  // row-local variant (see design-mode.row.js chipLabel handling).
  function escapeHtml(str) {
    return String(str == null ? '' : str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  // relativeTime — byte-identical to app.js's relativeTime.
  function relativeTime(dateStr) {
    var now = Date.now();
    var then = new Date(dateStr).getTime();
    var diff = Math.floor((now - then) / 1000);
    if (diff < 60) return 'just now';
    if (diff < 3600) return Math.floor(diff / 60) + 'm ago';
    if (diff < 86400) return Math.floor(diff / 3600) + 'h ago';
    if (diff < 604800) return Math.floor(diff / 86400) + 'd ago';
    return Math.floor(diff / 604800) + 'w ago';
  }

  // formatTime — byte-identical to app.js's formatTime (HH:MM in locale).
  function formatTime(isoStr) {
    if (!isoStr) return '';
    var d = new Date(isoStr);
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }

  // Standard class strings. These are deliberately simple constants so that
  // both renderers stay in sync if we tweak them later. Adding a class here
  // does NOT make every existing card pick it up — call sites still need to
  // include them — but having a single source-of-truth string makes drift
  // harder.
  var BTN_CLASS = 'btn';
  var BTN_SM_CLASS = 'btn btn-sm';
  var COMMENT_CARD_CLASS = 'comment-card';
  var COMMENT_BODY_CLASS = 'comment-body';
  var COMMENT_ACTIONS_CLASS = 'comment-actions';

  return {
    escapeHtml: escapeHtml,
    relativeTime: relativeTime,
    formatTime: formatTime,
    BTN_CLASS: BTN_CLASS,
    BTN_SM_CLASS: BTN_SM_CLASS,
    COMMENT_CARD_CLASS: COMMENT_CARD_CLASS,
    COMMENT_BODY_CLASS: COMMENT_BODY_CLASS,
    COMMENT_ACTIONS_CLASS: COMMENT_ACTIONS_CLASS,
  };
});
