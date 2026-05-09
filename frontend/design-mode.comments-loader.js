// design-mode.comments-loader.js — fetches the canonical design-mode
// comment list from the daemon and writes it onto state.comments.
//
// This module exists for one specific reason: `loadAllComments` used to
// derive its file list from `state.session.files`, which is only set once
// at boot. In design mode the very first `/api/session` returns
// `files: []` (the daemon doesn't add a FileEntry until the first pin
// POST). Subsequent reloads — `comments-changed` SSE after an agent reply,
// or `design-round-start` after a round bump — therefore short-circuited
// on `if (!files.length) return;` and left state.comments stale (most
// visibly: replies posted via `crit comment --reply-to` between rounds
// stayed invisible until a full browser refresh).
//
// Fix: refetch /api/session before reading the file list, so we always
// see the daemon's current truth. The session payload is small and the
// reload is already user-triggered (SSE event or round transition), so
// the extra roundtrip is unconditionally cheap.
'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.design = root.crit.design || {};
    root.crit.design.commentsLoader = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {

  // create — returns { loadAllComments }.
  //
  // deps:
  //   state   — design state object (we read state.currentRound and write
  //             state.session + state.comments)
  //   shared  — window.crit.shared (fetchJSON)
  function create(deps) {
    deps = deps || {};
    var state = deps.state;
    var shared = deps.shared;

    async function loadAllComments() {
      // Refresh session so a brand-new design daemon (which boots with
      // files: []) picks up file entries the daemon adds when pins are
      // created. Failing to refresh meant every comments-changed reload
      // was a no-op for design mode — the bug this module exists to fix.
      try {
        var freshSession = await shared.fetchJSON('/api/session');
        if (freshSession && typeof freshSession === 'object') {
          state.session = freshSession;
        }
      } catch (e) {
        // If /api/session is briefly unavailable (503 during a transition)
        // fall back to the stale session — better to render something than
        // wipe state.comments.
        if (typeof console !== 'undefined' && console.warn) {
          console.warn('[design-mode] session refresh failed:', e);
        }
      }

      var s = state.session || {};
      var files = (s.files || []).map(function (f) { return f.path; });
      if (!files.length) return;

      var results = await Promise.all(files.map(function (p) {
        return shared.fetchJSON('/api/file/comments?path=' + encodeURIComponent(p))
          .then(function (list) {
            if (!Array.isArray(list)) return [];
            return list.map(function (c) {
              // Use dom_anchor.pathname for design comments; fallback to
              // file path.
              var path = (c.dom_anchor && c.dom_anchor.pathname) || p;
              c.path = path;
              // Stamp _createdInRound from the persisted review_round so
              // the drift guard in handlePinResolutionResult survives
              // reloads (initial boot, SSE refresh,
              // refreshCommentsForRoute). Without this, navigating away
              // and back triggers a resolution scan whose results land
              // before route data settles, marking the just-created pin
              // as drifted.
              c._createdInRound = c.review_round || state.currentRound || 1;
              return c;
            });
          })
          .catch(function (e) {
            if (typeof console !== 'undefined' && console.warn) {
              console.warn('[design-mode] failed to load comments for', p, e);
            }
            return [];
          });
      }));
      state.comments = results.reduce(
        function (acc, arr) { return acc.concat(arr); },
        []
      );
    }

    return { loadAllComments: loadAllComments };
  }

  return { create: create };
});
