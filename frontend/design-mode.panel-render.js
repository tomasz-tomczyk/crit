// design-mode.panel-render.js — comments-panel rendering, filter pill,
// expand-all toggle, panel show/hide + unresolved badge, panel resize.
//
// All concerns scoped to the right-side comments panel live here. The
// installer + render functions are exposed as factories that close over a
// `deps` bundle supplied by design-mode.js (state, els, refresh helpers,
// shared utils). This keeps the module side-effect free until the
// controller wires it up.
'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.design = root.crit.design || {};
    root.crit.design.panelRender = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {

  // create — returns an object exposing render functions and an installer
  // suitable for design-mode.js's installer registry.
  //
  // deps:
  //   state          — window.crit.design state object
  //   els            — element cache (panelBody, commentsPanel)
  //   utils          — window.crit.designUtils (groupCommentsByRoute)
  //   shared         — window.crit.shared (settings, indicators)
  //   refreshPanel   — () => void
  //   panelHelpers   — window.crit.design.panel (computeResizeWidth)
  function create(deps) {
    deps = deps || {};
    var state = deps.state;
    var els = deps.els;
    var utils = deps.utils;
    var shared = deps.shared;
    var refreshPanel = deps.refreshPanel || function () {};
    var panelHelpers = deps.panelHelpers || null;

    // Build the deps bundle once per render — buildCommentCard wants a
    // markdown-it instance + a few helpers. Code-review's app.js wires these
    // to its own module-scoped state; design mode supplies its own bundle.
    var _designCardDeps = null;
    function getCardDeps() {
      if (_designCardDeps) return _designCardDeps;
      var helpers = (window.crit && window.crit.commentCardHelpers) || {};
      var commentMd = null;
      try {
        if (typeof window.markdownit === 'function') {
          commentMd = window.markdownit({ html: false, linkify: true, breaks: true });
        }
      } catch (_) {}
      _designCardDeps = {
        commentMd: commentMd,
        formatTime: helpers.formatTime || function () { return ''; },
        authorColorIndex: helpers.authorColorIndex || function () { return 0; },
        getReviewRound: function () {
          return (state.session && state.session.review_round) || 0;
        },
        getCollapseOverride: function (id) {
          return state.designCollapseOverrides.has(id)
            ? state.designCollapseOverrides.get(id)
            : undefined;
        },
        setCollapseOverride: function (id, val) {
          state.designCollapseOverrides.set(id, val);
        },
        iconChevron: '<svg viewBox="0 0 16 16" fill="currentColor" width="16" height="16"><path d="M12.78 5.22a.75.75 0 0 1 0 1.06l-4.25 4.25a.75.75 0 0 1-1.06 0L3.22 6.28a.75.75 0 0 1 1.06-1.06L8 8.94l3.72-3.72a.75.75 0 0 1 1.06 0Z"/></svg>',
        // Icon SVGs — kept byte-equivalent to code-review's ICON_* constants
        // in app.js so the design-mode action buttons inherit the exact same
        // visual treatment via the shared .comment-actions / .resolve-btn CSS.
        iconEdit: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17 3a2.85 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/><path d="m15 5 4 4"/></svg>',
        iconDelete: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/></svg>',
        iconResolve: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>',
        iconUnresolve: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 0 1 9-9 9 9 0 0 1 6.36 2.64M21 12a9 9 0 0 1-9 9 9 9 0 0 1-6.36-2.64"/><polyline points="21 3 21 8 16 8"/><polyline points="3 21 3 16 8 16"/></svg>',
        iconReply: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 17 4 12 9 7"/><path d="M20 18v-2a4 4 0 0 0-4-4H4"/></svg>',
      };
      return _designCardDeps;
    }

    function renderEmptyPanel() {
      if (!els.panelBody) return;
      els.panelBody.innerHTML =
        '<div class="comments-panel-empty" style="padding:32px 16px;text-align:center;color:var(--crit-editor-fg-muted);font-size:13px;line-height:1.5">' +
        'No pins yet.<br>' +
        'Switch to Pin mode and click an element to leave a comment.' +
        '</div>';
    }

    function renderCommentsPanel() {
      if (!els.panelBody) return;
      var groups = utils.groupCommentsByRoute(state.comments);
      if (groups.size === 0) { renderEmptyPanel(); return; }

      // Build the panel as a DOM tree so design pins can mount the shared
      // buildCommentCard (DOM-composed). Non-pin comments still render as a
      // light-weight "comment-card" for click-to-navigate routing.
      var rowMod = window.crit && window.crit.design && window.crit.design.row;
      var cardDeps = getCardDeps();

      var filter = state.designFilter || 'all';
      var frag = document.createDocumentFragment();
      var anyRendered = false;
      groups.forEach(function (rows, route) {
        var visibleRows = rows.filter(function (c) {
          if (filter === 'open') return !c.resolved;
          if (filter === 'resolved') return !!c.resolved;
          return true;
        });
        if (!visibleRows.length) return;
        anyRendered = true;
        var group = document.createElement('div');
        group.className = 'comments-panel-file-group';
        var name = document.createElement('div');
        name.className = 'comments-panel-file-name';
        name.textContent = route;
        group.appendChild(name);
        var cards = document.createElement('div');
        cards.className = 'comments-panel-file-cards';

        visibleRows.forEach(function (c) {
          if (c.dom_anchor && rowMod && typeof rowMod.renderDesignPinRow === 'function') {
            cards.appendChild(rowMod.renderDesignPinRow(c, cardDeps));
            return;
          }
          // Fallback for non-pin (e.g. legacy review-level) comments — light
          // navigation card.
          var body = (c.body || '').replace(/\s+/g, ' ').trim();
          var excerpt = body.length > 200 ? body.slice(0, 200) + '…' : body;
          var fb = document.createElement('div');
          fb.className = 'comment-card';
          fb.dataset.designRoute = route;
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
          cards.appendChild(fb);
        });

        group.appendChild(cards);
        frag.appendChild(group);
      });

      els.panelBody.innerHTML = '';
      if (!anyRendered) {
        // All comments hidden by current filter.
        var msg = filter === 'open' ? 'No open pins.' :
                  filter === 'resolved' ? 'No resolved pins.' : 'No pins yet.';
        els.panelBody.innerHTML =
          '<div class="comments-panel-empty" style="padding:32px 16px;text-align:center;color:var(--crit-editor-fg-muted);font-size:13px;line-height:1.5">' +
          msg + '</div>';
        return;
      }
      els.panelBody.appendChild(frag);
      if (state.designExpandAll) applyExpandAllToRenderedCards(true);
    }

    function applyExpandAllToRenderedCards(expand) {
      if (!els.panelBody) return;
      var cards = els.panelBody.querySelectorAll('.comment-card');
      cards.forEach(function (card) {
        // Mirror buildCommentCard's collapse model: it persists per-id via
        // designCollapseOverrides. Toggle the override AND any rendered
        // collapsed class so the visible state matches immediately.
        var id = card.dataset && card.dataset.id;
        if (id) state.designCollapseOverrides.set(id, !expand);
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

    // panelRefresh — the function design-mode.js registers as the panel's
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
          state.designFilter = btn.dataset.filter || 'all';
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
          state.designExpandAll = !state.designExpandAll;
          applyExpandAllToRenderedCards(state.designExpandAll);
          // Mirror app.js#updateExpandAllLabel — the visible label flips to
          // "Collapse all" when any card is expanded, in addition to the
          // aria-pressed state. Without this the button reads "Expand all"
          // even after the user has expanded everything.
          expandBtn.textContent = state.designExpandAll ? 'Collapse all' : 'Expand all';
          expandBtn.setAttribute('aria-pressed', state.designExpandAll ? 'true' : 'false');
          expandBtn.title = state.designExpandAll ? 'Collapse all' : 'Expand all';
        });
      }
    }

    // Comments panel toggle + unresolved count badge. Reuses the navbar's
    // #commentCount button and the #commentsPanelCountBadge inside the panel
    // header. Persistence under crit-settings.design_commentsPanelOpen so
    // design mode keeps its own preference distinct from code review.
    function installCommentsPanelToggle() {
      var btn = document.getElementById('commentCount');
      var closeBtn = document.querySelector('.comments-panel-close');
      var openSetting = (shared && shared.getSetting)
        ? shared.getSetting('design_commentsPanelOpen', true)
        : true;
      applyCommentsPanelOpen(!!openSetting);

      function toggle() {
        var next = !state.commentsPanelOpen;
        applyCommentsPanelOpen(next);
        if (shared && shared.setSetting) {
          try { shared.setSetting('design_commentsPanelOpen', next); } catch (_) {}
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
            try { shared.setSetting('design_commentsPanelOpen', false); } catch (_) {}
          }
        });
      }
      updateUnresolvedBadge();
    }

    // Resizable side panel. Reuses #commentsPanelResizer. NO clamping
    // against viewport preset width — the user gets the width they ask
    // for. Persisted to crit-settings.design_commentsPanelWidth (separate
    // from code review's commentsPanelWidth so the two modes don't fight).
    function installCommentsPanelResize() {
      var handle = document.getElementById('commentsPanelResizer');
      var panel = els.commentsPanel;
      if (!handle || !panel || !panelHelpers) return;

      // Apply persisted width on boot.
      var saved = (shared && shared.getSetting)
        ? shared.getSetting('design_commentsPanelWidth', null)
        : null;
      if (typeof saved === 'number' && saved > 0) {
        panel.style.width = saved + 'px';
      }

      var dragging = false;
      var activePointerId = null;
      var startX = 0;
      var startW = 0;

      function onMove(e) {
        if (!dragging || e.pointerId !== activePointerId) return;
        var w = panelHelpers.computeResizeWidth(startW, startX, e.clientX, 200);
        panel.style.width = w + 'px';
      }
      function onUp(e) {
        if (!dragging || e.pointerId !== activePointerId) return;
        dragging = false;
        document.body.style.userSelect = '';
        try { handle.releasePointerCapture(activePointerId); } catch (_) {}
        handle.removeEventListener('pointermove', onMove);
        handle.removeEventListener('pointerup', onUp);
        handle.removeEventListener('pointercancel', onUp);
        activePointerId = null;
        var finalW = panel.getBoundingClientRect().width;
        if (shared && shared.setSetting) {
          try { shared.setSetting('design_commentsPanelWidth', Math.round(finalW)); } catch (_) {}
        }
      }
      handle.addEventListener('pointerdown', function (e) {
        e.preventDefault();
        dragging = true;
        activePointerId = e.pointerId;
        startX = e.clientX;
        startW = panel.getBoundingClientRect().width;
        document.body.style.userSelect = 'none';
        try { handle.setPointerCapture(e.pointerId); } catch (_) {}
        handle.addEventListener('pointermove', onMove);
        handle.addEventListener('pointerup', onUp);
        handle.addEventListener('pointercancel', onUp);
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
    };
  }

  return { create: create };
});
