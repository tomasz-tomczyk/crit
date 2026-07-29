// crit-tab-ready.js — tab title badge when a review round becomes ready
// while the page is hidden.
//
// Deliberately no browser Notification API: crit serves on an ephemeral
// localhost port, so a permission grant never carries over to the next run
// and Notification.permission is effectively always 'default'. Desktop
// notifications are the server's job instead, gated by the
// `notify_on_round_ready` config key (see internal/notify).
//
// Browser: attach via window.crit.createTabReady / window.crit.tabReady.
// Node path: module.exports for unit tests.
'use strict';

function createTabReady(deps) {
  deps = deps || {};
  var documentRef = deps.document || (typeof document !== 'undefined' ? document : null);
  var BADGE_PREFIX = '\u25CF ';
  var baseTitle = deps.baseTitle || (documentRef && documentRef.title) || '';
  var badgeActive = false;

  function setDocumentTitle(nextBase) {
    baseTitle = nextBase;
    if (documentRef) {
      documentRef.title = badgeActive ? BADGE_PREFIX + baseTitle : baseTitle;
    }
  }

  function setTabBadge() {
    if (badgeActive) return;
    badgeActive = true;
    if (documentRef && !documentRef.title.startsWith(BADGE_PREFIX)) {
      documentRef.title = BADGE_PREFIX + baseTitle;
    }
  }

  function clearTabBadge() {
    if (!badgeActive) return;
    badgeActive = false;
    if (documentRef) documentRef.title = baseTitle;
  }

  function visibilityState() {
    return documentRef ? documentRef.visibilityState : 'visible';
  }

  function notifyRoundReady(opts) {
    opts = opts || {};
    if (visibilityState() !== 'hidden' && !opts.force) return { badged: false };
    setTabBadge();
    return { badged: true };
  }

  return {
    setDocumentTitle: setDocumentTitle,
    setTabBadge: setTabBadge,
    clearTabBadge: clearTabBadge,
    notifyRoundReady: notifyRoundReady,
    isBadgeActive: function () { return badgeActive; },
    BADGE_PREFIX: BADGE_PREFIX,
  };
}

var api = { create: createTabReady };

if (typeof window !== 'undefined') {
  window.crit = window.crit || {};
  window.crit.createTabReady = createTabReady;
}
if (typeof module === 'object' && module.exports) module.exports = api;
