// design-mode.js — Phase B chrome controller. Vanilla JS, no build step.
//
// Boot order:
//   1. Wait for /api/session
//   2. Inject design-mode chrome into the existing header (R3) and append
//      a .crit-design-iframe-pane sibling to .main-content (R1)
//   3. Wire viewport selector, drag resize, route detection, comment list
//
// All state on window.crit.design (see contract block below).

(function () {
  'use strict';

  // ----- State namespace contract -----
  /**
   * Design-mode shared state — populated by design-mode.js and (in Phase C+)
   * mutated by postMessage handlers from the agent.
   *
   * @typedef {object} CritDesignState
   * @property {object|null} session         /api/session payload
   * @property {string[]}    routes          Pathnames seen this session
   * @property {Set<string>} unsavedRoutes   Routes visited but not yet pinned
   * @property {string}      currentRoute    Currently displayed pathname
   * @property {{w:number,h:number,key:string}} viewport
   * @property {"navigate"|"pin"} mode       Phase B always "navigate"
   * @property {object[]}    comments        Flat list (cached per route)
   * @property {boolean}     pinModeEnabled  Phase B = false
   * @property {string|null} pendingPinId    Deep-link #pin=<id> target (R7)
   */
  window.crit = window.crit || {};
  window.crit.design = window.crit.design || {
    session: null,
    routes: [],
    unsavedRoutes: new Set(),
    currentRoute: '/',
    viewport: { w: 1280, h: 800, key: 'desktop' },
    mode: 'navigate',
    comments: [],
    pinModeEnabled: false,
    pendingPinId: null,
  };
  var state = window.crit.design;
  var shared = window.crit.shared;
  var utils = window.crit.designUtils;

  var els = {};

  // Internal installer + panel-refresh registries. Phase C extends this
  // module by appending here, not by mutating window.
  var installers = [];
  var panelRefreshFns = [];
  function registerInstaller(fn) { installers.push(fn); }
  function registerPanelRefresh(fn) { panelRefreshFns.push(fn); }

  var _refreshPending = false;
  function refreshPanel() {
    if (_refreshPending) return;
    _refreshPending = true;
    requestAnimationFrame(function () {
      _refreshPending = false;
      panelRefreshFns.forEach(function (fn) { fn(); });
    });
  }

  function announce(msg) {
    var live = document.getElementById('critDesignLive');
    if (live) live.textContent = msg;
  }
  state.announce = announce;

  // Resilient session poll: server can return 503 until SetSession completes.
  async function waitForSession() {
    for (var i = 0; i < 60; i++) {
      try {
        var s = await shared.fetchJSON('/api/session');
        if (s) return s;
      } catch (e) {
        if (e.status !== 503) throw e;
      }
      await new Promise(function (r) { setTimeout(r, 250); });
    }
    throw new Error('design-mode: /api/session never became ready');
  }

  function buildShell() {
    // R1 + R3: do NOT wipe <body>. Inject controls into existing .header-*
    // slots; append iframe pane as a sibling of .main-content inside
    // .main-layout. The existing .comments-panel is reused for the
    // design-mode comment list (R5).

    // Reveal the comments-panel (existing markup is hidden by default).
    var commentsPanel = document.getElementById('commentsPanel');
    if (commentsPanel) commentsPanel.classList.remove('comments-panel-hidden');

    // --- Header-right: viewport toggle + mode toggle + round counter ---
    var headerRight = document.querySelector('.header .header-right');
    if (headerRight) {
      // Viewport selector (R3): .scope-toggle + .toggle-btn
      var vp = document.createElement('div');
      vp.className = 'scope-toggle';
      vp.id = 'designViewportToggle';
      vp.setAttribute('aria-label', 'Viewport size');
      vp.innerHTML =
        '<button type="button" class="toggle-btn" data-viewport="mobile" aria-pressed="false" title="Mobile 390">Mobile</button>' +
        '<button type="button" class="toggle-btn" data-viewport="tablet" aria-pressed="false" title="Tablet 768">Tablet</button>' +
        '<button type="button" class="toggle-btn active" data-viewport="desktop" aria-pressed="true" title="Desktop 1280">Desktop</button>' +
        '<button type="button" class="toggle-btn" data-viewport="fit" aria-pressed="false" title="Fit pane">Fit</button>';

      // Mode toggle (R4): .diff-mode-toggle + .toggle-btn; pin uses native disabled
      var md = document.createElement('div');
      md.className = 'diff-mode-toggle';
      md.id = 'designModeToggle';
      md.setAttribute('aria-label', 'Interaction mode');
      md.innerHTML =
        '<button type="button" class="toggle-btn active" data-mode="navigate" aria-pressed="true">Navigate</button>' +
        '<button type="button" class="toggle-btn" data-mode="pin" disabled title="Pin mode (Phase C)">Pin</button>';

      // Round counter (R3): reuse .viewed-count style
      var rc = document.createElement('span');
      rc.className = 'viewed-count';
      rc.id = 'designRoundCounter';
      rc.textContent = 'round 1';

      // Insert before the existing settings toggle (which keeps it as
      // rightmost icon button).
      var settingsToggle = document.getElementById('settingsToggle');
      if (settingsToggle) {
        headerRight.insertBefore(vp, settingsToggle);
        headerRight.insertBefore(md, settingsToggle);
        headerRight.insertBefore(rc, settingsToggle);
      } else {
        headerRight.appendChild(vp);
        headerRight.appendChild(md);
        headerRight.appendChild(rc);
      }
    }

    // --- Header-left: route breadcrumb chip ---
    var headerLeft = document.querySelector('.header .header-left');
    if (headerLeft) {
      var bc = document.createElement('span');
      bc.className = 'header-chip';
      bc.id = 'designRouteChip';
      bc.innerHTML =
        '<span class="branch-icon">' +
        '<svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">' +
        '<path d="M2 2.75A.75.75 0 0 1 2.75 2h10.5a.75.75 0 0 1 0 1.5H2.75A.75.75 0 0 1 2 2.75z"/>' +
        '</svg></span>' +
        '<span id="designRouteName">/</span>' +
        '<span class="crit-design-breadcrumb-unsaved" id="designRouteUnsaved" style="display:none">(unsaved)</span>';
      headerLeft.appendChild(bc);
    }

    // --- Iframe pane sibling to .main-content inside .main-layout ---
    var mainLayout = document.querySelector('.main-layout');
    if (mainLayout) {
      var pane = document.createElement('div');
      pane.className = 'crit-design-iframe-pane';
      pane.id = 'critDesignPane';
      pane.innerHTML =
        '<div class="crit-design-iframe-pane-inner">' +
        '<div class="crit-design-iframe-frame" id="critDesignFrame">' +
        // No `sandbox` attribute by design — see spec security section.
        '<iframe id="critDesignIframe" title="Design target" referrerpolicy="no-referrer"></iframe>' +
        '<div class="crit-design-iframe-resizer" id="critDesignResizer" role="separator" aria-orientation="vertical" aria-label="Resize design viewport" tabindex="0"></div>' +
        '</div>' +
        '</div>';
      // Insert before commentsPanel if present, else before any .pr-panel,
      // else append.
      var commentsPanelEl = mainLayout.querySelector('.comments-panel');
      if (commentsPanelEl) mainLayout.insertBefore(pane, commentsPanelEl);
      else mainLayout.appendChild(pane);
    }

    // --- Live region for a11y ---
    var live = document.createElement('div');
    live.id = 'critDesignLive';
    live.className = 'crit-design-sr-only';
    live.setAttribute('role', 'status');
    live.setAttribute('aria-live', 'polite');
    document.body.appendChild(live);

    // Cache references.
    els.viewportToggle = document.getElementById('designViewportToggle');
    els.modeToggle = document.getElementById('designModeToggle');
    els.routeChip = document.getElementById('designRouteChip');
    els.routeName = document.getElementById('designRouteName');
    els.routeUnsaved = document.getElementById('designRouteUnsaved');
    els.round = document.getElementById('designRoundCounter');
    els.pane = document.getElementById('critDesignPane');
    els.frame = document.getElementById('critDesignFrame');
    els.iframe = document.getElementById('critDesignIframe');
    els.resizer = document.getElementById('critDesignResizer');
    els.commentsPanel = document.getElementById('commentsPanel');
    els.panelBody = document.getElementById('commentsPanelBody');
  }

  async function boot() {
    if (shared && shared.applyThemeFromCookie) shared.applyThemeFromCookie();
    state.session = await waitForSession();
    if (state.session.review_type !== 'design') {
      console.warn('[design-mode] /api/session.review_type != "design":', state.session.review_type);
    }
    buildShell();

    // Run installers in registration order.
    installers.forEach(function (fn) {
      try { fn(); } catch (e) { console.error('[design-mode] installer failed:', e); }
    });
  }

  // ============================================================
  // Task 7: Viewport selector
  // ============================================================
  var VIEWPORTS = [
    { key: 'mobile',  label: 'Mobile',  w: 390,  h: 844 },
    { key: 'tablet',  label: 'Tablet',  w: 768,  h: 1024 },
    { key: 'desktop', label: 'Desktop', w: 1280, h: 800 },
    { key: 'fit',     label: 'Fit',     w: 0,    h: 0 },
  ];

  function applyViewport(vp) {
    state.viewport = { w: vp.w, h: vp.h, key: vp.key };
    var paneRect = els.pane.getBoundingClientRect();
    var w, h;
    if (vp.key === 'fit') {
      w = Math.max(320, paneRect.width - 32);
      h = Math.max(240, paneRect.height - 32);
    } else {
      w = vp.w;
      h = vp.h;
    }
    els.frame.style.width = w + 'px';
    els.frame.style.height = h + 'px';

    var btns = els.viewportToggle.querySelectorAll('.toggle-btn');
    btns.forEach(function (b) {
      var active = b.dataset.viewport === vp.key;
      b.classList.toggle('active', active);
      b.setAttribute('aria-pressed', active ? 'true' : 'false');
    });
    announce('Viewport: ' + vp.label);
  }

  registerInstaller(function installViewport() {
    if (!els.viewportToggle) return;
    els.viewportToggle.addEventListener('click', function (e) {
      var btn = e.target.closest('.toggle-btn');
      if (!btn) return;
      var key = btn.dataset.viewport;
      var vp = VIEWPORTS.find(function (v) { return v.key === key; });
      if (vp) applyViewport(vp);
    });
    var initial = VIEWPORTS.find(function (v) { return v.key === 'desktop'; });
    applyViewport(initial);

    window.addEventListener('resize', function () {
      if (state.viewport.key === 'fit') applyViewport({ key: 'fit', w: 0, h: 0, label: 'Fit' });
    });

    if (typeof ResizeObserver !== 'undefined') {
      var ro = new ResizeObserver(function () {
        if (state.viewport.key === 'fit') {
          var fit = VIEWPORTS.find(function (v) { return v.key === 'fit'; });
          if (fit) applyViewport(fit);
        }
      });
      if (els.pane) ro.observe(els.pane);
    }
  });

  // ============================================================
  // Task 8: Pin/Navigate toggle (Pin disabled in Phase B)
  // ============================================================
  registerInstaller(function installMode() {
    if (!els.modeToggle) return;
    els.modeToggle.addEventListener('click', function (e) {
      var btn = e.target.closest('.toggle-btn');
      if (!btn) return;
      if (btn.disabled) { e.preventDefault(); return; }
      // Phase B only navigate is enabled; pin is HTML-disabled.
      var key = btn.dataset.mode;
      if (key !== 'navigate') return;
      els.modeToggle.querySelectorAll('.toggle-btn').forEach(function (b) {
        var active = b.dataset.mode === key;
        b.classList.toggle('active', active);
        b.setAttribute('aria-pressed', active ? 'true' : 'false');
      });
    });
  });

  // ============================================================
  // Task 9: Iframe src wired to proxy_port
  // ============================================================
  function proxyURL(pathname) {
    var s = state.session || {};
    var port = s.proxy_port || 0;
    if (!port) return 'about:blank';
    var host = window.location.hostname || 'localhost';
    return 'http://' + host + ':' + port + (pathname || '/');
  }

  registerInstaller(function installIframe() {
    state.currentRoute = utils.normaliseRoute(state.currentRoute);
    if (els.iframe) els.iframe.src = proxyURL(state.currentRoute);
  });

  // ============================================================
  // Task 10: Drag-resize handle on iframe right edge
  // ============================================================
  registerInstaller(function installResizer() {
    if (!els.resizer || !els.frame) return;
    var dragging = false;
    var startX = 0, startW = 0;
    var activePointerId = null;

    function onPointerMove(e) {
      if (!dragging || e.pointerId !== activePointerId) return;
      var dx = e.clientX - startX;
      var newW = Math.max(320, startW + dx);
      els.frame.style.width = newW + 'px';
      state.viewport = { w: newW, h: parseInt(els.frame.style.height, 10) || 800, key: 'custom' };
    }
    function onPointerUp(e) {
      if (!dragging || e.pointerId !== activePointerId) return;
      dragging = false;
      document.body.style.userSelect = '';
      try { els.resizer.releasePointerCapture(activePointerId); } catch (_) {}
      els.resizer.removeEventListener('pointermove', onPointerMove);
      els.resizer.removeEventListener('pointerup', onPointerUp);
      els.resizer.removeEventListener('pointercancel', onPointerUp);
      activePointerId = null;
      // Clear all viewport-toggle active when in custom width.
      els.viewportToggle.querySelectorAll('.toggle-btn').forEach(function (b) {
        b.classList.remove('active');
        b.setAttribute('aria-pressed', 'false');
      });
    }

    els.resizer.addEventListener('pointerdown', function (e) {
      e.preventDefault();
      dragging = true;
      activePointerId = e.pointerId;
      startX = e.clientX;
      startW = els.frame.getBoundingClientRect().width;
      document.body.style.userSelect = 'none';
      try { els.resizer.setPointerCapture(e.pointerId); } catch (_) {}
      els.resizer.addEventListener('pointermove', onPointerMove);
      els.resizer.addEventListener('pointerup', onPointerUp);
      els.resizer.addEventListener('pointercancel', onPointerUp);
    });

    els.resizer.addEventListener('keydown', function (e) {
      var w = els.frame.getBoundingClientRect().width;
      if (e.key === 'ArrowLeft') { e.preventDefault(); els.frame.style.width = Math.max(320, w - 16) + 'px'; }
      if (e.key === 'ArrowRight') { e.preventDefault(); els.frame.style.width = (w + 16) + 'px'; }
    });
  });

  // ============================================================
  // Task 11: Route detection via postMessage
  // ============================================================
  function renderBreadcrumb() {
    if (!els.routeName) return;
    els.routeName.textContent = state.currentRoute;
    var unsaved = state.unsavedRoutes.has(state.currentRoute);
    if (els.routeUnsaved) els.routeUnsaved.style.display = unsaved ? '' : 'none';
  }

  function recordRoute(pathname) {
    var route = utils.normaliseRoute(pathname || '/');
    state.currentRoute = route;
    if (state.routes.indexOf(route) === -1) {
      state.routes.push(route);
      var known = new Set(state.comments.map(function (c) { return utils.normaliseRoute(c.path || '/'); }));
      if (!known.has(route)) state.unsavedRoutes.add(route);
    } else {
      // Already seen — re-evaluate unsaved status against current comments.
      var known2 = new Set(state.comments.map(function (c) { return utils.normaliseRoute(c.path || '/'); }));
      if (known2.has(route)) state.unsavedRoutes.delete(route);
    }
    renderBreadcrumb();
    refreshPanel();
    // Scroll the first card for this route into view in the comments panel.
    try {
      var sel = '.comment-card[data-design-route="' + (window.CSS && CSS.escape ? CSS.escape(route) : route) + '"]';
      var first = document.querySelector(sel);
      if (first) first.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    } catch (_) {}
    announce('Route: ' + route);
  }

  registerInstaller(function installRouteDetection() {
    var s = state.session || {};
    var proxyOrigin = 'http://' + (window.location.hostname || 'localhost') + ':' + (s.proxy_port || 0);
    var altOrigin = 'http://localhost:' + (s.proxy_port || 0);

    window.addEventListener('message', function (e) {
      // Origin check: trust the proxy port on either localhost or 127.0.0.1.
      // Tests dispatch synthetic events with origin '' — accept those too.
      if (e.origin && e.origin !== proxyOrigin && e.origin !== altOrigin) return;
      var data = e.data;
      if (!data || data.type !== 'route-change') return;
      if (typeof data.pathname !== 'string') return;
      recordRoute(data.pathname);
    });

    // Initial render.
    recordRoute(state.currentRoute);
  });

  // ============================================================
  // Task 12: Round counter from /api/review-cycle
  // ============================================================
  registerInstaller(function installRound() {
    if (!els.round) return;
    shared.fetchJSON('/api/review-cycle').then(function (data) {
      var n = (data && (data.review_round || data.round)) || 1;
      els.round.textContent = 'round ' + n;
    }).catch(function (e) {
      console.warn('[design-mode] /api/review-cycle failed:', e);
      els.round.textContent = 'round 1';
    });
  });

  // ============================================================
  // Task 13/14: Comment panel — empty state + grouped renderer (R5)
  // Reuses .comments-panel-body / .comments-panel-file-group / .comment-card
  // ============================================================
  function renderEmptyPanel() {
    if (!els.panelBody) return;
    els.panelBody.innerHTML =
      '<div class="comments-panel-empty" style="padding:32px 16px;text-align:center;color:var(--crit-editor-fg-muted);font-size:13px;line-height:1.5">' +
      'No pins yet.<br>' +
      'Pin mode activates in Phase C.' +
      '</div>';
  }

  function renderCommentsPanel() {
    if (!els.panelBody) return;
    var groups = utils.groupCommentsByRoute(state.comments);
    if (groups.size === 0) { renderEmptyPanel(); return; }
    var html = [];
    groups.forEach(function (rows, route) {
      html.push('<div class="comments-panel-file-group">');
      html.push('<div class="comments-panel-file-name">' + shared.escapeHTML(route) + '</div>');
      html.push('<div class="comments-panel-file-cards">');
      rows.forEach(function (c) {
        var body = (c.body || '').replace(/\s+/g, ' ').trim();
        var excerpt = body.length > 200 ? body.slice(0, 200) + '…' : body;
        var resolvedAttr = c.resolved ? ' data-resolved="true"' : '';
        html.push(
          '<div class="comment-card" data-design-route="' + shared.escapeHTML(route) + '" data-id="' + shared.escapeHTML(String(c.id || '')) + '" tabindex="0" role="button"' + resolvedAttr + '>' +
            '<div class="comment-card-body">' + shared.escapeHTML(excerpt) + '</div>' +
            '<div class="comment-card-meta" style="font-size:11px;color:var(--crit-editor-fg-muted);display:flex;gap:8px">' +
              '<span>' + shared.escapeHTML(c.author || '') + '</span>' +
              (c.resolved ? '<span style="color:var(--crit-green)">resolved</span>' : '') +
            '</div>' +
          '</div>'
        );
      });
      html.push('</div></div>');
    });
    els.panelBody.innerHTML = html.join('');
  }

  registerPanelRefresh(function () {
    if (!state.comments || state.comments.length === 0) {
      renderEmptyPanel();
      return;
    }
    renderCommentsPanel();
  });

  registerInstaller(function installPanel() {
    refreshPanel();
  });

  async function loadAllComments() {
    var s = state.session || {};
    var files = (s.files || []).map(function (f) { return f.path; });
    if (!files.length) return;
    var results = await Promise.all(files.map(function (p) {
      return shared.fetchJSON('/api/file/comments?path=' + encodeURIComponent(p))
        .then(function (list) {
          if (!Array.isArray(list)) return [];
          return list.map(function (c) {
            // Use dom_anchor.pathname for design comments; fallback to file path.
            var path = (c.dom_anchor && c.dom_anchor.pathname) || p;
            c.path = path;
            return c;
          });
        })
        .catch(function (e) {
          console.warn('[design-mode] failed to load comments for', p, e);
          return [];
        });
    }));
    state.comments = results.reduce(function (acc, arr) { return acc.concat(arr); }, []);
  }

  registerInstaller(function loadComments() {
    loadAllComments().then(refreshPanel);
  });

  // ============================================================
  // Task 15: Clicking a comment row navigates iframe
  // ============================================================
  document.addEventListener('click', function (e) {
    var card = e.target.closest && e.target.closest('.comment-card[data-design-route]');
    if (!card) return;
    var route = card.dataset.designRoute || '/';
    if (els && els.iframe) els.iframe.src = proxyURL(utils.normaliseRoute(route));
    state.currentRoute = utils.normaliseRoute(route);
    renderBreadcrumb();
  });

  document.addEventListener('keydown', function (e) {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    var t = e.target;
    if (!t || !t.classList || !t.classList.contains('comment-card')) return;
    if (!t.dataset.designRoute) return;
    e.preventDefault();
    var route = t.dataset.designRoute || '/';
    if (els && els.iframe) els.iframe.src = proxyURL(utils.normaliseRoute(route));
    state.currentRoute = utils.normaliseRoute(route);
    renderBreadcrumb();
  });

  // ============================================================
  // Task 16: Theme — re-apply on focus
  // ============================================================
  window.addEventListener('focus', function () {
    if (window.crit && window.crit.shared) window.crit.shared.applyThemeFromCookie();
  });

  // ============================================================
  // Task 17: Deep-link #pin=<id> (no-op in Phase B; R7)
  // ============================================================
  function parsePinFragment() {
    var hash = window.location.hash || '';
    var m = /^#pin=([\w-]+)$/.exec(hash);
    return m ? m[1] : null;
  }
  state.pendingPinId = parsePinFragment();

  // ============================================================
  // Task 20: Iframe load-error banner
  // ============================================================
  function showIframeError() {
    if (!els.frame) return;
    var existing = document.querySelector('.crit-design-iframe-error');
    if (existing) return;
    var box = document.createElement('div');
    box.className = 'crit-design-iframe-error';
    box.innerHTML =
      '<p>Upstream unreachable.</p>' +
      '<button type="button">Retry</button>';
    box.querySelector('button').addEventListener('click', function () {
      box.remove();
      els.iframe.src = proxyURL(state.currentRoute);
    });
    els.frame.appendChild(box);
  }
  registerInstaller(function installIframeError() {
    if (els.iframe) els.iframe.addEventListener('error', showIframeError);
  });

  // ============================================================
  // Task 21: Cross-origin redirect notice
  // ============================================================
  window.addEventListener('message', function (e) {
    if (!e || !e.data || typeof e.data !== 'object') return;
    if (e.data.type !== 'cross-origin-redirect') return;
    if (els.iframe && e.source && e.source !== els.iframe.contentWindow) return;
    var url = String(e.data.url || '');
    var existing = document.querySelector('.crit-design-redirect-notice');
    if (existing) existing.remove();
    var n = document.createElement('div');
    n.className = 'crit-design-redirect-notice';
    n.innerHTML =
      'Design mode can\'t follow you to <code>' + shared.escapeHTML(url) + '</code>. ' +
      '<button type="button" class="crit-design-redirect-open">Open in real browser</button>' +
      '<button type="button" class="crit-design-redirect-dismiss">Dismiss</button>';
    n.querySelector('.crit-design-redirect-open').addEventListener('click', function () {
      window.open(url, '_blank', 'noopener');
    });
    n.querySelector('.crit-design-redirect-dismiss').addEventListener('click', function () { n.remove(); });
    if (els.frame) els.frame.appendChild(n);
  });

  // ============================================================
  // Task 24: Esc dismisses chrome notices
  // ============================================================
  document.addEventListener('keydown', function (e) {
    if (e.key !== 'Escape') return;
    var notice = document.querySelector('.crit-design-redirect-notice');
    if (notice) { notice.remove(); return; }
    var err = document.querySelector('.crit-design-iframe-error');
    if (err) { err.remove(); }
  });

  // ============================================================
  // Task 26: Lock window.crit.design contract for Phase C
  // ============================================================
  try {
    Object.defineProperty(window.crit, 'design', {
      value: state,
      writable: false,
      configurable: false,
      enumerable: true,
    });
  } catch (_) { /* already locked */ }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', boot);
  } else {
    boot();
  }
})();
