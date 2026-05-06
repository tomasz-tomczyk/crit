// design-mode.redirect-notice.js — cross-origin redirect notice UI.
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
    root.crit.design = root.crit.design || {};
    root.crit.design.redirectNotice = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {

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
      var url = String(e.data.url || '');
      var existing = document.querySelector('.crit-design-redirect-notice');
      if (existing) existing.remove();
      var n = document.createElement('div');
      n.className = 'crit-design-redirect-notice';
      n.innerHTML =
        'Design mode can\'t follow you to <code>' + escape(url) + '</code>. ' +
        '<button type="button" class="crit-design-redirect-open">Open in real browser</button>' +
        '<button type="button" class="crit-design-redirect-dismiss">Dismiss</button>';
      n.querySelector('.crit-design-redirect-open').addEventListener('click', function () {
        window.open(url, '_blank', 'noopener');
      });
      n.querySelector('.crit-design-redirect-dismiss').addEventListener('click', function () { n.remove(); });
      if (els.frame) els.frame.appendChild(n);
    });

    // Esc dismisses chrome notices (redirect notice takes priority over
    // the iframe-error banner so a single Esc press always clears the
    // most-recently-relevant overlay).
    document.addEventListener('keydown', function (e) {
      if (e.key !== 'Escape') return;
      var notice = document.querySelector('.crit-design-redirect-notice');
      if (notice) { notice.remove(); return; }
      var err = document.querySelector('.crit-design-iframe-error');
      if (err) { err.remove(); }
    });
  }

  return { install: install };
});
