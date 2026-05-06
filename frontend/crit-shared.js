// crit-shared.js — helpers consumed by both app.js (code review) and
// design-mode.js (design review). Vanilla JS, no module loader.
//
// Exports onto window.crit.shared. Order of <script> tags in index.html
// guarantees this file loads before app.js or design-mode.js.

(function () {
  'use strict';

  // escapeHTML mirrors app.js's escapeHtml (lowercase h) semantics: escapes
  // &, <, >, " — not single quotes. Phase B does not touch app.js, so we
  // duplicate the logic here under the camelCase public name.
  function escapeHTML(s) {
    if (s === null || s === undefined) return '';
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  async function fetchJSON(url, opts) {
    const o = Object.assign({}, opts || {});
    o.headers = Object.assign({ 'Accept': 'application/json' }, (opts && opts.headers) || {});
    const r = await fetch(url, o);
    if (!r.ok) {
      const text = await r.text().catch(() => '');
      const err = new Error('fetchJSON ' + url + ' ' + r.status + ' ' + text);
      err.status = r.status;
      throw err;
    }
    const ct = (r.headers.get && r.headers.get('content-type')) || '';
    if (ct.indexOf('application/json') === -1) return null;
    return r.json();
  }

  function getCookie(name) {
    const parts = (document.cookie || '').split(';');
    for (let i = 0; i < parts.length; i++) {
      const kv = parts[i].trim();
      const eq = kv.indexOf('=');
      if (eq < 0) continue;
      if (kv.slice(0, eq) === name) return kv.slice(eq + 1);
    }
    return null;
  }

  // 2-arg signature, matching app.js. No expiry (session cookie semantics
  // are fine; app.js's existing setCookie also omits expiry).
  function setCookie(name, value) {
    document.cookie = name + '=' + value + '; path=/; SameSite=Lax';
  }

  // The crit-settings cookie is JSON-encoded (URL-encoded JSON). app.js
  // writes it via JSON.stringify(...) — we read it the same way.
  function readThemeFromSettings() {
    const raw = getCookie('crit-settings');
    if (!raw) return 'system';
    try {
      const parsed = JSON.parse(decodeURIComponent(raw));
      return (parsed && parsed.theme) || 'system';
    } catch (_) {
      return 'system';
    }
  }

  function applyThemeFromCookie() {
    const t = readThemeFromSettings();
    const html = document.documentElement;
    if (t === 'light' || t === 'dark') html.setAttribute('data-theme', t);
    else html.removeAttribute('data-theme');
  }

  // Generic crit-settings JSON cookie accessors (mirror app.js semantics).
  function readSettings() {
    const raw = getCookie('crit-settings');
    if (!raw) return {};
    try { return JSON.parse(decodeURIComponent(raw)) || {}; }
    catch (_) { return {}; }
  }
  function writeSettings(obj) {
    setCookie('crit-settings', encodeURIComponent(JSON.stringify(obj || {})));
  }
  function getSetting(key, fallback) {
    const s = readSettings();
    return Object.prototype.hasOwnProperty.call(s, key) ? s[key] : fallback;
  }
  function setSetting(key, value) {
    const s = readSettings();
    s[key] = value;
    writeSettings(s);
  }

  window.crit = window.crit || {};
  window.crit.shared = {
    escapeHTML,
    fetchJSON,
    getCookie,
    setCookie,
    readThemeFromSettings,
    applyThemeFromCookie,
    getSetting,
    setSetting,
  };
})();
