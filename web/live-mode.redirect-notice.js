// live-mode.redirect-notice.js — cross-origin redirect notice UI.
//
// When the iframe's agent posts a `cross-origin-redirect` message (because
// the page tried to navigate to a different origin we can't proxy), we show
// a banner inside the iframe frame with "Open in real browser" + Dismiss
// actions. Esc key dismisses the notice (and any iframe-error banner).
'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.live = root.crit.live || {};
    root.crit.live.redirectNotice = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {

  // openableURL returns the URL to open from the notice, or '' when it must
  // not be opened. The message comes from the framed page, which is
  // untrusted (live app or previewed file). The chrome opens the URL as a
  // top-level, unsandboxed window, so a URL on the daemon's own origin (say
  // /files/<the previewed file>.html) would run the page's script with full
  // API access, and a javascript: URL could run in a context the page chose.
  // Only http(s) URLs on another origin are opened.
  function openableURL(raw, selfOrigin) {
    var u;
    try { u = new URL(String(raw || ''), selfOrigin); } catch (_) { return ''; }
    if (u.protocol !== 'http:' && u.protocol !== 'https:') return '';
    if (u.origin === selfOrigin) return '';
    return u.href;
  }

  // install — wires the postMessage + Escape listeners.
  //
  // deps:
  //   els         — { iframe, frame }
  //   shared      — window.crit.shared (for escapeHTML)
  function install(deps) {
    deps = deps || {};
    var els = deps.els || {};
    var shared = deps.shared || (window.crit && window.crit.shared) || {};
    var escape = (shared && shared.escapeHTML) || function (s) {
      return String(s == null ? '' : s)
        .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
    };

    window.addEventListener('message', function (e) {
      if (!e || !e.data || typeof e.data !== 'object') return;
      if (e.data.type !== 'cross-origin-redirect') return;
      // Source check guards against stray messages from other windows; the
      // chrome only trusts messages originating from its own iframe.
      if (els.iframe && e.source && e.source !== els.iframe.contentWindow) return;
      var url = openableURL(e.data.url, window.location.origin);
      if (!url) return;
      var existing = document.querySelector('.crit-live-redirect-notice');
      if (existing) existing.remove();
      var n = document.createElement('div');
      n.className = 'crit-live-redirect-notice';
      n.innerHTML =
        'Live mode can\'t follow you to <code>' + escape(url) + '</code>. ' +
        '<button type="button" class="crit-live-redirect-open">Open in real browser</button>' +
        '<button type="button" class="crit-live-redirect-dismiss">Dismiss</button>';
      n.querySelector('.crit-live-redirect-open').addEventListener('click', function () {
        window.open(url, '_blank', 'noopener');
      });
      n.querySelector('.crit-live-redirect-dismiss').addEventListener('click', function () { n.remove(); });
      if (els.frame) els.frame.appendChild(n);
    });

    // Esc dismisses chrome notices (redirect notice takes priority over
    // the iframe-error banner so a single Esc press always clears the
    // most-recently-relevant overlay).
    document.addEventListener('keydown', function (e) {
      if (e.key !== 'Escape') return;
      var notice = document.querySelector('.crit-live-redirect-notice');
      if (notice) { notice.remove(); return; }
      var err = document.querySelector('.crit-live-iframe-error');
      if (err) { err.remove(); }
    });
  }

  return { install: install, openableURL: openableURL };
});
