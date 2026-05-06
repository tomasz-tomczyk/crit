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

  // Mirrors design-mode.composer.js's chip label heuristic, kept in sync but
  // duplicated to avoid module-load ordering surprises.
  function chipLabel(a) {
    var name = (a.accessible_name || '').trim();
    if (name) return name.length > 60 ? name.slice(0, 60) + '…' : name;
    var html = a.outer_html || '';
    var text = html.replace(/<[^>]*>/g, ' ').replace(/\s+/g, ' ').trim();
    if (text) return text.length > 60 ? text.slice(0, 60) + '…' : text;
    var chain = Array.isArray(a.tag_chain) ? a.tag_chain : [];
    var tag = chain.length ? chain[chain.length - 1] : '';
    return tag ? '<' + tag.toLowerCase() + '>' : 'element';
  }

  function formatTime(s) {
    if (!s) return '';
    try {
      var d = new Date(s);
      if (isNaN(d.getTime())) return '';
      return d.toLocaleString();
    } catch (_) { return ''; }
  }

  function renderDesignPinRowHTML(c) {
    var a = c.dom_anchor || {};
    var thumb = a.screenshot
      ? '<img class="crit-design-comment-thumb" src="' + escapeHTML(a.screenshot) + '" alt="">'
      : '';
    var resolved = c.resolved ? ' data-resolved="true"' : '';
    var author = c.author ? '<span class="crit-design-comment-author">@' + escapeHTML(c.author) + '</span>' : '';
    var ts = formatTime(c.created_at);
    var time = ts ? '<span class="crit-design-comment-time">' + escapeHTML(ts) + '</span>' : '';
    var resolveLabel = c.resolved ? 'Reopen' : 'Resolve';
    return [
      '<div class="comment-card crit-design-comment-row" data-id="' + escapeHTML(c.id || '') + '" data-comment-id="' + escapeHTML(c.id || '') + '" data-design-route="' + escapeHTML(a.pathname || '') + '"' + resolved + '>',
        '<div class="crit-design-comment-header">',
          '<span class="crit-design-comment-route-badge">' + escapeHTML(a.pathname || '') + '</span>',
          '<span class="crit-design-comment-chip">' + escapeHTML(chipLabel(a)) + '</span>',
          author,
          time,
        '</div>',
        thumb,
        '<div class="crit-design-comment-body">' + escapeHTML(c.body || '') + '</div>',
        '<div class="crit-design-comment-actions">',
          '<button type="button" class="btn btn-sm crit-design-comment-resolve" data-comment-id="' + escapeHTML(c.id || '') + '" data-pathname="' + escapeHTML(a.pathname || '') + '">' + resolveLabel + '</button>',
        '</div>',
      '</div>',
    ].join('');
  }
  return { renderDesignPinRowHTML, chipLabel };
});
