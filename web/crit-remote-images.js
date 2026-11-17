// crit-remote-images.js — click-to-load placeholders for remote images.
//
// Reviewed files and comments may be written by an AI agent. A remote image
// in them would make the reviewer's browser send a request, with whatever the
// URL encodes, to a host the agent chose. The page's Content-Security-Policy
// (internal/server/csp.go) blocks remote images and media except for the
// hosts in the user's remote_images config; this module turns every other
// remote image into a placeholder before the HTML reaches the page, and on a
// click loads the image in a sandboxed frame served by /remote-image.
//
// Reads: <meta name="crit-remote-images"> (the allowlist, filled in by the
// server so it always matches the policy). Exposes window.crit.remoteImages.
(function () {
  'use strict';

  var MESSAGE_TYPE = 'crit-remote-image';
  // Cheap pre-check so HTML without an absolute URL in a URL-bearing
  // attribute skips parsing. Protocol-relative (//host) counts as remote.
  var REMOTE_HINT = /(?:src|srcset|poster|background|href|data)\s*=\s*["']?\s*(?:https?:)?\/\//i;
  var REMOTE_URL = /^\s*(?:https?:)?\/\//i;
  // Attributes other than img src/srcset that make the browser fetch a URL
  // without a click. <a href> and <area href> are left alone: a link needs a
  // click, which is the reviewer's choice.
  var FETCHING_ATTRS = ['src', 'srcset', 'poster', 'background', 'data', 'href', 'xlink:href'];
  var LINK_TAGS = { a: true, area: true };
  // Largest size a frame takes from what the image reports.
  var MAX_FRAME_PX = 4000;
  // A frame that hasn't reported by then (the remote host never answers,
  // or /remote-image refused the URL) shows the error instead of spinning.
  var LOAD_TIMEOUT_MS = 20000;

  var patterns = readPatterns();

  function readPatterns() {
    if (typeof document === 'undefined') return [];
    var meta = document.querySelector('meta[name="crit-remote-images"]');
    if (!meta) return [];
    try {
      var list = JSON.parse(meta.getAttribute('content') || '[]');
      return normalizePatterns(list);
    } catch (_) {
      return [];
    }
  }

  function normalizePatterns(list) {
    if (!Array.isArray(list)) return [];
    return list
      .filter(function (p) { return typeof p === 'string'; })
      .map(function (p) { return p.trim().toLowerCase(); })
      .filter(Boolean);
  }

  function isRemote(url) {
    return typeof url === 'string' && REMOTE_URL.test(url);
  }

  // Lowercase host of an http(s) or protocol-relative URL, or '' when the
  // URL does not parse.
  function hostOf(url) {
    try {
      var trimmed = String(url).trim();
      if (trimmed.indexOf('//') === 0) trimmed = 'https:' + trimmed;
      var u = new URL(trimmed);
      if (u.protocol !== 'http:' && u.protocol !== 'https:') return '';
      return u.hostname.toLowerCase();
    } catch (_) {
      return '';
    }
  }

  // Same rules as a CSP host source: "*" is any host, "*.example.com" any
  // subdomain (not example.com itself), anything else an exact host.
  function hostAllowed(host, list) {
    if (!host) return false;
    for (var i = 0; i < list.length; i++) {
      var p = list[i];
      if (p === '*') return true;
      if (p.indexOf('*.') === 0) {
        var suffix = p.slice(1);
        if (host.length > suffix.length && host.slice(-suffix.length) === suffix) return true;
      } else if (host === p) {
        return true;
      }
    }
    return false;
  }

  function isBlocked(url, list) {
    return isRemote(url) && !hostAllowed(hostOf(url), list || patterns);
  }

  function filterSrcset(value, list) {
    var kept = [];
    var removed = [];
    String(value).split(',').forEach(function (entry) {
      var trimmed = entry.trim();
      if (!trimmed) return;
      var url = trimmed.split(/\s+/)[0];
      if (isBlocked(url, list)) removed.push(url);
      else kept.push(trimmed);
    });
    return { value: kept.join(', '), removed: removed };
  }

  var ICON_IMAGE = '<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.25" aria-hidden="true"><rect x="2" y="2.5" width="12" height="11" rx="1.5"/><circle cx="5.75" cy="6.25" r="1.25"/><path d="m2.5 12 3.5-3.5 2.5 2.5 2-2 3 3"/></svg>';

  function placeholderFor(doc, src, alt, width, height) {
    var host = hostOf(src) || 'remote host';
    var wrap = doc.createElement('span');
    wrap.className = 'crit-remote-image';
    wrap.setAttribute('data-crit-remote-src', String(src).trim());
    if (alt) wrap.setAttribute('data-crit-remote-alt', alt);
    if (/^\d{1,5}$/.test(width || '')) wrap.setAttribute('data-crit-remote-width', width);
    if (/^\d{1,5}$/.test(height || '')) wrap.setAttribute('data-crit-remote-height', height);
    var btn = doc.createElement('button');
    btn.type = 'button';
    btn.className = 'crit-remote-image-load';
    btn.title = (alt ? alt + '\n' : '') + String(src).trim() + '\nClick to load. Add the host to remote_images in ~/.crit.config.json to load images from it automatically.';
    btn.setAttribute('aria-label', 'Load remote image' + (alt ? ' "' + alt + '"' : '') + ' from ' + host);
    btn.innerHTML = ICON_IMAGE;
    var label = doc.createElement('span');
    label.className = 'crit-remote-image-label';
    label.textContent = alt ? alt : 'Remote image';
    var hostEl = doc.createElement('span');
    hostEl.className = 'crit-remote-image-host';
    hostEl.textContent = host;
    btn.appendChild(label);
    btn.appendChild(hostEl);
    wrap.appendChild(btn);
    return wrap;
  }

  function mediaLinkFor(doc, src, kind) {
    var a = doc.createElement('a');
    a.className = 'crit-remote-media-link';
    a.href = String(src).trim();
    a.target = '_blank';
    a.rel = 'noopener noreferrer';
    a.textContent = 'Remote ' + kind + ' from ' + (hostOf(src) || 'remote host') + ' (opens in a new tab)';
    return a;
  }

  function rewriteImg(img, list) {
    var src = img.getAttribute('src');
    var srcset = img.getAttribute('srcset');
    var filtered = srcset != null ? filterSrcset(srcset, list) : null;
    if (filtered) {
      if (filtered.value) img.setAttribute('srcset', filtered.value);
      else img.removeAttribute('srcset');
    }
    var blockedSrc = src != null && isBlocked(src, list) ? src : null;
    if (!blockedSrc && !src && filtered && filtered.removed.length) blockedSrc = filtered.removed[0];
    if (!blockedSrc) return;
    var ph = placeholderFor(img.ownerDocument, blockedSrc, img.getAttribute('alt') || '',
      img.getAttribute('width'), img.getAttribute('height'));
    img.parentNode.replaceChild(ph, img);
  }

  function rewriteMedia(el, list) {
    var kind = el.tagName === 'AUDIO' ? 'audio' : 'video';
    var remote = null;
    if (isBlocked(el.getAttribute('src'), list)) remote = el.getAttribute('src');
    var sources = el.querySelectorAll('source[src], track[src]');
    for (var i = 0; i < sources.length; i++) {
      if (isBlocked(sources[i].getAttribute('src'), list)) {
        remote = remote || sources[i].getAttribute('src');
        sources[i].parentNode.removeChild(sources[i]);
      }
    }
    if (isBlocked(el.getAttribute('poster'), list)) el.removeAttribute('poster');
    if (!remote) return;
    el.parentNode.replaceChild(mediaLinkFor(el.ownerDocument, remote, kind), el);
  }

  // Strip any other attribute that would fetch a blocked URL on render.
  function stripFetchingAttrs(el, list) {
    for (var i = 0; i < FETCHING_ATTRS.length; i++) {
      var name = FETCHING_ATTRS[i];
      if (name === 'href' && LINK_TAGS[el.localName]) continue;
      if (!el.hasAttribute(name)) continue;
      var value = el.getAttribute(name);
      if (name === 'srcset') {
        var filtered = filterSrcset(value, list);
        if (filtered.value) el.setAttribute(name, filtered.value);
        else el.removeAttribute(name);
      } else if (isBlocked(value, list)) {
        el.removeAttribute(name);
      }
    }
  }

  // Rewrite sanitized HTML so nothing in it loads a blocked remote URL:
  // images become click-to-load placeholders, video/audio become links, and
  // other fetching attributes are dropped. Run it on HTML that already went
  // through DOMPurify, before it is assigned to innerHTML (an <img> starts
  // loading as soon as it is created, attached or not). The page CSP blocks
  // whatever this misses; this keeps the page from showing broken images.
  function rewriteHtml(html, list) {
    if (typeof html !== 'string' || !html || !REMOTE_HINT.test(html)) return html;
    if (typeof document === 'undefined') return html;
    list = list || patterns;
    // <template> content is inert: parsing it here fetches nothing.
    var tpl = document.createElement('template');
    tpl.innerHTML = html;
    var root = tpl.content;
    var imgs = root.querySelectorAll('img');
    for (var i = 0; i < imgs.length; i++) rewriteImg(imgs[i], list);
    var media = root.querySelectorAll('video, audio');
    for (var m = 0; m < media.length; m++) rewriteMedia(media[m], list);
    var all = root.querySelectorAll('*');
    for (var j = 0; j < all.length; j++) {
      if (all[j].tagName !== 'IMG') stripFetchingAttrs(all[j], list);
    }
    return tpl.innerHTML;
  }

  // ===== Click to load =====

  function frameURL(src, alt) {
    var url = '/remote-image?src=' + encodeURIComponent(src);
    if (alt) url += '&alt=' + encodeURIComponent(alt);
    return url;
  }

  function sizeFrame(frame, naturalWidth, naturalHeight) {
    var w = parseInt(frame.getAttribute('data-crit-width'), 10) || naturalWidth;
    var h = parseInt(frame.getAttribute('data-crit-height'), 10) ||
      (naturalWidth ? Math.round(w * naturalHeight / naturalWidth) : naturalHeight);
    if (!w || !h) return;
    if (w > MAX_FRAME_PX || h > MAX_FRAME_PX) {
      var scale = MAX_FRAME_PX / Math.max(w, h);
      w = Math.max(1, Math.round(w * scale));
      h = Math.max(1, Math.round(h * scale));
    }
    frame.style.width = w + 'px';
    frame.style.aspectRatio = w + ' / ' + h;
    frame.style.height = 'auto';
  }

  function load(placeholder) {
    var src = placeholder.getAttribute('data-crit-remote-src');
    if (!src || !isRemote(src)) return;
    // /remote-image needs an absolute URL; resolve //host/x.png here.
    try {
      src = new URL(src, location.href).href;
    } catch (_) {
      return;
    }
    var alt = placeholder.getAttribute('data-crit-remote-alt') || '';
    var doc = placeholder.ownerDocument;
    var frame = doc.createElement('iframe');
    frame.className = 'crit-remote-image-frame';
    // allow-scripts without allow-same-origin: the frame runs in an opaque
    // origin and can only post its size back.
    frame.setAttribute('sandbox', 'allow-scripts');
    frame.setAttribute('referrerpolicy', 'no-referrer');
    frame.setAttribute('title', alt || 'Remote image from ' + (hostOf(src) || 'remote host'));
    frame.setAttribute('data-crit-remote-src', src);
    var width = placeholder.getAttribute('data-crit-remote-width');
    var height = placeholder.getAttribute('data-crit-remote-height');
    if (width) frame.setAttribute('data-crit-width', width);
    if (height) frame.setAttribute('data-crit-height', height);
    if (width && height) sizeFrame(frame, +width, +height);
    frame.src = frameURL(src, alt);
    placeholder.classList.add('is-loading');
    placeholder.replaceChildren(frame);
    var failIfStillLoading = function () {
      if (frame.isConnected && placeholder.classList.contains('is-loading')) showError(frame);
    };
    // The frame's load event fires after its image settles, and its script
    // posts before that; a frame with no report by then was refused.
    frame.addEventListener('load', function () { setTimeout(failIfStillLoading, 500); });
    setTimeout(failIfStillLoading, LOAD_TIMEOUT_MS);
  }

  function showError(frame) {
    var src = frame.getAttribute('data-crit-remote-src') || '';
    var wrap = frame.parentNode;
    if (!wrap) return;
    var doc = frame.ownerDocument;
    var msg = doc.createElement('span');
    msg.className = 'crit-remote-image-error';
    msg.textContent = 'Couldn’t load image from ' + (hostOf(src) || 'remote host') + '. ';
    var a = doc.createElement('a');
    a.href = src;
    a.target = '_blank';
    a.rel = 'noopener noreferrer';
    a.textContent = 'Open in a new tab';
    msg.appendChild(a);
    wrap.classList.remove('is-loading');
    wrap.replaceChildren(msg);
  }

  function frameFor(source) {
    var frames = document.querySelectorAll('iframe.crit-remote-image-frame');
    for (var i = 0; i < frames.length; i++) {
      if (frames[i].contentWindow === source) return frames[i];
    }
    return null;
  }

  function onMessage(e) {
    var data = e.data;
    if (!data || data.type !== MESSAGE_TYPE) return;
    // The frame is sandboxed (origin "null"); trust only our own frames.
    var frame = frameFor(e.source);
    if (!frame) return;
    if (data.status === 'loaded') {
      sizeFrame(frame, Number(data.width) || 0, Number(data.height) || 0);
      if (frame.parentNode) frame.parentNode.classList.remove('is-loading');
    } else {
      showError(frame);
    }
  }

  function onClick(e) {
    var btn = e.target && e.target.closest && e.target.closest('.crit-remote-image-load');
    if (!btn) return;
    var placeholder = btn.closest('.crit-remote-image');
    if (!placeholder) return;
    // Capture phase: a placeholder can sit inside a link or a commentable
    // line, and loading the image should not follow the link or open a form.
    e.preventDefault();
    e.stopPropagation();
    load(placeholder);
  }

  if (typeof document !== 'undefined' && typeof window !== 'undefined') {
    document.addEventListener('click', onClick, true);
    window.addEventListener('message', onMessage);
  }

  var api = {
    rewriteHtml: rewriteHtml,
    isBlocked: isBlocked,
    hostAllowed: hostAllowed,
    hostOf: hostOf,
    normalizePatterns: normalizePatterns,
    patterns: function () { return patterns.slice(); },
  };
  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.remoteImages = api;
  }
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
