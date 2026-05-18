'use strict';
//
// Marker overlay — coordinate-system reasoning (load-bearing; do not delete).
//
//   * Markers live INSIDE the proxied iframe's document, mounted under a root
//     anchored at the document origin (position:absolute; top:0; left:0).
//
//   * Markers are `position: absolute` and positioned in DOCUMENT coords:
//        x = rect.left + scrollX
//        y = rect.top  + scrollY
//     With document-coord placement we don't need a scroll listener — when
//     the user scrolls, the marker scrolls naturally with the document.
//
//   * Why not `position: fixed`? Fixed markers stay glued to the viewport.
//     `getBoundingClientRect()` returns viewport coords, so as the page
//     scrolls the rect moves but our recompute only fires on mutations.
//     The marker drifts away from the element it annotates. That was Bug B.
//
//   * Transformed ancestors are still handled correctly by
//     `getBoundingClientRect` — we just add scroll offsets after.
//
// In short: `position: absolute` + `getBoundingClientRect` + scroll offsets
// is the entire model.
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.agent = root.crit.agent || {};
    root.crit.agent.markers = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {

  function createOverlay(doc) {
    const root = doc.createElement('div');
    root.setAttribute('id', 'crit-marker-root');
    root.setAttribute('aria-hidden', 'true');
    // Anchor at document origin so absolute-positioned children resolve
    // against page coords (not viewport coords). See top-of-file comment.
    root.style.position = 'absolute';
    root.style.top = '0';
    root.style.left = '0';
    root.style.pointerEvents = 'none';
    root.style.zIndex = '2147483600';
    doc.body.appendChild(root);
    // markersById: pin_id -> { el, anchor, status, element, rect }
    const markersById = new Map();
    return { root, markersById };
  }

  function makeMarker(doc, pin, index) {
    const el = doc.createElement('div');
    el.className = 'crit-live-marker';
    el.setAttribute('role', 'button');
    el.setAttribute('tabindex', '0');
    el.setAttribute('data-pin-id', pin.id);
    // Pin number is GLOBAL within the review (REVISION). Fall back to index+1
    // for tests/back-compat, but production set-pins payloads carry pin_number.
    const number = (typeof pin.pin_number === 'number') ? pin.pin_number : (index + 1);
    el.setAttribute('aria-label', 'Comment ' + number);
    el.style.position = 'absolute';
    el.style.top = '0';
    el.style.left = '0';
    el.style.pointerEvents = 'auto';
    el.textContent = String(number);
    return el;
  }

  // Read all rects, then write all positions (no interleave).
  // `win` is optional and defaults to the global window — pass it explicitly
  // from tests. Scroll offsets convert viewport coords (rect.left/top) into
  // document coords, so absolute-positioned markers stay anchored to the
  // element they annotate as the page scrolls.
  function applyRects(markers, win) {
    if (typeof win === 'undefined') {
      win = (typeof window !== 'undefined') ? window : { scrollX: 0, scrollY: 0 };
    }
    const sx = (win && typeof win.scrollX === 'number') ? win.scrollX : 0;
    const sy = (win && typeof win.scrollY === 'number') ? win.scrollY : 0;
    const reads = markers.map(m => {
      if (!m.target) return null;
      if (typeof m.target.isConnected !== 'undefined' && !m.target.isConnected) return null;
      const rect = m.target.getBoundingClientRect();
      if (rect.width === 0 && rect.height === 0) return null;
      return rect;
    });
    for (let i = 0; i < markers.length; i++) {
      const m = markers[i];
      const r = reads[i];
      if (!r) { m.el.style.display = 'none'; continue; }
      m.el.style.display = '';
      m.el.style.transform = `translate(${Math.round(r.left + sx)}px, ${Math.round(r.top + sy)}px)`;
    }
  }

  function setMarkersTabindex(markersById, value) {
    if (!markersById || !markersById.forEach) return;
    markersById.forEach(m => {
      if (m && m.el && typeof m.el.setAttribute === 'function') {
        m.el.setAttribute('tabindex', value);
      }
    });
  }

  return { createOverlay, makeMarker, applyRects, setMarkersTabindex };
});
