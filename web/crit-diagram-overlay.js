// crit-diagram-overlay.js — fullscreen pan/zoom view for rendered SVG output
// (GitHub-style). Used by crit-renderers.js for renderers registered with
// zoomable: true whose output contains an <svg>.
// Dependencies: the #diagramOverlay shell in index.html. No window.crit deps.
(function () {
  'use strict';

  // Expand button on each rendered diagram opens #diagramOverlay with a
  // detached SVG clone. Pan via drag, zoom via wheel / pinch / buttons.
  // Pure CSS transform — no new dependencies. The clone is detached, so
  // inline re-renders never disturb the overlay; a theme change closes it
  // because the clone keeps the old theme's colors.
  var state = {
    open: false,
    trigger: null,
    scale: 1,
    x: 0,
    y: 0,
    installed: false
  };
  var ZOOM_MIN = 0.1;
  var ZOOM_MAX = 8;

  function overlayNodes() {
    return {
      overlay: document.getElementById('diagramOverlay'),
      viewport: document.getElementById('diagramOverlayViewport'),
      canvas: document.getElementById('diagramOverlayCanvas'),
      label: document.getElementById('diagramZoomLabel'),
      closeBtn: document.getElementById('diagramOverlayClose'),
      zoomIn: document.getElementById('diagramZoomIn'),
      zoomOut: document.getElementById('diagramZoomOut'),
      zoomReset: document.getElementById('diagramZoomReset')
    };
  }

  function applyTransform() {
    var nodes = overlayNodes();
    if (!nodes.canvas) return;
    nodes.canvas.style.transform = 'translate(' + state.x + 'px, ' + state.y + 'px) scale(' + state.scale + ')';
    if (nodes.label) nodes.label.textContent = Math.round(state.scale * 100) + '%';
  }

  function zoomAt(factor, clientX, clientY) {
    var nodes = overlayNodes();
    if (!nodes.viewport) return;
    var rect = nodes.viewport.getBoundingClientRect();
    var px = clientX - rect.left;
    var py = clientY - rect.top;
    var next = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, state.scale * factor));
    if (next === state.scale) return;
    var ratio = next / state.scale;
    state.x = px - (px - state.x) * ratio;
    state.y = py - (py - state.y) * ratio;
    state.scale = next;
    applyTransform();
  }

  function zoomCenter(factor) {
    var nodes = overlayNodes();
    if (!nodes.viewport) return;
    var rect = nodes.viewport.getBoundingClientRect();
    zoomAt(factor, rect.left + rect.width / 2, rect.top + rect.height / 2);
  }

  // Fit the cloned SVG into the viewport (capped so tiny diagrams don't blow up).
  function fit() {
    var nodes = overlayNodes();
    if (!nodes.viewport || !nodes.canvas) return;
    var svg = nodes.canvas.querySelector('svg');
    state.scale = 1;
    state.x = 0;
    state.y = 0;
    if (svg) {
      var w = 0;
      var h = 0;
      var vb = svg.getAttribute('viewBox');
      if (vb) {
        var parts = vb.trim().split(/[\s,]+/);
        w = parseFloat(parts[2]);
        h = parseFloat(parts[3]);
      }
      if (!(w > 0) || !(h > 0)) {
        var r = svg.getBoundingClientRect();
        w = r.width;
        h = r.height;
      }
      if (w > 0 && h > 0) {
        var vw = nodes.viewport.clientWidth - 48;
        var vh = nodes.viewport.clientHeight - 48;
        if (vw > 0 && vh > 0) {
          state.scale = Math.min(vw / w, vh / h, 2);
          state.x = (nodes.viewport.clientWidth - w * state.scale) / 2;
          state.y = (nodes.viewport.clientHeight - h * state.scale) / 2;
        }
      }
    }
    applyTransform();
  }

  function openOverlay(sourceSvg, trigger) {
    var nodes = overlayNodes();
    if (!nodes.overlay || !sourceSvg || !nodes.canvas) return;
    nodes.canvas.innerHTML = '';
    var clone = sourceSvg.cloneNode(true);
    // Mermaid emits width="100%" + a max-width cap. Inside the overlay the
    // canvas sizes to content (width: max-content), which makes a percentage
    // width resolve circularly and the diagram collapse. Pin the clone to its
    // natural size so fit/center math holds. Prefer the viewBox; fall back to
    // the live source rect (the clone itself may already be collapsed).
    var natW = 0;
    var natH = 0;
    var vb = clone.getAttribute('viewBox');
    if (vb) {
      var parts = vb.trim().split(/[\s,]+/);
      natW = parseFloat(parts[2]);
      natH = parseFloat(parts[3]);
    }
    if (!(natW > 0) || !(natH > 0)) {
      var srcRect = sourceSvg.getBoundingClientRect();
      natW = srcRect.width;
      natH = srcRect.height;
    }
    if (natW > 0 && natH > 0) {
      clone.setAttribute('width', String(natW));
      clone.setAttribute('height', String(natH));
      if (clone.style) clone.style.removeProperty('max-width');
    }
    nodes.canvas.appendChild(clone);
    state.open = true;
    state.trigger = trigger || null;
    installOverlay();
    nodes.overlay.classList.add('active');
    document.body.style.overflow = 'hidden';
    fit();
    if (nodes.closeBtn && nodes.closeBtn.focus) nodes.closeBtn.focus();
  }

  function closeOverlay() {
    var nodes = overlayNodes();
    if (!state.open) return;
    state.open = false;
    if (nodes.overlay) nodes.overlay.classList.remove('active');
    if (nodes.canvas) nodes.canvas.innerHTML = '';
    document.body.style.overflow = '';
    var trigger = state.trigger;
    state.trigger = null;
    // The trigger stays rendered (transparent until hover/focus), so focus()
    // lands even outside hover — e.g. the keyboard flow.
    if (trigger && document.contains(trigger) && trigger.focus) trigger.focus();
  }

  function trapTab(e) {
    if (e.key !== 'Tab') return;
    var nodes = overlayNodes();
    if (!nodes.overlay) return;
    var focusables = nodes.overlay.querySelectorAll('button:not([disabled])');
    if (!focusables || focusables.length === 0) return;
    var first = focusables[0];
    var last = focusables[focusables.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  }

  // One-pointer drag pans; two-pointer pinch zooms. Installed once — the
  // overlay shell lives in index.html and survives document re-renders.
  function installOverlay() {
    if (state.installed) return;
    var nodes = overlayNodes();
    if (!nodes.overlay || !nodes.viewport) return;
    state.installed = true;

    var activePointers = new Map();
    var panAnchor = null;
    var pinchStart = 0;
    var pinchScale = 1;

    // Re-anchor the pinch baseline on every pointer-count change so adding
    // or lifting a finger mid-gesture never causes a zoom jump.
    function capturePinchBaseline() {
      var pts = Array.from(activePointers.values());
      if (pts.length < 2) return;
      pinchStart = Math.hypot(pts[0].x - pts[1].x, pts[0].y - pts[1].y);
      pinchScale = state.scale;
    }

    if (nodes.zoomIn) nodes.zoomIn.addEventListener('click', function () { zoomCenter(1.25); });
    if (nodes.zoomOut) nodes.zoomOut.addEventListener('click', function () { zoomCenter(1 / 1.25); });
    if (nodes.zoomReset) nodes.zoomReset.addEventListener('click', function () { fit(); });
    if (nodes.closeBtn) nodes.closeBtn.addEventListener('click', function () { closeOverlay(); });
    nodes.overlay.addEventListener('click', function (e) {
      if (e.target === nodes.overlay) closeOverlay();
    });
    nodes.overlay.addEventListener('keydown', trapTab);
    document.addEventListener('keydown', function (e) {
      // Note: no defaultPrevented check — app.js's keymap
      // unconditionally preventDefaults Escape, so honoring it would break
      // Esc-to-close entirely. Esc dismissing both the overlay and any
      // background form matches the existing "cancel whatever" convention.
      if (e.key !== 'Escape') return;
      if (state.open) {
        e.preventDefault();
        closeOverlay();
      }
    });

    nodes.viewport.addEventListener('wheel', function (e) {
      if (!state.open) return;
      e.preventDefault();
      zoomAt(e.deltaY < 0 ? 1.12 : 1 / 1.12, e.clientX, e.clientY);
    }, { passive: false });

    nodes.viewport.addEventListener('pointerdown', function (e) {
      if (!state.open) return;
      try { nodes.viewport.setPointerCapture(e.pointerId); } catch { /* noop */ }
      activePointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
      if (activePointers.size === 1) {
        panAnchor = { x: e.clientX - state.x, y: e.clientY - state.y };
        nodes.viewport.classList.add('panning');
      } else {
        panAnchor = null;
        capturePinchBaseline();
      }
    });
    nodes.viewport.addEventListener('pointermove', function (e) {
      if (!activePointers.has(e.pointerId)) return;
      activePointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
      if (activePointers.size === 2) {
        var pts = Array.from(activePointers.values());
        var dist = Math.hypot(pts[0].x - pts[1].x, pts[0].y - pts[1].y);
        if (pinchStart > 0 && dist > 0) {
          var midX = (pts[0].x + pts[1].x) / 2;
          var midY = (pts[0].y + pts[1].y) / 2;
          // Zoom relative to the scale captured when the second pointer
          // landed, anchored at the gesture midpoint.
          var rel = (pinchScale * dist / pinchStart) / state.scale;
          zoomAt(rel, midX, midY);
        }
        return;
      }
      if (panAnchor) {
        state.x = e.clientX - panAnchor.x;
        state.y = e.clientY - panAnchor.y;
        applyTransform();
      }
    });
    function endPointer(e) {
      activePointers.delete(e.pointerId);
      if (activePointers.size === 1) {
        var remaining = Array.from(activePointers.values())[0];
        panAnchor = { x: remaining.x - state.x, y: remaining.y - state.y };
      } else if (activePointers.size >= 2) {
        capturePinchBaseline();
      } else {
        panAnchor = null;
        nodes.viewport.classList.remove('panning');
      }
    }
    nodes.viewport.addEventListener('pointerup', endPointer);
    nodes.viewport.addEventListener('pointercancel', endPointer);
  }

  var api = {
    open: openOverlay,
    close: closeOverlay,
  };

  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.diagramOverlay = api;
  }
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
