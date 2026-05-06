// design-mode.panel.js — pure helpers for design-mode's comments panel.
// Counting unresolved pins + drag-resize math live here so they can be unit
// tested without a DOM. The integration (DOM wiring, persistence) lives in
// design-mode.js.
'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.design = root.crit.design || {};
    root.crit.design.panel = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {

  // countUnresolved — counts comments with !resolved across all routes in
  // pinsByRoute. Accepts either a flat list of comments OR a route->comments
  // map (object). Returns a number (>= 0).
  function countUnresolved(input) {
    var n = 0;
    if (!input) return 0;
    if (Array.isArray(input)) {
      for (var i = 0; i < input.length; i++) {
        var c = input[i];
        if (c && !c.resolved) n++;
      }
      return n;
    }
    if (typeof input === 'object') {
      var keys = Object.keys(input);
      for (var k = 0; k < keys.length; k++) {
        var arr = input[keys[k]] || [];
        for (var j = 0; j < arr.length; j++) {
          var c2 = arr[j];
          if (c2 && !c2.resolved) n++;
        }
      }
    }
    return n;
  }

  // computeResizeWidth — pure drag math for the comments-panel resize handle.
  // The panel sits on the right of the viewport; dragging the handle leftward
  // increases the panel width. NO clamping against viewport-preset width by
  // design — user gets what they ask for. We only enforce a low floor (`min`,
  // default 200) so the panel can't disappear.
  //
  //   startWidth: panel width at pointerdown
  //   startX: pointer.clientX at pointerdown
  //   currentX: current pointer.clientX
  //   min: minimum panel width (default 200)
  function computeResizeWidth(startWidth, startX, currentX, min) {
    if (typeof min !== 'number' || min < 0) min = 200;
    var dx = currentX - startX;
    // Handle is on the LEFT edge of the panel; dragging left grows the panel.
    var w = startWidth - dx;
    if (w < min) w = min;
    return Math.round(w);
  }

  return {
    countUnresolved: countUnresolved,
    computeResizeWidth: computeResizeWidth,
  };
});
