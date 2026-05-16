// crit-agent.js — runs inside the proxied (user app) origin. Communicates
// with the chrome (design page) via window.parent.postMessage. Origin and
// source are validated on every inbound message.
'use strict';
(function () {
  if (window.__critAgentBooted) return;
  window.__critAgentBooted = true;

  var protocol = window.crit && window.crit.agentProtocol;
  if (!protocol) {
    // Protocol script failed to load; bail silently to avoid breaking the user app.
    return;
  }
  var A2C = protocol.A2C;
  var C2A = protocol.C2A;
  var validateMessage = protocol.validateMessage;

  var utils = window.crit && window.crit.agent && window.crit.agent.anchorUtils;
  var markersAPI = window.crit && window.crit.agent && window.crit.agent.markers;
  var batcherAPI = window.crit && window.crit.agent && window.crit.agent.batcher;
  var resolutionAPI = window.crit && window.crit.agent && window.crit.agent.resolution;
  var ReanchorStateCtor = window.crit && window.crit.agent && window.crit.agent.reanchorState && window.crit.agent.reanchorState.ReanchorState;

  // Derive the API origin (where the chrome lives) from the agent <script> tag URL.
  // This is the only origin we accept inbound messages from and post to.
  function guessApiOriginFromAgentTag() {
    var scripts = document.querySelectorAll('script[src*="/crit-agent.js"]');
    for (var i = 0; i < scripts.length; i++) {
      try { return new URL(scripts[i].src).origin; } catch (_) { /* ignore */ }
    }
    return null;
  }
  var expectedApiOrigin = guessApiOriginFromAgentTag();
  if (!expectedApiOrigin) {
    return;
  }

  var state = {
    mode: 'navigate',
    pointer: { x: 0, y: 0 },
    overlayEl: null,
    pendingSelection: null,
    pendingAncestor: null,
    expectedApiOrigin: expectedApiOrigin,
    // Phase D
    overlay: null,            // { root, markersById }
    pins: [],                 // last set-pins payload (current pathname only)
    batcher: null,            // MutationBatcher instance
    observer: null,           // MutationObserver
    reanchor: ReanchorStateCtor ? new ReanchorStateCtor() : null,
    routePathname: typeof location !== 'undefined' ? location.pathname : '',
  };
  window.__critAgentState = state;

  function postToParent(msg) {
    try { window.parent.postMessage(msg, expectedApiOrigin); } catch (_) { /* noop */ }
  }

  // ---------- Phase D: marker overlay + MutationObserver ----------
  function bootMarkers() {
    // Inline marker CSS — fetched cross-origin from API port.
    try {
      fetch(expectedApiOrigin + '/agent-marker.css', { credentials: 'omit' })
        .then(function (res) { return res.ok ? res.text() : ''; })
        .then(function (css) {
          if (!css) return;
          var style = document.createElement('style');
          style.setAttribute('data-crit-marker-css', '1');
          style.textContent = css;
          document.head.appendChild(style);
        })
        .catch(function () { /* non-fatal */ });
    } catch (_) { /* ignore */ }
    if (markersAPI && markersAPI.createOverlay && document.body) {
      state.overlay = markersAPI.createOverlay(document);
    }
  }

  function repositionResolvedPins() {
    if (!state.overlay || !markersAPI) return;
    var markers = [];
    state.overlay.markersById.forEach(function (m) {
      if (m.status === 'resolved' || m.status === 'drifted-recoverable') {
        markers.push({ target: m.element || null, el: m.el });
      }
    });
    markersAPI.applyRects(markers, window);
  }

  function resolveAllPins() {
    if (!resolutionAPI || !state.overlay) return;
    resolutionAPI.resolveAllAndEmit({
      pins: state.pins,
      pathname: window.location.pathname,
      document: document,
      utils: utils,
      post: postToParent,
      onResolved: function (pinId, element, status) {
        var m = state.overlay.markersById.get(pinId);
        if (m) { m.element = element; m.status = status; }
      },
    });
    repositionResolvedPins();
  }

  function startMutationLoop() {
    if (!batcherAPI || !batcherAPI.MutationBatcher) return;
    var batcher = new batcherAPI.MutationBatcher({
      onDrain: function (count, fullReresolve) {
        if (fullReresolve) resolveAllPins();
        else repositionResolvedPins();
      },
    });
    state.batcher = batcher;
    if (typeof MutationObserver !== 'undefined' && document.body) {
      state.observer = new MutationObserver(function (records) { batcher.enqueue(records); });
      // Spec §Marker rendering: childList + subtree only. NO attribute observation.
      state.observer.observe(document.body, {
        childList: true,
        subtree: true,
        attributes: false,
      });
    }
  }

  function onSetPins(pins) {
    if (!state.overlay || !markersAPI) return;
    // Full rebuild semantics for v1.
    state.overlay.markersById.forEach(function (m) {
      if (m.el && m.el.parentElement) m.el.parentElement.removeChild(m.el);
    });
    state.overlay.markersById.clear();
    state.pins = Array.isArray(pins) ? pins.slice() : [];
    state.pins.forEach(function (pin, idx) {
      var el = markersAPI.makeMarker(document, pin, idx);
      el.addEventListener('click', function () {
        postToParent({ type: A2C.PIN_CLICKED, pin_id: pin.id });
      });
      el.addEventListener('keydown', function (e) {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          postToParent({ type: A2C.PIN_CLICKED, pin_id: pin.id });
        }
      });
      state.overlay.root.appendChild(el);
      if (state.mode === 'pin') el.setAttribute('tabindex', '-1');
      state.overlay.markersById.set(pin.id, {
        el: el, anchor: pin.dom_anchor, status: 'resolved', element: null,
      });
    });
    resolveAllPins();
  }

  function clearMarkersOnRouteChange() {
    if (!state.overlay) return;
    state.overlay.markersById.forEach(function (m) {
      if (m.el && m.el.parentElement) m.el.parentElement.removeChild(m.el);
    });
    state.overlay.markersById.clear();
    state.pins = [];
  }

  function notifyRouteChange() {
    var pathname = window.location.pathname;
    var search = window.location.search;
    var hash = window.location.hash;
    if (pathname === state.routePathname) return;
    state.routePathname = pathname;
    clearMarkersOnRouteChange();
    // Element from prior route is gone; drop the sustained highlight so we
    // don't leak class state between SPA navigations.
    try { onClearHighlight(); } catch (_) {}
    postToParent({ type: A2C.ROUTE_CHANGE, pathname: pathname, search: search, hash: hash });
    if (state.batcher) {
      state.batcher.pause(200);
      setTimeout(function () { state.batcher.scheduleCatchUpIfNeeded(); }, 210);
    }
  }

  // Hook history events for SPA route detection.
  (function hookHistory() {
    try {
      var origPush = history.pushState;
      var origReplace = history.replaceState;
      history.pushState = function () {
        var r = origPush.apply(this, arguments);
        try { notifyRouteChange(); } catch (_) {}
        return r;
      };
      history.replaceState = function () {
        var r = origReplace.apply(this, arguments);
        try { notifyRouteChange(); } catch (_) {}
        return r;
      };
      window.addEventListener('popstate', notifyRouteChange);
      window.addEventListener('hashchange', notifyRouteChange);
    } catch (_) { /* ignore */ }
  })();

  // Boot Phase D pieces, then signal ready.
  if (document.body) {
    bootMarkers();
    startMutationLoop();
  } else {
    document.addEventListener('DOMContentLoaded', function () {
      bootMarkers();
      startMutationLoop();
    });
  }

  // Boot signal
  postToParent({ type: A2C.AGENT_READY });

  // Inbound listener — strict origin + source guard.
  window.addEventListener('message', function (ev) {
    if (ev.source !== window.parent) return;
    if (ev.origin !== expectedApiOrigin) return;
    var v = validateMessage(ev.data);
    if (!v.ok) return;
    onCommand(ev.data);
  });

  function onCommand(msg) {
    switch (msg.type) {
      case C2A.SET_MODE: setMode(msg.value); break;
      case C2A.COMMIT_ANCESTOR_SELECTION: commitAncestor(msg.level); break;
      case C2A.CANCEL_ANCESTOR_SELECTION: cancelAncestor(); break;
      case C2A.SET_PINS: onSetPins(msg.pins); break;
      case C2A.REQUEST_RESOLUTION: resolveAllPins(); break;
      case C2A.ENTER_REANCHOR_MODE: onEnterReanchor(msg.pin_id); break;
      case C2A.SET_VIEWPORT: onSetViewport(msg.width, msg.height); break;
      case C2A.FLASH_MARKER: onFlashMarker(msg.pin_id); break;
      case C2A.CANCEL_REANCHOR: onCancelReanchor(); break;
      case C2A.SET_MARKER_TABINDEX: onSetMarkerTabindex(msg.value); break;
      case C2A.KEEP_HIGHLIGHT: onKeepHighlight(msg.selector); break;
      case C2A.CLEAR_HIGHLIGHT: onClearHighlight(); break;
      default: break;
    }
  }

  // M11: keep an outline on the clicked element while the chrome's composer
  // is open so the user can see what they're commenting on. Auto-clears on
  // route change (the element is gone) and on explicit CLEAR_HIGHLIGHT
  // (Save/Cancel/Esc/dismiss).
  function onKeepHighlight(selector) {
    onClearHighlight(); // ensure only one element highlighted at a time
    if (!selector) return;
    try {
      var el = document.querySelector(selector);
      if (!el) return;
      el.classList.add('crit-design-pending-highlight');
      state._pendingHighlightEl = el;
      // Suppress the dashed hover overlay while the user is composing —
      // chasing the cursor at this point is just visual noise. Overlay
      // resumes on CLEAR_HIGHLIGHT (Save / Cancel / Esc / dismiss).
      state.suppressHover = true;
      hideOverlay();
    } catch (_) { /* invalid selector */ }
  }
  function onClearHighlight() {
    var el = state._pendingHighlightEl;
    state.suppressHover = false;
    if (!el) return;
    try { el.classList.remove('crit-design-pending-highlight'); } catch (_) {}
    state._pendingHighlightEl = null;
  }

  // Phase E: flash a marker for 1500ms when chrome activates a deep-link.
  function onFlashMarker(pinId) {
    var overlay = state.overlay;
    var byId = overlay && overlay.markersById;
    if (!byId) return;
    var entry = byId.get ? byId.get(pinId) : byId[pinId];
    if (!entry || !entry.el) return;
    try {
      entry.el.classList.add('crit-design-marker--flash');
      setTimeout(function () {
        try { entry.el.classList.remove('crit-design-marker--flash'); } catch (_) { /* noop */ }
      }, 1500);
    } catch (_) { /* noop */ }
  }

  // Phase E: chrome cancels an armed re-anchor (Esc).
  function onCancelReanchor() {
    if (state.reanchor && typeof state.reanchor.disarm === 'function') {
      state.reanchor.disarm();
    }
    try { document.documentElement.classList.remove('crit-design-reanchor-active'); } catch (_) { /* noop */ }
  }

  // Phase E: chrome flips marker tabindex when entering/leaving Pin mode so
  // Tab order follows the user's intent instead of trapping in the iframe.
  function onSetMarkerTabindex(value) {
    if (state.overlay && markersAPI && markersAPI.setMarkersTabindex) {
      markersAPI.setMarkersTabindex(state.overlay.markersById, String(value));
    } else {
      try {
        var nodes = document.querySelectorAll('.crit-design-marker');
        for (var i = 0; i < nodes.length; i++) nodes[i].setAttribute('tabindex', String(value));
      } catch (_) { /* noop */ }
    }
  }

  function onEnterReanchor(pinId) {
    if (!state.reanchor) return;
    state.reanchor.arm(pinId);
    try { document.documentElement.classList.add('crit-design-reanchor-active'); } catch (_) {}
    // Ensure click capture is active so the next click is consumed regardless
    // of current mode. We don't flip state.mode — the chrome's mode toggle is
    // independent of re-anchor capture (one-shot).
    if (state.mode !== 'pin') {
      attachHoverListeners();
      attachClickCapture();
    }
  }

  function onSetViewport(_w, _h) {
    // Iframe is resized externally by chrome; ack on the next animation frame
    // when window.innerWidth/Height reflect the new size.
    requestAnimationFrame(function () {
      postToParent({
        type: A2C.VIEWPORT_APPLIED,
        width: window.innerWidth,
        height: window.innerHeight,
      });
    });
  }

  // ---------- Mode state ----------
  function setMode(value) {
    if (value !== 'navigate' && value !== 'pin') return;
    if (state.mode === value) return;
    state.mode = value;
    if (value === 'pin') {
      attachHoverListeners();
      attachClickCapture();
    } else {
      detachHoverListeners();
      detachClickCapture();
    }
    updateCursor();
    // Phase D: in Pin mode, suspend marker tabindex so Tab walks the app's focus order.
    if (state.overlay && markersAPI && markersAPI.setMarkersTabindex) {
      markersAPI.setMarkersTabindex(state.overlay.markersById, value === 'pin' ? '-1' : '0');
    }
    // Pin-mode: make markers pointer-events:none via a class on the overlay
    // root, so the hover overlay reads through to the element underneath the
    // numbered circle. Navigate mode keeps the markers clickable (deep-link).
    // CSS rule lives in agent-marker.css under .crit-marker-root--pin-mode.
    if (state.overlay && state.overlay.root && state.overlay.root.classList) {
      state.overlay.root.classList.toggle('crit-marker-root--pin-mode', value === 'pin');
    }
  }

  function updateCursor() {
    document.documentElement.style.cursor = state.mode === 'pin' ? 'crosshair' : '';
  }

  // ---------- Hover overlay ----------
  function ensureOverlay() {
    if (state.overlayEl) return state.overlayEl;
    var el = document.createElement('div');
    el.id = 'crit-agent-overlay';
    el.style.cssText = [
      'position: fixed',
      'pointer-events: none',
      'border: 2px solid #2d7ff9',
      'background: rgba(45,127,249,0.08)',
      'z-index: 2147483600',
      'box-sizing: border-box',
      'transition: none',
      'display: none',
    ].join(';');
    document.documentElement.appendChild(el);
    state.overlayEl = el;
    return el;
  }

  function showOverlayFor(target) {
    var el = ensureOverlay();
    if (!target || !target.getBoundingClientRect) {
      el.style.display = 'none';
      return;
    }
    var r = target.getBoundingClientRect();
    if (r.width < 1 || r.height < 1) {
      el.style.display = 'none';
      return;
    }
    el.style.display = 'block';
    el.style.left = r.left + 'px';
    el.style.top = r.top + 'px';
    el.style.width = r.width + 'px';
    el.style.height = r.height + 'px';
  }

  function hideOverlay() {
    if (state.overlayEl) state.overlayEl.style.display = 'none';
  }

  function topElementAt(x, y) {
    var overlay = state.overlayEl;
    var prev = overlay && overlay.style.display;
    if (overlay) overlay.style.display = 'none';
    var el = document.elementFromPoint(x, y);
    if (overlay && prev) overlay.style.display = prev;
    return el;
  }

  function onPointerMove(ev) {
    if (state.mode !== 'pin') return;
    if (state.suppressHover) return;
    state.pointer.x = ev.clientX;
    state.pointer.y = ev.clientY;
    var t = topElementAt(ev.clientX, ev.clientY);
    showOverlayFor(t);
  }

  function attachHoverListeners() {
    document.addEventListener('mousemove', onPointerMove, true);
  }
  function detachHoverListeners() {
    document.removeEventListener('mousemove', onPointerMove, true);
    hideOverlay();
  }

  // ---------- Shadow DOM detection ----------
  function isInShadowDOM(el) {
    var root = el && el.getRootNode && el.getRootNode();
    return !!(root && root !== document && root.nodeType === 11);
  }

  // ---------- DOMAnchor build + click capture ----------
  function buildDOMAnchorFor(el) {
    var root = utils.findAnchorRoot(el);
    return {
      pathname: window.location.pathname,
      css_selector: utils.cssSelectorFor(el, root),
      tag_chain: utils.tagChainFor(el, root),
      accessible_name: utils.accessibleNameFor(el),
      role: utils.roleFor(el),
      landmark: utils.landmarkFor(el),
      outer_html: utils.truncateOuterHTML(el.outerHTML || '', 2048),
      viewport_width: window.innerWidth,
      viewport_height: window.innerHeight,
    };
  }

  // Walk an event's composedPath() to find the deepest Element. Browsers
  // retarget elementFromPoint() to the shadow host, so composedPath() is the
  // only way to detect that a real click landed inside a shadow tree.
  function deepestElementFromEvent(ev) {
    if (typeof ev.composedPath !== 'function') return null;
    var path = ev.composedPath();
    for (var i = 0; i < path.length; i++) {
      var n = path[i];
      if (n && n.nodeType === 1) return n;
    }
    return null;
  }

  function onClickCapture(ev) {
    // Re-anchor capture is one-shot and mode-independent: when the chrome has
    // armed re-anchor, the next click must be consumed regardless of whether
    // the user is in Pin or Navigate mode. (onEnterReanchor attaches the
    // capture transiently when mode === 'navigate'.)
    var reanchorArmed = !!(state.reanchor && state.reanchor.armed);
    if (state.mode !== 'pin' && !reanchorArmed) return;
    if (ev.button !== 0) return;
    var target = topElementAt(ev.clientX, ev.clientY);
    if (!target) return;
    ev.preventDefault();
    ev.stopPropagation();
    // Use the event's composedPath() to detect shadow-DOM clicks: browsers
    // retarget elementFromPoint() to the shadow host (target above), but the
    // event's actual path includes the inner element. If the deepest element
    // in the path lives inside a shadow tree, refuse to pin.
    var deep = deepestElementFromEvent(ev);
    if (deep && isInShadowDOM(deep)) {
      postToParent({ type: A2C.AGENT_ERROR, kind: 'shadow-dom', message: "can't pin inside shadow DOM" });
      return;
    }
    if (isInShadowDOM(target)) {
      postToParent({ type: A2C.AGENT_ERROR, kind: 'shadow-dom', message: "can't pin inside shadow DOM" });
      return;
    }
    var anchor = buildDOMAnchorFor(target);
    showOverlayFor(target);
    state.pendingSelection = { target: target, anchor: anchor, pointer: { x: ev.clientX, y: ev.clientY } };
    emitSelection();
  }

  function suppressInPinMode(ev) {
    if (state.mode !== 'pin') return;
    var t = ev.target;
    if (t && t.matches && t.matches('input,textarea,select')) return;
    ev.preventDefault();
    ev.stopPropagation();
  }
  function suppressKeyboardActivation(ev) {
    if (state.mode !== 'pin') return;
    if (ev.key !== 'Enter' && ev.key !== ' ') return;
    var t = ev.target;
    if (!t || !t.matches) return;
    if (t.matches('input,textarea,select')) return;
    if (t.matches('button,a[href],[role="button"],[role="link"],summary')) {
      ev.preventDefault();
      ev.stopPropagation();
    }
  }

  function attachClickCapture() {
    document.addEventListener('click', onClickCapture, true);
    document.addEventListener('contextmenu', onContextMenu, true);
    document.addEventListener('submit', suppressInPinMode, true);
    document.addEventListener('pointerdown', suppressInPinMode, true);
    document.addEventListener('mousedown', suppressInPinMode, true);
    document.addEventListener('keydown', suppressKeyboardActivation, true);
  }
  function detachClickCapture() {
    document.removeEventListener('click', onClickCapture, true);
    document.removeEventListener('contextmenu', onContextMenu, true);
    document.removeEventListener('submit', suppressInPinMode, true);
    document.removeEventListener('pointerdown', suppressInPinMode, true);
    document.removeEventListener('mousedown', suppressInPinMode, true);
    document.removeEventListener('keydown', suppressKeyboardActivation, true);
  }

  // ---------- Selection emit ----------
  function emitSelection() {
    if (!state.pendingSelection) return;
    var sel = state.pendingSelection;
    var msg = {
      type: A2C.SELECTION,
      dom_anchor: sel.anchor,
      pointer: sel.pointer,
    };
    // Phase D: if in re-anchor mode, attach reanchor_for and exit one-shot capture.
    if (state.reanchor && state.reanchor.armed) {
      var pinId = state.reanchor.consume();
      if (pinId) msg.reanchor_for = pinId;
      try { document.documentElement.classList.remove('crit-design-reanchor-active'); } catch (_) {}
      // If we attached capture transiently for re-anchor (mode is navigate),
      // detach again so clicks resume normal app behavior.
      if (state.mode !== 'pin') {
        detachHoverListeners();
        detachClickCapture();
      }
    }
    postToParent(msg);
  }

  // ---------- Right-click ancestor menu ----------
  function labelFor(el) {
    var tag = (el.tagName || '').toLowerCase();
    if (el.id) return tag + '#' + el.id;
    var cls = el.className && typeof el.className === 'string'
      ? el.className.split(/\s+/).filter(Boolean)[0]
      : '';
    return cls ? tag + '.' + cls : tag;
  }

  function onContextMenu(ev) {
    if (state.mode !== 'pin') return;
    var target = topElementAt(ev.clientX, ev.clientY);
    if (!target) return;
    ev.preventDefault();
    ev.stopPropagation();
    var root = utils.findAnchorRoot(target);
    var chain = utils.walkAncestors(target, root);
    var options = chain.map(function (el, i) { return { level: i, label: labelFor(el) }; });
    state.pendingAncestor = { chain: chain, root: root };
    postToParent({
      type: A2C.REQUEST_ANCESTOR_MENU,
      options: options,
      pointer: { x: ev.clientX, y: ev.clientY },
    });
  }

  // Phase E: while an ancestor menu request is in flight, broadcast the
  // chain-level the user is currently hovering in the iframe so the chrome's
  // menu can preview-highlight the matching row.
  document.addEventListener('mousemove', function (ev) {
    if (!state.pendingAncestor) return;
    var t = topElementAt(ev.clientX, ev.clientY);
    if (!t) return;
    var chain = state.pendingAncestor.chain || [];
    for (var i = 0; i < chain.length; i++) {
      if (chain[i] === t || (chain[i] && chain[i].contains && chain[i].contains(t))) {
        if (state._lastHoveredLevel === i) return;
        state._lastHoveredLevel = i;
        postToParent({ type: A2C.HOVERED_ANCESTOR_LEVEL, level: i });
        return;
      }
    }
  }, true);

  function commitAncestor(level) {
    if (!state.pendingAncestor) return;
    var target = state.pendingAncestor.chain[level];
    if (!target) { state.pendingAncestor = null; return; }
    var anchor = buildDOMAnchorFor(target);
    state.pendingSelection = { target: target, anchor: anchor, pointer: state.pointer };
    state.pendingAncestor = null;
    showOverlayFor(target);
    emitSelection();
  }

  function cancelAncestor() {
    state.pendingAncestor = null;
    hideOverlay();
  }

  // ---------- Focus state reporting ----------
  function isInputLike(el) {
    if (!el || !el.tagName) return false;
    var t = el.tagName.toUpperCase();
    if (t === 'INPUT' || t === 'TEXTAREA' || t === 'SELECT') return true;
    if (el.isContentEditable) return true;
    return false;
  }

  document.addEventListener('focusin', function (ev) {
    if (isInputLike(ev.target)) postToParent({ type: A2C.FOCUS_STATE, in_input: true });
  }, true);
  document.addEventListener('focusout', function (ev) {
    if (isInputLike(ev.target)) postToParent({ type: A2C.FOCUS_STATE, in_input: false });
  }, true);
})();
