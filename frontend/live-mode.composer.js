'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.live = root.crit.live || {};
    root.crit.live.composer = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  // escapeHTML — delegates to the canonical window.crit.shared.escapeHTML.
  var escapeHTML = (typeof window !== 'undefined' && window.crit && window.crit.shared)
    ? window.crit.shared.escapeHTML
    : function (s) { return String(s == null ? '' : s)
        .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;').replace(/'/g, '&#39;'); };

  // chipLabel is the canonical version from crit-comment-card-helpers.js.
  var chipLabel = (typeof window !== 'undefined' && window.crit && window.crit.commentCardHelpers)
    ? window.crit.commentCardHelpers.chipLabel
    : function () { return 'pin'; };

  function renderComposerHTML(a) {
    var label = chipLabel(a);
    return [
      '<div class="crit-live-composer" role="dialog" aria-label="New live pin">',
        '<div class="crit-live-composer-meta">',
          '<span class="crit-live-composer-chip">' + escapeHTML(label) + '</span>',
        '</div>',
        '<textarea class="crit-live-composer-body" placeholder="Leave a live comment… (Ctrl+Enter to submit, Escape to cancel)" rows="4"></textarea>',
        '<div class="crit-live-composer-error" hidden></div>',
        '<div class="crit-live-composer-actions">',
          '<button type="button" class="btn btn-sm crit-live-composer-cancel">Cancel</button>',
          '<button type="button" class="btn btn-sm btn-primary crit-live-composer-save">Comment</button>',
        '</div>',
      '</div>',
    ].join('');
  }
  return { renderComposerHTML, escapeHTML, chipLabel };
});
