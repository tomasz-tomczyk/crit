// crit-comment-card.js — shared comment-card renderer for code-review and
// live-mode. Mounted on window.crit.commentCard.
//
// Extracted from app.js's buildCommentCard. The function is structurally
// identical to the original — same DOM, same classes, same data attrs —
// because code-review behaviour MUST stay byte-equivalent. The change is
// that all module-scoped collaborators are passed in via `opts.deps`
// instead of being closed over.
//
// Required deps (object on opts.deps):
//   commentMd          — markdown-it instance with .render(body, env)
//   formatTime(iso)    — short time string for the header timestamp
//   authorColorIndex(name) — int 0..N for the author colour swatch
//   getReviewRound()   — current session.review_round (number)
//   getAgentName()     — current agent name string (for pending @author)
//   buildCommentEnv(comment, filePath) — env object passed to commentMd.render
//   renderReplyList(comment, filePath, extraClass) — returns the replies <div>
//   createReplyInput(commentId, filePath) — returns the reply <form>
//   iconChevron        — inline SVG string for the collapse chevron
//
// Optional callbacks (already inverted in steps 1–3):
//   isPendingAgentRequest(id)    — defaults to () => false
//   getCollapseOverride(id)      — defaults to () => undefined
//   setCollapseOverride(id, val) — defaults to no-op
//   isLiveThread(comment)        — defaults to () => false
//
// Returns { wrapper, card, actions } so callers can append their own action
// buttons (Edit / Resolve / Reply / route-meta wrappers) onto `actions`.
'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.commentCard = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {

  function noop() {}
  function alwaysFalse() { return false; }
  function alwaysUndef() { return undefined; }

  function buildCommentCard(comment, filePath, opts) {
    opts = opts || {};
    var deps = opts.deps || {};

    var commentMd = deps.commentMd;
    var formatTime = deps.formatTime || function () { return ''; };
    var authorColorIndex = deps.authorColorIndex || function () { return 0; };
    var getReviewRound = deps.getReviewRound || function () { return 0; };
    var getAgentName = deps.getAgentName || function () { return 'agent'; };
    var buildCommentEnv = deps.buildCommentEnv || function () { return undefined; };
    var renderReplyList = deps.renderReplyList || function () { return document.createElement('div'); };
    var createReplyInput = deps.createReplyInput || function () { return document.createElement('div'); };
    var iconChevron = deps.iconChevron || '';

    var isPending = typeof opts.isPendingAgentRequest === 'function' ? opts.isPendingAgentRequest : alwaysFalse;
    var getCollapseOverride = typeof opts.getCollapseOverride === 'function' ? opts.getCollapseOverride : alwaysUndef;
    var setCollapseOverride = typeof opts.setCollapseOverride === 'function' ? opts.setCollapseOverride : noop;
    var liveThreadFn = typeof opts.isLiveThread === 'function' ? opts.isLiveThread : alwaysFalse;

    var wrapper = document.createElement('div');
    wrapper.className = opts.wrapperClass || 'comment-block';

    var card = document.createElement('div');
    var cardClass = 'comment-card';
    if (opts.cardClassExtra) cardClass += ' ' + opts.cardClassExtra;
    card.className = cardClass;
    card.dataset.commentId = comment.id;

    // Collapse state — live threads stay expanded unless resolved
    var liveOrPending = !comment.resolved && (liveThreadFn(comment) || isPending(comment.id));
    var collapseOverride = getCollapseOverride(comment.id);
    var isCollapsed = liveOrPending ? false
      : opts.collapseDefault
        ? (collapseOverride !== undefined ? collapseOverride : true)
        : (collapseOverride === true);
    if (isCollapsed) card.classList.add('collapsed');

    var header = document.createElement('div');
    header.className = 'comment-header';

    var collapseBtn = document.createElement('button');
    collapseBtn.className = 'comment-collapse-btn';
    collapseBtn.title = isCollapsed ? 'Expand comment' : 'Collapse comment';
    collapseBtn.setAttribute('aria-label', isCollapsed ? 'Expand comment' : 'Collapse comment');
    collapseBtn.innerHTML = iconChevron;
    collapseBtn.addEventListener('click', function (e) {
      e.stopPropagation();
      card.classList.toggle('collapsed');
      var collapsed = card.classList.contains('collapsed');
      setCollapseOverride(comment.id, collapsed);
      collapseBtn.title = collapsed ? 'Expand comment' : 'Collapse comment';
      collapseBtn.setAttribute('aria-label', collapsed ? 'Expand comment' : 'Collapse comment');
    });

    var headerLeft = document.createElement('div');
    headerLeft.className = 'comment-header-left';
    headerLeft.prepend(collapseBtn);
    if (comment.author) {
      var authorBadge = document.createElement('span');
      authorBadge.className = 'comment-author-badge author-color-' + authorColorIndex(comment.author);
      authorBadge.textContent = '@' + comment.author;
      headerLeft.appendChild(authorBadge);
    }
    if (comment.review_round >= 1) {
      var roundBadge = document.createElement('span');
      var currentRound = getReviewRound();
      var rc = comment.review_round === currentRound ? ' round-current'
        : comment.review_round === currentRound - 1 ? ' round-latest' : '';
      roundBadge.className = 'comment-round-badge' + rc;
      roundBadge.textContent = 'R' + comment.review_round;
      headerLeft.appendChild(roundBadge);
    }
    if (opts.showLineRef && comment.scope !== 'file') {
      var lineRef = document.createElement('span');
      lineRef.className = 'comment-line-ref';
      lineRef.textContent = comment.start_line === comment.end_line
        ? 'Line ' + comment.start_line
        : 'Lines ' + comment.start_line + '-' + comment.end_line;
      headerLeft.appendChild(lineRef);
    }
    var time = document.createElement('span');
    time.className = 'comment-time';
    time.textContent = formatTime(comment.created_at);
    headerLeft.appendChild(time);

    if (liveOrPending) {
      var badge = document.createElement('span');
      badge.className = 'live-thread-badge' + (isPending(comment.id) ? ' pulsing' : '');
      badge.innerHTML = '<svg viewBox="0 0 24 24" width="10" height="10" fill="currentColor" style="vertical-align: -1px"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10"/></svg> live';
      headerLeft.appendChild(badge);
    }

    // GitHub-synced badge — surfaces comments imported from a GitHub PR
    // so re-sharers and reviewers can tell them apart from native crit
    // comments. The signal is GitHubID != 0 on the Comment struct, which
    // serializes as `github_id` (omitempty) on the JSON the API returns.
    // See issue #370.
    if (comment.github_id) {
      const ghBadge = document.createElement('span');
      ghBadge.className = 'github-badge';
      ghBadge.textContent = 'GitHub';
      ghBadge.title = 'Synced from GitHub';
      ghBadge.setAttribute('aria-label', 'Synced from GitHub');
      headerLeft.appendChild(ghBadge);
    }

    // suppressDrift: live mode passes this so legacy comments carrying
    // `drifted: true` from before the field was retired don't paint a badge.
    if (comment.drifted && !opts.suppressDrift) {
      wrapper.classList.add('outdated-comment');
      var driftedBadge = document.createElement('span');
      driftedBadge.className = 'outdated-badge';
      driftedBadge.textContent = 'Drifted';
      headerLeft.appendChild(driftedBadge);
    }

    var actions = document.createElement('div');
    actions.className = 'comment-actions';

    header.appendChild(headerLeft);
    header.appendChild(actions);

    var bodyEl = document.createElement('div');
    bodyEl.className = 'comment-body';
    bodyEl.innerHTML = commentMd
      ? commentMd.render(comment.body, filePath ? buildCommentEnv(comment, filePath) : undefined)
      : (comment.body || '');
    if (typeof deps.linkifyDom === 'function') deps.linkifyDom(bodyEl);

    card.appendChild(header);

    // Drifted anchor context — show original content that was commented on
    if (comment.drifted && comment.anchor && !opts.suppressDrift) {
      var driftedCtx = document.createElement('div');
      driftedCtx.className = 'drifted-context';

      var toggle = document.createElement('button');
      toggle.className = 'drifted-toggle';
      toggle.type = 'button';

      var chevron = document.createElement('span');
      chevron.className = 'drifted-chevron';
      chevron.innerHTML = '<svg viewBox="0 0 10 10" width="10" height="10" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="3.5,1.5 7,5 3.5,8.5"/></svg>';

      var toggleLabel = document.createElement('span');
      toggleLabel.className = 'drifted-toggle-label';
      toggleLabel.textContent = 'Referenced content at time of review';

      var anchorLines = comment.anchor.split('\n');
      var toggleMeta = document.createElement('span');
      toggleMeta.className = 'drifted-toggle-meta';
      toggleMeta.textContent = anchorLines.length === 1 ? '1 line' : anchorLines.length + ' lines';

      toggle.appendChild(chevron);
      toggle.appendChild(toggleLabel);
      toggle.appendChild(toggleMeta);

      var panelId = 'drifted-panel-' + comment.id;
      var dwrap = document.createElement('div');
      dwrap.className = 'drifted-panel-wrapper';
      var inner = document.createElement('div');
      inner.className = 'drifted-panel-inner';
      var panel = document.createElement('div');
      panel.className = 'drifted-panel';
      panel.id = panelId;

      var pre = document.createElement('pre');
      pre.className = 'drifted-anchor-text';
      var startLine = comment.start_line || 1;
      anchorLines.forEach(function (line, i) {
        var lineEl = document.createElement('span');
        lineEl.className = 'drifted-line';
        var numEl = document.createElement('span');
        numEl.className = 'drifted-line-number';
        numEl.textContent = String(startLine + i);
        var contentEl = document.createElement('span');
        contentEl.className = 'drifted-line-content';
        contentEl.textContent = line;
        lineEl.appendChild(numEl);
        lineEl.appendChild(contentEl);
        pre.appendChild(lineEl);
      });

      panel.appendChild(pre);
      inner.appendChild(panel);
      dwrap.appendChild(inner);

      toggle.setAttribute('aria-expanded', 'false');
      toggle.setAttribute('aria-controls', panelId);
      toggle.addEventListener('click', function () {
        var isExpanded = driftedCtx.classList.contains('expanded');
        driftedCtx.classList.toggle('expanded', !isExpanded);
        toggle.setAttribute('aria-expanded', String(!isExpanded));
      });

      driftedCtx.appendChild(toggle);
      driftedCtx.appendChild(dwrap);
      card.appendChild(driftedCtx);
    }

    card.appendChild(bodyEl);

    if (comment.replies && comment.replies.length > 0) {
      card.appendChild(renderReplyList(comment, filePath || '', opts.repliesExtraClass));
    }

    if (isPending(comment.id)) {
      var pending = document.createElement('div');
      pending.className = 'agent-pending-reply';
      pending.dataset.commentId = comment.id;
      pending.innerHTML =
        '<span class="agent-pending-author">@' + getAgentName() + '</span>' +
        '<span class="agent-pending-cursor">_</span>';
      card.appendChild(pending);
    }

    if (opts.showReplyInput) {
      card.appendChild(createReplyInput(comment.id, filePath || ''));
    }

    if (isPending(comment.id) || liveThreadFn(comment)) {
      wrapper.classList.add('live-thread');
    }
    if (isPending(comment.id)) {
      wrapper.classList.add('agent-pending');
    }

    wrapper.appendChild(card);
    return { wrapper: wrapper, card: card, actions: actions };
  }

  return {
    buildCommentCard: buildCommentCard,
  };
});
