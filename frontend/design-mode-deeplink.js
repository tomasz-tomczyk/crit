'use strict';
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.design = root.crit.design || {};
    root.crit.design.deeplink = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  const PIN_RE = /^#pin=([A-Za-z0-9_-]+)$/;
  function parseDeepLink(hashStr) {
    if (!hashStr) return null;
    const m = PIN_RE.exec(hashStr);
    return m ? m[1] : null;
  }
  function serializePinFragment(pinId) {
    return '#pin=' + pinId;
  }
  function shouldClearOnRouteChange(state, newPath) {
    const open = state && state.openPin;
    if (!open || !open.dom_anchor) return false;
    return open.dom_anchor.pathname !== newPath;
  }
  return { parseDeepLink, serializePinFragment, shouldClearOnRouteChange };
});
