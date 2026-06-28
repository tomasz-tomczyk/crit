'use strict';
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.agent = root.crit.agent || {};
    root.crit.agent.scrollUtils = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {

  function isScrollableBox(el, win) {
    if (!el || !el.style) return false;
    try {
      var style = win.getComputedStyle(el);
      var oy = style.overflowY;
      var ox = style.overflowX;
      var scrollY = (oy === 'auto' || oy === 'scroll') && el.scrollHeight > el.clientHeight + 1;
      var scrollX = (ox === 'auto' || ox === 'scroll') && el.scrollWidth > el.clientWidth + 1;
      return scrollY || scrollX;
    } catch (_) {
      return false;
    }
  }

  function isDocumentScrollRoot(el, doc) {
    return el === doc.documentElement || el === doc.body || el === doc.scrollingElement;
  }

  // Nearest ancestor with overflow scroll/auto and scrollable overflow.
  function findScrollContainer(el, doc) {
    var node = el && el.parentElement;
    while (node && node !== doc.documentElement) {
      if (isScrollableBox(node, doc.defaultView || window)) return node;
      node = node.parentElement;
    }
    return doc.scrollingElement || doc.documentElement;
  }

  // Every scrollable ancestor from innermost to outermost, terminated by the
  // document scroll root. Nested scrollers (e.g. a list inside a horizontal
  // carousel inside the page) each need adjusting, not just the closest one.
  function findScrollContainerChain(el, doc) {
    var win = doc.defaultView || (typeof window !== 'undefined' ? window : null);
    var chain = [];
    var node = el && el.parentElement;
    while (node && node !== doc.documentElement) {
      if (isScrollableBox(node, win)) chain.push(node);
      node = node.parentElement;
    }
    chain.push(doc.scrollingElement || doc.documentElement);
    return chain;
  }

  // Skip scroll when at least half the element area is visible within the
  // relevant viewport (window for document root, container client rect otherwise).
  function isMostlyVisible(el, win, container, doc) {
    if (!el || !el.getBoundingClientRect) return true;
    var r = el.getBoundingClientRect();
    var bounds;
    if (container && doc && !isDocumentScrollRoot(container, doc)) {
      bounds = container.getBoundingClientRect();
    } else {
      bounds = {
        top: 0,
        left: 0,
        bottom: win.innerHeight || 0,
        right: win.innerWidth || 0,
      };
    }
    var visibleH = Math.max(0, Math.min(r.bottom, bounds.bottom) - Math.max(r.top, bounds.top));
    var visibleW = Math.max(0, Math.min(r.right, bounds.right) - Math.max(r.left, bounds.left));
    var visibleArea = visibleH * visibleW;
    var totalArea = Math.max(r.height, 1) * Math.max(r.width, 1);
    return visibleArea >= totalArea * 0.5;
  }

  // True on-screen visibility: the viewport clipped by every non-root ancestor
  // scroll container's rect. An element nested inside a scrolled-away container
  // looks "visible relative to its container" yet is off-screen — that case must
  // not short-circuit scrolling, so we intersect the whole chain instead.
  function isMostlyVisibleInChain(el, win, chain, doc) {
    if (!el || !el.getBoundingClientRect) return true;
    var r = el.getBoundingClientRect();
    var bounds = {
      top: 0,
      left: 0,
      bottom: win.innerHeight || 0,
      right: win.innerWidth || 0,
    };
    for (var i = 0; i < chain.length; i++) {
      var c = chain[i];
      if (isDocumentScrollRoot(c, doc) || !c.getBoundingClientRect) continue;
      var cRect = c.getBoundingClientRect();
      bounds.top = Math.max(bounds.top, cRect.top);
      bounds.left = Math.max(bounds.left, cRect.left);
      bounds.bottom = Math.min(bounds.bottom, cRect.bottom);
      bounds.right = Math.min(bounds.right, cRect.right);
    }
    var visibleH = Math.max(0, Math.min(r.bottom, bounds.bottom) - Math.max(r.top, bounds.top));
    var visibleW = Math.max(0, Math.min(r.right, bounds.right) - Math.max(r.left, bounds.left));
    var visibleArea = visibleH * visibleW;
    var totalArea = Math.max(r.height, 1) * Math.max(r.width, 1);
    return visibleArea >= totalArea * 0.5;
  }

  function scrollWithinContainer(el, container, win, doc) {
    var rect = el.getBoundingClientRect();
    if (isDocumentScrollRoot(container, doc)) {
      var scrollY = win.scrollY || win.pageYOffset || 0;
      var scrollX = win.scrollX || win.pageXOffset || 0;
      var targetY = scrollY + rect.top - (win.innerHeight / 2) + (rect.height / 2);
      var targetX = scrollX + rect.left - (win.innerWidth / 2) + (rect.width / 2);
      try {
        win.scrollTo({ left: targetX, top: targetY, behavior: 'auto' });
      } catch (_) {
        try { win.scrollTo(targetX, targetY); } catch (_2) { /* noop */ }
      }
      return;
    }
    var cRect = container.getBoundingClientRect();
    if (rect.top < cRect.top) {
      container.scrollTop += rect.top - cRect.top;
    } else if (rect.bottom > cRect.bottom) {
      container.scrollTop += rect.bottom - cRect.bottom;
    }
    if (rect.left < cRect.left) {
      container.scrollLeft += rect.left - cRect.left;
    } else if (rect.right > cRect.right) {
      container.scrollLeft += rect.right - cRect.right;
    }
  }

  // Scroll anchored element into view without disturbing document-level scroll
  // hijackers when possible. Walks every scrollable ancestor from innermost to
  // outermost so deeply nested targets are revealed, not just the closest box.
  // Returns true when at least one scroll was attempted.
  function scrollIntoNearestContainer(el, opts) {
    if (!el) return false;
    var win = (opts && opts.win) || window;
    var doc = (opts && opts.doc) || win.document;
    var chain = findScrollContainerChain(el, doc);
    if (isMostlyVisibleInChain(el, win, chain, doc)) return false;
    var unsafe = !!(opts && opts.unsafeDocumentScroll);
    var scrolled = false;
    for (var i = 0; i < chain.length; i++) {
      var container = chain[i];
      // Re-measure each step: scrolling an inner container shifts the element,
      // so the outer adjustment must read the updated position.
      if (isDocumentScrollRoot(container, doc)) {
        if (unsafe) continue;
        // Only center on the document when the element is still off-screen
        // after the inner containers have done their part.
        if (isMostlyVisibleInChain(el, win, chain, doc)) continue;
      }
      scrollWithinContainer(el, container, win, doc);
      scrolled = true;
    }
    return scrolled;
  }

  // Heuristics for pages that treat document scroll as navigation (slide decks,
  // fullpage.js, smooth-scroll libs). Cached once at agent boot.
  function detectUnsafeDocumentScroll(doc, win) {
    var html = doc.documentElement;
    var body = doc.body;
    if (!html || !body) return false;

    function overflowHidden(el) {
      try {
        var s = win.getComputedStyle(el);
        return s.overflow === 'hidden' || s.overflowY === 'hidden';
      } catch (_) {
        return false;
      }
    }

    if (overflowHidden(html) && overflowHidden(body)) {
      var vh = win.innerHeight || html.clientHeight || 0;
      var bodyH = body.scrollHeight || body.clientHeight || 0;
      if (vh > 0 && bodyH <= vh * 1.1) return true;
    }

    function hasScrollSnap(el) {
      try {
        var snap = win.getComputedStyle(el).scrollSnapType || '';
        return snap !== '' && snap !== 'none';
      } catch (_) {
        return false;
      }
    }
    if (hasScrollSnap(html) || hasScrollSnap(body)) return true;

    if (win.fullpage_api || win.Lenis || win.locomotiveScroll) return true;
    if (doc.querySelector('#fullpage, .fullpage-wrapper, [data-fullpage]')) return true;

    return false;
  }

  return {
    isScrollableBox,
    isDocumentScrollRoot,
    findScrollContainer,
    findScrollContainerChain,
    isMostlyVisible,
    isMostlyVisibleInChain,
    scrollWithinContainer,
    scrollIntoNearestContainer,
    detectUnsafeDocumentScroll,
  };
});
