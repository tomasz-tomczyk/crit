'use strict';
//
// Marker overlay — coordinate-system reasoning (load-bearing; do not delete).
//
//   * Markers live INSIDE the proxied iframe's document, mounted under
//     <body>. They are `position: fixed` relative to the iframe's viewport,
//     not the chrome's viewport.
//
//   * Because they are fixed inside the iframe, they track iframe-internal
//     scrolling natively — we never compute scroll offsets manually.
//
//   * `getBoundingClientRect()` on the target element returns coords in the
//     iframe's viewport space. Those are the exact coords we want for our
//     `position: fixed` markers. No translation step is needed.
//
//   * Transformed ancestors are handled correctly by `getBoundingClientRect`.
//
//   * The chrome wraps the iframe in a horizontal-scroll container for narrow-
//     viewport simulation. That wrapper lives in CHROME space, not iframe space,
//     so it is irrelevant to markers.
//
// In short: `position: fixed` + `getBoundingClientRect` is the entire model.
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
    root.style.position = 'fixed';
    root.style.inset = '0';
    root.style.pointerEvents = 'none';
    root.style.zIndex = '2147483600';
    doc.body.appendChild(root);
    // markersById: pin_id -> { el, anchor, status, element, rect }
    const markersById = new Map();
    return { root, markersById };
  }

  function makeMarker(doc, pin, index) {
    const el = doc.createElement('div');
    el.className = 'crit-design-marker';
    el.setAttribute('role', 'button');
    el.setAttribute('tabindex', '0');
    el.setAttribute('data-pin-id', pin.id);
    // Pin number is GLOBAL within the review (REVISION). Fall back to index+1
    // for tests/back-compat, but production set-pins payloads carry pin_number.
    const number = (typeof pin.pin_number === 'number') ? pin.pin_number : (index + 1);
    el.setAttribute('aria-label', 'Comment ' + number);
    el.style.position = 'fixed';
    el.style.pointerEvents = 'auto';
    el.textContent = String(number);
    return el;
  }

  // Read all rects, then write all positions (no interleave).
  function applyRects(markers) {
    const reads = markers.map(m => (m.target ? m.target.getBoundingClientRect() : null));
    for (let i = 0; i < markers.length; i++) {
      const m = markers[i];
      const r = reads[i];
      if (!r) { m.el.style.display = 'none'; continue; }
      m.el.style.display = '';
      m.el.style.transform = `translate(${Math.round(r.left)}px, ${Math.round(r.top)}px)`;
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
