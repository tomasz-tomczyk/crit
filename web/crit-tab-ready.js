// crit-tab-ready.js — tab title badge + browser Notification when a review
// round becomes ready while the page is hidden.
//
// Browser: attach via window.crit.createTabReady / window.crit.tabReady.
// Node path: module.exports for unit tests.
'use strict';

function createTabReady(deps) {
  deps = deps || {};
  var documentRef = deps.document || (typeof document !== 'undefined' ? document : null);
  var NotificationCtor = deps.Notification;
  if (NotificationCtor === undefined && typeof Notification !== 'undefined') {
    NotificationCtor = Notification;
  }
  var BADGE_PREFIX = '\u25CF ';
  var baseTitle = deps.baseTitle || (documentRef && documentRef.title) || '';
  var badgeActive = false;
  var lastNotification = null;

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
    if (visibilityState() !== 'hidden' && !opts.force) return { badged: false, notified: false };

    setTabBadge();

    var notified = false;
    if (NotificationCtor && NotificationCtor.permission === 'granted') {
      try {
        var title = opts.title || 'Crit';
        var body = opts.body || 'A review round is ready';
        lastNotification = new NotificationCtor(title, {
          body: body,
          tag: opts.tag || 'crit-round-ready',
        });
        notified = true;
      } catch (_) {
        notified = false;
      }
    }

    return { badged: true, notified: notified };
  }

  function requestPermission() {
    if (!NotificationCtor || typeof NotificationCtor.requestPermission !== 'function') {
      return Promise.resolve('denied');
    }
    return Promise.resolve(NotificationCtor.requestPermission());
  }

  return {
    setDocumentTitle: setDocumentTitle,
    setTabBadge: setTabBadge,
    clearTabBadge: clearTabBadge,
    notifyRoundReady: notifyRoundReady,
    requestPermission: requestPermission,
    isBadgeActive: function () { return badgeActive; },
    lastNotification: function () { return lastNotification; },
    BADGE_PREFIX: BADGE_PREFIX,
  };
}

var api = { create: createTabReady };

if (typeof window !== 'undefined') {
  window.crit = window.crit || {};
  window.crit.createTabReady = createTabReady;
}
if (typeof module === 'object' && module.exports) module.exports = api;
