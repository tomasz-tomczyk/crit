// design-mode.row.js — DOM-composed design pin row.
//
// Rows mount the shared buildCommentCard from frontend/crit-comment-card.js
// so design pins reach parity with code-review's comment-card affordances
// (Edit/Resolve/Reply/Collapse, body markdown render, live-thread badge).
// Design-specific meta — route badge, chip label — is composed into the
// shared card before the body so existing CSS rules
// (`.crit-design-comment-row`, `.crit-design-comment-header`) keep
// targeting the same nodes.
//
// Public API:
//   renderDesignPinRow(comment, deps) -> HTMLElement
//     Returns the wrapper element ready for appendChild.
//   chipLabel(domAnchor) -> string
//
// `deps` is an object with shared dependencies the card needs. design-mode.js
// builds it once at boot; passing it explicitly keeps this module easy to
// unit test.
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

  // chipLabel — canonical version from crit-comment-card-helpers.js.
  var chipLabel = (typeof window !== 'undefined' && window.crit && window.crit.commentCardHelpers)
    ? window.crit.commentCardHelpers.chipLabel
    : function () { return 'pin'; };

  function formatTimeFallback(s) {
    if (!s) return '';
    try {
      var d = new Date(s);
      if (isNaN(d.getTime())) return '';
      return d.toLocaleString();
    } catch (_) { return ''; }
  }

  // ----- DOM-composed row (mounts shared buildCommentCard) ----------------

  // makeReplyListBuilder — returns a (comment, filePath, extraClass) function
  // that buildCommentCard can invoke. Closes over `deps` so we get markdown
  // rendering, author colours, and icon SVGs. Output classes mirror
  // code-review's renderReplyList exactly (.comment-replies / .comment-reply
  // / .reply-header / .reply-meta / .reply-time / .reply-body) so the shared
  // style.css rules apply unchanged.
  function makeReplyListBuilder(deps) {
    deps = deps || {};
    var commentMd = deps.commentMd;
    var formatTime = deps.formatTime || formatTimeFallback;
    var authorColorIndex = deps.authorColorIndex || function () { return 0; };
    var iconEdit = deps.iconEdit || '';
    var iconDelete = deps.iconDelete || '';

    return function buildDesignReplyList(comment, _filePath, extraClass) {
      var container = document.createElement('div');
      // Carry both the shared (.comment-replies) and design-mode-legacy
      // (.crit-design-comment-replies) class so existing E2E selectors keep
      // matching while the shared style.css rules apply.
      container.className = 'comment-replies crit-design-comment-replies' + (extraClass ? ' ' + extraClass : '');
      var replies = Array.isArray(comment.replies) ? comment.replies : [];
      for (var i = 0; i < replies.length; i++) {
        var r = replies[i] || {};
        var row = document.createElement('div');
        row.className = 'comment-reply crit-design-comment-reply';
        row.dataset.replyId = r.id || '';

        var hdr = document.createElement('div');
        hdr.className = 'reply-header';

        var meta = document.createElement('div');
        meta.className = 'reply-meta';
        if (r.author) {
          var a = document.createElement('span');
          a.className = 'comment-author-badge author-color-' + authorColorIndex(r.author);
          a.textContent = '@' + r.author;
          meta.appendChild(a);
        }
        var ts = formatTime(r.created_at);
        if (ts) {
          var t = document.createElement('span');
          t.className = 'reply-time';
          t.textContent = ts;
          meta.appendChild(t);
        }
        hdr.appendChild(meta);

        // Per-reply Edit/Delete affordance — mirrors code-review's
        // .comment-reply:hover .reply-actions reveal. Wiring of the click
        // handlers is the design-mode controller's job; the chrome lives
        // here so visuals match without a separate stylesheet.
        var actions = document.createElement('div');
        actions.className = 'reply-actions';
        var editBtn = document.createElement('button');
        editBtn.type = 'button';
        editBtn.className = 'crit-design-reply-edit';
        editBtn.title = 'Edit';
        editBtn.setAttribute('aria-label', 'Edit reply');
        editBtn.dataset.commentId = comment.id || '';
        editBtn.dataset.replyId = r.id || '';
        editBtn.innerHTML = iconEdit;
        var delBtn = document.createElement('button');
        delBtn.type = 'button';
        delBtn.className = 'delete-btn crit-design-reply-delete';
        delBtn.title = 'Delete';
        delBtn.setAttribute('aria-label', 'Delete reply');
        delBtn.dataset.commentId = comment.id || '';
        delBtn.dataset.replyId = r.id || '';
        delBtn.innerHTML = iconDelete;
        actions.appendChild(editBtn);
        actions.appendChild(delBtn);
        hdr.appendChild(actions);

        row.appendChild(hdr);

        var body = document.createElement('div');
        body.className = 'reply-body';
        body.dataset.rawBody = r.body || '';
        if (commentMd && typeof commentMd.render === 'function') {
          body.innerHTML = commentMd.render(r.body || '');
        } else {
          body.textContent = r.body || '';
        }
        row.appendChild(body);
        container.appendChild(row);
      }
      return container;
    };
  }

  // Back-compat: plain (no markdown, no icons) builder. Older callers and
  // unit tests reach for this directly. New design-mode mounts go through
  // makeReplyListBuilder via renderDesignPinRow.
  var buildDesignReplyList = makeReplyListBuilder({});

  function buildDesignReplyComposer(commentId, pathname, draft) {
    var wrap = document.createElement('div');
    wrap.className = 'crit-design-reply-composer';
    wrap.dataset.commentId = commentId || '';
    wrap.dataset.pathname = pathname || '';

    var ta = document.createElement('textarea');
    ta.className = 'crit-design-reply-textarea';
    ta.rows = 3;
    ta.placeholder = 'Write a reply… (Ctrl+Enter to submit, Escape to cancel)';
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
    save.textContent = 'Reply';

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
    // Default author to 'Reviewer' when user_id present but name missing
    if (!c.author && c.user_id) c.author = 'Reviewer';
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

    var replyListBuilder = makeReplyListBuilder({
      commentMd: deps.commentMd,
      formatTime: deps.formatTime,
      authorColorIndex: deps.authorColorIndex,
      iconEdit: deps.iconEdit || '',
      iconDelete: deps.iconDelete || '',
    });

    var parts = card.buildCommentCard(c, pathname, {
      // Include `panel-comment-block` so the row gets the panel's tight
      // 12px padding instead of the 56px left-gutter padding that
      // `.comment-block` applies for inline (under-line) comments in
      // code-review. Design pins always render in the side panel — there's
      // no inline gutter to reserve space for. This matches code-review's
      // own panel mount in app.js.
      wrapperClass: 'comment-block panel-comment-block crit-design-comment-row-wrap',
      // Drop the bespoke .crit-design-comment-row chrome — code-review's
      // .comment-card already provides border, background, and padded header
      // bar. Adding a second border/background was the source of the
      // "card-in-a-card" mismatch with code-review.
      // Keep `crit-design-comment-row` as a marker class so existing
      // composer/edit rules can target it; the chrome comes from
      // `.comment-card` (border, header bar, body padding) — see
      // style-design.css where `.crit-design-comment-row` is now neutralised.
      cardClassExtra: 'crit-design-comment-row' + (c.resolved ? ' resolved-card' : ''),
      // Design mode has no drift concept — daemon stopped emitting the bit
      // and there is no per-pin drift UI surface in this mode.
      suppressDrift: true,
      // Auto-collapse resolved pins (parity with code-review's panel cards
      // at app.js#renderCommentsPanel — `collapseDefault: isResolved`).
      // Open pins stay expanded; resolved pins collapse to a one-line stub
      // unless the user has explicitly toggled an override via the chevron.
      collapseDefault: !!c.resolved,
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
        renderReplyList: replyListBuilder,
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

    // Slot the design-specific meta (route badge + chip) into the shared
    // header's left side, sitting where code-review puts the line-ref. This
    // gives us one consistent header bar instead of a second band stacked
    // above the body.
    var headerLeft = parts.card.querySelector('.comment-header-left');
    if (headerLeft && c.dom_anchor) {
      var chipText = chipLabel(anchor);
      if (chipText) {
        var chip = document.createElement('span');
        chip.className = 'crit-design-comment-chip';
        chip.textContent = chipText;
        chip.title = chipText;
        headerLeft.appendChild(chip);
      }
    }

    // Action buttons — match code-review's icon affordance + ordering
    // (Resolve, Edit, Reply). Reply has no analogue in code-review (which
    // uses an always-on reply input form below the card); design mode keeps
    // an explicit Reply button because the reply composer is opened on
    // demand, not always rendered. The icon-only treatment + .resolve-btn
    // pill class hooks straight into the shared style.css rules.
    var resolveBtn = document.createElement('button');
    resolveBtn.type = 'button';
    var resolveCls = 'resolve-btn crit-design-comment-resolve';
    if (c.resolved) resolveCls += ' resolve-btn--active';
    resolveBtn.className = resolveCls;
    resolveBtn.dataset.commentId = commentId;
    resolveBtn.dataset.pathname = pathname;
    var resolveLabel = c.resolved ? 'Unresolve' : 'Resolve';
    resolveBtn.title = resolveLabel;
    resolveBtn.setAttribute('aria-label', resolveLabel + ' thread');
    var resolveIcon = c.resolved
      ? (deps.iconUnresolve || '')
      : (deps.iconResolve || '');
    resolveBtn.innerHTML = resolveIcon + '<span>' + resolveLabel + '</span>';
    parts.actions.appendChild(resolveBtn);

    var editBtn = document.createElement('button');
    editBtn.type = 'button';
    editBtn.className = 'crit-design-comment-edit';
    editBtn.dataset.commentId = commentId;
    editBtn.dataset.pathname = pathname;
    editBtn.title = 'Edit';
    editBtn.setAttribute('aria-label', 'Edit comment');
    editBtn.innerHTML = deps.iconEdit || '';
    parts.actions.appendChild(editBtn);

    var replyBtn = document.createElement('button');
    replyBtn.type = 'button';
    replyBtn.className = 'crit-design-comment-reply';
    replyBtn.dataset.commentId = commentId;
    replyBtn.dataset.pathname = pathname;
    replyBtn.title = 'Reply';
    replyBtn.setAttribute('aria-label', 'Reply to comment');
    replyBtn.innerHTML = deps.iconReply || '';
    parts.actions.appendChild(replyBtn);

    // Delete affordance — mirrors code-review's `.delete-btn` icon button on
    // every comment card. Reply rows already had one (`.crit-design-reply-delete`);
    // top-level cards were missing the parent-comment delete entirely.
    // Wired to `DELETE /api/comment/{id}?path=<pathname>` in design-mode.js.
    var deleteBtn = document.createElement('button');
    deleteBtn.type = 'button';
    deleteBtn.className = 'delete-btn crit-design-comment-delete';
    deleteBtn.dataset.commentId = commentId;
    deleteBtn.dataset.pathname = pathname;
    deleteBtn.title = 'Delete';
    deleteBtn.setAttribute('aria-label', 'Delete comment');
    deleteBtn.innerHTML = deps.iconDelete || '';
    parts.actions.appendChild(deleteBtn);

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
    ta.placeholder = 'Edit comment… (Ctrl+Enter to submit, Escape to cancel)';
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
    save.textContent = 'Update';

    actions.appendChild(cancel);
    actions.appendChild(save);

    wrap.appendChild(ta);
    wrap.appendChild(err);
    wrap.appendChild(actions);
    return wrap;
  }

  return {
    renderDesignPinRow: renderDesignPinRow,
    chipLabel: chipLabel,
  };
});
