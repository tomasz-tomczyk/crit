// design-mode.inflight.js — small dedup helpers for async ops triggered
// from multiple sources (button click + Cmd+Enter, double-click, etc.).
//
// Two flavours:
//   - makeInFlightSet(): per-id Set guard for ops scoped to a comment id
//     (resolve toggle, reply submit, edit submit, drift PUT, comment create).
//   - makeInFlightFlag(): singleton boolean for ops with no id (finish review).
//
// Pattern (per-id):
//   if (guard.has(id)) return;
//   guard.add(id);
//   try { ... await ... } finally { guard.delete(id); }
//
// Pattern (singleton):
//   if (guard.busy()) return;
//   guard.set();
//   try { ... await ... } finally { guard.clear(); }
//
// Both are exported on window.crit.design.inflight in the browser, and via
// module.exports for Node tests.

(function (root, factory) {
  'use strict';
  var api = factory();
  if (typeof module !== 'undefined' && module.exports) {
    module.exports = api;
  }
  if (typeof window !== 'undefined') {
    root.crit = root.crit || {};
    root.crit.design = root.crit.design || {};
    root.crit.design.inflight = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  'use strict';

  function makeInFlightSet() {
    var s = new Set();
    return {
      has: function (id) { return s.has(id); },
      add: function (id) { s.add(id); },
      delete: function (id) { s.delete(id); },
      size: function () { return s.size; },
      // Convenience: wrap an async fn so concurrent calls for the same id
      // resolve to undefined instead of running twice.
      run: async function (id, fn) {
        if (s.has(id)) return undefined;
        s.add(id);
        try { return await fn(); }
        finally { s.delete(id); }
      },
    };
  }

  function makeInFlightFlag() {
    var busy = false;
    return {
      busy: function () { return busy; },
      set: function () { busy = true; },
      clear: function () { busy = false; },
      run: async function (fn) {
        if (busy) return undefined;
        busy = true;
        try { return await fn(); }
        finally { busy = false; }
      },
    };
  }

  return {
    makeInFlightSet: makeInFlightSet,
    makeInFlightFlag: makeInFlightFlag,
  };
});
