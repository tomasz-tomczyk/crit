// agent-screenshot-options.js — builds the html2canvas options object used by
// captureScreenshot in crit-agent.js.
//
// Why this exists: html2canvas defaults backgroundColor to '#ffffff' when the
// captured element has no own background. Most real apps put theme background
// on <html> or <body> via CSS variables, not on every leaf element. On dark
// themes, the default white background flooded the rasterised pin screenshot
// to pure white — the reviewer's dark UI vanished entirely.
//
// Strategy: walk target -> ancestors -> body -> html and pick the first
// non-transparent computed background-color we find. Pass that to html2canvas
// so the output reflects what the reviewer actually saw.
'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.agent = root.crit.agent || {};
    root.crit.agent.screenshotOptions = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {

  // isTransparent — treat the standard "no background" computed values as
  // transparent so we keep walking up the tree.
  function isTransparent(color) {
    if (!color) return true;
    var c = String(color).replace(/\s+/g, '').toLowerCase();
    if (c === 'transparent') return true;
    // rgba(...,0) — alpha exactly zero. Anything with non-zero alpha counts as
    // a real background even if very light, because that's still the page's
    // chosen colour and forcing it onto the screenshot is correct.
    var m = c.match(/^rgba?\(([^)]+)\)$/);
    if (m) {
      var parts = m[1].split(',');
      if (parts.length === 4 && parseFloat(parts[3]) === 0) return true;
    }
    return false;
  }

  // resolvePageBackground — returns the first non-transparent computed
  // background-color walking target -> ancestors -> body -> html. Returns null
  // if nothing has a background (rare; html2canvas will then keep its default).
  //
  // deps:
  //   target           — Element to capture (may be null)
  //   doc              — Document (defaults to global document)
  //   getComputedStyle — function (el) => { backgroundColor }
  //                      (defaults to global getComputedStyle)
  function resolvePageBackground(target, doc, getComputedStyleFn) {
    doc = doc || (typeof document !== 'undefined' ? document : null);
    getComputedStyleFn = getComputedStyleFn
      || (typeof window !== 'undefined' && window.getComputedStyle
        ? function (el) { return window.getComputedStyle(el); }
        : null);
    if (!doc || !getComputedStyleFn) return null;

    var chain = [];
    var el = target;
    while (el && el.nodeType === 1) {
      chain.push(el);
      el = el.parentElement;
    }
    if (doc.body && chain.indexOf(doc.body) === -1) chain.push(doc.body);
    if (doc.documentElement && chain.indexOf(doc.documentElement) === -1) {
      chain.push(doc.documentElement);
    }

    for (var i = 0; i < chain.length; i++) {
      try {
        var cs = getComputedStyleFn(chain[i]);
        if (!cs) continue;
        var bg = cs.backgroundColor;
        if (!isTransparent(bg)) return bg;
      } catch (_) { /* getComputedStyle can throw on detached nodes */ }
    }
    return null;
  }

  // buildCaptureOptions — returns the options object for html2canvas(target, opts).
  //
  // - target:  Element being captured (used to derive the bounding rect crop +
  //            page background colour).
  // - rect:    target.getBoundingClientRect() result, or null. Used for the
  //            x/y/width/height crop. Document-coords (caller adds scrollX/Y).
  // - scroll:  { x, y } page scroll offsets.
  // - doc, getComputedStyleFn: dependency-injected for testability.
  //
  // Always writes opts.backgroundColor — to the resolved page colour when we
  // found one, or `null` (transparent) when we didn't. Never `undefined`,
  // because html2canvas treats `undefined` as "use the default white".
  function buildCaptureOptions(target, rect, scroll, doc, getComputedStyleFn) {
    var opts = { scale: 1, logging: false };
    if (rect && rect.width > 0 && rect.height > 0) {
      var sx = (scroll && typeof scroll.x === 'number') ? scroll.x : 0;
      var sy = (scroll && typeof scroll.y === 'number') ? scroll.y : 0;
      opts.x = Math.max(0, Math.floor(rect.left + sx));
      opts.y = Math.max(0, Math.floor(rect.top + sy));
      opts.width = Math.ceil(rect.width);
      opts.height = Math.ceil(rect.height);
    }
    var bg = resolvePageBackground(target, doc, getComputedStyleFn);
    opts.backgroundColor = bg !== null ? bg : null;
    return opts;
  }

  return {
    isTransparent: isTransparent,
    resolvePageBackground: resolvePageBackground,
    buildCaptureOptions: buildCaptureOptions,
  };
});
