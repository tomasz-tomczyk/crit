// crit-excalidraw-canvas.js — the canvas page preview mode serves for an
// Excalidraw scene (excalidraw-canvas.html), in the style of Excalidraw's
// own: an infinite canvas with a grid and no scrollbars. The wheel or a
// trackpad pans, Ctrl/Cmd + wheel or a pinch zooms at the pointer, and a
// drag pans (Space + drag, a middle-button drag and the hand tool always do).
// The preview agent injected into the page outlines the element under the
// pointer and pins comments to it: each element's hit area carries an id
// (#excalidraw-<element id>) and a label. The empty canvas takes no pins
// and shows no outline, as in Excalidraw.
// Dependencies: window.crit.excalidraw (crit-renderer-excalidraw.js).
(function () {
  'use strict';

  var MIN_ZOOM = 0.1;
  var MAX_ZOOM = 10;
  var STEP = 1.25;
  // Gap kept between a fitted drawing and the viewport's edges.
  var FIT_GAP = 32;
  // How far the pointer moves before a press becomes a pan, not a click.
  var DRAG_THRESHOLD = 4;
  // Below this spacing (px) grid dots thin out to every 5th.
  var MIN_GRID_STEP = 10;
  // The view (theme, zoom, pan, grid) kept across the reload a theme switch does.
  var VIEW_KEY = 'crit-excalidraw-canvas-view';

  function clampZoom(zoom) {
    return Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, zoom));
  }

  // The zoom that shows the whole drawing, at most 100%. On opening, a
  // drawing much taller than the viewport is fitted to the width instead,
  // so its text stays readable; panning shows the rest.
  function fitZoom(size, viewport, whole) {
    var fitWidth = Math.max(viewport.width - 2 * FIT_GAP, 1) / size.width;
    var fitAll = Math.min(fitWidth, Math.max(viewport.height - 2 * FIT_GAP, 1) / size.height);
    var zoom = !whole && fitAll < fitWidth / 2.5 ? fitWidth : fitAll;
    return clampZoom(Math.min(1, zoom));
  }

  // Where a fitted drawing goes: centred, or its top in view when taller
  // than the viewport. `pan` is the viewport position of the drawing's
  // top-left corner.
  function fitPan(size, zoom, viewport) {
    var width = size.width * zoom;
    var height = size.height * zoom;
    return {
      x: (viewport.width - width) / 2,
      y: height + 2 * FIT_GAP > viewport.height ? FIT_GAP : (viewport.height - height) / 2,
    };
  }

  // The pan that keeps the drawing's point under `pointer` still while the
  // zoom changes.
  function zoomAt(pan, zoom, nextZoom, pointer) {
    return {
      x: pointer.x - (pointer.x - pan.x) * nextZoom / zoom,
      y: pointer.y - (pointer.y - pan.y) * nextZoom / zoom,
    };
  }

  // The grid's spacing and offset in the viewport: dots on multiples of
  // gridSize in scene coordinates (origin is the scene point at the
  // drawing's top-left corner), every 5th one when they would crowd.
  function gridFor(gridSize, zoom, pan, origin) {
    var step = gridSize * zoom;
    while (step < MIN_GRID_STEP) step *= 5;
    var x = pan.x - origin.x * zoom;
    var y = pan.y - origin.y * zoom;
    return { step: step, x: ((x % step) + step) % step, y: ((y % step) + step) % step };
  }

  // The review page's theme from the crit-settings cookie (URL-encoded JSON).
  function reviewTheme() {
    var m = /(?:^|;\s*)crit-settings=([^;]*)/.exec(document.cookie || '');
    var theme = 'system';
    try { theme = (m && JSON.parse(decodeURIComponent(m[1])).theme) || 'system'; } catch (_) { /* default */ }
    if (theme === 'light' || theme === 'dark') return theme;
    return window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }

  // sessionStorage can be missing or throw (private windows, blocked
  // storage); the view then just starts afresh.
  function saveView(view) {
    try { window.sessionStorage.setItem(VIEW_KEY, JSON.stringify(view)); } catch (_) { /* start afresh */ }
  }

  function takeSavedView() {
    try {
      var raw = window.sessionStorage.getItem(VIEW_KEY);
      window.sessionStorage.removeItem(VIEW_KEY);
      return raw ? JSON.parse(raw) : null;
    } catch (_) {
      return null;
    }
  }

  function start() {
    var root = document.documentElement;
    var bg = document.getElementById('crit-canvas-bg');
    var stage = document.getElementById('crit-canvas-stage');
    var islands = document.getElementById('crit-canvas-islands');
    var zoomLabel = document.getElementById('crit-canvas-zoom');
    var errorBox = document.getElementById('crit-canvas-error');
    var saved = takeSavedView();
    var state = {
      theme: (saved && saved.theme) || reviewTheme(),
      zoom: 1,
      paintedZoom: 0,
      pan: { x: 0, y: 0 },
      grid: !saved || saved.grid !== false,
      hand: false,
      space: false,
      drag: null,
      suppressClick: false,
      pointer: null,
      frame: 0,
      svg: null,
    };

    function viewport() {
      return { width: window.innerWidth, height: window.innerHeight };
    }

    function showError(err) {
      islands.hidden = true;
      errorBox.hidden = false;
      errorBox.textContent = 'Could not draw this scene: ' + (err && err.message ? err.message : String(err));
    }

    function inIslands(target) {
      return !!(target && target.closest && target.closest('#crit-canvas-islands'));
    }

    // Empty canvas: the grid layer or the page around it, not an element.
    function isCanvas(target) {
      return !target || target === bg || target === stage || target === root || target === document.body || target === document;
    }

    // The agent's hover outline: a fixed box it adds to the page. Hidden
    // over the empty canvas and while panning.
    var agentBox = null;
    function hideAgentBox() {
      if (!agentBox || !agentBox.isConnected) {
        agentBox = null;
        for (var el = root.firstElementChild; el; el = el.nextElementSibling) {
          if (el.style && el.style.position === 'fixed' && el.style.zIndex === '2147483600') { agentBox = el; break; }
        }
      }
      if (agentBox) agentBox.style.display = 'none';
    }

    // The agent repositions its pins when the page's children change. Panning
    // and zooming only move the drawing, so add and remove a node to tell it.
    function nudgeAgent() {
      var tick = document.createElement('i');
      tick.hidden = true;
      document.body.appendChild(tick);
      document.body.removeChild(tick);
    }

    // After the drawing moves under a still pointer, let the agent outline
    // what is now under it.
    function refreshHover() {
      var p = state.pointer;
      if (!p || state.drag) return;
      var target = document.elementFromPoint(p.x, p.y);
      if (target) target.dispatchEvent(new MouseEvent('mousemove', { bubbles: true, clientX: p.x, clientY: p.y }));
    }

    function paint() {
      state.frame = 0;
      var svg = state.svg;
      if (state.paintedZoom !== state.zoom) {
        // Resized rather than scaled, so the vector drawing stays sharp.
        svg.setAttribute('width', state.size.width * state.zoom);
        svg.setAttribute('height', state.size.height * state.zoom);
        state.paintedZoom = state.zoom;
        zoomLabel.textContent = Math.round(state.zoom * 100) + '%';
      }
      svg.style.transform = 'translate(' + state.pan.x + 'px, ' + state.pan.y + 'px)';
      var g = gridFor(state.gridSize, state.zoom, state.pan, state.origin);
      bg.style.backgroundSize = g.step + 'px ' + g.step + 'px';
      bg.style.backgroundPosition = (g.x - g.step / 2) + 'px ' + (g.y - g.step / 2) + 'px';
      nudgeAgent();
      refreshHover();
    }

    function schedule() {
      if (!state.frame) state.frame = window.requestAnimationFrame(paint);
    }

    function setZoom(zoom, pointer) {
      var next = clampZoom(zoom);
      state.pan = zoomAt(state.pan, state.zoom, next, pointer || { x: viewport().width / 2, y: viewport().height / 2 });
      state.zoom = next;
      schedule();
    }

    function fitView(whole) {
      state.zoom = fitZoom(state.size, viewport(), whole);
      state.pan = fitPan(state.size, state.zoom, viewport());
    }

    function fit(whole) {
      fitView(whole);
      schedule();
    }

    function setHand(on) {
      state.hand = on;
      root.classList.toggle('crit-canvas-hand', on || state.space);
      islands.querySelector('[data-action="hand"]').setAttribute('aria-pressed', on ? 'true' : 'false');
      if (on) hideAgentBox();
    }

    function setGrid(on) {
      state.grid = on;
      root.classList.toggle('crit-canvas-no-grid', !on);
      islands.querySelector('[data-action="grid"]').setAttribute('aria-pressed', on ? 'true' : 'false');
    }

    function act(action) {
      if (!state.svg) return;
      if (action === 'zoom-in') setZoom(state.zoom * STEP);
      else if (action === 'zoom-out') setZoom(state.zoom / STEP);
      else if (action === 'reset') setZoom(1);
      else if (action === 'fit') fit(true);
      else if (action === 'hand') setHand(!state.hand);
      else if (action === 'grid') setGrid(!state.grid);
      else if (action === 'theme') {
        // A redraw would leave the agent's pins on removed elements, so a
        // theme switch reloads the page with the view kept.
        saveView({ theme: state.theme === 'dark' ? 'light' : 'dark', zoom: state.zoom, pan: state.pan, grid: state.grid });
        window.location.reload();
      }
    }

    function endDrag() {
      if (!state.drag) return;
      state.drag = null;
      root.classList.remove('crit-canvas-grabbing');
      refreshHover();
    }

    // The islands handle their own events before the agent, which listens
    // on the document: in Comment mode it would turn a click into a pin.
    // Registered first, so they also come before the canvas's own handlers.
    ['pointerdown', 'pointerup', 'mousedown', 'mouseup', 'mousemove', 'click', 'dblclick', 'contextmenu', 'wheel'].forEach(function (type) {
      window.addEventListener(type, function (e) {
        if (!inIslands(e.target)) return;
        e.stopImmediatePropagation();
        if (type === 'mousemove') hideAgentBox();
        if (type !== 'click') return;
        var button = e.target.closest('button[data-action]');
        if (button) act(button.getAttribute('data-action'));
      }, true);
    });

    window.addEventListener('pointerdown', function (e) {
      state.suppressClick = false;
      if (!state.svg || (e.button !== 0 && e.button !== 1)) return;
      var panning = e.button === 1 || state.hand || state.space;
      state.drag = { x: e.clientX, y: e.clientY, pan: state.pan, moved: panning, canvas: isCanvas(e.target), buttons: e.button === 1 ? 4 : 1 };
      if (panning) {
        root.classList.add('crit-canvas-grabbing');
        hideAgentBox();
        e.preventDefault();
        e.stopImmediatePropagation();
      }
    }, true);

    window.addEventListener('pointermove', function (e) {
      state.pointer = { x: e.clientX, y: e.clientY };
      var d = state.drag;
      if (!d) return;
      if (!(e.buttons & d.buttons)) { endDrag(); return; }
      var dx = e.clientX - d.x;
      var dy = e.clientY - d.y;
      if (!d.moved && Math.hypot(dx, dy) < DRAG_THRESHOLD) return;
      if (!d.moved) {
        d.moved = true;
        root.classList.add('crit-canvas-grabbing');
        hideAgentBox();
      }
      state.pan = { x: d.pan.x + dx, y: d.pan.y + dy };
      schedule();
    }, true);

    window.addEventListener('pointerup', function () {
      var d = state.drag;
      // A pan, or a press on the empty canvas, is no click on an element.
      if (d && (d.moved || d.canvas)) state.suppressClick = true;
      endDrag();
    }, true);
    window.addEventListener('pointercancel', endDrag, true);
    window.addEventListener('blur', endDrag);

    window.addEventListener('click', function (e) {
      if (!state.suppressClick && !state.hand && !state.space) return;
      state.suppressClick = false;
      e.preventDefault();
      e.stopImmediatePropagation();
    }, true);

    window.addEventListener('mousemove', function (e) {
      state.pointer = { x: e.clientX, y: e.clientY };
      if (state.drag || state.hand || state.space || isCanvas(e.target)) {
        e.stopImmediatePropagation();
        hideAgentBox();
      }
    }, true);

    window.addEventListener('wheel', function (e) {
      if (!state.svg) return;
      // Pan or zoom the canvas, never the page (or swipe the browser back).
      e.preventDefault();
      if (e.ctrlKey || e.metaKey) {
        // A pinch arrives as a Ctrl + wheel.
        setZoom(state.zoom * Math.pow(2, -e.deltaY * (e.deltaMode === 1 ? 0.05 : 0.002)), { x: e.clientX, y: e.clientY });
        return;
      }
      var unit = e.deltaMode === 1 ? 16 : 1;
      var dx = e.deltaX * unit;
      var dy = e.deltaY * unit;
      if (e.shiftKey && !dx) { dx = dy; dy = 0; }
      state.pan = { x: state.pan.x - dx, y: state.pan.y - dy };
      schedule();
    }, { passive: false });

    function typing(e) {
      return e.target && e.target.closest && e.target.closest('input, textarea, [contenteditable]');
    }

    window.addEventListener('keydown', function (e) {
      if (!state.svg || e.ctrlKey || e.metaKey || e.altKey || typing(e)) return;
      if (e.key === ' ') {
        if (!state.space) {
          state.space = true;
          root.classList.add('crit-canvas-hand');
          hideAgentBox();
        }
      } else if (e.key === '+' || e.key === '=') act('zoom-in');
      else if (e.key === '-' || e.key === '_') act('zoom-out');
      else if (e.key === '0') act('reset');
      else if (e.key === '1' || e.key === '!') act('fit');
      else if (e.key === 'h' || e.key === 'H') act('hand');
      else return;
      e.preventDefault();
    });

    window.addEventListener('keyup', function (e) {
      if (e.key !== ' ' || !state.space) return;
      state.space = false;
      root.classList.toggle('crit-canvas-hand', state.hand);
      refreshHover();
    });

    window.addEventListener('resize', schedule);

    // Drawn once, without waiting for the fonts (they swap in): with the
    // libraries already loaded this finishes before the agent scripts after
    // this one run, so their pins resolve against the drawing.
    try {
      var source = JSON.parse(document.getElementById('crit-excalidraw-scene').textContent);
      window.crit.excalidraw.renderScene(source, { theme: state.theme, anchors: true, waitForFonts: false }).then(function (out) {
        var box = out.svg.viewBox.baseVal;
        state.size = { width: box.width, height: box.height };
        state.origin = out.origin;
        state.gridSize = out.gridSize;
        root.setAttribute('data-canvas-theme', state.theme);
        root.style.background = out.background === 'none' ? '' : out.background;
        stage.appendChild(out.svg);
        state.svg = out.svg;
        islands.hidden = false;
        setGrid(state.grid);
        if (saved && saved.zoom && saved.pan) {
          state.zoom = clampZoom(saved.zoom);
          state.pan = saved.pan;
        } else {
          fitView(false);
        }
        paint();
      }).catch(showError);
    } catch (err) {
      showError(err);
    }
  }

  var api = {
    clampZoom: clampZoom,
    fitZoom: fitZoom,
    fitPan: fitPan,
    zoomAt: zoomAt,
    gridFor: gridFor,
  };

  if (typeof window !== 'undefined' && document.getElementById('crit-excalidraw-scene')) start();
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
