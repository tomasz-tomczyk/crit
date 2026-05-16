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
  function escapeHTML(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
  }

  // Tidewave-style chip label: prefer accessible_name, then a short slice of
  // textContent extracted from outer_html, then fall back to the leaf tag.
  function chipLabel(a) {
    var name = (a.accessible_name || '').trim();
    if (name) return name.length > 60 ? name.slice(0, 60) + '…' : name;
    var html = a.outer_html || '';
    // crude text extract: strip tags, collapse whitespace.
    var text = html.replace(/<[^>]*>/g, ' ').replace(/\s+/g, ' ').trim();
    if (text) return text.length > 60 ? text.slice(0, 60) + '…' : text;
    var chain = Array.isArray(a.tag_chain) ? a.tag_chain : [];
    var tag = chain.length ? chain[chain.length - 1] : '';
    return tag ? '<' + tag.toLowerCase() + '>' : 'element';
  }

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
