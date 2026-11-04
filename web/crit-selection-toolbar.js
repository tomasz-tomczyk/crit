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

  // Positions a reader can choose for the bar (Settings > Selection toolbar).
  // The *-cursor ones follow the selection's direction: the bar goes to the
  // side where the selection ended, where the pointer was let go.
  const PLACEMENTS = ['top-left', 'top-center', 'top-right', 'bottom-left', 'bottom-center', 'bottom-right', 'top-cursor', 'bottom-cursor'];
  // Where the bar shows when the reader has not chosen: by the pointer.
  const DEFAULT_PLACEMENT = 'top-cursor';
  // The choices of Settings > Selection toolbar > Position, in menu order.
  const PLACEMENT_OPTIONS = [
    { id: 'top-cursor', name: 'Top, closest to the cursor' },
    { id: 'bottom-cursor', name: 'Bottom, closest to the cursor' },
    { id: 'top-left', name: 'Top left' },
    { id: 'top-center', name: 'Top center' },
    { id: 'top-right', name: 'Top right' },
    { id: 'bottom-left', name: 'Bottom left' },
    { id: 'bottom-center', name: 'Bottom center' },
    { id: 'bottom-right', name: 'Bottom right' },
  ];
  // What the Comment button shows (Settings > Selection toolbar > Button style).
  const BUTTON_STYLES = [
    { id: 'icon', name: 'Icon' },
    { id: 'text', name: 'Text' },
    { id: 'both', name: 'Both' },
  ];

  // The corner a *-cursor placement means for a selection made forwards (left
  // to right) or backwards; other placements are their own corner, and an
  // unknown one is the default.
  function resolvePlacement(placement, backward) {
    const chosen = PLACEMENTS.indexOf(placement) === -1 ? DEFAULT_PLACEMENT : placement;
    if (chosen === 'top-cursor') return backward ? 'top-left' : 'top-right';
    if (chosen === 'bottom-cursor') return backward ? 'bottom-left' : 'bottom-right';
    return chosen;
  }

  // Viewport position of a toolbar of `size` ({width, height}) for an
  // `anchor` (see anchorRect), kept inside the viewport and under
  // `topBoundary` (sticky headers). `placement` (resolved, see
  // resolvePlacement) picks the corner the bar starts from:
  //   top-center            centred above the first line;
  //   top-left / top-right  above the first line, from its start / its end;
  //   bottom-left / -right  below the last line, from its start / its end;
  //   bottom-center         centred below the last line.
  // A top bar goes below the whole selection when the space above is too
  // short, a bottom bar above the last line when the space below is.
  function computeToolbarPosition(anchor, size, viewport, topBoundary, placement) {
    const minTop = Math.max(0, topBoundary || 0) + MARGIN;
    const maxTop = viewport.height - size.height - MARGIN;
    const maxLeft = Math.max(MARGIN, viewport.width - size.width - MARGIN);
    const firstRight = anchor.left + anchor.width;
    let side = 'above';
    let top;
    let left;
    if (placement === 'bottom-left' || placement === 'bottom-center' || placement === 'bottom-right') {
      const lastTop = anchor.lastTop === undefined ? anchor.top : anchor.lastTop;
      const lastBottom = anchor.lastBottom === undefined ? anchor.bottom : anchor.lastBottom;
      side = 'below';
      top = lastBottom + GAP;
      if (top > maxTop) {
        side = 'above';
        top = lastTop - GAP - size.height;
      }
      const lastLeft = anchor.lastLeft === undefined ? anchor.left : anchor.lastLeft;
      const lastRight = anchor.lastRight === undefined ? firstRight : anchor.lastRight;
      if (placement === 'bottom-left') left = lastLeft;
      else if (placement === 'bottom-right') left = lastRight;
      else left = (lastLeft + lastRight) / 2 - size.width / 2;
    } else {
      top = anchor.top - GAP - size.height;
      if (top < minTop) {
        side = 'below';
        top = anchor.bottom + GAP;
      }
      if (placement === 'top-left') left = anchor.left;
      else if (placement === 'top-right') left = firstRight;
      else left = anchor.left + anchor.width / 2 - size.width / 2;
    }
    top = Math.max(minTop, Math.min(top, maxTop));
    left = Math.min(Math.max(left, MARGIN), maxLeft);
    return { top: Math.round(top), left: Math.round(left), placement: side };
  }

  // Whether `selection` was made backwards (its focus before its anchor).
  function isBackward(selection) {
    if (!selection) return false;
    if (typeof selection.direction === 'string') return selection.direction === 'backward';
    const anchor = selection.anchorNode;
    const focus = selection.focusNode;
    if (!anchor || !focus) return false;
    if (anchor === focus) return selection.focusOffset < selection.anchorOffset;
    return !!(anchor.compareDocumentPosition(focus) & 2); // Node.DOCUMENT_POSITION_PRECEDING
  }

  // Where the bar points: the first line of the selection (top placements, so
  // a multi-line selection gets the bar where it starts), the last line
  // (bottom placements) and the bottom of the whole selection (flipped bar).
  function anchorRect(range) {
    const bounds = range.getBoundingClientRect();
    const rects = range.getClientRects ? range.getClientRects() : [];
    let first = null;
    let last = null;
    for (let i = 0; i < rects.length; i++) {
      if (rects[i].width > 0 && rects[i].height > 0) {
        if (!first) first = rects[i];
        last = rects[i];
      }
    }
    first = first || bounds;
    last = last || bounds;
    return {
      top: first.top, left: first.left, width: first.width, bottom: bounds.bottom,
      lastTop: last.top, lastLeft: last.left, lastRight: last.right, lastBottom: last.bottom,
    };
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
  //   placement()    one of PLACEMENTS, read each time the bar shows
  //   pinned()       [{ text, title, emoji, shortcut, hideShortcut, display }]: one button per pinned
  //                  comment template before Comment, read each time the bar
  //                  shows; `display` is emoji, text or both
  //   showShortcuts() false to hide Comment's key; a template hides its own
  //                  through `hideShortcut`
  //   buttonStyle()  one of BUTTON_STYLES, what the Comment button shows; read
  //                  each time the bar shows (an icon-less button keeps its text)
  //   onPinned(data, item) called when a pinned button is clicked
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
    let icon = null;
    if (opts.icon) {
      icon = doc.createElement('span');
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
    // Pinned templates come first: they are the shortest path, one click.
    const pinnedGroup = doc.createElement('span');
    pinnedGroup.className = 'crit-selection-toolbar-pinned';
    pinnedGroup.hidden = true;
    bar.appendChild(pinnedGroup);
    bar.appendChild(button);
    doc.body.appendChild(bar);

    function showShortcuts() {
      return opts.showShortcuts ? opts.showShortcuts() !== false : true;
    }

    function appendKeys(parent, binding) {
      shortcutParts(binding).forEach(function(part) {
        const kbd = doc.createElement('kbd');
        kbd.textContent = part;
        parent.appendChild(kbd);
      });
    }

    // One button per pinned template, showing what its `display` asks for; its
    // text whenever it has no emoji, so no button is empty. Like Comment, a
    // press keeps the selection and the focus.
    function renderPinned() {
      const items = opts.pinned ? opts.pinned() : [];
      pinnedGroup.textContent = '';
      items.forEach(function(item) {
        const pin = doc.createElement('button');
        pin.type = 'button';
        pin.className = 'crit-selection-toolbar-btn crit-selection-toolbar-pin';
        const withEmoji = item.display !== 'text' && !!item.emoji;
        if (withEmoji) {
          const emoji = doc.createElement('span');
          emoji.className = 'crit-selection-toolbar-pin-emoji';
          emoji.setAttribute('aria-hidden', 'true');
          emoji.textContent = item.emoji;
          pin.appendChild(emoji);
        }
        if (item.display !== 'emoji' || !withEmoji) {
          const text = doc.createElement('span');
          text.className = 'crit-selection-toolbar-pin-text';
          text.textContent = item.title || item.text;
          pin.appendChild(text);
        }
        if (item.shortcut) {
          pin.setAttribute('aria-keyshortcuts', shortcutParts(item.shortcut).join('+'));
          if (!item.hideShortcut) {
            const hintKeys = doc.createElement('span');
            hintKeys.className = 'crit-selection-toolbar-shortcut';
            hintKeys.setAttribute('aria-hidden', 'true');
            appendKeys(hintKeys, item.shortcut);
            pin.appendChild(hintKeys);
          }
        }
        pin.title = item.text;
        pin.setAttribute('aria-label', 'Comment "' + (item.title || item.text) + '"');
        pin.addEventListener('mousedown', function(e) { e.preventDefault(); });
        pin.addEventListener('click', function() {
          const current = payload;
          hide();
          if (current && opts.onPinned) opts.onPinned(current, item);
        });
        pinnedGroup.appendChild(pin);
      });
      pinnedGroup.hidden = items.length === 0;
    }

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

    function renderButtonStyle() {
      const style = opts.buttonStyle ? opts.buttonStyle() : 'both';
      if (icon) icon.hidden = style === 'text';
      label.hidden = style === 'icon' && !!icon;
      button.classList.toggle('crit-selection-toolbar-btn--icon', style === 'icon' && !!icon);
    }

    function renderShortcut() {
      const binding = opts.shortcut ? opts.shortcut() : '';
      const parts = shortcutParts(binding);
      hint.textContent = '';
      appendKeys(hint, binding);
      hint.hidden = parts.length === 0 || !showShortcuts();
      if (parts.length) button.setAttribute('aria-keyshortcuts', parts.join('+'));
      else button.removeAttribute('aria-keyshortcuts');
    }

    // Show the bar for `range` (a live Range over the selection); `data` is
    // handed back to onComment or onPinned when a button is clicked.
    function show(range, data) {
      payload = data;
      shown = snapshot();
      renderButtonStyle();
      renderShortcut();
      renderPinned();
      bar.style.visibility = 'hidden';
      bar.hidden = false;
      const pos = computeToolbarPosition(
        anchorRect(range),
        { width: bar.offsetWidth, height: bar.offsetHeight },
        { width: win.innerWidth, height: win.innerHeight },
        opts.topBoundary ? opts.topBoundary() : 0,
        resolvePlacement(opts.placement ? opts.placement() : DEFAULT_PLACEMENT, isBackward(doc.getSelection()))
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
    PLACEMENTS: PLACEMENTS,
    PLACEMENT_OPTIONS: PLACEMENT_OPTIONS,
    DEFAULT_PLACEMENT: DEFAULT_PLACEMENT,
    BUTTON_STYLES: BUTTON_STYLES,
    resolvePlacement: resolvePlacement,
    isBackward: isBackward,
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
