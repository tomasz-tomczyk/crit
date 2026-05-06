'use strict';
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.design = root.crit.design || {};
    root.crit.design.reanchorPut = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  function buildReanchorRequest(pinId, domAnchor) {
    return {
      method: 'PUT',
      url: `/api/comment/${encodeURIComponent(pinId)}?path=${encodeURIComponent(domAnchor.pathname)}`,
      body: JSON.stringify({ dom_anchor: domAnchor }),
      headers: { 'Content-Type': 'application/json' },
    };
  }
  return { buildReanchorRequest };
});
