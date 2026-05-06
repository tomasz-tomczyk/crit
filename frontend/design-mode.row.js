// design-mode.row.js — DOM-composed design pin row.
//
// Phase C: rows now mount the shared buildCommentCard from
// frontend/crit-comment-card.js so design pins reach parity with code-review's
// comment-card affordances (Edit/Resolve/Reply/Collapse, body markdown render,
// drift context, live-thread badge). Design-specific meta — route badge, chip
// label, and screenshot thumbnail — is composed *into* the shared card before
// the body so existing CSS rules (`.crit-design-comment-row`,
// `.crit-design-comment-header`, `.crit-design-comment-thumb`) keep targeting
// the same nodes.
//
// Public API:
//   renderDesignPinRow(comment, deps) -> HTMLElement
//     Returns the wrapper element ready for appendChild.
//   chipLabel(domAnchor) -> string  (kept for legacy callers)
//
// `deps` is an object with shared dependencies the card needs. design-mode.js
// builds it once at boot; passing it explicitly keeps this module easy to
// unit test.
//
// The legacy string-template helpers (renderDesignPinRowHTML,
// renderRepliesHTML, renderReplyComposerHTML) remain exported for backwards
// compatibility with existing tests; they will be removed once design-mode.js
// finishes the DOM migration.
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

  function formatTimeFallback(s) {
    if (!s) return '';
    try {
      var d = new Date(s);
      if (isNaN(d.getTime())) return '';
      return d.toLocaleString();
    } catch (_) { return ''; }
  }

  // ----- Legacy string-template helpers (kept for backwards-compat tests) ---

  function renderRepliesHTML(replies) {
    if (!Array.isArray(replies) || replies.length === 0) return '';
    var out = ['<div class="crit-design-comment-replies">'];
    for (var i = 0; i < replies.length; i++) {
      var r = replies[i] || {};
      var author = r.author ? '<span class="crit-design-reply-author">@' + escapeHTML(r.author) + '</span>' : '';
      var ts = formatTimeFallback(r.created_at);
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

  function renderEditComposerHTML(commentId, pathname, draft) {
    var safeId = escapeHTML(commentId || '');
    var safePath = escapeHTML(pathname || '');
    var safeDraft = escapeHTML(draft || '');
    return [
      '<div class="crit-design-edit-composer" data-comment-id="' + safeId + '" data-pathname="' + safePath + '">',
        '<textarea class="crit-design-edit-textarea" rows="3">' + safeDraft + '</textarea>',
        '<div class="crit-design-edit-error" hidden></div>',
        '<div class="crit-design-edit-actions">',
          '<button type="button" class="btn btn-sm crit-design-edit-cancel" data-comment-id="' + safeId + '">Cancel</button>',
          '<button type="button" class="btn btn-sm btn-primary crit-design-edit-save" data-comment-id="' + safeId + '" data-pathname="' + safePath + '">Save</button>',
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
    var ts = formatTimeFallback(c.created_at);
    var time = ts ? '<span class="crit-design-comment-time">' + escapeHTML(ts) + '</span>' : '';
    var resolveLabel = c.resolved ? 'Reopen' : 'Resolve';
    var pathname = a.pathname || '';
    var commentId = c.id || '';
    var replies = renderRepliesHTML(c.replies);
    var composer = c._replyOpen
      ? renderReplyComposerHTML(commentId, pathname, c._replyDraft || '')
      : '';
    var editComposer = c._editOpen
      ? renderEditComposerHTML(commentId, pathname, c._editDraft != null ? c._editDraft : (c.body || ''))
      : '';
    var bodySection = c._editOpen
      ? editComposer
      : '<div class="crit-design-comment-body">' + escapeHTML(c.body || '') + '</div>';
    return [
      '<div class="comment-card crit-design-comment-row" data-id="' + escapeHTML(commentId) + '" data-comment-id="' + escapeHTML(commentId) + '" data-design-route="' + escapeHTML(pathname) + '"' + resolved + '>',
        '<div class="crit-design-comment-header">',
          '<span class="crit-design-comment-route-badge">' + escapeHTML(pathname) + '</span>',
          '<span class="crit-design-comment-chip">' + escapeHTML(chipLabel(a)) + '</span>',
          author,
          time,
        '</div>',
        thumb,
        bodySection,
        replies,
        '<div class="crit-design-comment-actions">',
          '<button type="button" class="btn btn-sm crit-design-comment-edit" data-comment-id="' + escapeHTML(commentId) + '" data-pathname="' + escapeHTML(pathname) + '">Edit</button>',
          '<button type="button" class="btn btn-sm crit-design-comment-reply" data-comment-id="' + escapeHTML(commentId) + '" data-pathname="' + escapeHTML(pathname) + '">Reply</button>',
          '<button type="button" class="btn btn-sm crit-design-comment-resolve" data-comment-id="' + escapeHTML(commentId) + '" data-pathname="' + escapeHTML(pathname) + '">' + resolveLabel + '</button>',
        '</div>',
        composer,
      '</div>',
    ].join('');
  }

  // ----- DOM-composed row (Phase C — mounts shared buildCommentCard) -------

  function buildDesignReplyList(comment, _filePath, _extraClass) {
    // Mirrors today's renderRepliesHTML output as a DOM tree so existing CSS
    // and any future selectors keep working. Falls back to text for the body
    // (no markdown render for replies — matches current design-mode behaviour).
    var container = document.createElement('div');
    container.className = 'crit-design-comment-replies';
    var replies = Array.isArray(comment.replies) ? comment.replies : [];
    for (var i = 0; i < replies.length; i++) {
      var r = replies[i] || {};
      var row = document.createElement('div');
      row.className = 'crit-design-comment-reply';
      row.dataset.replyId = r.id || '';
      var hdr = document.createElement('div');
      hdr.className = 'crit-design-reply-header';
      if (r.author) {
        var a = document.createElement('span');
        a.className = 'crit-design-reply-author';
        a.textContent = '@' + r.author;
        hdr.appendChild(a);
      }
      var ts = formatTimeFallback(r.created_at);
      if (ts) {
        var t = document.createElement('span');
        t.className = 'crit-design-reply-time';
        t.textContent = ts;
        hdr.appendChild(t);
      }
      var body = document.createElement('div');
      body.className = 'crit-design-reply-body';
      body.textContent = r.body || '';
      row.appendChild(hdr);
      row.appendChild(body);
      container.appendChild(row);
    }
    return container;
  }

  function buildDesignReplyComposer(commentId, pathname, draft) {
    var wrap = document.createElement('div');
    wrap.className = 'crit-design-reply-composer';
    wrap.dataset.commentId = commentId || '';
    wrap.dataset.pathname = pathname || '';

    var ta = document.createElement('textarea');
    ta.className = 'crit-design-reply-textarea';
    ta.rows = 3;
    ta.placeholder = 'Write a reply…';
    ta.value = draft || '';

    var err = document.createElement('div');
    err.className = 'crit-design-reply-error';
    err.hidden = true;

    var actions = document.createElement('div');
    actions.className = 'crit-design-reply-actions';

    var cancel = document.createElement('button');
    cancel.type = 'button';
    cancel.className = 'btn btn-sm crit-design-reply-cancel';
    cancel.dataset.commentId = commentId || '';
    cancel.textContent = 'Cancel';

    var save = document.createElement('button');
    save.type = 'button';
    save.className = 'btn btn-sm btn-primary crit-design-reply-save';
    save.dataset.commentId = commentId || '';
    save.dataset.pathname = pathname || '';
    save.textContent = 'Save';

    actions.appendChild(cancel);
    actions.appendChild(save);

    wrap.appendChild(ta);
    wrap.appendChild(err);
    wrap.appendChild(actions);
    return wrap;
  }

  // renderDesignPinRow — produces the full row element using buildCommentCard.
  // deps:
  //   commentMd       — markdown-it instance
  //   formatTime      — short timestamp renderer
  //   authorColorIndex— author colour swatch picker
  //   getReviewRound  — () => current review round
  //   getCollapseOverride / setCollapseOverride — design-mode-scoped store
  //   iconChevron     — SVG string
  function renderDesignPinRow(c, deps) {
    deps = deps || {};
    var anchor = c.dom_anchor || {};
    var pathname = anchor.pathname || '';
    var commentId = c.id || '';

    var card = window.crit && window.crit.commentCard;
    if (!card || typeof card.buildCommentCard !== 'function') {
      // Fallback: degrade to a minimal row so design mode still renders even
      // if the shared module is missing. Should never happen in practice.
      var fallback = document.createElement('div');
      fallback.className = 'comment-card crit-design-comment-row';
      fallback.dataset.id = commentId;
      fallback.dataset.commentId = commentId;
      fallback.dataset.designRoute = pathname;
      fallback.textContent = c.body || '';
      return fallback;
    }

    var parts = card.buildCommentCard(c, pathname, {
      wrapperClass: 'crit-design-comment-row-wrap',
      cardClassExtra: 'crit-design-comment-row' + (c.resolved ? ' resolved-card resolved' : ''),
      // Design pins default to expanded — buildCommentCard's collapseDefault
      // mode is for code-review's resolved-thread auto-fold; design rows
      // stay open until the user collapses them via the chevron.
      collapseDefault: false,
      showLineRef: false,
      // Keep the design-mode reply composer separate from the shared card's
      // built-in reply input. We append our own composer below when
      // c._replyOpen is true so existing handlers (crit-design-reply-*) keep
      // working.
      showReplyInput: false,
      // Design pins are not "live" agent threads — that badge is reserved
      // for code-review comments where the agent is actively responding.
      isLiveThread: function () { return false; },
      // Design mode does not dispatch agent requests via the comment card.
      isPendingAgentRequest: function () { return false; },
      // Per-pin collapse store lives on design state.
      getCollapseOverride: deps.getCollapseOverride,
      setCollapseOverride: deps.setCollapseOverride,
      deps: {
        commentMd: deps.commentMd,
        formatTime: deps.formatTime,
        authorColorIndex: deps.authorColorIndex,
        getReviewRound: deps.getReviewRound || function () { return 0; },
        getAgentName: function () { return 'agent'; },
        buildCommentEnv: function () { return undefined; },
        renderReplyList: buildDesignReplyList,
        createReplyInput: function () { return document.createElement('div'); },
        iconChevron: deps.iconChevron || '',
      },
    });

    // Mark wrapper + card with the data attributes the existing CSS / event
    // handlers / E2E selectors expect.
    parts.wrapper.dataset.commentId = commentId;
    parts.wrapper.dataset.designRoute = pathname;
    parts.card.dataset.id = commentId;
    parts.card.dataset.designRoute = pathname;
    if (c.resolved) {
      parts.card.dataset.resolved = 'true';
      parts.wrapper.dataset.resolved = 'true';
      // resolved-card matches code-review's hide-resolved logic and any
      // future scoped styling; the .resolved class lets shared styling
      // hooks (and the prior bespoke style-design rule, now removed)
      // converge on a single selector.
    }

    // Insert the design-specific meta (route badge, chip, thumbnail) at the
    // top of the card, before the body. We can locate the body element to
    // anchor the insertion point.
    var body = parts.card.querySelector('.comment-body');
    var meta = document.createElement('div');
    meta.className = 'crit-design-comment-header';
    var routeBadge = document.createElement('span');
    routeBadge.className = 'crit-design-comment-route-badge';
    routeBadge.textContent = pathname;
    meta.appendChild(routeBadge);
    var chip = document.createElement('span');
    chip.className = 'crit-design-comment-chip';
    chip.textContent = chipLabel(anchor);
    meta.appendChild(chip);
    parts.card.insertBefore(meta, body);

    if (anchor.screenshot) {
      var thumb = document.createElement('img');
      thumb.className = 'crit-design-comment-thumb';
      thumb.src = anchor.screenshot;
      thumb.alt = '';
      parts.card.insertBefore(thumb, body);
    }

    // Append Edit + Reply + Resolve buttons to the shared actions slot. These
    // carry data-comment-id / data-pathname so the existing dispatch handlers
    // in design-mode.js continue to work without changes.
    var editBtn = document.createElement('button');
    editBtn.type = 'button';
    editBtn.className = 'btn btn-sm crit-design-comment-edit';
    editBtn.dataset.commentId = commentId;
    editBtn.dataset.pathname = pathname;
    editBtn.textContent = 'Edit';
    parts.actions.appendChild(editBtn);

    var replyBtn = document.createElement('button');
    replyBtn.type = 'button';
    replyBtn.className = 'btn btn-sm crit-design-comment-reply';
    replyBtn.dataset.commentId = commentId;
    replyBtn.dataset.pathname = pathname;
    replyBtn.textContent = 'Reply';
    parts.actions.appendChild(replyBtn);

    var resolveBtn = document.createElement('button');
    resolveBtn.type = 'button';
    resolveBtn.className = 'btn btn-sm crit-design-comment-resolve';
    resolveBtn.dataset.commentId = commentId;
    resolveBtn.dataset.pathname = pathname;
    resolveBtn.textContent = c.resolved ? 'Reopen' : 'Resolve';
    parts.actions.appendChild(resolveBtn);

    // Inline reply composer when open.
    if (c._replyOpen) {
      parts.card.appendChild(buildDesignReplyComposer(commentId, pathname, c._replyDraft || ''));
    }

    // Inline edit composer — replaces the body when open. Locating the body
    // we rendered through buildCommentCard and swapping it for the textarea
    // form lets the existing markdown render path stay untouched while we're
    // not editing.
    if (c._editOpen) {
      var bodyEl = parts.card.querySelector('.comment-body');
      var draft = c._editDraft != null ? c._editDraft : (c.body || '');
      var ec = buildDesignEditComposer(commentId, pathname, draft);
      if (bodyEl && bodyEl.parentNode) {
        bodyEl.parentNode.insertBefore(ec, bodyEl);
        bodyEl.style.display = 'none';
      } else {
        parts.card.appendChild(ec);
      }
    }

    return parts.wrapper;
  }

  function buildDesignEditComposer(commentId, pathname, draft) {
    var wrap = document.createElement('div');
    wrap.className = 'crit-design-edit-composer';
    wrap.dataset.commentId = commentId || '';
    wrap.dataset.pathname = pathname || '';

    var ta = document.createElement('textarea');
    ta.className = 'crit-design-edit-textarea';
    ta.rows = 3;
    ta.value = draft || '';

    var err = document.createElement('div');
    err.className = 'crit-design-edit-error';
    err.hidden = true;

    var actions = document.createElement('div');
    actions.className = 'crit-design-edit-actions';

    var cancel = document.createElement('button');
    cancel.type = 'button';
    cancel.className = 'btn btn-sm crit-design-edit-cancel';
    cancel.dataset.commentId = commentId || '';
    cancel.textContent = 'Cancel';

    var save = document.createElement('button');
    save.type = 'button';
    save.className = 'btn btn-sm btn-primary crit-design-edit-save';
    save.dataset.commentId = commentId || '';
    save.dataset.pathname = pathname || '';
    save.textContent = 'Save';

    actions.appendChild(cancel);
    actions.appendChild(save);

    wrap.appendChild(ta);
    wrap.appendChild(err);
    wrap.appendChild(actions);
    return wrap;
  }

  return {
    renderDesignPinRow: renderDesignPinRow,
    renderDesignPinRowHTML: renderDesignPinRowHTML,
    chipLabel: chipLabel,
    renderRepliesHTML: renderRepliesHTML,
    renderReplyComposerHTML: renderReplyComposerHTML,
    renderEditComposerHTML: renderEditComposerHTML,
  };
});
