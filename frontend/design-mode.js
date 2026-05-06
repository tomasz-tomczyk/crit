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
  // Other phaseC modules (composer, dispatch, deeplink, etc.) load before this
  // file and each set `root.crit.design = root.crit.design || {}` to register
  // their sub-namespaces. That means the object often already exists by the
  // time we get here — we must MERGE defaults onto it rather than skip via
  // `||`, otherwise `state.routes` ends up undefined and recordRoute() throws
  // before boot completes.
  window.crit.design = window.crit.design || {};
  var state = window.crit.design;
  var stateDefaults = {
    session: null,
    routes: [],
    unsavedRoutes: new Set(),
    currentRoute: '/',
    viewport: { w: 1280, h: 800, key: 'desktop' },
    mode: 'navigate',
    comments: [],
    pinModeEnabled: false,
    pendingPinId: null,
    // Per-pin collapse override store (consumed by buildCommentCard via the
    // get/setCollapseOverride callbacks). Map<commentId, boolean>.
    designCollapseOverrides: new Map(),
  };
  Object.keys(stateDefaults).forEach(function (k) {
    if (state[k] === undefined) state[k] = stateDefaults[k];
  });
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

    // Sync --header-height + --crit-header-height to the live header height.
    // app.js does this for code-review but doesn't run on /design, so without
    // this the comments-panel + sidebar-resize-handle's sticky offsets fall
    // back to a hard-coded 49px and the resizer's hit area drifts off the
    // panel boundary in some viewports.
    var headerEl = document.querySelector('.header');
    if (headerEl) {
      var hh = headerEl.getBoundingClientRect().height;
      document.documentElement.style.setProperty('--header-height', hh + 'px');
      document.documentElement.style.setProperty('--crit-header-height', hh + 'px');
    }

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
        '<button type="button" class="toggle-btn" data-mode="pin" disabled title="Pin mode">Pin</button>';

      // Round counter: code-review writes its round indicator into
      // #headerNotify (header-left). Reuse that slot so the design-mode
      // counter appears in the same visual position as the code-review one.
      // Hide #viewedCount (used in code-review for the file-viewed counter,
      // irrelevant in design mode).
      var legacyViewed = document.getElementById('viewedCount');
      if (legacyViewed) legacyViewed.style.display = 'none';
      var rc = document.getElementById('headerNotify');
      if (!rc) {
        // Defensive fallback: synthesize the slot if the template ever drops
        // it. Sits in header-left so we can't insert into headerRight here.
        var headerLeftFallback = document.querySelector('.header .header-left');
        if (headerLeftFallback) {
          rc = document.createElement('span');
          rc.id = 'headerNotify';
          rc.className = 'header-notify';
          headerLeftFallback.appendChild(rc);
        }
      }
      if (rc) {
        rc.id = 'designRoundCounter';
        rc.classList.add('header-notify');
        rc.textContent = '';
        rc.style.display = '';
      }

      // Insert viewport + mode toggles before the existing settings toggle
      // (which keeps it as rightmost icon button).
      var settingsToggle = document.getElementById('settingsToggle');
      if (settingsToggle) {
        headerRight.insertBefore(vp, settingsToggle);
        headerRight.insertBefore(md, settingsToggle);
      } else {
        headerRight.appendChild(vp);
        headerRight.appendChild(md);
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

    // Phase E: dedicated aria-live announcer (used by announceLive()).
    if (!document.getElementById('crit-design-aria-live')) {
      var live2 = document.createElement('div');
      live2.id = 'crit-design-aria-live';
      live2.className = 'crit-design-sr-only';
      live2.setAttribute('role', 'status');
      live2.setAttribute('aria-live', 'polite');
      document.body.appendChild(live2);
    }

    // Phase E: skip-link to the comments panel.
    if (!document.querySelector('.crit-design-skip-link')) {
      var skip = document.createElement('a');
      skip.className = 'crit-design-skip-link';
      skip.href = '#commentsPanel';
      skip.textContent = 'Skip to comments';
      document.body.insertBefore(skip, document.body.firstChild);
    }

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
    // Phase C: capture proxyOrigin once for the message handler. The agent
    // posts from the proxy origin; the chrome lives on the API origin and
    // accepts only that source+origin pair.
    var s = state.session || {};
    var proxyHost = window.location.hostname || 'localhost';
    state.proxyOrigin = 'http://' + proxyHost + ':' + (s.proxy_port || 0);
    buildShell();
    // Cache the iframeWindow once buildShell has inserted it.
    state.iframeWindow = els.iframe ? els.iframe.contentWindow : null;

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
    // Bug 8: persist viewport key in crit-settings cookie. Skip 'custom'
    // (drag-resize) so the next session restarts at the nearest preset.
    if (vp.key && vp.key !== 'custom' && shared && shared.setSetting) {
      try { shared.setSetting('design_viewport', vp.key); } catch (_) { /* noop */ }
    }
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
    // Phase D: tell agent the viewport changed; gate request-resolution on
    // viewport-applied ack.
    if (state.resolutionGate) state.resolutionGate.beginViewportChange();
    if (state.postToAgent && w > 0 && h > 0) {
      state.postToAgent({ type: 'set-viewport', width: w, height: h });
    }
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
    // Bug 8: hydrate persisted viewport (desktop default).
    var savedKey = (shared && shared.getSetting) ? shared.getSetting('design_viewport', 'desktop') : 'desktop';
    var initial = VIEWPORTS.find(function (v) { return v.key === savedKey; })
      || VIEWPORTS.find(function (v) { return v.key === 'desktop'; });
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
  // Phase C: Pin/Navigate toggle activation + set-mode dispatch to agent
  // ============================================================
  function setActiveModeButton() {
    if (!els.modeToggle) return;
    els.modeToggle.querySelectorAll('.toggle-btn').forEach(function (b) {
      var active = b.dataset.mode === state.mode;
      b.classList.toggle('active', active);
      b.setAttribute('aria-pressed', active ? 'true' : 'false');
    });
  }

  function setMode(value) {
    var next = value === 'pin' ? 'pin' : 'navigate';
    if (state.mode === next) return;
    state.mode = next;
    postToAgent({ type: 'set-mode', value: next });
    // Phase E: also flip marker tabindex so Tab does not jump into the iframe
    // while Pin mode is active.
    postToAgent({ type: 'set-marker-tabindex', value: next === 'pin' ? -1 : 0 });
    setActiveModeButton();
    // Bug 9: announce mode change so the user knows it took effect.
    announce(next === 'pin' ? 'Pin mode' : 'Navigate mode');
  }
  state.setMode = setMode;

  registerInstaller(function installMode() {
    if (!els.modeToggle) return;
    var pinBtn = els.modeToggle.querySelector('.toggle-btn[data-mode="pin"]');
    // Bug 9: keep Pin disabled until the agent reports ready, so a click
    // never races the iframe→agent boot. handleAgentReady() re-enables.
    if (pinBtn) {
      pinBtn.setAttribute('disabled', '');
      pinBtn.setAttribute('title', 'Loading…');
      pinBtn.setAttribute('aria-disabled', 'true');
    }
    els.modeToggle.addEventListener('click', function (e) {
      var btn = e.target.closest('.toggle-btn');
      if (!btn || btn.hasAttribute('disabled')) return;
      var key = btn.dataset.mode;
      if (key !== 'navigate' && key !== 'pin') return;
      setMode(key);
    });
    setActiveModeButton();
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
  // Task 12: Round counter from /api/session.review_round
  // /api/review-cycle is POST-only (405 on GET); session payload already
  // carries review_round, and SSE design-round-start updates it on bumps.
  // ============================================================
  registerInstaller(function installRound() {
    if (!els.round) return;
    var n = (state.session && state.session.review_round) || 1;
    state.currentRound = n;
    els.round.textContent = n > 1 ? 'Round #' + n : '';
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
      'Switch to Pin mode and click an element to leave a comment.' +
      '</div>';
  }

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

  function renderCommentsPanel() {
    if (!els.panelBody) return;
    var groups = utils.groupCommentsByRoute(state.comments);
    if (groups.size === 0) { renderEmptyPanel(); return; }

    // Build the panel as a DOM tree so design pins can mount the shared
    // buildCommentCard (DOM-composed). Non-pin comments still render as a
    // light-weight "comment-card" for click-to-navigate routing.
    var rowMod = window.crit && window.crit.design && window.crit.design.row;
    var deps = getCardDeps();

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
          cards.appendChild(rowMod.renderDesignPinRow(c, deps));
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

  // ============================================================
  // Filter pill + Expand-all wiring (mirrors app.js handlers, scoped to
  // the design-mode panel — app.js doesn't run on /design).
  // ============================================================
  registerInstaller(function installFilterPillAndExpandAll() {
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
        expandBtn.setAttribute('aria-pressed', state.designExpandAll ? 'true' : 'false');
        expandBtn.title = state.designExpandAll ? 'Collapse all' : 'Expand all';
      });
    }
  });

  // ============================================================
  // M12: Comments panel toggle + unresolved count badge.
  // Reuses the navbar's #commentCount button and the
  // #commentsPanelCountBadge inside the panel header. Persistence lives
  // in crit-settings.design_commentsPanelOpen so design mode keeps its
  // own preference distinct from code review.
  // ============================================================
  var panelHelpers = (window.crit && window.crit.design && window.crit.design.panel) || null;

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
    // Navbar pill shows unresolved (matches code-review's #commentCountNumber).
    var navNum = document.getElementById('commentCountNumber');
    if (navNum) navNum.textContent = openCount > 0 ? String(openCount) : '';
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

  registerPanelRefresh(updateUnresolvedBadge);

  // ============================================================
  // Finish Review (parity with code-review's finish flow)
  //
  // - finishBtn text reflects unresolved count (Approve when 0, else Finish Review)
  // - "no changes this round" warns when all unresolved comments were carried
  //   forward and the user hasn't acted this round
  // - waitingOverlay + clipboard + back-to-editing reuse the existing modal
  // ============================================================
  function updateFinishBtn() {
    var btn = document.getElementById('finishBtn');
    if (!btn) return;
    if (state.uiState === 'waiting') return;
    var all = state.comments || [];
    var unresolved = 0;
    for (var i = 0; i < all.length; i++) {
      if (all[i] && !all[i].resolved) unresolved++;
    }
    btn.textContent = unresolved === 0 ? 'Approve' : 'Finish Review';
    btn.disabled = false;
    btn.classList.add('btn-primary');
  }
  registerPanelRefresh(updateFinishBtn);

  function setUIState(s) {
    state.uiState = s;
    var btn = document.getElementById('finishBtn');
    var overlay = document.getElementById('waitingOverlay');
    if (s === 'reviewing') {
      if (btn) {
        btn.disabled = false;
        btn.classList.add('btn-primary');
      }
      if (overlay) overlay.classList.remove('active');
      var edits = document.getElementById('waitingEdits');
      if (edits) edits.textContent = '';
      updateFinishBtn();
    } else if (s === 'waiting') {
      if (btn) {
        btn.textContent = 'Waiting...';
        btn.disabled = true;
        btn.classList.remove('btn-primary');
      }
      var edits2 = document.getElementById('waitingEdits');
      if (edits2) edits2.textContent = '';
      var prompt = document.getElementById('waitingPrompt');
      if (prompt) prompt.style.display = '';
      var clip = document.getElementById('waitingClipboard');
      if (clip) clip.style.display = '';
      if (overlay) overlay.classList.add('active');
    }
  }

  async function doFinishReview() {
    try {
      var resp = await fetch('/api/finish', { method: 'POST' });
      if (!resp.ok) throw new Error('Finish review failed: HTTP ' + resp.status);
      var data = await resp.json();
      var approved = !!data.approved;
      var prompt = data.prompt || 'I reviewed the changes, no feedback, good to go!';

      var dialog = document.getElementById('waitingDialog');
      var headingEl = document.getElementById('waitingHeading');
      var messageEl = document.getElementById('waitingMessage');
      var clipEl = document.getElementById('waitingClipboard');
      var promptEl = document.getElementById('waitingPrompt');

      if (promptEl) promptEl.textContent = prompt;
      if (clipEl) {
        clipEl.textContent = 'Copy prompt';
        clipEl.classList.remove('clipboard-confirm');
      }

      if (dialog) {
        dialog.classList.remove('approved');
        if (approved) {
          void dialog.offsetWidth;
          dialog.classList.add('approved');
        }
      }
      if (headingEl) headingEl.textContent = approved ? 'Approved' : 'Review Complete';
      if (messageEl) {
        if (approved) {
          messageEl.textContent =
            'Your agent has been notified — no further action needed. You can close this tab whenever you’re ready.';
        } else {
          messageEl.innerHTML =
            'Your agent has been notified. Waiting for updates…' +
            '<span class="waiting-fallback">If your agent wasn’t listening, paste the prompt below.</span>';
        }
      }

      try { await navigator.clipboard.writeText(prompt); } catch (_) {}
      setUIState('waiting');
    } catch (err) {
      console.error('[design-mode] finish review failed:', err);
      showToast('Failed to finish review');
    }
  }

  async function resolveAllAndFinish() {
    var all = state.comments || [];
    for (var i = 0; i < all.length; i++) {
      var c = all[i];
      if (!c || c.resolved) continue;
      var path = (c.dom_anchor && c.dom_anchor.pathname) || c.path || '/';
      try {
        await fetch('/api/comment/' + encodeURIComponent(c.id) + '/resolve?path=' + encodeURIComponent(path), {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ resolved: true }),
        });
        c.resolved = true;
      } catch (_) {}
    }
    refreshPanel();
    await doFinishReview();
  }

  registerInstaller(function installFinishReview() {
    var finishBtn = document.getElementById('finishBtn');
    if (!finishBtn) return;
    state.uiState = 'reviewing';
    updateFinishBtn();

    finishBtn.addEventListener('click', function () {
      if (state.uiState !== 'reviewing') return;
      var all = state.comments || [];
      var unresolved = 0;
      var hasNew = false;
      for (var i = 0; i < all.length; i++) {
        var c = all[i];
        if (!c) continue;
        if (!c.resolved) unresolved++;
        if (!c.carried_forward) hasNew = true;
      }
      if (!state.userActedThisRound && !hasNew && unresolved > 0) {
        var overlay = document.getElementById('noChangesOverlay');
        if (overlay) overlay.classList.add('active');
        return;
      }
      doFinishReview();
    });

    var back = document.getElementById('backToEditing');
    if (back) back.addEventListener('click', function () { setUIState('reviewing'); });

    var overlay = document.getElementById('waitingOverlay');
    if (overlay) {
      overlay.addEventListener('click', function (e) {
        if (e.target === overlay) setUIState('reviewing');
      });
    }

    var clip = document.getElementById('waitingClipboard');
    if (clip) {
      clip.addEventListener('click', async function () {
        var p = document.getElementById('waitingPrompt');
        var text = p ? p.textContent : '';
        try {
          await navigator.clipboard.writeText(text);
          clip.textContent = '✓ Copied';
          clip.setAttribute('aria-label', 'Copied');
          clip.classList.remove('clipboard-confirm');
          void clip.offsetWidth;
          clip.classList.add('clipboard-confirm');
          setTimeout(function () {
            clip.textContent = 'Copy prompt';
            clip.setAttribute('aria-label', 'Copy prompt');
          }, 2000);
        } catch (_) {}
      });
    }

    function hideNoChanges() {
      var ov = document.getElementById('noChangesOverlay');
      if (ov) ov.classList.remove('active');
    }
    var resolveAll = document.getElementById('noChangesResolveAll');
    if (resolveAll) resolveAll.addEventListener('click', async function () {
      hideNoChanges();
      await resolveAllAndFinish();
    });
    var sendAnyway = document.getElementById('noChangesSendAnyway');
    if (sendAnyway) sendAnyway.addEventListener('click', async function () {
      hideNoChanges();
      await doFinishReview();
    });
    var goBack = document.getElementById('noChangesGoBack');
    if (goBack) goBack.addEventListener('click', hideNoChanges);
  });

  registerInstaller(function installCommentsPanelToggle() {
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
  });

  // ============================================================
  // M13: Resizable side panel.
  // Reuses #commentsPanelResizer. NO clamping against viewport preset
  // width — the user gets the width they ask for. Persisted to
  // crit-settings.design_commentsPanelWidth (separate from code review's
  // commentsPanelWidth so the two modes don't fight).
  // ============================================================
  registerInstaller(function installCommentsPanelResize() {
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
    loadAllComments().then(function () {
      refreshPanel();
      pushPinsToAgent();
    });
  });

  // ============================================================
  // Bug 5 (partial): Resolve / Reopen click on design pin rows.
  // Full edit/reply parity with code-review's renderCommentCard requires
  // refactoring large chunks of app.js into a shared module — deferred.
  // ============================================================
  document.addEventListener('click', function (e) {
    var btn = e.target.closest && e.target.closest('.crit-design-comment-resolve');
    if (!btn) return;
    e.stopPropagation();
    var id = btn.dataset.commentId;
    var path = btn.dataset.pathname || '/';
    if (!id) return;
    var c = (state.comments || []).find(function (x) { return x && x.id === id; });
    var resolved = c ? !c.resolved : true;
    btn.disabled = true;
    fetch('/api/comment/' + encodeURIComponent(id) + '/resolve?path=' + encodeURIComponent(path), {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ resolved: resolved }),
    }).then(function (r) {
      if (!r.ok) throw new Error('resolve failed: ' + r.status);
      if (c) c.resolved = resolved;
      state.userActedThisRound = true;
      refreshPanel();
    }).catch(function (err) {
      showToast('Resolve failed: ' + (err && err.message || err));
    }).finally(function () {
      btn.disabled = false;
    });
  });

  // ============================================================
  // Reply on design-mode comment rows.
  // Endpoint: POST /api/comment/{id}/replies?path=<pathname>
  // The row template renders an inline composer when c._replyOpen is set.
  // Draft text is held on c._replyDraft so it survives panel re-renders
  // (matching code-review's activeReplyForms behaviour).
  // ============================================================
  function findCommentById(id) {
    return (state.comments || []).find(function (x) { return x && x.id === id; });
  }

  function focusReplyTextareaFor(id) {
    requestAnimationFrame(function () {
      var card = document.querySelector('.crit-design-comment-row[data-comment-id="' + (window.CSS && CSS.escape ? CSS.escape(id) : id) + '"]');
      if (!card) return;
      var ta = card.querySelector('.crit-design-reply-textarea');
      if (!ta) return;
      ta.focus();
      // Place cursor at end so existing draft text is preserved usefully.
      try { ta.setSelectionRange(ta.value.length, ta.value.length); } catch (_) {}
    });
  }

  function closeReplyComposer(c) {
    if (!c) return;
    c._replyOpen = false;
    c._replyDraft = '';
    refreshPanel();
  }

  document.addEventListener('click', function (e) {
    var btn = e.target.closest && e.target.closest('.crit-design-comment-reply');
    if (!btn) return;
    e.stopPropagation();
    var id = btn.dataset.commentId;
    if (!id) return;
    var c = findCommentById(id);
    if (!c) return;
    c._replyOpen = true;
    refreshPanel();
    focusReplyTextareaFor(id);
  });

  document.addEventListener('click', function (e) {
    var btn = e.target.closest && e.target.closest('.crit-design-reply-cancel');
    if (!btn) return;
    e.stopPropagation();
    var id = btn.dataset.commentId;
    var c = findCommentById(id);
    if (!c) return;
    var card = btn.closest('.crit-design-comment-row');
    var ta = card && card.querySelector('.crit-design-reply-textarea');
    var dirty = ta && ta.value.trim().length > 0;
    if (dirty) {
      var ok = window.confirm('Discard reply?');
      if (!ok) return;
    }
    closeReplyComposer(c);
  });

  async function submitReply(c, pathname, body, saveBtn, errEl) {
    if (!c || !c.id || !body) return;
    if (saveBtn) saveBtn.disabled = true;
    if (errEl) { errEl.hidden = true; errEl.textContent = ''; }
    try {
      var url = '/api/comment/' + encodeURIComponent(c.id) + '/replies?path=' + encodeURIComponent(pathname || '/');
      var res = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ body: body }),
      });
      if (!res.ok) throw new Error('Server returned ' + res.status);
      var reply = await res.json();
      c.replies = Array.isArray(c.replies) ? c.replies : [];
      c.replies.push(reply);
      c._replyOpen = false;
      c._replyDraft = '';
      state.userActedThisRound = true;
      refreshPanel();
    } catch (err) {
      if (errEl) {
        errEl.hidden = false;
        errEl.textContent = String(err && err.message || err);
      } else {
        showToast('Reply failed: ' + (err && err.message || err));
      }
      if (saveBtn) saveBtn.disabled = false;
    }
  }

  document.addEventListener('click', function (e) {
    var btn = e.target.closest && e.target.closest('.crit-design-reply-save');
    if (!btn) return;
    e.stopPropagation();
    var id = btn.dataset.commentId;
    var path = btn.dataset.pathname || '/';
    var c = findCommentById(id);
    if (!c) return;
    var card = btn.closest('.crit-design-comment-row');
    var ta = card && card.querySelector('.crit-design-reply-textarea');
    var errEl = card && card.querySelector('.crit-design-reply-error');
    var body = ta ? ta.value.trim() : '';
    if (!body) {
      if (errEl) { errEl.hidden = false; errEl.textContent = 'Reply body required'; }
      return;
    }
    submitReply(c, path, body, btn, errEl);
  });

  // Keep the in-memory draft in sync so refreshPanel doesn't drop typed text.
  document.addEventListener('input', function (e) {
    var ta = e.target;
    if (!ta || !ta.classList || !ta.classList.contains('crit-design-reply-textarea')) return;
    var card = ta.closest('.crit-design-comment-row');
    var id = card && card.dataset.commentId;
    if (!id) return;
    var c = findCommentById(id);
    if (!c) return;
    c._replyDraft = ta.value;
  });

  document.addEventListener('keydown', function (e) {
    var ta = e.target;
    if (!ta || !ta.classList || !ta.classList.contains('crit-design-reply-textarea')) return;
    if (e.isComposing) return;
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      e.stopPropagation();
      var card = ta.closest('.crit-design-comment-row');
      var saveBtn = card && card.querySelector('.crit-design-reply-save');
      if (saveBtn) saveBtn.click();
      return;
    }
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      var card2 = ta.closest('.crit-design-comment-row');
      var id = card2 && card2.dataset.commentId;
      var c = id ? findCommentById(id) : null;
      if (!c) return;
      var dirty = ta.value.trim().length > 0;
      if (dirty) {
        var ok = window.confirm('Discard reply?');
        if (!ok) return;
      }
      closeReplyComposer(c);
    }
  });

  // ============================================================
  // Edit on design-mode comment rows.
  // Endpoint: PUT /api/comment/{id}?path=<pathname>
  // The row template renders an inline editor when c._editOpen is set.
  // Draft text held on c._editDraft so it survives panel re-renders.
  // ============================================================
  function focusEditTextareaFor(id) {
    requestAnimationFrame(function () {
      var card = document.querySelector('.crit-design-comment-row[data-comment-id="' + (window.CSS && CSS.escape ? CSS.escape(id) : id) + '"]');
      if (!card) return;
      var ta = card.querySelector('.crit-design-edit-textarea');
      if (!ta) return;
      ta.focus();
      try { ta.setSelectionRange(ta.value.length, ta.value.length); } catch (_) {}
    });
  }

  function closeEditComposer(c) {
    if (!c) return;
    c._editOpen = false;
    c._editDraft = null;
    refreshPanel();
  }

  document.addEventListener('click', function (e) {
    var btn = e.target.closest && e.target.closest('.crit-design-comment-edit');
    if (!btn) return;
    e.stopPropagation();
    var id = btn.dataset.commentId;
    if (!id) return;
    var c = findCommentById(id);
    if (!c) return;
    c._editOpen = true;
    c._editDraft = c.body || '';
    refreshPanel();
    focusEditTextareaFor(id);
  });

  document.addEventListener('click', function (e) {
    var btn = e.target.closest && e.target.closest('.crit-design-edit-cancel');
    if (!btn) return;
    e.stopPropagation();
    var id = btn.dataset.commentId;
    var c = findCommentById(id);
    if (!c) return;
    var card = btn.closest('.crit-design-comment-row');
    var ta = card && card.querySelector('.crit-design-edit-textarea');
    var dirty = ta && ta.value.trim() !== (c.body || '').trim();
    if (dirty) {
      var ok = window.confirm('Discard edit?');
      if (!ok) return;
    }
    closeEditComposer(c);
  });

  async function submitEdit(c, pathname, body, saveBtn, errEl) {
    if (!c || !c.id) return;
    if (saveBtn) saveBtn.disabled = true;
    if (errEl) { errEl.hidden = true; errEl.textContent = ''; }
    try {
      var url = '/api/comment/' + encodeURIComponent(c.id) + '?path=' + encodeURIComponent(pathname || '/');
      var res = await fetch(url, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ body: body }),
      });
      if (!res.ok) throw new Error('Server returned ' + res.status);
      // Optimistic update — update body and close editor; comment-changed SSE
      // will refresh from canonical state.
      c.body = body;
      c._editOpen = false;
      c._editDraft = null;
      state.userActedThisRound = true;
      refreshPanel();
    } catch (err) {
      if (errEl) {
        errEl.hidden = false;
        errEl.textContent = String(err && err.message || err);
      } else {
        showToast('Edit failed: ' + (err && err.message || err));
      }
      if (saveBtn) saveBtn.disabled = false;
    }
  }

  document.addEventListener('click', function (e) {
    var btn = e.target.closest && e.target.closest('.crit-design-edit-save');
    if (!btn) return;
    e.stopPropagation();
    var id = btn.dataset.commentId;
    var path = btn.dataset.pathname || '/';
    var c = findCommentById(id);
    if (!c) return;
    var card = btn.closest('.crit-design-comment-row');
    var ta = card && card.querySelector('.crit-design-edit-textarea');
    var errEl = card && card.querySelector('.crit-design-edit-error');
    var body = ta ? ta.value.trim() : '';
    if (!body) {
      if (errEl) { errEl.hidden = false; errEl.textContent = 'Body required'; }
      return;
    }
    submitEdit(c, path, body, btn, errEl);
  });

  // Keep edit draft in sync so refreshPanel doesn't drop typed text.
  document.addEventListener('input', function (e) {
    var ta = e.target;
    if (!ta || !ta.classList || !ta.classList.contains('crit-design-edit-textarea')) return;
    var card = ta.closest('.crit-design-comment-row');
    var id = card && card.dataset.commentId;
    if (!id) return;
    var c = findCommentById(id);
    if (!c) return;
    c._editDraft = ta.value;
  });

  document.addEventListener('keydown', function (e) {
    var ta = e.target;
    if (!ta || !ta.classList || !ta.classList.contains('crit-design-edit-textarea')) return;
    if (e.isComposing) return;
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      e.stopPropagation();
      var card = ta.closest('.crit-design-comment-row');
      var saveBtn = card && card.querySelector('.crit-design-edit-save');
      if (saveBtn) saveBtn.click();
      return;
    }
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      var card2 = ta.closest('.crit-design-comment-row');
      var id = card2 && card2.dataset.commentId;
      var c = id ? findCommentById(id) : null;
      if (!c) return;
      var dirty = ta.value.trim() !== (c.body || '').trim();
      if (dirty) {
        var ok = window.confirm('Discard edit?');
        if (!ok) return;
      }
      closeEditComposer(c);
    }
  });

  // ============================================================
  // Task 15: Clicking a comment row navigates iframe
  // ============================================================
  document.addEventListener('click', function (e) {
    // Don't navigate when clicking interactive controls inside the card.
    if (e.target.closest && e.target.closest('button, a, input, textarea')) return;
    var card = e.target.closest && e.target.closest('.comment-card[data-design-route]');
    if (!card) return;
    var route = utils.normaliseRoute(card.dataset.designRoute || '/');
    // Skip iframe reassignment if already on this route — otherwise we'd
    // trigger a route-change → request-resolution → drift PUT cycle for a
    // pin that's still on its anchor.
    if (route === state.currentRoute) return;
    if (els && els.iframe) els.iframe.src = proxyURL(route);
    state.currentRoute = route;
    renderBreadcrumb();
  });

  document.addEventListener('keydown', function (e) {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    var t = e.target;
    if (!t || !t.classList || !t.classList.contains('comment-card')) return;
    if (!t.dataset.designRoute) return;
    e.preventDefault();
    var route = utils.normaliseRoute(t.dataset.designRoute || '/');
    if (route === state.currentRoute) return;
    if (els && els.iframe) els.iframe.src = proxyURL(route);
    state.currentRoute = route;
    renderBreadcrumb();
  });

  // ============================================================
  // Settings overlay: design mode mounts the same Settings overlay as
  // code review by delegating all three tabs to crit-settings-panes.js.
  // Mode-specific behaviour (no width pill; design-scoped hide-resolved)
  // is supplied via the hooks/show options.
  // ============================================================
  registerInstaller(function installSettingsOverlay() {
    var toggle = document.getElementById('settingsToggle');
    var overlay = document.getElementById('settingsOverlay');
    if (!toggle || !overlay) return;

    // Design mode persists hide-resolved under its own key so it doesn't
    // collide with code-review's preference (each mode toggles its own).
    function getHideResolved() {
      return !!(shared.getSetting && shared.getSetting('design_hideResolved', false));
    }
    function setHideResolved(v) {
      if (shared.setSetting) shared.setSetting('design_hideResolved', !!v);
      document.body.classList.toggle('hide-resolved', !!v);
    }
    function applyTheme(t) {
      if (shared.setSetting) shared.setSetting('theme', t);
      if (shared.applyThemeFromCookie) shared.applyThemeFromCookie();
    }

    function renderAllPanes() {
      var panes = window.crit && window.crit.settingsPanes;
      if (!panes) return;
      var cfg = (state && state.config) || {};
      var sess = (state && state.session) || {};
      var sessionDescriptor = {
        mode: 'design',
        vcs_name: 'design',
        review_round: sess.review_round || state.currentRound || 1,
        upstream_url: sess.upstream_url || state.upstreamURL,
      };
      if (panes.renderSettingsTab) {
        panes.renderSettingsTab(overlay.querySelector('#settingsPane'), {
          mode: 'design',
          cfg: cfg,
          hooks: {
            applyTheme: applyTheme,
            getHideResolved: getHideResolved,
            setHideResolved: setHideResolved,
            onHideResolvedChange: function () {
              // No file-list re-render in design mode; the body class
              // (toggled by setHideResolved) drives CSS visibility on pin
              // rows + comment cards.
            },
          },
        });
      }
      if (panes.renderShortcutsPane) {
        panes.renderShortcutsPane(overlay.querySelector('#shortcutsPane'));
      }
      if (panes.renderAboutPane) {
        panes.renderAboutPane(overlay.querySelector('#aboutPane'), cfg, sessionDescriptor);
      }
    }

    function switchTab(tab) {
      var tabs = overlay.querySelectorAll('.settings-tab[role="tab"]');
      tabs.forEach(function (t) {
        var isActive = t.dataset.tab === tab;
        t.classList.toggle('active', isActive);
        t.setAttribute('aria-selected', String(isActive));
      });
      var panes = overlay.querySelectorAll('.settings-pane');
      panes.forEach(function (p) {
        p.classList.toggle('active', p.dataset.pane === tab);
      });
    }

    function open() {
      overlay.classList.add('active');
      var tabs = overlay.querySelectorAll('.settings-tab[role="tab"]');
      tabs.forEach(function (t) { t.style.display = ''; });
      switchTab('settings');
      // Render once with whatever cfg/session we have, then re-render after
      // /api/config + /api/session resolve so cards (update / account /
      // integration / share) and the About session info populate.
      renderAllPanes();
      var pending = 2;
      function done() { pending--; if (pending === 0) renderAllPanes(); }
      try {
        fetch('/api/config').then(function (r) { return r.ok ? r.json() : {}; }).then(function (c) {
          state.config = c || {};
        }).catch(function () {}).finally(done);
        fetch('/api/session').then(function (r) { return r.ok ? r.json() : {}; }).then(function (s) {
          state.session = s || {};
        }).catch(function () {}).finally(done);
      } catch (_) {
        // best-effort
      }
    }
    function close() { overlay.classList.remove('active'); }

    // Apply persisted hide-resolved on boot so the body class is in sync
    // before the overlay is opened the first time.
    document.body.classList.toggle('hide-resolved', getHideResolved());

    toggle.addEventListener('click', function () {
      if (overlay.classList.contains('active')) close(); else open();
    });
    overlay.addEventListener('click', function (e) {
      if (e.target === overlay) { close(); return; }
      var closeBtn = e.target.closest && e.target.closest('#settingsClose');
      if (closeBtn) { close(); return; }
      var tabBtn = e.target.closest && e.target.closest('.settings-tab[data-tab]');
      if (tabBtn) { switchTab(tabBtn.dataset.tab); return; }
      // theme/width/hide-resolved/copy/dismiss wiring lives inside
      // renderSettingsTab — no overlay-level handlers needed for them.
    });
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && overlay.classList.contains('active')) close();
    });
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
    var dl = window.crit && window.crit.design && window.crit.design.deeplink;
    if (dl) return dl.parseDeepLink(window.location.hash || '');
    var hash = window.location.hash || '';
    var m = /^#pin=([\w-]+)$/.exec(hash);
    return m ? m[1] : null;
  }
  state.pendingPinId = parsePinFragment();
  state.pendingFlashOnLoad = false;
  state.resolutionCache = state.resolutionCache || {};
  state.currentRound = state.currentRound || 1;
  state.openPin = state.openPin || null;
  state.pendingByPath = state.pendingByPath || {};
  state.pendingResolutionPaths = state.pendingResolutionPaths || null;
  state.designFilter = state.designFilter || 'all';
  state.designExpandAll = !!state.designExpandAll;
  state.userActedThisRound = !!state.userActedThisRound;

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
  // Phase C: agent ↔ chrome wiring (dispatcher, queue, origin guard,
  // composer, ancestor menu, focus state, save flow)
  // ============================================================
  var _sender = null;
  function postToAgent(m) {
    if (_sender) _sender.send(m);
  }
  state.postToAgent = postToAgent;

  function showToast(message) {
    var host = document.querySelector('.crit-design-toast-host');
    if (!host) {
      host = document.createElement('div');
      host.className = 'crit-design-toast-host';
      document.body.appendChild(host);
    }
    var t = document.createElement('div');
    t.className = 'crit-design-toast';
    t.textContent = message;
    host.appendChild(t);
    setTimeout(function () { t.remove(); }, 4000);
  }
  state.showToast = showToast;

  // ---- composer ----
  function ensureComposerHost() {
    var h = document.querySelector('.crit-design-composer-host');
    if (h) return h;
    h = document.createElement('div');
    h.className = 'crit-design-composer-host';
    var panel = document.querySelector('.comments-panel') || document.body;
    panel.appendChild(h);
    return h;
  }

  function closeComposer() {
    var h = document.querySelector('.crit-design-composer-host');
    if (h) { h.innerHTML = ''; delete h.dataset.active; }
    // M11: drop the sustained outline on the captured element.
    try { postToAgent({ type: 'clear-highlight' }); } catch (_) {}
    // Intentional: do not change state.mode here — keep Pin mode for rapid pinning.
  }

  async function saveComposer(domAnchor) {
    var host = document.querySelector('.crit-design-composer-host');
    if (!host) return;
    var bodyEl = host.querySelector('.crit-design-composer-body');
    var body = bodyEl ? bodyEl.value.trim() : '';
    var errEl = host.querySelector('.crit-design-composer-error');
    if (errEl) { errEl.hidden = true; errEl.textContent = ''; }
    if (!body) {
      if (errEl) { errEl.hidden = false; errEl.textContent = 'Body required'; }
      return;
    }
    var saveBtn = host.querySelector('.crit-design-composer-save');
    if (saveBtn) saveBtn.disabled = true;
    try {
      var url = '/api/file/comments?path=' + encodeURIComponent(domAnchor.pathname);
      var res = await shared.fetchJSON(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ start_line: 0, end_line: 0, body: body, dom_anchor: domAnchor }),
      });
      optimisticInsertComment(domAnchor.pathname, res);
      closeComposer();
      refreshCommentsForRoute(domAnchor.pathname);
    } catch (err) {
      if (errEl) {
        errEl.hidden = false;
        errEl.textContent = String(err && err.message || err);
      }
    } finally {
      if (saveBtn) saveBtn.disabled = false;
    }
  }

  function optimisticInsertComment(pathname, comment) {
    if (!comment || !comment.id) return;
    var c = Object.assign({}, comment, { path: pathname });
    // Stamp the round the pin was created in so a later route-change /
    // round-start resolution scan can't mark it as drifted before the user
    // has had a chance to navigate. A pin that fails to resolve in the
    // round it was just created in is a transient agent miss, not drift.
    c._createdInRound = state.currentRound || 1;
    state.comments = state.comments || [];
    state.comments.unshift(c);
    state.userActedThisRound = true;
    refreshPanel();
  }

  async function refreshCommentsForRoute(pathname) {
    try {
      var list = await shared.fetchJSON('/api/file/comments?path=' + encodeURIComponent(pathname));
      var out = (list || []).map(function (c) {
        var path = (c.dom_anchor && c.dom_anchor.pathname) || pathname;
        c.path = path;
        return c;
      });
      // Replace comments for that route only.
      state.comments = (state.comments || []).filter(function (c) {
        var p = (c.dom_anchor && c.dom_anchor.pathname) || c.path;
        return p !== pathname;
      }).concat(out);
      refreshPanel();
      pushPinsToAgent();
    } catch (_) { /* swallow */ }
  }

  async function handleReanchorSelection(pinId, domAnchor) {
    if (!reanchorPutAPI) return;
    try {
      var req = reanchorPutAPI.buildReanchorRequest(pinId, domAnchor);
      var res = await fetch(req.url, { method: req.method, headers: req.headers, body: req.body });
      if (!res.ok) {
        showToast('Re-anchor failed: ' + res.status);
        return;
      }
      // Disarm UI side, refresh comments + re-trigger resolution.
      if (reanchorClickAPI) reanchorClickAPI.disarmReanchor({ state: state }, 'completed');
      await refreshCommentsForRoute(domAnchor.pathname);
      pushPinsToAgent();
      if (state.resolutionGate) state.resolutionGate.requestResolution();
      else fireRequestResolution();
    } catch (err) {
      showToast('Re-anchor error: ' + (err && err.message || err));
    }
  }

  function handleSelection(domAnchor, pointer, reanchorFor) {
    if (reanchorFor) {
      handleReanchorSelection(reanchorFor, domAnchor);
      return;
    }
    var sizeMod = window.crit.design.size;
    if (sizeMod && sizeMod.selectionTooLarge(domAnchor)) {
      showToast('selection too large to save');
      return;
    }
    var host = ensureComposerHost();
    host.innerHTML = window.crit.design.composer.renderComposerHTML(domAnchor);
    host.dataset.active = '1';
    // M11: ask the agent to keep the captured element outlined while the
    // composer is open. Cleared by closeComposer (Save / Cancel / Esc).
    if (domAnchor && domAnchor.css_selector) {
      try { postToAgent({ type: 'keep-highlight', selector: domAnchor.css_selector }); } catch (_) {}
    }
    var ta = host.querySelector('.crit-design-composer-body');
    if (ta) {
      ta.focus();
      ta.addEventListener('keydown', function (e) {
        // Don't intercept while an IME composition is in progress.
        if (e.isComposing) return;
        // Cmd/Ctrl+Enter saves.
        if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
          e.preventDefault();
          saveComposer(domAnchor);
          return;
        }
        if (e.key === 'Escape') {
          e.preventDefault();
          var dirty = ta.value.trim().length > 0;
          if (dirty) {
            var ok = window.confirm('Discard pin?');
            if (!ok) return;
          }
          closeComposer();
        }
      });
    }
    var cancelBtn = host.querySelector('.crit-design-composer-cancel');
    if (cancelBtn) cancelBtn.addEventListener('click', closeComposer);
    var saveBtn = host.querySelector('.crit-design-composer-save');
    if (saveBtn) saveBtn.addEventListener('click', function () { saveComposer(domAnchor); });
  }

  // ---- ancestor menu ----
  function closeAncestorMenu() {
    var h = document.querySelector('.crit-design-ancestor-menu-host');
    if (h) h.remove();
  }
  function closeAncestorMenuOnce(ev) {
    if (ev.target.closest && ev.target.closest('.crit-design-ancestor-menu-host')) return;
    closeAncestorMenu();
    postToAgent({ type: 'cancel-ancestor-selection' });
  }

  function handleAncestorMenu(options, pointer) {
    closeAncestorMenu();
    var iframe = els.iframe;
    if (!iframe) return;
    var r = iframe.getBoundingClientRect();
    var x = r.left + ((pointer && pointer.x) || 0);
    var y = r.top + ((pointer && pointer.y) || 0);
    var wrap = document.createElement('div');
    wrap.className = 'crit-design-ancestor-menu-host';
    wrap.style.cssText = 'position:fixed;left:' + x + 'px;top:' + y + 'px;z-index:2147483600;visibility:hidden;';
    wrap.innerHTML = window.crit.design.menu.renderAncestorMenuHTML(options);
    document.body.appendChild(wrap);
    var clamped = window.crit.design.menu.clampMenuPosition({
      x: x, y: y,
      width: wrap.offsetWidth,
      height: wrap.offsetHeight,
      vw: window.innerWidth,
      vh: window.innerHeight,
      pad: 8,
    });
    wrap.style.left = clamped.x + 'px';
    wrap.style.top = clamped.y + 'px';
    wrap.style.visibility = 'visible';
    wrap.addEventListener('click', function (ev) {
      var btn = ev.target.closest && ev.target.closest('.crit-design-ancestor-menu-item');
      if (!btn) return;
      var level = Number(btn.dataset.level);
      postToAgent({ type: 'commit-ancestor-selection', level: level });
      closeAncestorMenu();
    });

    // Phase E: keyboard nav controller + fade-in.
    var menuMod = window.crit && window.crit.design && window.crit.design.menuController;
    if (menuMod && menuMod.createMenuController) {
      var inner = wrap.querySelector('.crit-design-ancestor-menu') || wrap.firstElementChild;
      var items = wrap.querySelectorAll('.crit-design-ancestor-menu-item');
      var ctl = menuMod.createMenuController({
        options: options,
        onCommit: function (o) {
          if (!o) return;
          postToAgent({ type: 'commit-ancestor-selection', level: o.level });
          state.menuController = null;
          closeAncestorMenu();
        },
        onCancel: function () {
          state.menuController = null;
          closeAncestorMenu();
          postToAgent({ type: 'cancel-ancestor-selection' });
        },
        onHighlight: function (i) {
          items.forEach(function (el, j) {
            el.classList.toggle('crit-design-ancestor-menu-item--active', i === j);
          });
          if (items[i] && typeof items[i].focus === 'function') {
            try { items[i].focus(); } catch (_) { /* noop */ }
          }
        },
      });
      state.menuController = ctl;
      wrap.addEventListener('keydown', function (ev) { ctl.keydown(ev); });
      if (items[0] && typeof items[0].focus === 'function') {
        try { items[0].focus(); } catch (_) { /* noop */ }
      }
      requestAnimationFrame(function () {
        if (inner && inner.classList) inner.classList.add('crit-design-ancestor-menu--open');
      });
    }

    setTimeout(function () {
      document.addEventListener('click', closeAncestorMenuOnce, { once: true, capture: true });
    }, 0);
  }

  function handleAgentReady() {
    state.agentReady = true;
    if (_sender) _sender.markReady();
    // Bug 9: now that the agent is listening, enable the Pin toggle.
    if (els.modeToggle) {
      var pinBtn = els.modeToggle.querySelector('.toggle-btn[data-mode="pin"]');
      if (pinBtn) {
        pinBtn.removeAttribute('disabled');
        pinBtn.removeAttribute('aria-disabled');
        pinBtn.removeAttribute('title');
      }
    }
    pushPinsToAgent();
  }
  function handleAgentError(e) {
    showToast(e.kind + ': ' + e.message);
  }
  function handleFocusState(b) {
    state.focusInInput = !!b;
  }

  // ============================================================
  // Phase D: pins, drift tray, resolution gate, re-anchor flow
  // ============================================================
  var pinStateAPI = window.crit && window.crit.designModePinState;
  var pinFilterAPI = window.crit && window.crit.designModePinFilter;
  var driftTrayAPI = window.crit && window.crit.designModeDriftTray;
  var threadScrollAPI = window.crit && window.crit.designModeThreadScroll;
  var reanchorClickAPI = window.crit && window.crit.designModeReanchorClick;
  var reanchorPutAPI = window.crit && window.crit.designModeReanchorPut;
  var resolutionGateAPI = window.crit && window.crit.designModeResolutionGate;

  state.pinState = pinStateAPI && pinStateAPI.PinState ? new pinStateAPI.PinState() : null;
  state.reanchorPending = null;
  state.reanchorBtn = null;
  state.reanchorTimeoutId = null;

  function fireRequestResolution() {
    if (state.agentReady) postToAgent({ type: 'request-resolution' });
  }
  state.resolutionGate = resolutionGateAPI && resolutionGateAPI.ResolutionGate
    ? new resolutionGateAPI.ResolutionGate(fireRequestResolution)
    : null;

  function currentPathname() {
    // Active route in the iframe (last announced via route-change), falling
    // back to the iframe URL pathname if known.
    return state.currentPathname || '/';
  }

  function pushPinsToAgent() {
    if (!state.agentReady || !pinFilterAPI) return;
    var all = (state.comments || []).filter(function (c) { return c && c.dom_anchor; }).map(function (c) {
      return { id: c.id, pin_number: c.pin_number || 0, dom_anchor: c.dom_anchor };
    });
    var pins = pinFilterAPI.filterPinsForPath(all, currentPathname());
    postToAgent({ type: 'set-pins', pins: pins });
    if (state.pinState) state.pinState.setComments(state.comments || []);
    renderDriftTray();
  }

  function renderDriftTray() {
    if (!driftTrayAPI || !state.pinState) return;
    var host = document.querySelector('.crit-design-drifted-tray-host');
    if (!host) {
      var panel = document.querySelector('.comments-panel') || document.body;
      host = document.createElement('div');
      host.className = 'crit-design-drifted-tray-host';
      panel.insertBefore(host, panel.firstChild);
    }
    host.innerHTML = driftTrayAPI.renderDriftTrayHTML(state.pinState.driftedRows(), state.currentRound);
  }

  function handlePinResolutionResult(msg) {
    var prev = lookupPin && lookupPin(msg && msg.pin_id);
    if (state.pinState) state.pinState.applyResolution(msg);
    var rr = window.crit && window.crit.design && window.crit.design.roundResolve;
    if (rr && prev) {
      var path2 = (prev.dom_anchor && prev.dom_anchor.pathname) || state.currentPathname || '/';
      // Only PUT drifted_on_round during the *initial round-start scan*.
      // Resolution results that arrive outside an active scan (e.g. an
      // ad-hoc request-resolution from a route-change with no pending
      // pins, or a late result for an already-resolved pin) must not
      // mark drift — that caused click-on-comment to silently drift
      // unchanged pins.
      var inActiveScan = typeof state.pendingByPath[path2] === 'number' &&
                         state.pendingByPath[path2] > 0;
      var alreadyResolvedThisRound = !!prev._roundResolved;
      var createdThisRound = prev._createdInRound === state.currentRound;
      if (inActiveScan && !alreadyResolvedThisRound && !createdThisRound) {
        var c = rr.classifyPinForRound(prev, msg, state.currentRound);
        if (c.driftedOnRound) {
          var url = '/api/comment/' + encodeURIComponent(prev.id) + '?path=' + encodeURIComponent(path2);
          try {
            fetch(url, {
              method: 'PUT',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ drifted_on_round: c.driftedOnRound, drifted: true }),
            }).then(function (r) { if (r && !r.ok) console.warn('[design] PUT drifted_on_round failed', r.status); })
              .catch(function (e) { console.warn('[design] PUT drifted_on_round failed:', e); });
            prev.drifted = true;
            prev.drifted_on_round = c.driftedOnRound;
            announceLive('Pin ' + prev.id + ' drifted on round ' + c.driftedOnRound + '.');
          } catch (_) { /* noop */ }
        }
        prev._roundResolved = true;
        state.pendingByPath[path2] = Math.max(0, state.pendingByPath[path2] - 1);
        if (state.pendingByPath[path2] === 0) {
          state.resolutionCache[path2] = 'fresh';
          delete state.pendingByPath[path2];
        }
      }
    }
    renderDriftTray();
  }

  function handleViewportApplied(_msg) {
    if (state.resolutionGate) state.resolutionGate.onViewportApplied();
    state.viewportInFlight = false;
    if (state.pendingResolutionPaths && state.pendingResolutionPaths.size) {
      var paths = Array.from(state.pendingResolutionPaths);
      state.pendingResolutionPaths.clear();
      paths.forEach(function (p) { scheduleResolutionForPath(p); });
    }
  }

  function handleRouteChange(msg) {
    var prevPath = state.currentPathname;
    state.currentPathname = msg.pathname || '/';
    // Phase E: clear deep-link fragment when navigating away from the open pin.
    var dl = window.crit && window.crit.design && window.crit.design.deeplink;
    if (dl && dl.shouldClearOnRouteChange(state, state.currentPathname)) {
      try { history.replaceState(null, '', window.location.pathname + window.location.search); } catch (_) { /* noop */ }
      state.openPin = null;
    }
    pushPinsToAgent();
    if (state.resolutionCache[state.currentPathname] !== 'fresh') {
      scheduleResolutionForPath(state.currentPathname);
    }
    // Pending deep-link flash on first nav-committed for the target pathname.
    if (state.pendingFlashOnLoad && state.pendingPinId) {
      var pin = lookupPin(state.pendingPinId);
      if (pin && pin.dom_anchor && pin.dom_anchor.pathname === state.currentPathname) {
        performFlashAndScroll(pin);
      }
    }
    if (prevPath !== state.currentPathname) {
      // ignored — kept as anchor for future hooks
    }
  }

  function handlePinClicked(pinId) {
    var pinObj = lookupPin && lookupPin(pinId);
    if (pinObj) {
      state.openPin = pinObj;
      var dlMod = window.crit && window.crit.design && window.crit.design.deeplink;
      if (dlMod) {
        try { history.replaceState(null, '', dlMod.serializePinFragment(pinId)); } catch (_) { /* noop */ }
      }
    }
    if (threadScrollAPI && threadScrollAPI.scrollThreadToPin) {
      threadScrollAPI.scrollThreadToPin(document, pinId);
    }
    // Add transient highlight on the row.
    var sel = '[data-comment-id="' + String(pinId).replace(/"/g, '\\"') + '"]';
    var row = document.querySelector(sel);
    if (row && row.classList) {
      row.classList.add('crit-design-thread-highlight');
      setTimeout(function () {
        if (row.classList) row.classList.remove('crit-design-thread-highlight');
      }, 1500);
    }
  }

  // Drift-tray click delegation: armed when the user clicks "Re-anchor here?".
  document.addEventListener('click', function (ev) {
    if (!reanchorClickAPI) return;
    var t = ev.target;
    if (!t || typeof t.matches !== 'function') return;
    if (!t.matches('.crit-design-reanchor-btn')) return;
    var pinId = t.getAttribute('data-pin-id');
    if (!pinId) return;
    reanchorClickAPI.armReanchor(
      { state: state, post: postToAgent, toast: showToast },
      pinId,
      t,
    );
  });

  registerInstaller(function installAgentBridge() {
    if (!state.iframeWindow || !state.proxyOrigin) return;
    var protocol = window.crit && window.crit.agentProtocol;
    if (!protocol) return;
    var dispatchMod = window.crit.design.dispatch;
    var queueMod = window.crit.design.queue;
    var originMod = window.crit.design.origin;
    if (!dispatchMod || !queueMod || !originMod) return;

    _sender = queueMod.makeAgentSender({
      post: function (m) {
        var iw = state.iframeWindow;
        if (!iw) { _sender.requeue(m); return; }
        try { iw.postMessage(m, state.proxyOrigin); } catch (_) { /* noop */ }
      },
    });

    var dispatch = dispatchMod.makeMessageDispatcher({
      onAgentReady: handleAgentReady,
      onAgentError: handleAgentError,
      onSelection: handleSelection,
      onRequestAncestorMenu: handleAncestorMenu,
      onFocusState: handleFocusState,
      onRouteChange: handleRouteChange,
      onPinClicked: handlePinClicked,
      onPinResolutionResult: handlePinResolutionResult,
      onViewportApplied: handleViewportApplied,
      onHoveredAncestorLevel: function (level) {
        if (state.menuController && typeof state.menuController.setHoveredLevel === 'function') {
          state.menuController.setHoveredLevel(level);
        }
      },
    });

    var guard = originMod.makeOriginGuard({
      expectSource: state.iframeWindow,
      expectOrigin: state.proxyOrigin,
    });

    window.__critDesignMessages = [];
    window.addEventListener('message', function (ev) {
      if (!guard(ev)) return;
      window.__critDesignMessages.push(ev.data);
      try { dispatch(ev.data); } catch (e) { console.error('[design-mode] dispatch error:', e); }
    });
  });

  // ---- keyboard shortcut (p/Esc) gated on focus-state ----
  document.addEventListener('keydown', function (ev) {
    var t = ev.target;
    var localFocus = t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || (t.isContentEditable));
    if (localFocus) return;
    var sc = window.crit && window.crit.design && window.crit.design.shortcut;
    if (!sc) return;
    sc.handleShortcut(ev, {
      focusInInput: !!state.focusInInput,
      getMode: function () { return state.mode; },
      setMode: function (m) { setMode(m); },
    });
  });

  // ============================================================
  // Phase E: SSE round-start, lazy round resolution, deep-link, aria-live,
  // round-counter tooltip, ancestor menu controller wiring, Esc cancel re-anchor.
  // ============================================================
  function announceLive(msg) {
    var el = state.ariaLiveEl || document.getElementById('crit-design-aria-live');
    state.ariaLiveEl = el;
    if (!el) {
      // Fall back to the existing critDesignLive announcer (Phase B).
      var legacy = document.getElementById('critDesignLive');
      if (legacy) {
        legacy.textContent = '';
        setTimeout(function () { legacy.textContent = msg; }, 30);
      }
      return;
    }
    el.textContent = '';
    setTimeout(function () { el.textContent = msg; }, 30);
  }
  state.announceLive = announceLive;

  function lookupPin(pinId) {
    var list = state.comments || [];
    for (var i = 0; i < list.length; i++) {
      if (list[i] && list[i].id === pinId) return list[i];
    }
    return null;
  }

  // ---- pinsByRoute view derived from state.comments ----
  function pinsByRoute() {
    var out = {};
    var list = state.comments || [];
    for (var i = 0; i < list.length; i++) {
      var c = list[i];
      if (!c || !c.dom_anchor) continue;
      var p = c.dom_anchor.pathname || '/';
      (out[p] = out[p] || []).push(c);
    }
    return out;
  }

  function scheduleResolutionForPath(path) {
    var rr = window.crit && window.crit.design && window.crit.design.roundResolve;
    if (!rr) return;
    var pinsHere = (pinsByRoute()[path] || []);
    var ids = rr.pinsToResolveAtRoundStart(pinsHere, path);
    if (!ids.length) return;
    if (state.viewportInFlight) {
      if (!state.pendingResolutionPaths) state.pendingResolutionPaths = new Set();
      state.pendingResolutionPaths.add(path);
      state.resolutionCache[path] = 'queued-on-viewport';
      return;
    }
    postToAgent({ type: 'request-resolution' });
    state.resolutionCache[path] = 'in-flight';
    state.pendingByPath[path] = ids.length;
  }

  function applyRoundStart(roundN) {
    state.currentRound = roundN;
    state.resolutionCache = {};
    state.userActedThisRound = false;
    var by = pinsByRoute();
    Object.keys(by).forEach(function (path) {
      by[path].forEach(function (p) { p._roundResolved = false; });
    });
    var rcEl = document.getElementById('designRoundCounter');
    if (rcEl) rcEl.textContent = roundN > 1 ? 'Round #' + roundN : '';
    if (typeof setUIState === 'function') setUIState('reviewing');
    scheduleResolutionForPath(state.currentPathname || state.currentRoute || '/');
    announceLive('Round ' + roundN + ' started.');
  }

  // SSE subscription. /api/events emits `design-round-start` { round: N }
  // among other event types (file-changed etc. are owned by the existing
  // app.js code-review handlers and ignored here).
  registerInstaller(function installDesignSSE() {
    var es;
    try { es = new EventSource('/api/events'); } catch (_) { return; }
    es.addEventListener('design-round-start', function (ev) {
      var payload;
      try { payload = JSON.parse(ev.data); } catch (_) { return; }
      if (!payload || typeof payload.round !== 'number') return;
      applyRoundStart(payload.round);
    });
    state.designSSE = es;
  });

  // Note: prior reconcileCurrentRound polled GET /api/review-cycle (405,
  // POST-only) and could never succeed. Initial round is read from
  // /api/session at install time; SSE design-round-start handles bumps.

  // ---- deep-link activation ----
  function performFlashAndScroll(pin) {
    var threadScrollAPI = window.crit && window.crit.designModeThreadScroll;
    if (threadScrollAPI && threadScrollAPI.scrollThreadToPin) {
      threadScrollAPI.scrollThreadToPin(document, pin.id);
    }
    postToAgent({ type: 'flash-marker', pin_id: pin.id });
    state.openPin = pin;
    var dl = window.crit.design.deeplink;
    if (dl) {
      try { history.replaceState(null, '', dl.serializePinFragment(pin.id)); } catch (_) { /* noop */ }
    }
    state.pendingFlashOnLoad = false;
    state.pendingPinId = null;
    announceLive('Opened pin ' + pin.id + '.');
  }

  function activatePendingPinId() {
    var pinId = state.pendingPinId;
    if (!pinId) return;
    var pin = lookupPin(pinId);
    if (!pin) {
      announceLive('Pin ' + pinId + ' not found.');
      state.pendingPinId = null;
      return;
    }
    var targetPath = (pin.dom_anchor && pin.dom_anchor.pathname) || '/';
    if (state.currentRoute !== targetPath && state.currentPathname !== targetPath) {
      if (els && els.iframe) {
        try { els.iframe.src = proxyURL(targetPath); } catch (_) { /* noop */ }
      }
      state.currentRoute = targetPath;
      state.pendingFlashOnLoad = true;
      return;
    }
    performFlashAndScroll(pin);
  }
  state.activatePendingPinId = activatePendingPinId;

  registerInstaller(function installDeepLinkActivation() {
    // Defer until comments are loaded.
    var tries = 0;
    function attempt() {
      if (!state.pendingPinId) return;
      if (state.comments && state.comments.length) {
        activatePendingPinId();
        return;
      }
      if (++tries > 80) return; // ~20s cap
      setTimeout(attempt, 250);
    }
    attempt();
  });

  // Public helper for any code path that opens a pin (marker click, thread
  // row click, programmatic). Updates fragment via replaceState.
  function openPin(pin) {
    if (!pin) return;
    state.openPin = pin;
    var dl = window.crit.design.deeplink;
    if (dl) {
      try { history.replaceState(null, '', dl.serializePinFragment(pin.id)); } catch (_) { /* noop */ }
    }
  }
  state.openPin_ = state.openPin_ || openPin;

  // ============================================================
  // Phase E Task 16: Esc cancels re-anchor (chrome side).
  // ============================================================
  document.addEventListener('keydown', function (ev) {
    if (ev.key !== 'Escape') return;
    if (!state.reanchorPending) return;
    var t = ev.target;
    var localFocus = t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || (t.isContentEditable));
    if (localFocus) return;
    ev.preventDefault();
    postToAgent({ type: 'cancel-reanchor' });
    var reanchorClickAPI = window.crit && window.crit.designModeReanchorClick;
    if (reanchorClickAPI && reanchorClickAPI.disarmReanchor) {
      reanchorClickAPI.disarmReanchor({ state: state }, 'escape');
    } else {
      state.reanchorPending = null;
      if (state.reanchorBtn) state.reanchorBtn.disabled = false;
      if (state.reanchorTimeoutId) { clearTimeout(state.reanchorTimeoutId); state.reanchorTimeoutId = null; }
    }
  }, true);

  // ============================================================
  // Phase E Task 20: Round-counter tooltip
  // ============================================================
  registerInstaller(function bindRoundTooltip() {
    var btn = document.getElementById('designRoundCounter');
    if (!btn) return;
    var tooltipMod = window.crit && window.crit.design && window.crit.design.roundTooltip;
    if (!tooltipMod) return;
    // Make focusable for keyboard users.
    if (!btn.hasAttribute('tabindex')) btn.setAttribute('tabindex', '0');
    var tip = document.createElement('div');
    tip.className = 'crit-design-round-tooltip';
    tip.setAttribute('role', 'tooltip');
    tip.id = 'crit-design-round-tooltip';
    btn.setAttribute('aria-describedby', tip.id);
    document.body.appendChild(tip);
    state.roundTooltipEl = tip;

    function show() {
      var allPins = (state.comments || []).filter(function (c) { return c && c.dom_anchor; });
      var t = tooltipMod.composeRoundTooltip({ round: state.currentRound, pins: allPins });
      tip.textContent = 'Round ' + t.round + '. ' + t.carried + ' carried, ' + t.resolved + ' resolved, ' + t.driftedThisRound + ' drifted this round.';
      var r = btn.getBoundingClientRect();
      tip.style.left = r.left + 'px';
      tip.style.top  = (r.bottom + 6) + 'px';
      tip.classList.add('crit-design-round-tooltip--open');
    }
    function hide() { tip.classList.remove('crit-design-round-tooltip--open'); }
    btn.addEventListener('mouseenter', show);
    btn.addEventListener('mouseleave', hide);
    btn.addEventListener('focus', show);
    btn.addEventListener('blur', hide);
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
