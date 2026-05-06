// design-route-utils.js — pure URL/route helpers shared by design-mode.js
// and Node-based tests. Browser path: <script src=...> attaches to
// window.crit.designUtils. Node path: module.exports.

(function (root, factory) {
  const api = factory();
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  root.crit = root.crit || {};
  root.crit.designUtils = api;
})(typeof window !== 'undefined' ? window : globalThis, function () {
  'use strict';

  function extractPathname(rawUrl) {
    if (!rawUrl || typeof rawUrl !== 'string') return '/';
    try {
      const u = new URL(rawUrl);
      return u.pathname || '/';
    } catch (_) {
      return '/';
    }
  }

  function normaliseRoute(p) {
    if (!p) return '/';
    if (p === '/') return '/';
    if (p.endsWith('/')) return p.slice(0, -1);
    return p;
  }

  function isSameOrigin(a, b) {
    try {
      const ua = new URL(a);
      const ub = new URL(b);
      return ua.protocol === ub.protocol && ua.hostname === ub.hostname && ua.port === ub.port;
    } catch (_) {
      return false;
    }
  }

  function groupCommentsByRoute(comments) {
    const m = new Map();
    for (const c of comments || []) {
      const key = normaliseRoute(c.path || '/');
      if (!m.has(key)) m.set(key, []);
      m.get(key).push(c);
    }
    return m;
  }

  return { extractPathname, normaliseRoute, isSameOrigin, groupCommentsByRoute };
});
