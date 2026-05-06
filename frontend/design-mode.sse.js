// design-mode.sse.js — SSE round-start handler.
//
// Subscribes to /api/events and reacts to `design-round-start` events by
// resetting per-round state (resolution cache, _roundResolved flags),
// updating the round counter, scheduling lazy resolution for the current
// pathname, and announcing via aria-live.
//
// The actual scheduling/announcing helpers live on the controller; this
// module just wires the SSE connection and calls them. file-changed and
// other event kinds are owned by app.js's code-review handlers and ignored
// here.
'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.design = root.crit.design || {};
    root.crit.design.sse = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {

  // create — returns { applyRoundStart, install }.
  //
  // deps:
  //   state                       — design state object
  //   pinsByRoute                 — () => { [path]: pin[] }
  //   scheduleResolutionForPath   — (path) => void
  //   announceLive                — (msg) => void
  //   setUIState                  — (s) => void  (for state transition on round start)
  function create(deps) {
    deps = deps || {};
    var state = deps.state;
    var pinsByRoute = deps.pinsByRoute || function () { return {}; };
    var scheduleResolutionForPath = deps.scheduleResolutionForPath || function () {};
    var announceLive = deps.announceLive || function () {};
    var setUIState = deps.setUIState || function () {};

    function applyRoundStart(roundN) {
      state.currentRound = roundN;
      state.resolutionCache = {};
      state.userActedThisRound = false;
      var by = pinsByRoute();
      Object.keys(by).forEach(function (path) {
        by[path].forEach(function (p) { p._roundResolved = false; });
      });
      var rcEl = (typeof document !== 'undefined' && document.getElementById)
        ? document.getElementById('designRoundCounter')
        : null;
      if (rcEl) rcEl.textContent = roundN > 1 ? 'Round #' + roundN : '';
      setUIState('reviewing');
      scheduleResolutionForPath(state.currentPathname || state.currentRoute || '/');
      announceLive('Round ' + roundN + ' started.');
    }

    function install() {
      var es;
      try { es = new EventSource('/api/events'); } catch (_) { return; }
      es.addEventListener('design-round-start', function (ev) {
        var payload;
        try { payload = JSON.parse(ev.data); } catch (_) { return; }
        if (!payload || typeof payload.round !== 'number') return;
        applyRoundStart(payload.round);
      });
      state.designSSE = es;
      return es;
    }

    return {
      applyRoundStart: applyRoundStart,
      install: install,
    };
  }

  return { create: create };
});
