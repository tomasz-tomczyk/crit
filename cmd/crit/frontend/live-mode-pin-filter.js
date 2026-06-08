'use strict';
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.live = root.crit.live || {};
    root.crit.live.pinFilter = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  function normalise(p) {
    if (!p || p === '/') return '/';
    return p.endsWith('/') ? p.slice(0, -1) : p;
  }
  function filterPinsForPath(pins, pathname) {
    if (!Array.isArray(pins) || !pathname) return [];
    var norm = normalise(pathname);
    // Resolved comments must not surface as pin markers on the proxied page —
    // the marker overlay paints whatever is in `set-pins`, so dropping
    // resolved pins here is the single source of truth for visibility.
    return pins.filter(p => p && p.dom_anchor && normalise(p.dom_anchor.pathname) === norm && !p.resolved);
  }
  return { filterPinsForPath };
});
