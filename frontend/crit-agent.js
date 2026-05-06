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
  };
  window.__critAgentState = state;

  function postToParent(msg) {
    try { window.parent.postMessage(msg, expectedApiOrigin); } catch (_) { /* noop */ }
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
      default: break;
    }
  }

  // ---------- Mode state ----------
  function setMode(value) {
    if (value !== 'navigate' && value !== 'pin') return;
    if (state.mode === value) return;
    state.mode = value;
    if (value === 'pin') {
      attachHoverListeners();
      attachClickCapture();
      warmHtml2Canvas();
    } else {
      detachHoverListeners();
      detachClickCapture();
    }
    updateCursor();
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
      screenshot: '',
      viewport_width: window.innerWidth,
      viewport_height: window.innerHeight,
    };
  }

  function onClickCapture(ev) {
    if (state.mode !== 'pin') return;
    if (ev.button !== 0) return;
    var target = topElementAt(ev.clientX, ev.clientY);
    if (!target) return;
    ev.preventDefault();
    ev.stopPropagation();
    if (isInShadowDOM(target)) {
      postToParent({ type: A2C.AGENT_ERROR, kind: 'shadow-dom', message: "can't pin inside shadow DOM" });
      return;
    }
    var anchor = buildDOMAnchorFor(target);
    showOverlayFor(target);
    state.pendingSelection = { target: target, anchor: anchor, pointer: { x: ev.clientX, y: ev.clientY } };
    emitSelection().catch(function () {});
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

  // ---------- Selection emit (with screenshot) ----------
  async function emitSelection() {
    if (!state.pendingSelection) return;
    var sel = state.pendingSelection;
    var shot = await captureScreenshot(sel.target);
    sel.anchor.screenshot = shot;
    postToParent({
      type: A2C.SELECTION,
      dom_anchor: sel.anchor,
      pointer: sel.pointer,
    });
  }

  // ---------- html2canvas lazy loader ----------
  var h2cPromise = null;
  function loadHtml2Canvas() {
    if (h2cPromise) return h2cPromise;
    h2cPromise = new Promise(function (resolve, reject) {
      if (window.html2canvas) { resolve(window.html2canvas); return; }
      var s = document.createElement('script');
      s.src = expectedApiOrigin + '/crit-vendor/html2canvas.js';
      s.async = true;
      s.crossOrigin = 'anonymous';
      s.onload = function () {
        if (window.html2canvas) resolve(window.html2canvas);
        else reject(new Error('html2canvas missing after load'));
      };
      s.onerror = function () { reject(new Error('html2canvas load failed')); };
      document.head.appendChild(s);
    });
    return h2cPromise;
  }
  function warmHtml2Canvas() {
    loadHtml2Canvas().catch(function () { /* swallow */ });
  }

  async function captureScreenshot(target) {
    try {
      if (document.fonts && document.fonts.ready) {
        await document.fonts.ready;
      }
      var h2c = await loadHtml2Canvas();
      var canvas = await h2c(target, { scale: 1, logging: false });
      return canvas.toDataURL('image/jpeg', 0.7);
    } catch (err) {
      postToParent({ type: A2C.AGENT_ERROR, kind: 'capture-failed', message: String(err && err.message || err) });
      return '';
    }
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

  function commitAncestor(level) {
    if (!state.pendingAncestor) return;
    var target = state.pendingAncestor.chain[level];
    if (!target) { state.pendingAncestor = null; return; }
    var anchor = buildDOMAnchorFor(target);
    state.pendingSelection = { target: target, anchor: anchor, pointer: state.pointer };
    state.pendingAncestor = null;
    showOverlayFor(target);
    emitSelection().catch(function () {});
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
