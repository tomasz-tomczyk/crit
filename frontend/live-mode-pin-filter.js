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
  function filterPinsForPath(pins, pathname) {
    if (!Array.isArray(pins) || !pathname) return [];
    // Resolved comments must not surface as pin markers on the proxied page —
    // the marker overlay paints whatever is in `set-pins`, so dropping
    // resolved pins here is the single source of truth for visibility.
    return pins.filter(p => p && p.dom_anchor && p.dom_anchor.pathname === pathname && !p.resolved);
  }
  return { filterPinsForPath };
});
