'use strict';
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.design = root.crit.design || {};
    root.crit.design.threadScroll = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  function scrollThreadToPin(doc, pinId, opts) {
    if (!doc || !pinId) return false;
    const sel = `[data-comment-id="${String(pinId).replace(/"/g, '\\"')}"]`;
    const el = doc.querySelector(sel);
    if (!el) return false;
    if (opts && opts.scroller) opts.scroller(el);
    else if (typeof el.scrollIntoView === 'function') el.scrollIntoView({ behavior: 'smooth', block: 'center' });
    return true;
  }
  return { scrollThreadToPin };
});
