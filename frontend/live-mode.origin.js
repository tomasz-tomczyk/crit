'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.live = root.crit.live || {};
    root.crit.live.origin = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  function makeOriginGuard(opts) {
    var expectSource = opts.expectSource;
    var expectOrigin = opts.expectOrigin;
    return function check(ev) {
      if (ev.source !== expectSource) return false;
      if (ev.origin !== expectOrigin) return false;
      return true;
    };
  }
  return { makeOriginGuard };
});
