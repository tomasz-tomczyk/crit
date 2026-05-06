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
  // Row attribute values are double-quoted, so we additionally escape single
  // quotes to be safe against bodies that contain attribute-breaking
  // sequences. Cannot use crit.commentCardHelpers.escapeHtml here because it
  // does not escape single quotes (matches app.js semantics where attribute
  // values are also double-quoted but bodies pass through markdown-it first).
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

  function renderRepliesHTML(replies) {
    if (!Array.isArray(replies) || replies.length === 0) return '';
    var out = ['<div class="crit-design-comment-replies">'];
    for (var i = 0; i < replies.length; i++) {
      var r = replies[i] || {};
      var author = r.author ? '<span class="crit-design-reply-author">@' + escapeHTML(r.author) + '</span>' : '';
      var ts = formatTime(r.created_at);
      var time = ts ? '<span class="crit-design-reply-time">' + escapeHTML(ts) + '</span>' : '';
      out.push(
        '<div class="crit-design-comment-reply" data-reply-id="' + escapeHTML(r.id || '') + '">' +
          '<div class="crit-design-reply-header">' + author + time + '</div>' +
          '<div class="crit-design-reply-body">' + escapeHTML(r.body || '') + '</div>' +
        '</div>'
      );
    }
    out.push('</div>');
    return out.join('');
  }

  function renderReplyComposerHTML(commentId, pathname, draft) {
    var safeId = escapeHTML(commentId || '');
    var safePath = escapeHTML(pathname || '');
    var safeDraft = escapeHTML(draft || '');
    return [
      '<div class="crit-design-reply-composer" data-comment-id="' + safeId + '" data-pathname="' + safePath + '">',
        '<textarea class="crit-design-reply-textarea" rows="3" placeholder="Write a reply…">' + safeDraft + '</textarea>',
        '<div class="crit-design-reply-error" hidden></div>',
        '<div class="crit-design-reply-actions">',
          '<button type="button" class="btn btn-sm crit-design-reply-cancel" data-comment-id="' + safeId + '">Cancel</button>',
          '<button type="button" class="btn btn-sm btn-primary crit-design-reply-save" data-comment-id="' + safeId + '" data-pathname="' + safePath + '">Save</button>',
        '</div>',
      '</div>',
    ].join('');
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
    var pathname = a.pathname || '';
    var commentId = c.id || '';
    var replies = renderRepliesHTML(c.replies);
    var composer = c._replyOpen
      ? renderReplyComposerHTML(commentId, pathname, c._replyDraft || '')
      : '';
    return [
      '<div class="comment-card crit-design-comment-row" data-id="' + escapeHTML(commentId) + '" data-comment-id="' + escapeHTML(commentId) + '" data-design-route="' + escapeHTML(pathname) + '"' + resolved + '>',
        '<div class="crit-design-comment-header">',
          '<span class="crit-design-comment-route-badge">' + escapeHTML(pathname) + '</span>',
          '<span class="crit-design-comment-chip">' + escapeHTML(chipLabel(a)) + '</span>',
          author,
          time,
        '</div>',
        thumb,
        '<div class="crit-design-comment-body">' + escapeHTML(c.body || '') + '</div>',
        replies,
        '<div class="crit-design-comment-actions">',
          '<button type="button" class="btn btn-sm crit-design-comment-reply" data-comment-id="' + escapeHTML(commentId) + '" data-pathname="' + escapeHTML(pathname) + '">Reply</button>',
          '<button type="button" class="btn btn-sm crit-design-comment-resolve" data-comment-id="' + escapeHTML(commentId) + '" data-pathname="' + escapeHTML(pathname) + '">' + resolveLabel + '</button>',
        '</div>',
        composer,
      '</div>',
    ].join('');
  }
  return { renderDesignPinRowHTML, chipLabel, renderRepliesHTML, renderReplyComposerHTML };
});
