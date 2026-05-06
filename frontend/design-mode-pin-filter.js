'use strict';
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else { root.crit = root.crit || {}; root.crit.designModePinFilter = api; }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  function filterPinsForPath(pins, pathname) {
    if (!Array.isArray(pins) || !pathname) return [];
    return pins.filter(p => p && p.dom_anchor && p.dom_anchor.pathname === pathname);
  }
  return { filterPinsForPath };
});
