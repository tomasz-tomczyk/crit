'use strict';
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.agent = root.crit.agent || {};
    root.crit.agent.resolution = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {

  function getUtils(ctx) {
    if (ctx && ctx.utils) return ctx.utils;
    if (typeof require === 'function') {
      try { return require('./agent-anchor-utils.js'); } catch (e) { /* ignore */ }
    }
    if (typeof window !== 'undefined' && window.crit && window.crit.agent && window.crit.agent.anchorUtils) {
      return window.crit.agent.anchorUtils;
    }
    return null;
  }

  // Pure-read pin resolution. Reads getBoundingClientRect on resolved elements,
  // posts pin-resolution-result messages via ctx.post, optionally calls
  // ctx.onResolved(pin_id, element, status). Never writes to el.style — that's
  // the caller's batched write phase.
  function resolveAllAndEmit(ctx) {
    const utils = getUtils(ctx);
    if (!utils || !utils.resolvePin) return;
    const onPath = ctx.pathname;
    for (const pin of ctx.pins || []) {
      if (onPath && pin.dom_anchor && pin.dom_anchor.pathname !== onPath) continue;
      const r = utils.resolvePin(pin.dom_anchor, ctx.document);
      const msg = { type: 'pin-resolution-result', pin_id: pin.id, status: r.status };
      if (r.element && typeof r.element.getBoundingClientRect === 'function') {
        const b = r.element.getBoundingClientRect();
        msg.rect = { x: b.left, y: b.top, w: b.width, h: b.height };
      }
      if (r.recovered_via) msg.recovered_via = r.recovered_via;
      if (typeof ctx.post === 'function') ctx.post(msg);
      if (typeof ctx.onResolved === 'function') ctx.onResolved(pin.id, r.element, r.status);
    }
  }

  return { resolveAllAndEmit };
});
