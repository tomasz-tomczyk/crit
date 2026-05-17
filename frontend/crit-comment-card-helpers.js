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

  // escapeHtml — delegates to the canonical window.crit.shared.escapeHTML.
  // Alias preserves the lowercase-h export name for existing consumers.
  var escapeHtml = (typeof window !== 'undefined' && window.crit && window.crit.shared)
    ? window.crit.shared.escapeHTML
    : function (str) {
        return String(str == null ? '' : str)
          .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
          .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
      };

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

  // authorColorIndex — byte-identical to app.js's authorColorIndex. Picks a
  // 0..N swatch slot for the author colour badge.
  var AUTHOR_COLOR_COUNT = 6;
  function authorColorIndex(name) {
    var s = String(name == null ? '' : name);
    var hash = 0;
    for (var i = 0; i < s.length; i++) {
      hash = ((hash << 5) - hash + s.charCodeAt(i)) | 0;
    }
    return Math.abs(hash) % AUTHOR_COLOR_COUNT;
  }

  // formKeyFor — convention-based form-key for edit/reply/etc. forms keyed by
  // comment id. Used by the shared comment-card / comment-form modules and by
  // design-mode mounts so both controllers produce matching keys.
  // kind: 'edit' | 'reply' | string
  function formKeyFor(commentId, kind) {
    return 'comment:' + kind + ':' + commentId;
  }

  // chipLabel — canonical design-pin label heuristic. Prefers accessible_name,
  // then a short slice of textContent extracted from outer_html, then the leaf
  // tag from tag_chain, then a.role, and finally falls back to 'pin'.
  function chipLabel(a) {
    var name = (a.accessible_name || '').trim();
    if (name) return name.length > 60 ? name.slice(0, 60) + '…' : name;
    var html = a.outer_html || '';
    var text = html.replace(/<[^>]*>/g, ' ').replace(/\s+/g, ' ').trim();
    if (text) return text.length > 60 ? text.slice(0, 60) + '…' : text;
    var chain = Array.isArray(a.tag_chain) ? a.tag_chain : [];
    var tag = chain.length ? chain[chain.length - 1] : '';
    if (tag) return '<' + tag.toLowerCase() + '>';
    if (a.role) return a.role;
    return 'pin';
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
    chipLabel: chipLabel,
    relativeTime: relativeTime,
    formatTime: formatTime,
    formKeyFor: formKeyFor,
    authorColorIndex: authorColorIndex,
    AUTHOR_COLOR_COUNT: AUTHOR_COLOR_COUNT,
    BTN_CLASS: BTN_CLASS,
    BTN_SM_CLASS: BTN_SM_CLASS,
    COMMENT_CARD_CLASS: COMMENT_CARD_CLASS,
    COMMENT_BODY_CLASS: COMMENT_BODY_CLASS,
    COMMENT_ACTIONS_CLASS: COMMENT_ACTIONS_CLASS,
  };
});
