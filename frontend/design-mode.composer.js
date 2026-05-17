'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.design = root.crit.design || {};
    root.crit.design.composer = api;
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
      '<div class="crit-design-composer" role="dialog" aria-label="New design pin">',
        '<div class="crit-design-composer-meta">',
          '<span class="crit-design-composer-chip">' + escapeHTML(label) + '</span>',
        '</div>',
        '<textarea class="crit-design-composer-body" placeholder="Leave a design comment… (Ctrl+Enter to submit, Escape to cancel)" rows="4"></textarea>',
        '<div class="crit-design-composer-error" hidden></div>',
        '<div class="crit-design-composer-actions">',
          '<button type="button" class="btn btn-sm crit-design-composer-cancel">Cancel</button>',
          '<button type="button" class="btn btn-sm btn-primary crit-design-composer-save">Comment</button>',
        '</div>',
      '</div>',
    ].join('');
  }
  return { renderComposerHTML, escapeHTML, chipLabel };
});
