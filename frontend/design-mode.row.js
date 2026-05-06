'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.design = root.crit.design || {};
    root.crit.design.row = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  function escapeHTML(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
  }
  function renderDesignPinRowHTML(c) {
    var a = c.dom_anchor || {};
    var thumb = a.screenshot
      ? '<img class="crit-design-comment-thumb" src="' + escapeHTML(a.screenshot) + '" alt="">'
      : '<pre class="crit-design-comment-html-preview">' + escapeHTML(a.outer_html || '') + '</pre>';
    return [
      '<div class="crit-design-comment-row" data-id="' + escapeHTML(c.id || '') + '">',
        '<span class="crit-design-comment-route-badge">' + escapeHTML(a.pathname || '') + '</span>',
        thumb,
        '<div class="crit-design-comment-body">' + escapeHTML(c.body || '') + '</div>',
        '<code class="crit-design-comment-selector">' + escapeHTML(a.css_selector || '') + '</code>',
      '</div>',
    ].join('');
  }
  return { renderDesignPinRowHTML };
});
