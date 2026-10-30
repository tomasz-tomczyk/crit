(function() {
  'use strict';

  // Floating toolbar shown above a text selection, with a Comment button.
  // It makes select-then-`c` discoverable without opening anything on its
  // own: the selection, focus and copy stay untouched until the button is
  // clicked.
  //
  // This module owns the trigger (a mouse gesture that made a selection),
  // the element, its position and its dismissal. The caller resolves the
  // selection into what a comment needs (lines, quote) and opens the form.
  // Shared with crit-web (vendored by scripts/sync-pierre.sh there).

  const GAP = 8; // between the selection and the bar
  const MARGIN = 8; // from the viewport edges and the top boundary

  // Viewport position of a toolbar of `size` ({width, height}) for an
  // `anchor` ({top, left, width, bottom}): centred above it, below it when
  // the space above is under `topBoundary` (sticky headers), and kept inside
  // the viewport.
  function computeToolbarPosition(anchor, size, viewport, topBoundary) {
    const minTop = Math.max(0, topBoundary || 0) + MARGIN;
    let placement = 'above';
    let top = anchor.top - GAP - size.height;
    if (top < minTop) {
      placement = 'below';
      top = anchor.bottom + GAP;
    }
    top = Math.max(minTop, Math.min(top, viewport.height - size.height - MARGIN));
    const centre = anchor.left + anchor.width / 2;
    const maxLeft = Math.max(MARGIN, viewport.width - size.width - MARGIN);
    const left = Math.min(Math.max(centre - size.width / 2, MARGIN), maxLeft);
    return { top: Math.round(top), left: Math.round(left), placement: placement };
  }

  // Where the bar points: centred on the first line of the selection, so a
  // multi-line selection gets the bar where it starts, and below the whole
  // selection when it flips.
  function anchorRect(range) {
    const bounds = range.getBoundingClientRect();
    const rects = range.getClientRects ? range.getClientRects() : [];
    let first = bounds;
    for (let i = 0; i < rects.length; i++) {
      if (rects[i].width > 0 && rects[i].height > 0) { first = rects[i]; break; }
    }
    return { top: first.top, left: first.left, width: first.width, bottom: bounds.bottom };
  }

  // Key chord parts of a shortcut binding ("Shift+C" -> ["Shift", "C"]);
  // empty when the action is unassigned.
  function shortcutParts(binding) {
    return binding ? binding.split('+').filter(Boolean) : [];
  }

  // A key press that keeps the bar: a lone modifier, or copy (Ctrl/Cmd+C,
  // Ctrl+Insert). Anything else (typing, `c`, Shift+arrows, select all) makes
  // the bar stale.
  function keepsSelection(e) {
    if (e.key === 'Shift' || e.key === 'Control' || e.key === 'Alt' || e.key === 'Meta') return true;
    if (!(e.ctrlKey || e.metaKey) || e.altKey) return false;
    return e.key === 'c' || e.key === 'C' || e.key === 'Insert';
  }

  // The extent of `selection`, through shadow roots when the browser can see
  // into them; null when nothing is selected.
  function selectionSnapshot(selection, shadowRoots) {
    if (!selection || selection.rangeCount === 0) return null;
    let range = null;
    if (typeof selection.getComposedRanges === 'function') {
      const ranges = selection.getComposedRanges({ shadowRoots: shadowRoots || [] });
      range = ranges && ranges[0];
    }
    if (!range) range = selection.getRangeAt(0);
    if (!range || range.collapsed) return null;
    return { sc: range.startContainer, so: range.startOffset, ec: range.endContainer, eo: range.endOffset };
  }

  function sameSnapshot(a, b) {
    if (!a || !b) return a === b;
    return a.sc === b.sc && a.so === b.so && a.ec === b.ec && a.eo === b.eo;
  }

  function isEditable(el) {
    return !!el && (el.tagName === 'TEXTAREA' || el.tagName === 'INPUT' || el.tagName === 'SELECT' || el.isContentEditable);
  }

  // Options:
  //   label          visible button text ("Comment")
  //   ariaLabel      accessible name of the button
  //   icon           trusted SVG markup for the button
  //   shortcut()     current binding of the comment action, or '' if unassigned
  //   topBoundary()  bottom of the sticky chrome the bar must not cover
  //   shadowRoots()  shadow roots the selection may live in (Pierre hosts)
  //   enabled()      false to never show the bar (settings open, no right to comment)
  //   resolve(selection) -> { range, data } | null: the live Range to point
  //                  at and the data for onComment, or null if not commentable
  //   onComment(data) called when the button is clicked
  function create(options) {
    const opts = options || {};
    const doc = opts.document || document;
    const win = doc.defaultView || window;
    let payload = null;
    let shown = null; // selection snapshot the bar was shown for
    let gestureStart; // snapshot at the mouse press, undefined outside a gesture
    let destroyed = false;

    const bar = doc.createElement('div');
    bar.className = 'crit-selection-toolbar';
    bar.hidden = true;
    bar.setAttribute('role', 'toolbar');
    bar.setAttribute('aria-label', 'Selection actions');
    const button = doc.createElement('button');
    button.type = 'button';
    button.className = 'crit-selection-toolbar-btn';
    button.setAttribute('aria-label', opts.ariaLabel || 'Comment on selection');
    if (opts.icon) {
      const icon = doc.createElement('span');
      icon.className = 'crit-selection-toolbar-icon';
      icon.setAttribute('aria-hidden', 'true');
      icon.innerHTML = opts.icon;
      button.appendChild(icon);
    }
    const label = doc.createElement('span');
    label.textContent = opts.label || 'Comment';
    button.appendChild(label);
    const hint = doc.createElement('span');
    hint.className = 'crit-selection-toolbar-shortcut';
    hint.setAttribute('aria-hidden', 'true');
    button.appendChild(hint);
    bar.appendChild(button);
    doc.body.appendChild(bar);

    function snapshot() {
      return selectionSnapshot(doc.getSelection(), opts.shadowRoots ? opts.shadowRoots() : []);
    }

    // Keep the selection and the focus where they are: a click would
    // otherwise collapse the selection before the comment can read it.
    button.addEventListener('mousedown', function(e) { e.preventDefault(); });
    button.addEventListener('click', function() {
      const current = payload;
      hide();
      if (current && opts.onComment) opts.onComment(current);
    });

    // A mouse gesture shows the bar only if it changed the selection: a
    // click on a control that keeps an older (or keyboard) selection does not.
    function onPointerDown(e) {
      if (bar.contains(e.target)) return;
      hide();
      gestureStart = e.pointerType === 'mouse' && e.button === 0 ? snapshot() : undefined;
    }
    function onPointerUp(e) {
      // Mouse only: keyboard selections have `c`, and touch has the system's
      // own selection menu (emulated mouse events are not pointer events).
      if (e.pointerType !== 'mouse' || e.button !== 0 || bar.contains(e.target)) return;
      const before = gestureStart;
      gestureStart = undefined;
      if (before === undefined) return;
      // Let the browser settle the selection (double click, shadow-root drags).
      win.requestAnimationFrame(function() {
        if (destroyed) return;
        const after = snapshot();
        if (!after || sameSnapshot(before, after)) return;
        if (opts.enabled && !opts.enabled()) return;
        const resolved = opts.resolve ? opts.resolve(doc.getSelection()) : null;
        if (resolved) show(resolved.range, resolved.data);
      });
    }
    doc.addEventListener('pointerdown', onPointerDown, true);
    doc.addEventListener('pointerup', onPointerUp);

    function onKeyDown(e) {
      if (e.key === 'Escape') {
        // Close the bar only, not whatever Escape closes underneath it.
        e.preventDefault();
        e.stopPropagation();
        hide();
      } else if (!keepsSelection(e)) {
        hide();
      }
    }
    function onSelectionChange() {
      if (!sameSnapshot(snapshot(), shown)) hide();
    }
    function onFocusIn(e) {
      if (isEditable(e.target)) hide(); // a form opened or the reader started typing
    }
    function onViewportChange() { hide(); }

    function renderShortcut() {
      const parts = shortcutParts(opts.shortcut ? opts.shortcut() : '');
      hint.textContent = '';
      parts.forEach(function(part) {
        const kbd = doc.createElement('kbd');
        kbd.textContent = part;
        hint.appendChild(kbd);
      });
      hint.hidden = parts.length === 0;
      if (parts.length) button.setAttribute('aria-keyshortcuts', parts.join('+'));
      else button.removeAttribute('aria-keyshortcuts');
    }

    // Show the bar for `range` (a live Range over the selection); `data` is
    // handed back to onComment when the button is clicked.
    function show(range, data) {
      payload = data;
      shown = snapshot();
      renderShortcut();
      bar.style.visibility = 'hidden';
      bar.hidden = false;
      const pos = computeToolbarPosition(
        anchorRect(range),
        { width: bar.offsetWidth, height: bar.offsetHeight },
        { width: win.innerWidth, height: win.innerHeight },
        opts.topBoundary ? opts.topBoundary() : 0
      );
      bar.style.top = pos.top + 'px';
      bar.style.left = pos.left + 'px';
      bar.dataset.placement = pos.placement;
      bar.style.visibility = '';
      doc.addEventListener('keydown', onKeyDown, true);
      doc.addEventListener('selectionchange', onSelectionChange);
      doc.addEventListener('focusin', onFocusIn, true);
      // Code scrolls sideways inside Pierre's shadow roots, where scroll
      // events do not reach the document: wheel catches that too.
      doc.addEventListener('scroll', onViewportChange, true);
      doc.addEventListener('wheel', onViewportChange, { capture: true, passive: true });
      win.addEventListener('resize', onViewportChange);
    }

    function hide() {
      payload = null;
      shown = null;
      if (bar.hidden) return;
      bar.hidden = true;
      doc.removeEventListener('keydown', onKeyDown, true);
      doc.removeEventListener('selectionchange', onSelectionChange);
      doc.removeEventListener('focusin', onFocusIn, true);
      doc.removeEventListener('scroll', onViewportChange, true);
      doc.removeEventListener('wheel', onViewportChange, { capture: true });
      win.removeEventListener('resize', onViewportChange);
    }

    function destroy() {
      destroyed = true;
      hide();
      doc.removeEventListener('pointerdown', onPointerDown, true);
      doc.removeEventListener('pointerup', onPointerUp);
      bar.remove();
    }

    return {
      element: bar,
      show: show,
      hide: hide,
      destroy: destroy,
      isOpen: function() { return !bar.hidden; },
      contains: function(node) { return bar.contains(node); },
    };
  }

  const api = {
    create: create,
    computeToolbarPosition: computeToolbarPosition,
    anchorRect: anchorRect,
    shortcutParts: shortcutParts,
    keepsSelection: keepsSelection,
    selectionSnapshot: selectionSnapshot,
    sameSnapshot: sameSnapshot,
  };
  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.selectionToolbar = api;
  }
  if (typeof module === 'object' && module.exports) module.exports = api;
})();
