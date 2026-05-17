// live-mode.sse.js — SSE handlers for live mode.
//
// Subscribes to /api/events and reacts to:
//   - `live-round-start`: resets per-round state (resolution cache,
//     _roundResolved flags), updates the round counter, schedules lazy
//     resolution for the current pathname, announces via aria-live.
//   - `comments-changed`: re-fetches comments and re-renders the panel so
//     CLI-driven mutations (`crit comment --reply-to`, etc.) appear live
//     without a manual refresh.
//
// The scheduling/announcing/reload helpers live on the controller; this
// module just wires the SSE connection and calls them. file-changed is
// owned by app.js's code-review handlers and ignored here.
'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.live = root.crit.live || {};
    root.crit.live.sse = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {

  // create — returns { applyRoundStart, install }.
  //
  // deps:
  //   state                       — live state object
  //   pinsByRoute                 — () => { [path]: pin[] }
  //   scheduleResolutionForPath   — (path) => void
  //   announceLive                — (msg) => void
  //   setUIState                  — (s) => void  (for state transition on round start)
  //   reloadComments              — () => Promise<void>  (handler for comments-changed)
  //   reloadIframe                — () => void  (re-set iframe.src on round transition)
  function create(deps) {
    deps = deps || {};
    var state = deps.state;
    var pinsByRoute = deps.pinsByRoute || function () { return {}; };
    var scheduleResolutionForPath = deps.scheduleResolutionForPath || function () {};
    var announceLive = deps.announceLive || function () {};
    var setUIState = deps.setUIState || function () {};
    var reloadComments = deps.reloadComments || function () { return Promise.resolve(); };
    var reloadIframe = deps.reloadIframe || function () {};

    // Dedup guard — if a burst of comments-changed events arrives while a
    // reload is in flight, coalesce them into a single trailing reload.
    var reloadInFlight = null;
    var reloadPending = false;
    function applyCommentsChanged() {
      if (reloadInFlight) {
        reloadPending = true;
        return reloadInFlight;
      }
      reloadInFlight = Promise.resolve()
        .then(function () { return reloadComments(); })
        .catch(function (err) {
          // Don't break the SSE stream if a reload throws — log and move on.
          if (typeof console !== 'undefined' && console.warn) {
            console.warn('[live-mode] comments-changed reload failed:', err);
          }
        })
        .then(function () {
          reloadInFlight = null;
          if (reloadPending) {
            reloadPending = false;
            return applyCommentsChanged();
          }
          return null;
        });
      return reloadInFlight;
    }

    function applyRoundStart(roundN) {
      state.currentRound = roundN;
      state.resolutionCache = {};
      state.userActedThisRound = false;
      var by = pinsByRoute();
      Object.keys(by).forEach(function (path) {
        by[path].forEach(function (p) { p._roundResolved = false; });
      });
      var rcEl = (typeof document !== 'undefined' && document.getElementById)
        ? document.getElementById('liveRoundCounter')
        : null;
      if (rcEl) rcEl.textContent = roundN > 1 ? 'Round #' + roundN : '';
      setUIState('reviewing');
      // Re-fetch the canonical comment list. Replies posted during the
      // previous round (e.g. via `crit comment --reply-to`) might still be
      // in flight or emitted via a comments-changed event that races with
      // this round-start re-render. Pulling fresh state here makes the
      // re-render authoritative regardless of event timing.
      applyCommentsChanged();
      // Reload the iframe so reviewers see the agent's freshly-rendered UI
      // for round N+1. Without this the proxied page kept its previous
      // round's DOM and stale assets, even after the agent shipped fixes.
      // TODO: skip the reload if a comment composer is currently focused
      // and dirty (non-trivial dirty-detection — requires reaching into the
      // composer module's per-pin draft state). Always-reload is safe for
      // now because round transitions are agent-driven, not user-driven.
      reloadIframe();
      scheduleResolutionForPath(state.currentRoute || '/');
      announceLive('Round ' + roundN + ' started.');
    }

    function showLiveDisconnected() {
      var shared = window.crit && window.crit.shared;
      if (shared && typeof shared.showDisconnected === 'function') {
        shared.showDisconnected();
      }
    }

    function install() {
      if (!window.crit || !window.crit.sse) return;
      var conn = window.crit.sse.createSSE('/api/events', {
        'live-round-start': function (data) {
          if (!data || typeof data.round !== 'number') return;
          applyRoundStart(data.round);
        },
        'comments-changed': function () {
          // Server emits this on any comment mutation (add/edit/delete/reply
          // /resolve), including CLI-driven writes via `crit comment`. The
          // payload is informational only — we always re-fetch the canonical
          // list so we don't have to mirror reconciliation rules client-side.
          applyCommentsChanged();
        },
        'server-shutdown': function () {
          conn.close();
          showLiveDisconnected();
        },
      });
      state.liveSSE = conn;
      return conn;
    }

    return {
      applyRoundStart: applyRoundStart,
      applyCommentsChanged: applyCommentsChanged,
      install: install,
    };
  }

  return { create: create };
});
