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
  function renderComposerHTML(a) {
    var thumb = a.screenshot
      ? '<img class="crit-design-composer-thumb" src="' + escapeHTML(a.screenshot) + '" alt="">'
      : '<pre class="crit-design-composer-html-preview">' + escapeHTML(a.outer_html || '') + '</pre>';
    return [
      '<div class="crit-design-composer" role="dialog" aria-label="New design pin">',
        '<div class="crit-design-composer-meta">',
          '<code>' + escapeHTML(a.css_selector) + '</code>',
          '<span class="crit-design-composer-route">' + escapeHTML(a.pathname) + '</span>',
        '</div>',
        thumb,
        '<textarea class="crit-design-composer-body" placeholder="Comment..." rows="4"></textarea>',
        '<div class="crit-design-composer-error" hidden></div>',
        '<div class="crit-design-composer-actions">',
          '<button type="button" class="crit-design-composer-cancel">Cancel</button>',
          '<button type="button" class="crit-design-composer-save">Save</button>',
        '</div>',
      '</div>',
    ].join('');
  }
  return { renderComposerHTML, escapeHTML };
});
