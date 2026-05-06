'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.design = root.crit.design || {};
    root.crit.design.toggle = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  function reduceToggle(mode) { return mode === 'pin' ? 'navigate' : 'pin'; }
  return { reduceToggle };
});
