// live-mode.panel-render.js — comments-panel rendering, filter pill,
// expand-all toggle, panel show/hide + unresolved badge, panel resize.
//
// All concerns scoped to the right-side comments panel live here. The
// installer + render functions are exposed as factories that close over a
// `deps` bundle supplied by live-mode.js (state, els, refresh helpers,
// shared utils). This keeps the module side-effect free until the
// controller wires it up.
'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.live = root.crit.live || {};
    root.crit.live.panelRender = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {

  // create — returns an object exposing render functions and an installer
  // suitable for live-mode.js's installer registry.
  //
  // deps:
  //   state          — window.crit.live state object
  //   els            — element cache (panelBody, commentsPanel)
  //   utils          — window.crit.liveUtils (groupCommentsByRoute)
  //   shared         — window.crit.shared (settings, indicators)
  //   refreshPanel   — () => void
  //   (resize math/DOM wiring now lives in shared.installSidebarResize, so
  //    panelHelpers is no longer required here)
  function create(deps) {
    deps = deps || {};
    var state = deps.state;
    var els = deps.els;
    var utils = deps.utils;
    var shared = deps.shared;
    var refreshPanel = deps.refreshPanel || function () {};

    // Build the deps bundle once per render — buildCommentCard wants a
    // markdown-it instance + a few helpers. Code-review's app.js wires these
    // to its own module-scoped state; live mode supplies its own bundle.
    var _liveCardDeps = null;
    function getCardDeps() {
      if (_liveCardDeps) return _liveCardDeps;
      var helpers = (window.crit && window.crit.commentCardHelpers) || {};
      var commentMd = null;
      try {
        if (typeof window.markdownit === 'function') {
          commentMd = window.markdownit({ html: false, linkify: true, breaks: true });
          commentMd.renderer.rules.image = function (tokens, idx, options, _env, self) {
            var token = tokens[idx];
            var srcIdx = token.attrIndex('src');
            if (srcIdx >= 0) {
              var src = token.attrs[srcIdx][1];
              if (!/^https?:\/\/|^data:|^\//.test(src) && /^attachments\//.test(src)) {
                token.attrs[srcIdx][1] = '/api/' + src;
              }
            }
            return self.renderToken(tokens, idx, options);
          };
        }
      } catch (_) {}
      _liveCardDeps = {
        commentMd: commentMd,
        formatTime: helpers.formatTime || function () { return ''; },
        authorColorIndex: helpers.authorColorIndex || function () { return 0; },
        getReviewRound: function () {
          return (state.session && state.session.review_round) || 0;
        },
        getCollapseOverride: function (id) {
          return state.liveCollapseOverrides.has(id)
            ? state.liveCollapseOverrides.get(id)
            : undefined;
        },
        setCollapseOverride: function (id, val) {
          state.liveCollapseOverrides.set(id, val);
        },
        iconChevron: '<svg viewBox="0 0 16 16" fill="currentColor" width="16" height="16"><path d="M12.78 5.22a.75.75 0 0 1 0 1.06l-4.25 4.25a.75.75 0 0 1-1.06 0L3.22 6.28a.75.75 0 0 1 1.06-1.06L8 8.94l3.72-3.72a.75.75 0 0 1 1.06 0Z"/></svg>',
        // Icon SVGs — kept byte-equivalent to code-review's ICON_* constants
        // in app.js so the live-mode action buttons inherit the exact same
        // visual treatment via the shared .comment-actions / .resolve-btn CSS.
        iconEdit: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17 3a2.85 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/><path d="m15 5 4 4"/></svg>',
        iconDelete: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/></svg>',
        iconResolve: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>',
        iconUnresolve: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 0 1 9-9 9 9 0 0 1 6.36 2.64M21 12a9 9 0 0 1-9 9 9 9 0 0 1-6.36-2.64"/><polyline points="21 3 21 8 16 8"/><polyline points="3 21 3 16 8 16"/></svg>',
        iconReply: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 17 4 12 9 7"/><path d="M20 18v-2a4 4 0 0 0-4-4H4"/></svg>',
      };
      return _liveCardDeps;
    }

    // Granular-update bookkeeping for renderCommentsPanel. Maps keyed by
    // commentId / route point at the live DOM nodes so subsequent renders can
    // diff (insert / move / replace / remove) instead of doing a full
    // panelBody rebuild. The full-rebuild path was visibly flickering, losing
    // scroll position, and — worst — stealing focus from any open composer
    // when an unrelated pin's reply submitted in parallel (because the
    // textarea's DOM node was thrown away on every comments-changed event).
    //
    // Mirrors the per-card upsert pattern used by code-review's panel render
    // in app.js, adapted to live-mode's group-by-route layout.
    var _cardEntries = new Map();   // id -> { wrapper, sig, route, isPin }
    var _groupEntries = new Map();  // route -> { group, cards }
    // Track the current empty/filtered-empty state node so we can swap it
    // for the populated tree (and back) without leaking either kind.
    var _emptyEl = null;

    function clearPanelBookkeeping() {
      _cardEntries.clear();
      _groupEntries.clear();
      _emptyEl = null;
    }

    function showEmptyState(msg, withHint) {
      if (!els.panelBody) return;
      // Tear down any populated tree before mounting the empty placeholder.
      while (els.panelBody.firstChild) els.panelBody.removeChild(els.panelBody.firstChild);
      clearPanelBookkeeping();
      var empty = document.createElement('div');
      empty.className = 'comments-panel-empty';
      if (withHint) {
        empty.innerHTML = msg + '<br>Switch to Pin mode and click an element to leave a comment.';
      } else {
        empty.textContent = msg;
      }
      els.panelBody.appendChild(empty);
      _emptyEl = empty;
    }

    function renderEmptyPanel() {
      showEmptyState('No pins yet.', true);
    }

    // Stable signature of every comment field that affects rendering. Two
    // comments with the same signature produce byte-identical DOM, so we can
    // safely reuse the existing wrapper instead of rebuilding it. Anything
    // that changes the rendered card MUST be reflected here.
    function commentSignature(c, isPin, reviewRound) {
      var anchor = c.dom_anchor || null;
      var anchorKey = anchor
        ? (anchor.pathname || '') + '|' +
          (anchor.accessible_name || '') + '|' +
          ((anchor.tag_chain && anchor.tag_chain.join('>')) || '') + '|' +
          (anchor.outer_html || '')
        : '';
      var replies = '';
      if (c.replies && c.replies.length) {
        for (var i = 0; i < c.replies.length; i++) {
          var r = c.replies[i] || {};
          replies += (r.id || '') + ':' + (r.body || '') + ':' + (r.author || '') + ':' + (r.created_at || '') + ':' + (r.updated_at || '') + '|';
        }
      }
      return [
        c.id || '',
        isPin ? '1' : '0',
        c.body || '',
        c.author || '',
        c.resolved ? '1' : '0',
        c.created_at || '',
        c.updated_at || '',
        c.review_round == null ? '' : c.review_round,
        reviewRound,
        c._replyOpen ? '1' : '0',
        c._replyDraft || '',
        c._editOpen ? '1' : '0',
        c._editDraft == null ? '' : c._editDraft,
        anchorKey,
        replies,
      ].join('\x1f');
    }

    function buildPinCard(c, cardDeps) {
      var rowMod = window.crit && window.crit.live && window.crit.live.row;
      if (rowMod && typeof rowMod.renderLivePinRow === 'function') {
        return rowMod.renderLivePinRow(c, cardDeps);
      }
      // Row module not loaded — fall through to buildCommentCard directly.
      var ccMod = window.crit && window.crit.commentCard;
      if (ccMod && typeof ccMod.buildCommentCard === 'function') {
        var pathname = (c.dom_anchor && c.dom_anchor.pathname) || '/';
        var parts = ccMod.buildCommentCard(c, pathname, {
          wrapperClass: 'comment-block panel-comment-block crit-live-comment-row-wrap',
          deps: cardDeps,
        });
        return parts.wrapper;
      }
      return buildFallbackCard(c, (c.dom_anchor && c.dom_anchor.pathname) || '/');
    }

    function buildFallbackCard(c, route) {
      var body = (c.body || '').replace(/\s+/g, ' ').trim();
      var excerpt = body.length > 200 ? body.slice(0, 200) + '…' : body;
      var fb = document.createElement('div');
      fb.className = 'comment-card';
      fb.dataset.liveRoute = route;
      fb.dataset.id = String(c.id || '');
      fb.tabIndex = 0;
      fb.setAttribute('role', 'button');
      if (c.resolved) fb.dataset.resolved = 'true';
      var fbBody = document.createElement('div');
      fbBody.className = 'comment-card-body';
      fbBody.textContent = excerpt;
      fb.appendChild(fbBody);
      var meta = document.createElement('div');
      meta.className = 'comment-card-meta';
      meta.style.cssText = 'font-size:11px;color:var(--crit-editor-fg-muted);display:flex;gap:8px';
      var who = document.createElement('span');
      who.textContent = c.author || '';
      meta.appendChild(who);
      if (c.resolved) {
        var resolvedTag = document.createElement('span');
        resolvedTag.style.color = 'var(--crit-green)';
        resolvedTag.textContent = 'resolved';
        meta.appendChild(resolvedTag);
      }
      fb.appendChild(meta);
      return fb;
    }

    function ensureGroup(route, insertBeforeNode) {
      var entry = _groupEntries.get(route);
      if (entry) return entry;
      var group = document.createElement('div');
      group.className = 'comments-panel-file-group';
      group.dataset.liveRoute = route;
      var name = document.createElement('div');
      name.className = 'comments-panel-file-name';
      name.textContent = route;
      group.appendChild(name);
      var cards = document.createElement('div');
      cards.className = 'comments-panel-file-cards';
      group.appendChild(cards);
      els.panelBody.insertBefore(group, insertBeforeNode || null);
      entry = { group: group, cards: cards };
      _groupEntries.set(route, entry);
      return entry;
    }

    function renderCommentsPanel() {
      if (!els.panelBody) return;
      var groups = utils.groupCommentsByRoute(state.comments);
      if (groups.size === 0) { renderEmptyPanel(); return; }

      var cardDeps = getCardDeps();
      var reviewRound = (state.session && state.session.review_round) || 0;
      var filter = state.liveFilter || 'all';

      // Compute the desired layout: ordered list of routes, each with an
      // ordered list of visible comments.
      var desiredRoutes = [];
      var desiredByRoute = new Map();
      var desiredIds = new Set();
      groups.forEach(function (rows, route) {
        var visibleRows = rows.filter(function (c) {
          if (filter === 'open') return !c.resolved;
          if (filter === 'resolved') return !!c.resolved;
          return true;
        });
        if (!visibleRows.length) return;
        desiredRoutes.push(route);
        desiredByRoute.set(route, visibleRows);
        for (var i = 0; i < visibleRows.length; i++) {
          desiredIds.add(String(visibleRows[i].id || ''));
        }
      });

      if (desiredRoutes.length === 0) {
        var msg = filter === 'open' ? 'No open pins.' :
                  filter === 'resolved' ? 'No resolved pins.' : 'No pins yet.';
        showEmptyState(msg, false);
        return;
      }

      // If we were showing an empty placeholder, drop it before mounting
      // anything — we're about to populate.
      if (_emptyEl && _emptyEl.parentNode === els.panelBody) {
        els.panelBody.removeChild(_emptyEl);
        _emptyEl = null;
      }

      // Preserve scroll position across the diff. We do all DOM work in a
      // single pass without intermediate measurements, and only assign
      // scrollTop at the end if it drifted.
      var savedScroll = els.panelBody.scrollTop;

      // 1. Drop cards whose comment is no longer present (or was filtered
      // out). This also removes them from their group's `cards` container.
      var toDelete = [];
      _cardEntries.forEach(function (entry, id) {
        if (!desiredIds.has(id)) toDelete.push(id);
      });
      for (var d = 0; d < toDelete.length; d++) {
        var dEntry = _cardEntries.get(toDelete[d]);
        if (dEntry && dEntry.wrapper && dEntry.wrapper.parentNode) {
          dEntry.wrapper.parentNode.removeChild(dEntry.wrapper);
        }
        _cardEntries.delete(toDelete[d]);
      }

      // 2. Drop groups whose route is no longer desired. We handle this
      // before placing groups so the next pass gets a clean ordering.
      var groupsToDelete = [];
      _groupEntries.forEach(function (gEntry, route) {
        if (!desiredByRoute.has(route)) groupsToDelete.push(route);
      });
      for (var gd = 0; gd < groupsToDelete.length; gd++) {
        var gEntry2 = _groupEntries.get(groupsToDelete[gd]);
        if (gEntry2 && gEntry2.group && gEntry2.group.parentNode) {
          gEntry2.group.parentNode.removeChild(gEntry2.group);
        }
        _groupEntries.delete(groupsToDelete[gd]);
      }

      // 3. Walk desired routes in order, ensuring each group exists at the
      // correct position in panelBody, then upsert its cards.
      var nextGroupNode = els.panelBody.firstChild;
      for (var r = 0; r < desiredRoutes.length; r++) {
        var route = desiredRoutes[r];
        var gEntry = _groupEntries.get(route);
        if (!gEntry) {
          gEntry = ensureGroup(route, nextGroupNode);
        } else if (gEntry.group !== nextGroupNode) {
          // Re-order: move the existing group into position. insertBefore
          // is a single mutation and preserves all listeners + state inside
          // the group.
          els.panelBody.insertBefore(gEntry.group, nextGroupNode || null);
        }
        nextGroupNode = gEntry.group.nextSibling;

        // Upsert cards within this group. We compute the desired wrapper
        // for each row first (reusing existing nodes when their signature
        // matches), then place them in order using insertBefore. After the
        // loop, anything still in the container that we didn't position is
        // an orphan and gets removed.
        var rows = desiredByRoute.get(route);
        var cardsContainer = gEntry.cards;
        var desiredWrappers = new Array(rows.length);
        for (var i2 = 0; i2 < rows.length; i2++) {
          var c = rows[i2];
          var idStr = String(c.id || '');
          var isPin = !!(c.dom_anchor);
          var sig = commentSignature(c, isPin, reviewRound);
          var existing = _cardEntries.get(idStr);
          var wrapper;
          if (existing && existing.sig === sig && existing.route === route && existing.isPin === isPin) {
            // Reuse the live DOM node — the most important branch for the
            // focus/scroll preservation contract. Any focused element
            // inside this wrapper stays focused, since we never detach the
            // node from the document.
            wrapper = existing.wrapper;
          } else {
            wrapper = buildPinCard(c, cardDeps);
            _cardEntries.set(idStr, {
              wrapper: wrapper, sig: sig, route: route, isPin: isPin,
            });
          }
          desiredWrappers[i2] = wrapper;
        }

        // Position each desired wrapper in order. insertBefore on a node
        // already at the correct position is essentially a no-op (the
        // browser short-circuits), so reused-in-place rows do not move.
        var anchorNode = cardsContainer.firstChild;
        for (var i3 = 0; i3 < desiredWrappers.length; i3++) {
          var w = desiredWrappers[i3];
          if (w !== anchorNode) {
            cardsContainer.insertBefore(w, anchorNode || null);
          } else {
            anchorNode = anchorNode.nextSibling;
          }
        }
        // 4. Trim any leftover children in this group's cards container
        // (cards that belonged to a different desired set or stale moves).
        while (anchorNode) {
          var stale = anchorNode;
          anchorNode = anchorNode.nextSibling;
          cardsContainer.removeChild(stale);
        }
      }

      // 5. Trim orphan nodes after the last desired group (e.g. an empty-
      // state placeholder that snuck in via someone else mutating the DOM).
      while (nextGroupNode) {
        var staleG = nextGroupNode;
        nextGroupNode = nextGroupNode.nextSibling;
        if (!_groupEntries.has(staleG.dataset && staleG.dataset.liveRoute)) {
          els.panelBody.removeChild(staleG);
        }
      }

      if (els.panelBody.scrollTop !== savedScroll) {
        els.panelBody.scrollTop = savedScroll;
      }
      if (state.liveExpandAll) applyExpandAllToRenderedCards(true);
    }

    function applyExpandAllToRenderedCards(expand) {
      if (!els.panelBody) return;
      var cards = els.panelBody.querySelectorAll('.comment-card');
      cards.forEach(function (card) {
        // Mirror buildCommentCard's collapse model: it persists per-id via
        // liveCollapseOverrides. Toggle the override AND any rendered
        // collapsed class so the visible state matches immediately.
        var id = card.dataset && card.dataset.id;
        if (id) state.liveCollapseOverrides.set(id, !expand);
        card.classList.toggle('collapsed', !expand);
        var body = card.querySelector('.comment-card-body, .crit-comment-card-body');
        if (body) body.style.display = expand ? '' : 'none';
      });
    }

    function applyCommentsPanelOpen(open) {
      var panel = els.commentsPanel;
      if (!panel) return;
      if (open) panel.classList.remove('comments-panel-hidden');
      else panel.classList.add('comments-panel-hidden');
      state.commentsPanelOpen = !!open;
    }

    function updateUnresolvedBadge() {
      var all = state.comments || [];
      var totalCount = all.length;
      var openCount = 0;
      var resolvedCount = 0;
      for (var i = 0; i < all.length; i++) {
        if (all[i] && all[i].resolved) resolvedCount++;
        else if (all[i]) openCount++;
      }
      // Panel-header badge mirrors code-review (total count).
      var badge = document.getElementById('commentsPanelCountBadge');
      if (badge) badge.textContent = String(totalCount);
      // Navbar pill: shared with code-review so the tooltip + resolved-state
      // class never drift between modes.
      if (shared && shared.updateCommentCountIndicator) {
        shared.updateCommentCountIndicator({ totalCount: totalCount, openCount: openCount });
      }
      // Filter pill per-button counts.
      var pillBtns = document.querySelectorAll('#commentsFilterPill .toggle-btn');
      pillBtns.forEach(function (btn) {
        var f = btn.dataset.filter;
        var countEl = btn.querySelector('.filter-count');
        if (!countEl) return;
        if (f === 'all') countEl.textContent = totalCount;
        else if (f === 'open') countEl.textContent = openCount;
        else if (f === 'resolved') countEl.textContent = resolvedCount;
      });
    }

    // panelRefresh — the function live-mode.js registers as the panel's
    // top-level refresh entry point.
    function panelRefresh() {
      if (!state.comments || state.comments.length === 0) {
        renderEmptyPanel();
        return;
      }
      renderCommentsPanel();
    }

    function installFilterPillAndExpandAll() {
      var pill = document.getElementById('commentsFilterPill');
      if (pill) {
        var activate = function (btn, focusBtn) {
          if (!btn) return;
          state.liveFilter = btn.dataset.filter || 'all';
          pill.querySelectorAll('.toggle-btn').forEach(function (b) {
            var active = b === btn;
            b.classList.toggle('active', active);
            b.setAttribute('aria-checked', active ? 'true' : 'false');
            b.setAttribute('tabindex', active ? '0' : '-1');
          });
          if (focusBtn) btn.focus();
          refreshPanel();
        };
        pill.addEventListener('click', function (e) {
          var btn = e.target.closest && e.target.closest('.toggle-btn');
          if (!btn) return;
          activate(btn, false);
        });
        pill.addEventListener('keydown', function (e) {
          var btns = Array.from(pill.querySelectorAll('.toggle-btn'));
          var idx = btns.findIndex(function (b) { return b === document.activeElement; });
          if (idx === -1) return;
          var next;
          if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next = (idx + 1) % btns.length;
          else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') next = (idx - 1 + btns.length) % btns.length;
          else if (e.key === 'Home') next = 0;
          else if (e.key === 'End') next = btns.length - 1;
          else return;
          e.preventDefault();
          activate(btns[next], true);
        });
      }

      var expandBtn = document.getElementById('commentsPanelExpandAll');
      if (expandBtn) {
        expandBtn.addEventListener('click', function () {
          state.liveExpandAll = !state.liveExpandAll;
          applyExpandAllToRenderedCards(state.liveExpandAll);
          // Mirror app.js#updateExpandAllLabel — the visible label flips to
          // "Collapse all" when any card is expanded, in addition to the
          // aria-pressed state. Without this the button reads "Expand all"
          // even after the user has expanded everything.
          expandBtn.textContent = state.liveExpandAll ? 'Collapse all' : 'Expand all';
          expandBtn.setAttribute('aria-pressed', state.liveExpandAll ? 'true' : 'false');
          expandBtn.title = state.liveExpandAll ? 'Collapse all' : 'Expand all';
        });
      }
    }

    // Comments panel toggle + unresolved count badge. Reuses the navbar's
    // #commentCount button and the #commentsPanelCountBadge inside the panel
    // header. Persistence under crit-settings.live_commentsPanelOpen so
    // live mode keeps its own preference distinct from code review.
    function installCommentsPanelToggle() {
      var btn = document.getElementById('commentCount');
      var closeBtn = document.querySelector('.comments-panel-close');
      var openSetting = (shared && shared.getSetting)
        ? shared.getSetting('live_commentsPanelOpen', true)
        : true;
      applyCommentsPanelOpen(!!openSetting);

      function toggle() {
        var next = !state.commentsPanelOpen;
        applyCommentsPanelOpen(next);
        if (shared && shared.setSetting) {
          try { shared.setSetting('live_commentsPanelOpen', next); } catch (_) {}
        }
      }
      if (btn) {
        btn.addEventListener('click', function () { toggle(); });
        btn.addEventListener('keydown', function (e) {
          if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggle(); }
        });
      }
      if (closeBtn) {
        closeBtn.addEventListener('click', function () {
          applyCommentsPanelOpen(false);
          if (shared && shared.setSetting) {
            try { shared.setSetting('live_commentsPanelOpen', false); } catch (_) {}
          }
        });
      }
      updateUnresolvedBadge();
    }

    // Scroll to pinned element and flash its marker badge when a comment
    // card is clicked in the panel. keep-highlight scrolls into view +
    // adds a transient highlight; clear-highlight removes it after 1s.
    // flash-marker pulses the badge overlay (1.5s, agent-managed).
    var _highlightTimer = null;
    function scrollAndFlashPin(comment) {
      if (!comment || !comment.id) return;
      if (!state || !state.postToAgent) return;
      if (_highlightTimer) { clearTimeout(_highlightTimer); _highlightTimer = null; }
      var anchor = comment.dom_anchor || comment.domAnchor;
      if (anchor && anchor.css_selector) {
        state.postToAgent({ type: 'keep-highlight', selector: anchor.css_selector });
        _highlightTimer = setTimeout(function () {
          state.postToAgent({ type: 'clear-highlight' });
          _highlightTimer = null;
        }, 1000);
      }
      state.postToAgent({ type: 'flash-marker', pin_id: comment.id });
    }

    var _cardClickInstalled = false;
    function installPanelCardRendererClick() {
      if (!els.panelBody || _cardClickInstalled) return;
      _cardClickInstalled = true;
      els.panelBody.addEventListener('click', function (e) {
        // Don't interfere with interactive controls
        if (e.target.closest && e.target.closest('button, a, input, textarea')) return;
        var card = e.target.closest && e.target.closest('.comment-card[data-id]');
        if (!card) return;
        var id = card.dataset.id;
        if (!id || !state.comments) return;
        var comment = state.comments.find(function (c) { return String(c.id) === id; });
        if (comment) scrollAndFlashPin(comment);
      });
    }

    // Resizable side panel. Reuses #commentsPanelResizer. NO clamping
    // against viewport preset width — the user gets the width they ask
    // for. Persisted to crit-settings.live_commentsPanelWidth (separate
    // from code review's commentsPanelWidth so the two modes don't fight).
    //
    // Delegates to shared.installSidebarResize so live-mode picks up the
    // body.sidebar-resizing cursor lock (no flicker when the pointer leaves
    // the strip) and keyboard a11y for free, in lockstep with code-review.
    function installCommentsPanelResize() {
      var handle = document.getElementById('commentsPanelResizer');
      var panel = els.commentsPanel;
      if (!handle || !panel) return;
      if (!shared || typeof shared.installSidebarResize !== 'function') return;
      shared.installSidebarResize(handle, panel, {
        settingKey: 'live_commentsPanelWidth',
        min: 200,
        edge: 'left',
      });
    }

    return {
      // Render functions
      renderEmptyPanel: renderEmptyPanel,
      renderCommentsPanel: renderCommentsPanel,
      applyExpandAllToRenderedCards: applyExpandAllToRenderedCards,
      applyCommentsPanelOpen: applyCommentsPanelOpen,
      updateUnresolvedBadge: updateUnresolvedBadge,
      panelRefresh: panelRefresh,
      // Installers
      installFilterPillAndExpandAll: installFilterPillAndExpandAll,
      installCommentsPanelToggle: installCommentsPanelToggle,
      installCommentsPanelResize: installCommentsPanelResize,
      installPanelCardRendererClick: installPanelCardRendererClick,
    };
  }

  return { create: create };
});
