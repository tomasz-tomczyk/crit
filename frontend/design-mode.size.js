'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.design = root.crit.design || {};
    root.crit.design.size = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  var MAX_BYTES = 5 * 1024 * 1024;
  function selectionTooLarge(a) {
    var n = 0;
    var keys = ['css_selector', 'outer_html', 'accessible_name', 'role', 'landmark'];
    for (var i = 0; i < keys.length; i++) {
      if (a[keys[i]]) n += a[keys[i]].length;
    }
    return n > MAX_BYTES;
  }
  return { selectionTooLarge: selectionTooLarge, MAX_BYTES: MAX_BYTES };
});
