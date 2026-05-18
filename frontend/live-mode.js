// live-mode.js — live-mode chrome controller. Vanilla JS, no build step.
//
// Boot order:
//   1. Wait for /api/session
//   2. Inject live-mode chrome into the existing header (R3) and append
//      a .crit-live-iframe-pane sibling to .main-content (R1)
//   3. Wire viewport selector, drag resize, route detection, comment list
//
// All state on window.crit.live (see contract block below).

(function () {
  'use strict';

  // ----- State namespace contract -----
  /**
   * Live-mode shared state — populated by live-mode.js and mutated by
   * postMessage handlers from the agent.
   *
   * @typedef {object} CritLiveState
   * @property {object|null} session         /api/session payload
   * @property {string[]}    routes          Pathnames seen this session
   * @property {string}      currentRoute    Currently displayed pathname
   * @property {{w:number,h:number,key:string}} viewport
   * @property {"navigate"|"pin"} mode
   * @property {object[]}    comments        Flat list (cached per route)
   * @property {boolean}     pinModeEnabled  Gated until agent reports ready
   * @property {string|null} pendingPinId    Deep-link #pin=<id> target
   */
  window.crit = window.crit || {};
  // Sub-modules (composer, dispatch, deeplink, etc.) load before this
  // file and each set `root.crit.live = root.crit.live || {}` to register
  // their sub-namespaces. That means the object often already exists by the
  // time we get here — we must MERGE defaults onto it rather than skip via
  // `||`, otherwise `state.routes` ends up undefined and recordRoute() throws
  // before boot completes.
  window.crit.live = window.crit.live || {};
  var state = window.crit.live;
  var stateDefaults = {
    session: null,
    routes: [],
    currentRoute: '/',
    viewport: { w: 1280, h: 800, key: 'desktop' },
    mode: 'navigate',
    comments: [],
    pinModeEnabled: false,
    pendingPinId: null,
    // Per-pin collapse override store (consumed by buildCommentCard via the
    // get/setCollapseOverride callbacks). Map<commentId, boolean>.
    liveCollapseOverrides: new Map(),
  };
  Object.keys(stateDefaults).forEach(function (k) {
    if (state[k] === undefined) state[k] = stateDefaults[k];
  });
  var shared = window.crit.shared;
  var utils = window.crit.liveUtils;
  var inflightAPI = (window.crit && window.crit.live && window.crit.live.inflight) || null;

  // Dedup guards for async ops triggerable from multiple sources (button
  // click + Cmd+Enter, double-click, race between Esc-then-Save, etc.).
  // Per-id Sets for comment-scoped ops; singleton flag for finish review.
  var resolveInFlight = inflightAPI ? inflightAPI.makeInFlightSet() : null;
  var replyInFlight = inflightAPI ? inflightAPI.makeInFlightSet() : null;
  var editInFlight = inflightAPI ? inflightAPI.makeInFlightSet() : null;
  var composerInFlight = inflightAPI ? inflightAPI.makeInFlightFlag() : null;
  var finishInFlight = inflightAPI ? inflightAPI.makeInFlightFlag() : null;

  var els = {};

  // Internal installer + panel-refresh registries. Sub-modules append here,
  // not by mutating window.
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
    var live = document.getElementById('critLiveAnnouncer');
    if (live) live.textContent = msg;
  }
  state.announce = announce;

  // Resilient session poll: delegates to the shared base poll, which
  // handles the 503-until-SetSession-completes window. Cap matches the
  // historical 60 * 250ms = 15s budget; the prior implementation used
  // shared.fetchJSON (which throws on 503) — the new helper polls
  // natively, no error rethrow is needed.
  async function waitForSession() {
    return await shared.waitForSession({
      url: '/api/session',
      intervalMs: 250,
      maxWaitMs: 15000,
    });
  }

  function buildShell() {
    // R1 + R3: do NOT wipe <body>. Inject controls into existing .header-*
    // slots; append iframe pane as a sibling of .main-content inside
    // .main-layout. The existing .comments-panel is reused for the
    // live-mode comment list (R5).

    // Reveal the comments-panel (existing markup is hidden by default).
    var commentsPanel = document.getElementById('commentsPanel');
    if (commentsPanel) commentsPanel.classList.remove('comments-panel-hidden');

    // Sync --header-height + --crit-header-height to the live header height.
    // app.js does this for code-review but doesn't run on /live, so without
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
      vp.id = 'liveViewportToggle';
      vp.setAttribute('aria-label', 'Viewport size');
      vp.innerHTML =
        '<button type="button" class="toggle-btn" data-viewport="mobile" aria-pressed="false" title="Mobile 390">Mobile</button>' +
        '<button type="button" class="toggle-btn" data-viewport="tablet" aria-pressed="false" title="Tablet 768">Tablet</button>' +
        '<button type="button" class="toggle-btn active" data-viewport="desktop" aria-pressed="true" title="Desktop 1280">Desktop</button>' +
        '<button type="button" class="toggle-btn" data-viewport="fit" aria-pressed="false" title="Fit pane">Fit</button>';

      // Mode toggle (R4): .diff-mode-toggle + .toggle-btn; pin uses native disabled
      var md = document.createElement('div');
      md.className = 'diff-mode-toggle';
      md.id = 'liveModeToggle';
      md.setAttribute('aria-label', 'Interaction mode');
      md.innerHTML =
        '<button type="button" class="toggle-btn active" data-mode="navigate" aria-pressed="true">Navigate</button>' +
        '<button type="button" class="toggle-btn" data-mode="pin" disabled title="Pin mode">Pin</button>';

      // Round counter: code-review writes its round indicator into
      // #headerNotify (header-left). Reuse that slot so the live-mode
      // counter appears in the same visual position as the code-review one.
      // Hide #viewedCount (used in code-review for the file-viewed counter,
      // irrelevant in live mode).
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
        rc.id = 'liveRoundCounter';
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
      bc.id = 'liveRouteChip';
      bc.innerHTML =
        '<span id="liveRouteName">/</span>';
      headerLeft.appendChild(bc);
    }

    // --- Iframe pane sibling to .main-content inside .main-layout ---
    var mainLayout = document.querySelector('.main-layout');
    if (mainLayout) {
      var pane = document.createElement('div');
      pane.className = 'crit-live-iframe-pane';
      pane.id = 'critLivePane';
      pane.innerHTML =
        '<div class="crit-live-iframe-pane-inner">' +
        '<div class="crit-live-iframe-frame" id="critLiveFrame">' +
        // No `sandbox` attribute by design — see spec security section.
        '<iframe id="critLiveIframe" title="Live target" referrerpolicy="no-referrer"></iframe>' +
        '<div class="crit-live-iframe-resizer" id="critLiveResizer" role="separator" aria-orientation="vertical" aria-label="Resize live viewport" tabindex="0"></div>' +
        '</div>' +
        '</div>';
      // Insert before #commentsPanelResizer if present so the resizer stays
      // adjacent to the comments panel (the handle's drag target). Inserting
      // before .comments-panel directly leaves the resizer stranded on the
      // LEFT edge of the iframe pane — visually disconnected from the panel
      // boundary the user wants to drag, so the resize handle never feels
      // hit-testable. Falls back to .comments-panel / append for safety.
      var resizerEl = mainLayout.querySelector('#commentsPanelResizer');
      var commentsPanelEl = mainLayout.querySelector('.comments-panel');
      if (resizerEl) mainLayout.insertBefore(pane, resizerEl);
      else if (commentsPanelEl) mainLayout.insertBefore(pane, commentsPanelEl);
      else mainLayout.appendChild(pane);
    }

    // --- Live region for a11y ---
    var live = document.createElement('div');
    live.id = 'critLiveAnnouncer';
    live.className = 'crit-live-sr-only';
    live.setAttribute('role', 'status');
    live.setAttribute('aria-live', 'polite');
    document.body.appendChild(live);

    // Dedicated aria-live announcer (used by announceLive()).
    if (!document.getElementById('crit-live-aria-live')) {
      var live2 = document.createElement('div');
      live2.id = 'crit-live-aria-live';
      live2.className = 'crit-live-sr-only';
      live2.setAttribute('role', 'status');
      live2.setAttribute('aria-live', 'polite');
      document.body.appendChild(live2);
    }

    // Skip-link to the comments panel for keyboard users.
    if (!document.querySelector('.crit-live-skip-link')) {
      var skip = document.createElement('a');
      skip.className = 'crit-live-skip-link';
      skip.href = '#commentsPanel';
      skip.textContent = 'Skip to comments';
      document.body.insertBefore(skip, document.body.firstChild);
    }

    // Cache references.
    els.viewportToggle = document.getElementById('liveViewportToggle');
    els.modeToggle = document.getElementById('liveModeToggle');
    els.routeChip = document.getElementById('liveRouteChip');
    els.routeName = document.getElementById('liveRouteName');
    els.round = document.getElementById('liveRoundCounter');
    els.pane = document.getElementById('critLivePane');
    els.frame = document.getElementById('critLiveFrame');
    els.iframe = document.getElementById('critLiveIframe');
    els.resizer = document.getElementById('critLiveResizer');
    els.commentsPanel = document.getElementById('commentsPanel');
    els.panelBody = document.getElementById('commentsPanelBody');
  }

  async function boot() {
    if (shared && shared.applyThemeFromCookie) shared.applyThemeFromCookie();
    state.session = await waitForSession();
    if (state.session.review_type !== 'live' && state.session.review_type !== 'preview') {
      console.warn('[live-mode] unexpected review_type:', state.session.review_type);
    }
    state.isPreview = state.session.review_type === 'preview';
    // Capture proxyOrigin once for the message handler. The agent
    // posts from the proxy origin; the chrome lives on the API origin and
    // accepts only that source+origin pair.
    // Preview mode: same-origin (agent served from /preview-content/).
    var s = state.session || {};
    var proxyHost = window.location.hostname || 'localhost';
    if (state.isPreview) {
      state.proxyOrigin = window.location.origin;
    } else {
      state.proxyOrigin = 'http://' + proxyHost + ':' + (s.proxy_port || 0);
    }
    buildShell();
    // Cache the iframeWindow once buildShell has inserted it.
    state.iframeWindow = els.iframe ? els.iframe.contentWindow : null;

    // Run installers in registration order.
    installers.forEach(function (fn) {
      try { fn(); } catch (e) { console.error('[live-mode] installer failed:', e); }
    });
  }

  // ============================================================
  // Viewport selector
  // ============================================================
  var VIEWPORTS = [
    { key: 'mobile',  label: 'Mobile',  w: 390,  h: 844 },
    { key: 'tablet',  label: 'Tablet',  w: 768,  h: 1024 },
    { key: 'desktop', label: 'Desktop', w: 1280, h: 800 },
    { key: 'fit',     label: 'Fit',     w: 0,    h: 0 },
  ];

  function applyViewport(vp) {
    state.viewport = { w: vp.w, h: vp.h, key: vp.key };
    // Persist viewport key in crit-settings cookie. Skip 'custom'
    // (drag-resize) so the next session restarts at the nearest preset.
    if (vp.key && vp.key !== 'custom' && shared && shared.setSetting) {
      try { shared.setSetting('live_viewport', vp.key); } catch (_) { /* noop */ }
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
    // Tell agent the viewport changed; gate request-resolution on
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
    // Hydrate persisted viewport (desktop default).
    var savedKey = (shared && shared.getSetting) ? shared.getSetting('live_viewport', 'desktop') : 'desktop';
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
  // Pin/Navigate toggle activation + set-mode dispatch to agent
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
    // Also flip marker tabindex so Tab does not jump into the iframe
    // while Pin mode is active.
    postToAgent({ type: 'set-marker-tabindex', value: next === 'pin' ? -1 : 0 });
    setActiveModeButton();
    // Announce mode change so the user knows it took effect.
    announce(next === 'pin' ? 'Pin mode' : 'Navigate mode');
  }
  state.setMode = setMode;

  registerInstaller(function installMode() {
    if (!els.modeToggle) return;
    var pinBtn = els.modeToggle.querySelector('.toggle-btn[data-mode="pin"]');
    // Keep Pin disabled until the agent reports ready, so a click
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
  // Iframe src wired to proxy_port
  // ============================================================
  function proxyURL(pathname) {
    if (state.isPreview) {
      return '/preview-content' + (pathname || '/');
    }
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
  // Drag-resize handle on iframe right edge
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
  // Route detection via postMessage
  // ============================================================
  function renderBreadcrumb() {
    if (!els.routeName) return;
    if (state.isPreview && state.session && state.session.files && state.session.files.length) {
      els.routeName.textContent = state.session.files[0].path;
    } else {
      els.routeName.textContent = state.currentRoute;
    }
  }

  function recordRoute(pathname) {
    var route = utils.normaliseRoute(pathname || '/');
    state.currentRoute = route;
    if (state.routes.indexOf(route) === -1) {
      state.routes.push(route);
    }
    renderBreadcrumb();
    refreshPanel();
    // Scroll the first card for this route into view in the comments panel.
    try {
      var sel = '.comment-card[data-live-route="' + (window.CSS && CSS.escape ? CSS.escape(route) : route) + '"]';
      var first = document.querySelector(sel);
      if (first) first.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    } catch (_) {}
    announce('Route: ' + route);
  }

  registerInstaller(function installRouteDetection() {
    // Initial breadcrumb render. Subsequent route-change messages are handled
    // by the agent bridge (handleRouteChange) which calls recordRoute().
    recordRoute(state.currentRoute);
  });

  // ============================================================
  // Round counter from /api/session.review_round.
  // /api/review-cycle is POST-only (405 on GET); session payload already
  // carries review_round, and SSE live-round-start updates it on bumps.
  // ============================================================
  registerInstaller(function installRound() {
    if (!els.round) return;
    var n = (state.session && state.session.review_round) || 1;
    state.currentRound = n;
    els.round.textContent = n > 1 ? 'Round #' + n : '';
  });

  // ============================================================
  // Comments panel — empty state + grouped renderer + filter pill +
  // expand-all + show/hide toggle + resize. All concerns are scoped to
  // the right-side panel and live in live-mode.panel-render.js. We
  // create a controller bound to local state/els here, then register its
  // installers and refresh hooks.
  // ============================================================
  var panelHelpers = (window.crit && window.crit.live && window.crit.live.panel) || null;
  var panelRenderMod = (window.crit && window.crit.live && window.crit.live.panelRender) || null;
  var panelCtl = panelRenderMod && panelRenderMod.create({
    state: state,
    els: els,
    utils: utils,
    shared: shared,
    refreshPanel: refreshPanel,
    panelHelpers: panelHelpers,
  });
  registerPanelRefresh(function () { if (panelCtl) panelCtl.panelRefresh(); });
  registerPanelRefresh(function () { if (panelCtl) panelCtl.updateUnresolvedBadge(); });
  registerInstaller(function installPanel() { refreshPanel(); });
  registerInstaller(function installFilterPillAndExpandAll() {
    if (panelCtl) panelCtl.installFilterPillAndExpandAll();
  });
  registerInstaller(function installCommentsPanelToggle() {
    if (panelCtl) panelCtl.installCommentsPanelToggle();
  });
  registerInstaller(function installCommentsPanelResize() {
    if (panelCtl) panelCtl.installCommentsPanelResize();
  });


  function startLiveTipRotation() {
    if (shared && shared.startTipRotation) shared.startTipRotation();
  }

  function stopLiveTipRotation() {
    if (shared && shared.stopTipRotation) shared.stopTipRotation();
  }

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
      stopLiveTipRotation();
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
      var copyRow = document.getElementById('promptCopyRow');
      if (copyRow) copyRow.style.display = '';
      var divider = document.getElementById('waitingDivider');
      if (divider) divider.style.display = '';
      var tipSection = document.getElementById('tipSection');
      if (tipSection) tipSection.style.display = '';
      startLiveTipRotation();
      if (overlay) overlay.classList.add('active');
    }
  }

  async function doFinishReview() {
    // Delegates DOM/clipboard/animation logic to crit.shared.runFinishReview;
    // this wrapper supplies the live-mode dedup flag and uiState wiring.
    return await shared.runFinishReview({
      dedup: finishInFlight,
      onApproved: function () { setUIState('waiting'); },
      onWaiting: function () { setUIState('waiting'); },
      onError: function (err) {
        console.error('[live-mode] finish review failed:', err);
        showToast('Failed to finish review');
      },
    });
  }
  async function resolveAllAndFinish() {
    var all = state.comments || [];
    for (var i = 0; i < all.length; i++) {
      var c = all[i];
      if (!c || c.resolved) continue;
      var path = (c.dom_anchor && c.dom_anchor.pathname) || c.path || '/';
      try {
        var rr = await fetch('/api/comment/' + encodeURIComponent(c.id) + '/resolve?path=' + encodeURIComponent(path), {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ resolved: true }),
        });
        if (!rr.ok) throw new Error('resolve-all HTTP ' + rr.status);
        c.resolved = true;
      } catch (e) {
        console.warn('[live-mode] resolve-all skipped a comment:', c && c.id, e);
      }
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
          var label = clip.querySelector('.copy-label');
          if (label) label.textContent = 'Copied';
          clip.classList.add('copied');
          clip.setAttribute('aria-label', 'Copied');
          setTimeout(function () {
            if (label) label.textContent = 'Copy';
            clip.classList.remove('copied');
            clip.setAttribute('aria-label', 'Copy prompt to clipboard');
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

  // loadAllComments delegates to the shared comments-loader module. The
  // loader refetches /api/session before reading files so a stale cached
  // `files: []` (the live daemon's pre-first-pin state captured at boot)
  // doesn't suppress every subsequent reload. Without that refresh, a
  // reply posted via `crit comment --reply-to` between rounds was
  // invisible until a full browser refresh — see the module header for
  // the full failure mode.
  var _commentsLoader = null;
  function getCommentsLoader() {
    if (_commentsLoader) return _commentsLoader;
    var mod = window.crit && window.crit.live && window.crit.live.commentsLoader;
    if (mod && typeof mod.create === 'function') {
      _commentsLoader = mod.create({ state: state, shared: shared });
    }
    return _commentsLoader;
  }
  async function loadAllComments() {
    var loader = getCommentsLoader();
    if (loader && loader.loadAllComments) return loader.loadAllComments();
    // Defensive fallback: should never happen in production (the loader
    // module is loaded by index.html before live-mode.js runs).
    return Promise.resolve();
  }

  registerInstaller(function loadComments() {
    loadAllComments().then(function () {
      refreshPanel();
      pushPinsToAgent();
    });
  });

  // ============================================================
  // Resolve / Reopen click on live pin rows. Full edit/reply parity
  // with code-review's renderCommentCard requires refactoring large
  // chunks of app.js into a shared module — deferred.
  // ============================================================
  document.addEventListener('click', function (e) {
    var btn = e.target.closest && e.target.closest('.crit-live-comment-resolve');
    if (!btn) return;
    e.stopPropagation();
    var id = btn.dataset.commentId;
    var path = btn.dataset.pathname || '/';
    if (!id) return;
    var c = (state.comments || []).find(function (x) { return x && x.id === id; });
    var resolved = c ? !c.resolved : true;
    // Dedup: second click (or click on a different button targeting the same
    // pin from the panel vs. a thread row) while the first PUT is in flight
    // would race optimistic state. Per-id Set survives the async tail even
    // if the originating button is removed by refreshPanel.
    if (resolveInFlight && resolveInFlight.has(id)) return;
    if (resolveInFlight) resolveInFlight.add(id);
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
      // Repaint the iframe overlay: live-mode-pin-filter drops resolved
      // pins from the set-pins payload, so the marker for the just-resolved
      // (or just-reopened) pin must be re-pushed in the originating tab.
      // Without this, the marker stays painted until the next chrome boot.
      pushPinsToAgent();
    }).catch(function (err) {
      showToast('Resolve failed: ' + (err && err.message || err));
    }).finally(function () {
      btn.disabled = false;
      if (resolveInFlight) resolveInFlight.delete(id);
    });
  });

  // ============================================================
  // Reply on live-mode comment rows.
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
      var card = document.querySelector('.crit-live-comment-row[data-comment-id="' + (window.CSS && CSS.escape ? CSS.escape(id) : id) + '"]');
      if (!card) return;
      var ta = card.querySelector('.crit-live-reply-textarea');
      if (!ta) return;
      if (window.crit && window.crit.shared && window.crit.shared.attachImageUploads && !ta._imageUploadsAttached) {
        window.crit.shared.attachImageUploads(ta);
        ta._imageUploadsAttached = true;
      }
      ta.focus();
      // Place cursor at end so existing draft text is preserved usefully.
      try { ta.setSelectionRange(ta.value.length, ta.value.length); } catch (_) {}
    });
  }

  function closeReplyComposer(c) {
    if (!c) return;
    c._replyOpen = false;
    c._replyDraft = '';
    if (c.id) draftMod.clearDraft('live-reply-' + c.id);
    refreshPanel();
  }

  document.addEventListener('click', function (e) {
    // Don't open the reply-create composer when the user clicked Edit/Delete
    // affordances inside an existing reply, or the parent-comment Delete
    // button (those have their own handlers and would otherwise double-fire
    // and `refreshPanel` would wipe our inline edit textarea).
    if (e.target.closest && (
      e.target.closest('.crit-live-reply-edit') ||
      e.target.closest('.crit-live-reply-delete') ||
      e.target.closest('.crit-live-comment-delete')
    )) return;
    var btn = e.target.closest && e.target.closest('.crit-live-comment-reply');
    if (!btn) return;
    e.stopPropagation();
    var id = btn.dataset.commentId;
    if (!id) return;
    var c = findCommentById(id);
    if (!c) return;
    c._replyOpen = true;
    var saved = draftMod.loadDraft('live-reply-' + id);
    if (saved && saved.body && !c._replyDraft) { c._replyDraft = saved.body; }
    refreshPanel();
    focusReplyTextareaFor(id);
  });

  document.addEventListener('click', function (e) {
    var btn = e.target.closest && e.target.closest('.crit-live-reply-cancel');
    if (!btn) return;
    e.stopPropagation();
    var id = btn.dataset.commentId;
    var c = findCommentById(id);
    if (!c) return;
    var card = btn.closest('.crit-live-comment-row');
    var ta = card && card.querySelector('.crit-live-reply-textarea');
    var dirty = ta && ta.value.trim().length > 0;
    if (dirty) {
      var ok = window.confirm('Discard reply?');
      if (!ok) return;
    }
    closeReplyComposer(c);
  });

  async function submitReply(c, pathname, body, saveBtn, errEl) {
    if (!c || !c.id || !body) return;
    // Dedup: Cmd+Enter inside the textarea synthesizes saveBtn.click(), so
    // a fast double-tap or a click+Cmd+Enter race could double-post.
    if (replyInFlight && replyInFlight.has(c.id)) return;
    if (replyInFlight) replyInFlight.add(c.id);
    if (saveBtn) saveBtn.disabled = true;
    if (errEl) { errEl.hidden = true; errEl.textContent = ''; }
    try {
      var url = '/api/comment/' + encodeURIComponent(c.id) + '/replies?path=' + encodeURIComponent(pathname || '/');
      var res = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ body: body, author: (state.config && state.config.author) || '' }),
      });
      if (!res.ok) throw new Error('Server returned ' + res.status);
      var reply = await res.json();
      c.replies = Array.isArray(c.replies) ? c.replies : [];
      c.replies.push(reply);
      c._replyOpen = false;
      c._replyDraft = '';
      if (c.id) draftMod.clearDraft('live-reply-' + c.id);
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
    } finally {
      if (replyInFlight) replyInFlight.delete(c.id);
    }
  }

  document.addEventListener('click', function (e) {
    var btn = e.target.closest && e.target.closest('.crit-live-reply-save');
    if (!btn) return;
    e.stopPropagation();
    var id = btn.dataset.commentId;
    var path = btn.dataset.pathname || '/';
    var c = findCommentById(id);
    if (!c) return;
    var card = btn.closest('.crit-live-comment-row');
    var ta = card && card.querySelector('.crit-live-reply-textarea');
    var errEl = card && card.querySelector('.crit-live-reply-error');
    var body = ta ? ta.value.trim() : '';
    if (!body) {
      if (errEl) { errEl.hidden = false; errEl.textContent = 'Reply body required'; }
      return;
    }
    submitReply(c, path, body, btn, errEl);
  });

  // ============================================================
  // Reply edit / delete — parity with code-review's `editReply` /
  // `deleteReply` (app.js: renderReplyList wires these directly on each
  // reply's edit/delete buttons). Live-mode renders the same chrome via
  // makeReplyListBuilder (see live-mode.row.js: .crit-live-reply-edit /
  // .crit-live-reply-delete) but the click wiring lived nowhere — replies
  // appeared editable but nothing happened on click.
  //
  // Endpoints are mode-agnostic:
  //   PUT    /api/comment/{id}/replies/{rid}?path=<pathname>
  //   DELETE /api/comment/{id}/replies/{rid}?path=<pathname>
  // ============================================================
  function replyMutationUrl(commentId, replyId, pathname) {
    return '/api/comment/' + encodeURIComponent(commentId) +
      '/replies/' + encodeURIComponent(replyId) +
      '?path=' + encodeURIComponent(pathname || '/');
  }

  document.addEventListener('click', function (e) {
    var btn = e.target.closest && e.target.closest('.crit-live-reply-edit');
    if (!btn) return;
    e.stopPropagation();
    var commentId = btn.dataset.commentId;
    var replyId = btn.dataset.replyId;
    if (!commentId || !replyId) return;
    var c = findCommentById(commentId);
    if (!c) return;
    var pathname = (c.dom_anchor && c.dom_anchor.pathname) || '/';
    var replyEl = document.querySelector('[data-reply-id="' + (window.CSS && CSS.escape ? CSS.escape(replyId) : replyId) + '"]');
    if (!replyEl) return;
    if (replyEl.querySelector('.crit-live-reply-edit-textarea')) return; // already editing
    var bodyEl = replyEl.querySelector('.reply-body');
    if (!bodyEl) return;
    var currentText = bodyEl.dataset.rawBody || bodyEl.textContent || '';

    var ta = document.createElement('textarea');
    ta.className = 'crit-live-reply-edit-textarea crit-live-reply-textarea';
    ta.rows = 3;
    ta.value = currentText;
    bodyEl.replaceWith(ta);
    if (window.crit && window.crit.shared && window.crit.shared.attachImageUploads) {
      window.crit.shared.attachImageUploads(ta);
    }
    ta.focus();
    try { ta.setSelectionRange(ta.value.length, ta.value.length); } catch (_) {}

    var actions = document.createElement('div');
    actions.className = 'crit-live-reply-actions';
    var saveBtn = document.createElement('button');
    saveBtn.type = 'button';
    saveBtn.className = 'btn btn-sm btn-primary';
    saveBtn.textContent = 'Save';
    var cancelBtn = document.createElement('button');
    cancelBtn.type = 'button';
    cancelBtn.className = 'btn btn-sm';
    cancelBtn.textContent = 'Cancel';
    actions.appendChild(saveBtn);
    actions.appendChild(cancelBtn);
    replyEl.appendChild(actions);

    function close() { refreshPanel(); }
    cancelBtn.addEventListener('click', function (ev) { ev.stopPropagation(); close(); });
    saveBtn.addEventListener('click', async function (ev) {
      ev.stopPropagation();
      var newBody = ta.value.trim();
      if (!newBody) return;
      saveBtn.disabled = true;
      try {
        var res = await fetch(replyMutationUrl(commentId, replyId, pathname), {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ body: newBody }),
        });
        if (!res.ok) throw new Error('Server returned ' + res.status);
      } catch (err) {
        saveBtn.disabled = false;
        showToast('Edit failed: ' + (err && err.message || err));
        return;
      }
      // Optimistic local update — comment-changed SSE refreshes canonical state.
      if (Array.isArray(c.replies)) {
        for (var i = 0; i < c.replies.length; i++) {
          if (c.replies[i] && c.replies[i].id === replyId) {
            c.replies[i].body = newBody;
            break;
          }
        }
      }
      state.userActedThisRound = true;
      close();
    });
    ta.addEventListener('keydown', function (ev) {
      if (ev.isComposing) return;
      if (ev.key === 'Enter' && (ev.ctrlKey || ev.metaKey)) {
        ev.preventDefault();
        ev.stopPropagation();
        saveBtn.click();
      } else if (ev.key === 'Escape') {
        ev.preventDefault();
        ev.stopPropagation();
        close();
      }
    });
  });

  document.addEventListener('click', async function (e) {
    var btn = e.target.closest && e.target.closest('.crit-live-reply-delete');
    if (!btn) return;
    e.stopPropagation();
    var commentId = btn.dataset.commentId;
    var replyId = btn.dataset.replyId;
    if (!commentId || !replyId) return;
    var c = findCommentById(commentId);
    if (!c) return;
    var pathname = (c.dom_anchor && c.dom_anchor.pathname) || '/';
    try {
      var res = await fetch(replyMutationUrl(commentId, replyId, pathname), { method: 'DELETE' });
      if (!res.ok) throw new Error('Server returned ' + res.status);
    } catch (err) {
      showToast('Delete failed: ' + (err && err.message || err));
      return;
    }
    if (Array.isArray(c.replies)) {
      c.replies = c.replies.filter(function (r) { return r && r.id !== replyId; });
    }
    state.userActedThisRound = true;
    refreshPanel();
  });

  // Delete top-level live pin — DELETE /api/comment/{id}?path=<pathname>.
  // Mirrors code-review's deleteComment in app.js (no confirm prompt) but
  // routes through the live-mode state path so SSE/round-state updates
  // stay consistent.
  document.addEventListener('click', async function (e) {
    var btn = e.target.closest && e.target.closest('.crit-live-comment-delete');
    if (!btn) return;
    e.stopPropagation();
    var commentId = btn.dataset.commentId;
    if (!commentId) return;
    var c = findCommentById(commentId);
    if (!c) return;
    var pathname = btn.dataset.pathname || (c.dom_anchor && c.dom_anchor.pathname) || '/';
    try {
      var res = await fetch('/api/comment/' + encodeURIComponent(commentId) + '?path=' + encodeURIComponent(pathname), { method: 'DELETE' });
      if (!res.ok) throw new Error('Server returned ' + res.status);
    } catch (err) {
      showToast('Delete failed: ' + (err && err.message || err));
      return;
    }
    if (Array.isArray(state.comments)) {
      state.comments = state.comments.filter(function (cc) { return cc && cc.id !== commentId; });
    }
    state.userActedThisRound = true;
    refreshPanel();
    pushPinsToAgent();
  });

  // Keep the in-memory draft in sync so refreshPanel doesn't drop typed text.
  // Also persist to localStorage via crit-draft module.
  document.addEventListener('input', function (e) {
    var ta = e.target;
    if (!ta || !ta.classList || !ta.classList.contains('crit-live-reply-textarea')) return;
    var card = ta.closest('.crit-live-comment-row');
    var id = card && card.dataset.commentId;
    if (!id) return;
    var c = findCommentById(id);
    if (!c) return;
    c._replyDraft = ta.value;
    draftMod.saveDraft('live-reply-' + id, { body: ta.value, savedAt: Date.now() });
  });

  document.addEventListener('keydown', function (e) {
    var ta = e.target;
    if (!ta || !ta.classList || !ta.classList.contains('crit-live-reply-textarea')) return;
    if (e.isComposing) return;
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      e.stopPropagation();
      var card = ta.closest('.crit-live-comment-row');
      var saveBtn = card && card.querySelector('.crit-live-reply-save');
      if (saveBtn) saveBtn.click();
      return;
    }
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      var card2 = ta.closest('.crit-live-comment-row');
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
  // Edit on live-mode comment rows.
  // Endpoint: PUT /api/comment/{id}?path=<pathname>
  // The row template renders an inline editor when c._editOpen is set.
  // Draft text held on c._editDraft so it survives panel re-renders.
  // ============================================================
  function focusEditTextareaFor(id) {
    requestAnimationFrame(function () {
      var card = document.querySelector('.crit-live-comment-row[data-comment-id="' + (window.CSS && CSS.escape ? CSS.escape(id) : id) + '"]');
      if (!card) return;
      var ta = card.querySelector('.crit-live-edit-textarea');
      if (!ta) return;
      if (window.crit && window.crit.shared && window.crit.shared.attachImageUploads && !ta._imageUploadsAttached) {
        window.crit.shared.attachImageUploads(ta);
        ta._imageUploadsAttached = true;
      }
      ta.focus();
      try { ta.setSelectionRange(ta.value.length, ta.value.length); } catch (_) {}
    });
  }

  function closeEditComposer(c) {
    if (!c) return;
    c._editOpen = false;
    c._editDraft = null;
    if (c.id) draftMod.clearDraft('live-edit-' + c.id);
    refreshPanel();
  }

  document.addEventListener('click', function (e) {
    var btn = e.target.closest && e.target.closest('.crit-live-comment-edit');
    if (!btn) return;
    e.stopPropagation();
    var id = btn.dataset.commentId;
    if (!id) return;
    var c = findCommentById(id);
    if (!c) return;
    c._editOpen = true;
    var savedEdit = draftMod.loadDraft('live-edit-' + id);
    c._editDraft = (savedEdit && savedEdit.body) ? savedEdit.body : (c.body || '');
    refreshPanel();
    focusEditTextareaFor(id);
  });

  document.addEventListener('click', function (e) {
    var btn = e.target.closest && e.target.closest('.crit-live-edit-cancel');
    if (!btn) return;
    e.stopPropagation();
    var id = btn.dataset.commentId;
    var c = findCommentById(id);
    if (!c) return;
    var card = btn.closest('.crit-live-comment-row');
    var ta = card && card.querySelector('.crit-live-edit-textarea');
    var dirty = ta && ta.value.trim() !== (c.body || '').trim();
    if (dirty) {
      var ok = window.confirm('Discard edit?');
      if (!ok) return;
    }
    closeEditComposer(c);
  });

  async function submitEdit(c, pathname, body, saveBtn, errEl) {
    if (!c || !c.id) return;
    // Dedup: same multi-source-trigger surface as reply submit.
    if (editInFlight && editInFlight.has(c.id)) return;
    if (editInFlight) editInFlight.add(c.id);
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
      if (c.id) draftMod.clearDraft('live-edit-' + c.id);
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
    } finally {
      if (editInFlight) editInFlight.delete(c.id);
    }
  }

  document.addEventListener('click', function (e) {
    var btn = e.target.closest && e.target.closest('.crit-live-edit-save');
    if (!btn) return;
    e.stopPropagation();
    var id = btn.dataset.commentId;
    var path = btn.dataset.pathname || '/';
    var c = findCommentById(id);
    if (!c) return;
    var card = btn.closest('.crit-live-comment-row');
    var ta = card && card.querySelector('.crit-live-edit-textarea');
    var errEl = card && card.querySelector('.crit-live-edit-error');
    var body = ta ? ta.value.trim() : '';
    if (!body) {
      if (errEl) { errEl.hidden = false; errEl.textContent = 'Body required'; }
      return;
    }
    submitEdit(c, path, body, btn, errEl);
  });

  // Keep edit draft in sync so refreshPanel doesn't drop typed text.
  // Also persist to localStorage via crit-draft module.
  document.addEventListener('input', function (e) {
    var ta = e.target;
    if (!ta || !ta.classList || !ta.classList.contains('crit-live-edit-textarea')) return;
    var card = ta.closest('.crit-live-comment-row');
    var id = card && card.dataset.commentId;
    if (!id) return;
    var c = findCommentById(id);
    if (!c) return;
    c._editDraft = ta.value;
    draftMod.saveDraft('live-edit-' + id, { body: ta.value, savedAt: Date.now() });
  });

  document.addEventListener('keydown', function (e) {
    var ta = e.target;
    if (!ta || !ta.classList || !ta.classList.contains('crit-live-edit-textarea')) return;
    if (e.isComposing) return;
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      e.stopPropagation();
      var card = ta.closest('.crit-live-comment-row');
      var saveBtn = card && card.querySelector('.crit-live-edit-save');
      if (saveBtn) saveBtn.click();
      return;
    }
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      var card2 = ta.closest('.crit-live-comment-row');
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
  // Clicking a comment row navigates iframe
  // ============================================================
  document.addEventListener('click', function (e) {
    // Don't navigate when clicking interactive controls inside the card.
    if (e.target.closest && e.target.closest('button, a, input, textarea')) return;
    var card = e.target.closest && e.target.closest('.comment-card[data-live-route]');
    if (!card) return;
    var route = utils.normaliseRoute(card.dataset.liveRoute || '/');
    // Skip iframe reassignment if already on this route — otherwise we'd
    // trigger a redundant route-change → request-resolution cycle.
    if (route === state.currentRoute) return;
    if (els && els.iframe) els.iframe.src = proxyURL(route);
    state.currentRoute = route;
    renderBreadcrumb();
  });

  document.addEventListener('keydown', function (e) {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    var t = e.target;
    if (!t || !t.classList || !t.classList.contains('comment-card')) return;
    if (!t.dataset.liveRoute) return;
    e.preventDefault();
    var route = utils.normaliseRoute(t.dataset.liveRoute || '/');
    if (route === state.currentRoute) return;
    if (els && els.iframe) els.iframe.src = proxyURL(route);
    state.currentRoute = route;
    renderBreadcrumb();
  });

  // ============================================================
  // Settings overlay: live mode mounts the same Settings overlay as
  // code review by delegating all three tabs to crit-settings-panes.js.
  // Mode-specific behaviour (no width pill; live-scoped hide-resolved)
  // is supplied via the hooks/show options.
  // ============================================================
  registerInstaller(function installSettingsOverlay() {
    var toggle = document.getElementById('settingsToggle');
    var overlay = document.getElementById('settingsOverlay');
    if (!toggle || !overlay) return;

    // Live mode persists hide-resolved under its own key so it doesn't
    // collide with code-review's preference (each mode toggles its own).
    function getHideResolved() {
      return !!(shared.getSetting && shared.getSetting('live_hideResolved', false));
    }
    function setHideResolved(v) {
      if (shared.setSetting) shared.setSetting('live_hideResolved', !!v);
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
        mode: 'live',
        vcs_name: 'live',
        review_round: sess.review_round || state.currentRound || 1,
        upstream_url: sess.upstream_url || state.upstreamURL,
      };
      if (panes.renderSettingsTab) {
        panes.renderSettingsTab(overlay.querySelector('#settingsPane'), {
          mode: 'live',
          cfg: cfg,
          hooks: {
            applyTheme: applyTheme,
            getHideResolved: getHideResolved,
            setHideResolved: setHideResolved,
            onHideResolvedChange: function () {
              // No file-list re-render in live mode; the body class
              // (toggled by setHideResolved) drives CSS visibility on pin
              // rows + comment cards.
            },
          },
        });
      }
      if (panes.renderShortcutsPane) {
        panes.renderShortcutsPane(overlay.querySelector('#shortcutsPane'), { mode: 'live' });
      }
      if (panes.renderAboutPane) {
        panes.renderAboutPane(overlay.querySelector('#aboutPane'), cfg, sessionDescriptor);
      }
    }

    // Apply persisted hide-resolved on boot so the body class is in sync
    // before the overlay is opened the first time.
    document.body.classList.toggle('hide-resolved', getHideResolved());

    // The overlay shell (open/close/Esc/?/focus-trap/sliding-underline/tab
    // click + arrow-key nav) is shared with code-review via
    // crit-settings-overlay.js. Pane content rendering happens in onOpen and
    // is re-rendered after /api/config + /api/session resolve.
    var overlayApi = window.crit && window.crit.settingsOverlay;
    if (!overlayApi || !overlayApi.install) return;
    var closeBtn = overlay.querySelector && overlay.querySelector('#settingsClose');
    overlayApi.install({
      overlay: overlay,
      toggle: toggle,
      closeBtn: closeBtn,
      initialTab: 'settings',
      onOpen: function () {
        // Ensure all tabs are visible (some flows hide tabs; reset on open).
        var tabs = overlay.querySelectorAll('.settings-tab[role="tab"]');
        for (var i = 0; i < tabs.length; i++) tabs[i].style.display = '';
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
      },
    });
  });

  // ============================================================
  // Theme — re-apply on focus (catches changes made in another tab).
  // ============================================================
  window.addEventListener('focus', function () {
    if (window.crit && window.crit.shared) window.crit.shared.applyThemeFromCookie();
  });

  // ============================================================
  // Deep-link #pin=<id> parsing — activation lives below.
  // ============================================================
  function parsePinFragment() {
    var dl = window.crit && window.crit.live && window.crit.live.deeplink;
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
  state.liveFilter = state.liveFilter || 'all';
  state.liveExpandAll = !!state.liveExpandAll;
  state.userActedThisRound = !!state.userActedThisRound;

  // ============================================================
  // Iframe load-error banner
  // ============================================================
  function showIframeError() {
    if (!els.frame) return;
    var existing = document.querySelector('.crit-live-iframe-error');
    if (existing) return;
    var box = document.createElement('div');
    box.className = 'crit-live-iframe-error';
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

  // Cross-origin redirect notice + Esc dismissal — installed by the
  // dedicated module so the message-listener and the keydown handler stay
  // co-located. Runs as an installer to ensure els.iframe / els.frame are
  // populated before the listener captures the iframe contentWindow.
  registerInstaller(function installRedirectNotice() {
    var mod = window.crit && window.crit.live && window.crit.live.redirectNotice;
    if (mod && mod.install) mod.install({ els: els, shared: shared });
  });

  // ============================================================
  // Agent ↔ chrome wiring: dispatcher, queue, origin guard, composer,
  // ancestor menu, focus state, save flow.
  // ============================================================
  var _sender = null;
  function postToAgent(m) {
    if (_sender) _sender.send(m);
  }
  state.postToAgent = postToAgent;

  // Delegates to the unified crit.shared.showToast helper (crit-shared.js).
  // Previously called .remove() directly on the element — violated
  // frontend-js.md "Never call .remove() on elements with CSS exit
  // animations". The shared helper uses transitionend cleanup.
  function showToast(message) {
    if (window.crit && window.crit.shared && window.crit.shared.showToast) {
      // Prior behaviour used a 4s timeout; preserved here for live-mode.
      window.crit.shared.showToast(message, { timeout: 4000 });
    }
  }
  state.showToast = showToast;

  // ---- composer ----
  function ensureComposerHost() {
    var h = document.querySelector('.crit-live-composer-host');
    if (h) return h;
    h = document.createElement('div');
    h.className = 'crit-live-composer-host';
    var panel = document.querySelector('.comments-panel') || document.body;
    panel.appendChild(h);
    return h;
  }

  // ===== Draft persistence (delegates to window.crit.draft) =====
  var draftMod = window.crit.draft;
  var activeComposerKey = null;

  function composerDraftKey(domAnchor) {
    // Stable key per page path + selector so re-opening the same element restores text.
    var base = (domAnchor && domAnchor.pathname || '/') + '|' + (domAnchor && domAnchor.css_selector || '');
    // Simple hash to keep key short.
    var h = 0;
    for (var i = 0; i < base.length; i++) { h = ((h << 5) - h + base.charCodeAt(i)) | 0; }
    return 'live-new-' + (h >>> 0).toString(36);
  }

  function closeComposer() {
    if (activeComposerKey) { draftMod.clearDraft(activeComposerKey); activeComposerKey = null; }
    var h = document.querySelector('.crit-live-composer-host');
    if (h) { h.innerHTML = ''; delete h.dataset.active; }
    // Drop the sustained outline on the captured element.
    try { postToAgent({ type: 'clear-highlight' }); } catch (_) {}
    // Intentional: do not change state.mode here — keep Pin mode for rapid pinning.
  }

  async function saveComposer(domAnchor) {
    var host = document.querySelector('.crit-live-composer-host');
    if (!host) return;
    var bodyEl = host.querySelector('.crit-live-composer-body');
    var body = bodyEl ? bodyEl.value.trim() : '';
    var errEl = host.querySelector('.crit-live-composer-error');
    if (errEl) { errEl.hidden = true; errEl.textContent = ''; }
    if (!body) {
      if (errEl) { errEl.hidden = false; errEl.textContent = 'Body required'; }
      return;
    }
    // Dedup: Cmd+Enter and Save-button click both call this. A fast
    // sequence would otherwise create two pins.
    if (composerInFlight && composerInFlight.busy()) return;
    if (composerInFlight) composerInFlight.set();
    var saveBtn = host.querySelector('.crit-live-composer-save');
    if (saveBtn) saveBtn.disabled = true;
    try {
      var url = '/api/file/comments?path=' + encodeURIComponent(domAnchor.pathname);
      var res = await shared.fetchJSON(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ start_line: 0, end_line: 0, body: body, dom_anchor: domAnchor, author: (state.config && state.config.author) || '' }),
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
      if (composerInFlight) composerInFlight.clear();
    }
  }

  function optimisticInsertComment(pathname, comment) {
    if (!comment || !comment.id) return;
    var c = Object.assign({}, comment, { path: pathname });
    // Stamp the round the pin was created in. Drift PUTs were removed,
    // but the stamp is still consulted by round-resolve bookkeeping so
    // freshly created pins aren't double-counted on route-change scans.
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
        // Same _createdInRound stamping as loadAllComments — see comment
        // there. refreshCommentsForRoute fires immediately after a pin POST
        // (saveComposer) and would otherwise wipe the optimistic stamp.
        c._createdInRound = c.review_round || state.currentRound || 1;
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
    var sizeMod = window.crit.live.size;
    if (sizeMod && sizeMod.selectionTooLarge(domAnchor)) {
      showToast('selection too large to save');
      return;
    }
    var host = ensureComposerHost();
    host.innerHTML = window.crit.live.composer.renderComposerHTML(domAnchor);
    host.dataset.active = '1';
    // Ask the agent to keep the captured element outlined while the
    // composer is open. Cleared by closeComposer (Save / Cancel / Esc).
    if (domAnchor && domAnchor.css_selector) {
      try { postToAgent({ type: 'keep-highlight', selector: domAnchor.css_selector }); } catch (_) {}
    }
    var ta = host.querySelector('.crit-live-composer-body');
    if (ta) {
      if (window.crit && window.crit.shared && window.crit.shared.attachImageUploads) {
        window.crit.shared.attachImageUploads(ta);
      }
      // Restore draft if one exists for this element.
      activeComposerKey = composerDraftKey(domAnchor);
      var existing = draftMod.loadDraft(activeComposerKey);
      if (existing && existing.body) { ta.value = existing.body; }
      ta.addEventListener('input', function () {
        draftMod.saveDraft(activeComposerKey, { body: ta.value, savedAt: Date.now() });
      });
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
    var cancelBtn = host.querySelector('.crit-live-composer-cancel');
    if (cancelBtn) cancelBtn.addEventListener('click', closeComposer);
    var saveBtn = host.querySelector('.crit-live-composer-save');
    if (saveBtn) saveBtn.addEventListener('click', function () { saveComposer(domAnchor); });
  }

  // ---- ancestor menu ----
  function closeAncestorMenu() {
    var h = document.querySelector('.crit-live-ancestor-menu-host');
    if (h) h.remove();
  }
  function closeAncestorMenuOnce(ev) {
    if (ev.target.closest && ev.target.closest('.crit-live-ancestor-menu-host')) return;
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
    wrap.className = 'crit-live-ancestor-menu-host';
    wrap.style.cssText = 'position:fixed;left:' + x + 'px;top:' + y + 'px;z-index:2147483600;visibility:hidden;';
    wrap.innerHTML = window.crit.live.menu.renderAncestorMenuHTML(options);
    document.body.appendChild(wrap);
    var clamped = window.crit.live.menu.clampMenuPosition({
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
      var btn = ev.target.closest && ev.target.closest('.crit-live-ancestor-menu-item');
      if (!btn) return;
      var level = Number(btn.dataset.level);
      postToAgent({ type: 'commit-ancestor-selection', level: level });
      closeAncestorMenu();
    });

    // Keyboard nav controller + fade-in.
    var menuMod = window.crit && window.crit.live && window.crit.live.menuController;
    if (menuMod && menuMod.createMenuController) {
      var inner = wrap.querySelector('.crit-live-ancestor-menu') || wrap.firstElementChild;
      var items = wrap.querySelectorAll('.crit-live-ancestor-menu-item');
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
            el.classList.toggle('crit-live-ancestor-menu-item--active', i === j);
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
        if (inner && inner.classList) inner.classList.add('crit-live-ancestor-menu--open');
      });
    }

    setTimeout(function () {
      document.addEventListener('click', closeAncestorMenuOnce, { once: true, capture: true });
    }, 0);
  }

  function handleAgentReady() {
    state.agentReady = true;
    if (_sender) _sender.markReady();
    // Now that the agent is listening, enable the Pin toggle.
    if (els.modeToggle) {
      var pinBtn = els.modeToggle.querySelector('.toggle-btn[data-mode="pin"]');
      if (pinBtn) {
        pinBtn.removeAttribute('disabled');
        pinBtn.removeAttribute('aria-disabled');
        pinBtn.removeAttribute('title');
      }
    }
    pushPinsToAgent();

    // Register as the active ContentRenderer so chrome modules (comment
    // list click-to-scroll, deeplinks) can interact with the live iframe
    // through the unified renderer interface.
    if (window.crit && window.crit.renderer) {
      window.crit.renderer.register({
        scrollToAnchor: function (anchor) {
          if (anchor.type !== 'dom') return Promise.resolve();
          // Use keep-highlight which causes the agent to find and scroll
          // the element into view. Resolve after a timeout since the agent
          // does not ack scroll completion.
          return new Promise(function (resolve) {
            try {
              postToAgent({ type: 'keep-highlight', selector: anchor.selector });
            } catch (_) { /* noop */ }
            setTimeout(resolve, 500);
          });
        },

        highlightAnchor: function (anchor) {
          if (anchor.type !== 'dom') return Promise.resolve();
          try {
            postToAgent({ type: 'keep-highlight', selector: anchor.selector });
          } catch (_) { /* noop */ }
          return Promise.resolve();
        },

        clearHighlight: function () {
          try {
            postToAgent({ type: 'clear-highlight' });
          } catch (_) { /* noop */ }
        },

        onAnnotationIntent: function () {
          // Live mode's pin flow IS the annotation intent. The pin
          // composer UX is already built and triggered by the agent's
          // selection message — no additional wiring needed here.
          return function () {};
        },

        getMode: function () {
          return 'live';
        },

        getAnchorType: function () {
          return 'dom';
        },
      });
    }
  }
  function handleAgentError(e) {
    // Screenshot capture is best-effort. Real-world pages frequently use CSS
    // features html2canvas can't parse (CSS Color Module Level 4 `color()`,
    // wide-gamut variants, etc.), and the capture path already degrades to an
    // empty thumbnail — pins still work without it. Surfacing a scary toast
    // for every such page makes the feature feel broken when it isn't.
    // The agent still emits the AGENT_ERROR for the E2E contract; we just
    // don't toast it.
    if (e && e.kind === 'capture-failed') {
      try { console.warn('[live-mode] screenshot capture skipped:', e.message); } catch (_) {}
      return;
    }
    showToast(e.kind + ': ' + e.message);
  }
  function handleFocusState(b) {
    state.focusInInput = !!b;
  }

  // ============================================================
  // Pins, resolution gate, re-anchor flow.
  // ============================================================
  var pinStateAPI = window.crit && window.crit.live && window.crit.live.pinState;
  var pinFilterAPI = window.crit && window.crit.live && window.crit.live.pinFilter;
  var threadScrollAPI = window.crit && window.crit.live && window.crit.live.threadScroll;
  var reanchorClickAPI = window.crit && window.crit.live && window.crit.live.reanchorClick;
  var reanchorPutAPI = window.crit && window.crit.live && window.crit.live.reanchorPut;
  var resolutionGateAPI = window.crit && window.crit.live && window.crit.live.resolutionGate;

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
    return state.currentRoute || '/';
  }

  function pushPinsToAgent() {
    if (!state.agentReady || !pinFilterAPI) return;
    var all = (state.comments || []).filter(function (c) { return c && c.dom_anchor; }).map(function (c) {
      return { id: c.id, pin_number: c.pin_number || 0, dom_anchor: c.dom_anchor, resolved: !!c.resolved };
    });
    var pins = pinFilterAPI.filterPinsForPath(all, currentPathname());
    postToAgent({ type: 'set-pins', pins: pins });
    if (state.pinState) state.pinState.setComments(state.comments || []);
  }

  function handlePinResolutionResult(msg) {
    var prev = lookupPin && lookupPin(msg && msg.pin_id);
    if (state.pinState) state.pinState.applyResolution(msg);
    if (prev) {
      var path2 = (prev.dom_anchor && prev.dom_anchor.pathname) || state.currentRoute || '/';
      var inActiveScan = typeof state.pendingByPath[path2] === 'number' &&
                         state.pendingByPath[path2] > 0;
      if (inActiveScan && !prev._roundResolved) {
        // Drift detection PUT was removed — daemon no longer emits the
        // drift bit on live pins. We still bookkeep
        // pendingByPath so the resolution-gate / re-anchor flow stays
        // accurate for route changes.
        prev._roundResolved = true;
        state.pendingByPath[path2] = Math.max(0, state.pendingByPath[path2] - 1);
        if (state.pendingByPath[path2] === 0) {
          state.resolutionCache[path2] = 'fresh';
          delete state.pendingByPath[path2];
        }
      }
    }
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
    var prevPath = state.currentRoute;
    recordRoute(msg.pathname);
    var dl = window.crit && window.crit.live && window.crit.live.deeplink;
    if (dl && dl.shouldClearOnRouteChange(state, state.currentRoute)) {
      try { history.replaceState(null, '', window.location.pathname + window.location.search); } catch (_) { /* noop */ }
      state.openPin = null;
    }
    pushPinsToAgent();
    if (state.resolutionCache[state.currentRoute] !== 'fresh') {
      scheduleResolutionForPath(state.currentRoute);
    }
    if (state.pendingFlashOnLoad && state.pendingPinId) {
      var pin = lookupPin(state.pendingPinId);
      if (pin && pin.dom_anchor && pin.dom_anchor.pathname === state.currentRoute) {
        performFlashAndScroll(pin);
      }
    }
    if (prevPath !== state.currentRoute) {
      // ignored — kept as anchor for future hooks
    }
  }

  function handlePinClicked(pinId) {
    var pinObj = lookupPin && lookupPin(pinId);
    if (pinObj) {
      state.openPin = pinObj;
      var dlMod = window.crit && window.crit.live && window.crit.live.deeplink;
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
      row.classList.add('crit-live-thread-highlight');
      setTimeout(function () {
        if (row.classList) row.classList.remove('crit-live-thread-highlight');
      }, 1500);
    }
  }

  // Re-anchor click delegation: armed when the user clicks "Re-anchor here?".
  document.addEventListener('click', function (ev) {
    if (!reanchorClickAPI) return;
    var t = ev.target;
    if (!t || typeof t.matches !== 'function') return;
    if (!t.matches('.crit-live-reanchor-btn')) return;
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
    var dispatchMod = window.crit.live.dispatch;
    var queueMod = window.crit.live.queue;
    var originMod = window.crit.live.origin;
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

    window.__critLiveMessages = [];
    window.addEventListener('message', function (ev) {
      if (!guard(ev)) return;
      window.__critLiveMessages.push(ev.data);
      try { dispatch(ev.data); } catch (e) { console.error('[live-mode] dispatch error:', e); }
    });
  });

  // ---- keyboard shortcut (p/Esc) gated on focus-state ----
  document.addEventListener('keydown', function (ev) {
    var t = ev.target;
    var localFocus = t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || (t.isContentEditable));
    if (localFocus) return;
    var sc = window.crit && window.crit.live && window.crit.live.shortcut;
    if (!sc) return;
    sc.handleShortcut(ev, {
      focusInInput: !!state.focusInInput,
      getMode: function () { return state.mode; },
      setMode: function (m) { setMode(m); },
      // Shift+F → click the finishBtn so the "no changes this round"
      // overlay + dedup guard inside the click handler fire just like a
      // mouse click. Skip when the button isn't installed yet or the UI
      // is already in a non-reviewing state (Waiting for agent, etc).
      finishReview: function () {
        var btn = document.getElementById('finishBtn');
        if (!btn || btn.disabled) return;
        if (state.uiState && state.uiState !== 'reviewing') return;
        btn.click();
      },
    });
  });

  // ============================================================
  // Lazy round resolution, deep-link activation, aria-live announcer,
  // round-counter tooltip, ancestor menu controller wiring, Esc cancel re-anchor.
  // ============================================================
  function announceLive(msg) {
    var el = state.ariaLiveEl || document.getElementById('crit-live-aria-live');
    state.ariaLiveEl = el;
    if (!el) {
      // Fall back to the existing critLiveAnnouncer announcer.
      var legacy = document.getElementById('critLiveAnnouncer');
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
    var rr = window.crit && window.crit.live && window.crit.live.roundResolve;
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

  // SSE subscription + per-round-start state reset. /api/events emits
  // `live-round-start` { round: N }; file-changed and other event kinds
  // are owned by app.js's code-review handlers and ignored here.
  var sseMod = window.crit && window.crit.live && window.crit.live.sse;
  var sseCtl = sseMod && sseMod.create({
    state: state,
    pinsByRoute: pinsByRoute,
    scheduleResolutionForPath: scheduleResolutionForPath,
    announceLive: announceLive,
    setUIState: setUIState,
    // comments-changed handler: re-fetch the canonical comment list so
    // CLI-driven mutations (`crit comment --reply-to`, etc.) and other
    // client edits surface live without a manual refresh. refreshPanel
    // already does granular DOM upsert (see commit 93b19fe), so this
    // preserves scroll/focus inside open reply composers.
    reloadComments: function () {
      return loadAllComments().then(function () {
        refreshPanel();
        pushPinsToAgent();
      });
    },
    // Reload the proxied target page on round transition so reviewers see
    // the agent's freshly-rendered UI. Same-origin between agent and
    // proxied page is guaranteed by proxy.go, so contentWindow.location
    // .reload() is the cleanest path; fall back to re-setting iframe.src
    // (with a cache-buster) if contentWindow access throws — defensive
    // against detached frames during teardown.
    reloadIframe: function () {
      if (!els || !els.iframe) return;
      try {
        var w = els.iframe.contentWindow;
        if (w && w.location && typeof w.location.reload === 'function') {
          w.location.reload();
          return;
        }
      } catch (_) { /* fall through to src reset */ }
      try {
        var url = els.iframe.src || proxyURL(state.currentRoute || '/');
        var sep = url.indexOf('?') >= 0 ? '&' : '?';
        els.iframe.src = url + sep + '_critRoundReload=' + Date.now();
      } catch (_) { /* noop */ }
    },
  });
  registerInstaller(function installLiveSSE() {
    if (sseCtl) sseCtl.install();
  });

  // Note: prior reconcileCurrentRound polled GET /api/review-cycle (405,
  // POST-only) and could never succeed. Initial round is read from
  // /api/session at install time; SSE live-round-start handles bumps.

  // ---- deep-link activation ----
  function performFlashAndScroll(pin) {
    var threadScrollAPI = window.crit && window.crit.live && window.crit.live.threadScroll;
    if (threadScrollAPI && threadScrollAPI.scrollThreadToPin) {
      threadScrollAPI.scrollThreadToPin(document, pin.id);
    }
    postToAgent({ type: 'flash-marker', pin_id: pin.id });
    state.openPin = pin;
    var dl = window.crit.live.deeplink;
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
    if (state.currentRoute !== targetPath) {
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
    var dl = window.crit.live.deeplink;
    if (dl) {
      try { history.replaceState(null, '', dl.serializePinFragment(pin.id)); } catch (_) { /* noop */ }
    }
  }
  state.openPin_ = state.openPin_ || openPin;

  // ============================================================
  // Esc cancels re-anchor (chrome side).
  // ============================================================
  document.addEventListener('keydown', function (ev) {
    if (ev.key !== 'Escape') return;
    if (!state.reanchorPending) return;
    var t = ev.target;
    var localFocus = t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || (t.isContentEditable));
    if (localFocus) return;
    ev.preventDefault();
    postToAgent({ type: 'cancel-reanchor' });
    var reanchorClickAPI = window.crit && window.crit.live && window.crit.live.reanchorClick;
    if (reanchorClickAPI && reanchorClickAPI.disarmReanchor) {
      reanchorClickAPI.disarmReanchor({ state: state }, 'escape');
    } else {
      state.reanchorPending = null;
      if (state.reanchorBtn) state.reanchorBtn.disabled = false;
      if (state.reanchorTimeoutId) { clearTimeout(state.reanchorTimeoutId); state.reanchorTimeoutId = null; }
    }
  }, true);

  // ============================================================
  // Round-counter tooltip
  // ============================================================
  registerInstaller(function bindRoundTooltip() {
    var btn = document.getElementById('liveRoundCounter');
    if (!btn) return;
    var tooltipMod = window.crit && window.crit.live && window.crit.live.roundTooltip;
    if (!tooltipMod) return;
    // Make focusable for keyboard users.
    if (!btn.hasAttribute('tabindex')) btn.setAttribute('tabindex', '0');
    var tip = document.createElement('div');
    tip.className = 'crit-live-round-tooltip';
    tip.setAttribute('role', 'tooltip');
    tip.id = 'crit-live-round-tooltip';
    btn.setAttribute('aria-describedby', tip.id);
    document.body.appendChild(tip);
    state.roundTooltipEl = tip;

    function show() {
      var allPins = (state.comments || []).filter(function (c) { return c && c.dom_anchor; });
      var t = tooltipMod.composeRoundTooltip({ round: state.currentRound, pins: allPins });
      tip.textContent = 'Round ' + t.round + '. ' + t.carried + ' carried, ' + t.resolved + ' resolved.';
      var r = btn.getBoundingClientRect();
      tip.style.left = r.left + 'px';
      tip.style.top  = (r.bottom + 6) + 'px';
      tip.classList.add('crit-live-round-tooltip--open');
    }
    function hide() { tip.classList.remove('crit-live-round-tooltip--open'); }
    btn.addEventListener('mouseenter', show);
    btn.addEventListener('mouseleave', hide);
    btn.addEventListener('focus', show);
    btn.addEventListener('blur', hide);
  });

  // ============================================================
  // Lock window.crit.live contract — submodules must use the documented
  // namespace (registered before this file loaded) instead of mutating it
  // here.
  // ============================================================
  try {
    Object.defineProperty(window.crit, 'live', {
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
