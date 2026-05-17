// crit-shared.js — helpers consumed by both app.js (code review) and
// design-mode.js (design review). Vanilla JS, no module loader.
//
// Exports onto window.crit.shared. Order of <script> tags in index.html
// guarantees this file loads before app.js or design-mode.js.

(function () {
  'use strict';

  // escapeHTML — canonical HTML escaper for the entire frontend. Escapes
  // &, <, >, ", and ' (single quote). Safe in both content and attribute
  // contexts.
  function escapeHTML(s) {
    if (s === null || s === undefined) return '';
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
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

  // URL-decodes the value, matching setCookie's URL-encode on write. Keeping
  // get/set symmetric means callers don't sprinkle encode/decode at use sites.
  function getCookie(name) {
    const parts = (document.cookie || '').split(';');
    for (let i = 0; i < parts.length; i++) {
      const kv = parts[i].trim();
      const eq = kv.indexOf('=');
      if (eq < 0) continue;
      if (kv.slice(0, eq) === name) {
        const raw = kv.slice(eq + 1);
        try { return decodeURIComponent(raw); }
        catch (_) { return raw; }
      }
    }
    return null;
  }

  // 2-arg signature, matching app.js's policy byte-for-byte: 1-year max-age
  // (preferences should survive browser restarts), SameSite=Strict, and
  // URL-encode the value so JSON / special chars round-trip safely.
  function setCookie(name, value) {
    document.cookie = name + '=' + encodeURIComponent(value)
      + '; path=/; max-age=31536000; SameSite=Strict';
  }

  // The crit-settings cookie is JSON. getCookie URL-decodes for us, so we
  // hand the raw JSON straight to JSON.parse — same shape as app.js.
  function readThemeFromSettings() {
    const raw = getCookie('crit-settings');
    if (!raw) return 'system';
    try {
      const parsed = JSON.parse(raw);
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
    try { return JSON.parse(raw) || {}; }
    catch (_) { return {}; }
  }
  function writeSettings(obj) {
    setCookie('crit-settings', JSON.stringify(obj || {}));
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

  // Updates the navbar comment-count indicator. Both code-review (app.js)
  // and design-mode (design-mode.js) call this so the pill, classes, and
  // tooltip stay in lockstep — drift here is the navbar inconsistency the
  // user keeps noticing. Each mode still owns its own filter-pill counts,
  // since the filter pill itself isn't shared.
  //
  // opts: { totalCount, openCount }
  //   - totalCount: total comments (open + resolved)
  //   - openCount:  unresolved comments
  // Touches: #commentCountNumber (text), #commentCount (.comment-count-resolved
  // + title), #commentNavGroup (.has-comments + display).
  function updateCommentCountIndicator(opts) {
    var o = opts || {};
    var totalCount = o.totalCount | 0;
    var openCount = o.openCount | 0;
    var navGroup = document.getElementById('commentNavGroup');
    var navBtn = document.getElementById('commentCount');
    var numEl = document.getElementById('commentCountNumber');
    if (navGroup) navGroup.style.display = '';
    if (totalCount === 0) {
      if (navGroup) navGroup.classList.remove('has-comments');
      if (navBtn) {
        navBtn.classList.add('comment-count-resolved');
        navBtn.title = 'Toggle comments panel';
      }
      if (numEl) numEl.textContent = '';
    } else if (openCount > 0) {
      if (navGroup) navGroup.classList.add('has-comments');
      if (navBtn) {
        navBtn.classList.remove('comment-count-resolved');
        navBtn.title = openCount + ' unresolved comment' + (openCount === 1 ? '' : 's') + ' — toggle panel';
      }
      if (numEl) numEl.textContent = String(openCount);
    } else {
      if (navGroup) navGroup.classList.add('has-comments');
      if (navBtn) {
        navBtn.classList.add('comment-count-resolved');
        navBtn.title = totalCount + ' resolved comment' + (totalCount === 1 ? '' : 's') + ' — toggle panel';
      }
      if (numEl) numEl.textContent = String(totalCount);
    }
  }

  // ===== Toast =====
  // Unified mini-toast helper used by both code-review (app.js) and
  // design-mode (design-mode.js). Replaces the prior `showMiniToast`
  // (app.js, transition-based, rule-compliant) and `showToast`
  // (design-mode.js, called .remove() directly — violated frontend-js.md
  // "Never call .remove() on elements with CSS exit animations").
  //
  // API: showToast(message, opts?) -> dismiss()
  //   opts.timeout: ms before auto-dismiss (default 3000; pass 0 to keep open)
  //   opts.kind:    'info' (default) | 'error' | 'success' (sets modifier class)
  //
  // The returned function dismisses the toast early (idempotent).
  // Cleanup is driven by `transitionend` on the visibility class toggle —
  // never by an unconditional setTimeout(remove).
  function ensureToastHost() {
    if (typeof document === 'undefined' || !document.body) return null;
    var host = document.querySelector('.mini-toast-host');
    if (host) return host;
    host = document.createElement('div');
    host.className = 'mini-toast-host';
    document.body.appendChild(host);
    return host;
  }

  function showToast(message, opts) {
    var host = ensureToastHost();
    if (!host) return function () {};
    var o = opts || {};
    var timeout = (typeof o.timeout === 'number') ? o.timeout : 3000;
    var kind = (o.kind === 'error' || o.kind === 'success') ? o.kind : 'info';

    var t = document.createElement('div');
    t.className = 'mini-toast mini-toast--' + kind;
    t.textContent = (message == null) ? '' : String(message);
    host.appendChild(t);

    var raf = (typeof requestAnimationFrame === 'function')
      ? requestAnimationFrame
      : function (f) { return setTimeout(f, 16); };
    raf(function () { t.classList.add('mini-toast-visible'); });

    var dismissed = false;
    var settled = false;
    var timer = null;

    function finish() {
      if (settled) return;
      settled = true;
      if (t.parentNode) t.parentNode.removeChild(t);
    }

    function dismiss() {
      if (dismissed) return;
      dismissed = true;
      if (timer) { clearTimeout(timer); timer = null; }
      t.addEventListener('transitionend', finish, { once: true });
      t.classList.remove('mini-toast-visible');
      // Fallback: transitionend may not fire (reduced-motion, hidden tab,
      // tests). 400ms > the 300ms CSS transition.
      setTimeout(finish, 400);
    }

    if (timeout > 0) {
      timer = setTimeout(dismiss, timeout);
    }
    return dismiss;
  }

  // ===== runFinishReview =====
  // Shared finish-review flow used by both code-review (app.js) and
  // design-mode (design-mode.js). POSTs /api/finish, parses
  // {approved, prompt}, drives the #waitingDialog modal (heading,
  // message, prompt body, "Copy prompt" affordance), replays the
  // approved-checkmark CSS animation via the offsetWidth reflow trick,
  // and copies the prompt to the clipboard. The caller owns its own
  // uiState transition via the onWaiting/onApproved callbacks.
  //
  // opts:
  //   onWaiting()        — called after a non-approved finish (caller flips uiState).
  //   onApproved(prompt) — called after an approved finish. Receives the prompt string.
  //   onError(err)       — error surfacer (caller decides toast vs. console). Default: console.error.
  //   dedup              — optional inflight flag (window.crit.design.inflight.makeInFlightFlag()).
  //                        If provided and busy, the call is a no-op (returns null).
  //
  // Returns: Promise<{approved, prompt} | null>. Rejects only when onError is not
  // supplied; if onError is supplied, the error is delivered there and the promise
  // resolves to null (matches existing call-site ergonomics).
  async function runFinishReview(opts) {
    var o = opts || {};
    var dedup = o.dedup;
    if (dedup && typeof dedup.busy === 'function' && dedup.busy()) return null;
    if (dedup && typeof dedup.set === 'function') dedup.set();
    try {
      if (typeof o.checkConsent === 'function') {
        var consentOk = await o.checkConsent();
        if (!consentOk) return null;
      }
      var resp = await fetch('/api/finish', { method: 'POST' });
      if (!resp.ok) throw new Error('Finish review failed: HTTP ' + resp.status);
      var data = await resp.json();
      var approved = !!data.approved;
      var prompt = data.prompt || 'I reviewed the changes, no feedback, good to go!';

      var dialog = document.getElementById('waitingDialog');
      var headingEl = document.getElementById('waitingHeading');
      var messageEl = document.getElementById('waitingMessage');
      var clipEl = document.getElementById('waitingClipboard');
      var promptEl = document.getElementById('waitingPrompt');
      var previewEl = document.getElementById('promptPreview');

      if (promptEl) promptEl.textContent = prompt;
      if (previewEl) previewEl.textContent = prompt;
      if (clipEl) {
        clipEl.textContent = 'Copy prompt';
        clipEl.classList.remove('clipboard-confirm');
      }

      if (dialog) {
        dialog.classList.remove('approved');
        if (approved) {
          // Force reflow so the CSS animation restarts when the class is re-added.
          void dialog.offsetWidth;
          dialog.classList.add('approved');
        }
      }
      if (headingEl) headingEl.textContent = approved ? 'Approved' : 'Review Complete';
      if (messageEl) {
        if (approved) {
          messageEl.textContent =
            'Your agent has been notified — no further action needed. ' +
            'You can close this tab whenever you\'re ready.';
        } else {
          messageEl.textContent =
            "Agent notified. Copy the prompt below if it wasn't listening.";
        }
      }

      try { await navigator.clipboard.writeText(prompt); } catch (_) {}

      if (approved && typeof o.onApproved === 'function') o.onApproved(prompt);
      else if (!approved && typeof o.onWaiting === 'function') o.onWaiting();

      return { approved: approved, prompt: prompt };
    } catch (err) {
      if (typeof o.onError === 'function') {
        o.onError(err);
        return null;
      }
      throw err;
    } finally {
      if (dedup && typeof dedup.clear === 'function') dedup.clear();
    }
  }

  // ===== waitForSession =====
  // Shared base poll for the deferred-init readiness gate. The server
  // returns 503 until SetSession() completes — every endpoint other
  // than /api/health is gated. Callers (code-review init,
  // design-mode init) wrap this with their own UI hook via
  // onProgress(elapsedMs).
  //
  // opts:
  //   url        — defaults to '/api/session'.
  //   intervalMs — poll interval, default 200.
  //   maxWaitMs  — optional cap; rejects after with a timeout error.
  //   onProgress — optional (elapsedMs) => void, called before each retry sleep.
  //   signal     — optional AbortSignal; rejects with AbortError when aborted.
  //
  // Resolves with the parsed JSON payload on the first non-503 response.
  // Throws on network error, 5xx (other than 503), or non-JSON-shaped failures.
  async function waitForSession(opts) {
    var o = opts || {};
    var url = o.url || '/api/session';
    var intervalMs = (typeof o.intervalMs === 'number') ? o.intervalMs : 200;
    var maxWaitMs = (typeof o.maxWaitMs === 'number') ? o.maxWaitMs : 0;
    var onProgress = (typeof o.onProgress === 'function') ? o.onProgress : null;
    var signal = o.signal;
    var start = Date.now();

    function aborted() {
      return signal && signal.aborted;
    }
    function abortError() {
      var e = new Error('Aborted');
      e.name = 'AbortError';
      return e;
    }

    while (true) {
      if (aborted()) throw abortError();
      var elapsed = Date.now() - start;
      if (maxWaitMs > 0 && elapsed > maxWaitMs) {
        throw new Error('waitForSession: timed out after ' + maxWaitMs + 'ms');
      }

      var fetchOpts = signal ? { signal: signal } : undefined;
      var r = await fetch(url, fetchOpts);
      if (r.status !== 503) {
        if (!r.ok) {
          var err = new Error('waitForSession: HTTP ' + r.status);
          err.status = r.status;
          err.response = r;
          throw err;
        }
        return await r.json();
      }
      if (onProgress) onProgress(elapsed);
      await new Promise(function (resolve, reject) {
        var t = setTimeout(function () {
          if (signal) signal.removeEventListener('abort', onAbort);
          resolve();
        }, intervalMs);
        function onAbort() {
          clearTimeout(t);
          reject(abortError());
        }
        if (signal) signal.addEventListener('abort', onAbort, { once: true });
      });
    }
  }

  // ===== installSidebarResize =====
  // Shared sidebar/panel pointer-drag resize helper. Used by code-review
  // (app.js: file-tree handle, comments-panel handle) and design-mode
  // (panel-render: comments-panel handle).
  //
  // Owns all the bits the bare-bones design-mode implementation was missing:
  //   - Pointer capture (drag survives leaving the handle / window).
  //   - body.sidebar-resizing class — locks the cursor and disables text
  //     selection page-wide so the cursor doesn't flicker when the pointer
  //     leaves the strip mid-drag (style.css owns the rules).
  //   - Persistence on pointerup via setSetting(settingKey, width).
  //   - Min clamp (no upper bound — overflow is a horizontal scrollbar).
  //   - Keyboard a11y: ArrowLeft / ArrowRight nudges by 16px.
  //
  // Args:
  //   handle — the resize handle element (gets pointer events).
  //   panel  — the panel whose width is being changed.
  //   opts:
  //     settingKey — string. crit-settings key for persistence (required for save).
  //     min        — number. Minimum width in px (default 200).
  //     edge       — 'left' | 'right'. Which edge of the panel the handle sits on.
  //                  'right' (default): handle on panel's right edge, drag-right grows.
  //                  'left':  handle on panel's left edge,  drag-left  grows.
  //
  // Returns a teardown function that removes the listeners and clears state.
  //
  // Pure helper exposed alongside (computeResizeDelta) so tests can exercise
  // the math without a DOM.
  function computeResizeDelta(startWidth, startX, currentX, edge, min) {
    if (typeof min !== 'number' || min < 0) min = 200;
    var dir = edge === 'left' ? -1 : 1;
    var delta = (currentX - startX) * dir;
    var w = startWidth + delta;
    if (w < min) w = min;
    return w;
  }

  function installSidebarResize(handle, panel, opts) {
    if (!handle || !panel) return function () {};
    var o = opts || {};
    var settingKey = o.settingKey || null;
    var min = (typeof o.min === 'number') ? o.min : 200;
    var edge = (o.edge === 'left') ? 'left' : 'right';

    // Apply persisted width on install.
    if (settingKey) {
      var saved = getSetting(settingKey, null);
      if (typeof saved === 'number' && saved >= min) {
        panel.style.width = saved + 'px';
      }
    }

    var activePointerId = null;
    var startX = 0;
    var startW = 0;
    var lastWidth = 0;

    function onMove(ev) {
      if (ev.pointerId !== activePointerId) return;
      var w = computeResizeDelta(startW, startX, ev.clientX, edge, min);
      panel.style.width = w + 'px';
      lastWidth = w;
    }
    function onEnd(ev) {
      if (ev.pointerId !== activePointerId) return;
      handle.removeEventListener('pointermove', onMove);
      handle.removeEventListener('pointerup', onEnd);
      handle.removeEventListener('pointercancel', onEnd);
      try { handle.releasePointerCapture(activePointerId); } catch (_) {}
      activePointerId = null;
      handle.classList.remove('dragging');
      document.body.classList.remove('sidebar-resizing');
      if (settingKey) {
        try { setSetting(settingKey, Math.round(lastWidth)); } catch (_) {}
      }
    }
    function onDown(e) {
      if (e.button !== 0) return;
      e.preventDefault();
      activePointerId = e.pointerId;
      startX = e.clientX;
      startW = panel.getBoundingClientRect().width;
      lastWidth = startW;
      try { handle.setPointerCapture(e.pointerId); } catch (_) {}
      handle.classList.add('dragging');
      document.body.classList.add('sidebar-resizing');
      handle.addEventListener('pointermove', onMove);
      handle.addEventListener('pointerup', onEnd);
      handle.addEventListener('pointercancel', onEnd);
    }
    function onKey(e) {
      if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
      e.preventDefault();
      var dir = edge === 'left' ? -1 : 1;
      var sign = e.key === 'ArrowRight' ? 1 : -1;
      var current = panel.getBoundingClientRect().width;
      var w = Math.max(min, current + sign * dir * 16);
      panel.style.width = w + 'px';
      if (settingKey) {
        try { setSetting(settingKey, Math.round(w)); } catch (_) {}
      }
    }

    handle.addEventListener('pointerdown', onDown);
    handle.addEventListener('keydown', onKey);

    return function teardown() {
      handle.removeEventListener('pointerdown', onDown);
      handle.removeEventListener('keydown', onKey);
      handle.removeEventListener('pointermove', onMove);
      handle.removeEventListener('pointerup', onEnd);
      handle.removeEventListener('pointercancel', onEnd);
      if (activePointerId !== null) {
        try { handle.releasePointerCapture(activePointerId); } catch (_) {}
      }
      handle.classList.remove('dragging');
      document.body.classList.remove('sidebar-resizing');
    };
  }

  // ===== Image upload (paste + drag-drop) for comment textareas =====
  var pendingImageSeq = 0;

  function insertAtCursor(textarea, text) {
    var start = textarea.selectionStart;
    var end = textarea.selectionEnd;
    var before = textarea.value.substring(0, start);
    var after = textarea.value.substring(end);
    textarea.value = before + text + after;
    var cursor = start + text.length;
    textarea.selectionStart = textarea.selectionEnd = cursor;
    textarea.focus();
    textarea.dispatchEvent(new Event('input', { bubbles: true }));
  }

  function replaceInTextarea(textarea, needle, replacement) {
    var idx = textarea.value.indexOf(needle);
    if (idx === -1) return;
    var selStart = textarea.selectionStart;
    var selEnd = textarea.selectionEnd;
    textarea.value = textarea.value.substring(0, idx) + replacement + textarea.value.substring(idx + needle.length);
    var delta = replacement.length - needle.length;
    if (selStart > idx + needle.length) {
      textarea.selectionStart = selStart + delta;
      textarea.selectionEnd = selEnd + delta;
    } else if (selStart >= idx) {
      textarea.selectionStart = textarea.selectionEnd = idx + replacement.length;
    }
    textarea.dispatchEvent(new Event('input', { bubbles: true }));
  }

  function uploadAndInsertImage(textarea, file) {
    var seq = ++pendingImageSeq;
    var placeholder = '![uploading…](crit-pending-' + seq + ')';
    insertAtCursor(textarea, placeholder);
    var formData = new FormData();
    formData.append('file', file, file.name || '');
    fetch('/api/attachments', { method: 'POST', body: formData })
      .then(function (res) {
        if (!res.ok) return res.text().then(function (msg) { throw new Error(msg || 'Upload failed: ' + res.status); });
        return res.json();
      })
      .then(function (data) {
        if (!data || !data.url) throw new Error('Malformed upload response');
        var alt = (data.original_filename || '').trim();
        replaceInTextarea(textarea, placeholder, '![' + alt + '](' + data.url + ')');
      })
      .catch(function (err) {
        console.error('Image paste upload failed:', err);
        replaceInTextarea(textarea, placeholder, '_[image upload failed]_');
      });
  }

  function attachImagePaste(textarea) {
    textarea.addEventListener('paste', function (event) {
      var clipboard = event.clipboardData;
      if (!clipboard) return;
      var items = clipboard.items;
      if (!items || items.length === 0) return;
      var images = [];
      for (var i = 0; i < items.length; i++) {
        if (items[i].kind === 'file' && items[i].type && items[i].type.indexOf('image/') === 0) {
          var f = items[i].getAsFile();
          if (f) images.push(f);
        }
      }
      if (images.length === 0) return;
      event.preventDefault();
      images.forEach(function (file) { uploadAndInsertImage(textarea, file); });
    });
  }

  function attachImageDragDrop(textarea) {
    function hasFiles(event) {
      var dt = event.dataTransfer;
      return !!(dt && dt.types && Array.prototype.indexOf.call(dt.types, 'Files') !== -1);
    }
    textarea.addEventListener('dragenter', function (event) {
      if (!hasFiles(event)) return;
      event.preventDefault();
      textarea.classList.add('drag-active');
    });
    textarea.addEventListener('dragover', function (event) {
      if (!hasFiles(event)) return;
      event.preventDefault();
      if (event.dataTransfer) event.dataTransfer.dropEffect = 'copy';
      textarea.classList.add('drag-active');
    });
    textarea.addEventListener('dragleave', function (event) {
      if (event.target === textarea) textarea.classList.remove('drag-active');
    });
    textarea.addEventListener('drop', function (event) {
      var dt = event.dataTransfer;
      if (!dt || !dt.files || dt.files.length === 0) { textarea.classList.remove('drag-active'); return; }
      var images = [];
      for (var i = 0; i < dt.files.length; i++) {
        var file = dt.files[i];
        if (file && file.type && file.type.indexOf('image/') === 0) images.push(file);
      }
      if (images.length === 0) { textarea.classList.remove('drag-active'); return; }
      event.preventDefault();
      textarea.classList.remove('drag-active');
      textarea.focus();
      images.forEach(function (file) { uploadAndInsertImage(textarea, file); });
    });
  }

  function attachImageUploads(textarea) {
    attachImagePaste(textarea);
    attachImageDragDrop(textarea);
  }

  // ===== Tip rotation for waiting modal =====
  var _tipInterval = null;
  var _lastTip = '';
  var _baseTips = [
    'Press <kbd>?</kbd> to see all keyboard shortcuts.',
    'Press <kbd>@</kbd> to reference other files in your comments.',
    'Select text and press <kbd>c</kbd> to comment on your selection.',
    'Use <kbd>crit pull</kbd> to load existing GitHub PR comments into your local review.',
    'Use <kbd>crit push</kbd> to post your comments as a GitHub PR review. Add <kbd>--dry-run</kbd> to preview first.',
    'Comments persist across rounds until you resolve them.',
    'Run <kbd>crit</kbd> with a URL to review your local website visually.',
    'Run <kbd>crit overview.html</kbd> to review an artifact HTML file visually.',
    'Ask your agent to review your work with Crit and leave comments with it.',
    'Enjoying Crit? A GitHub star or sharing it with colleagues helps a lot!',
  ];

  function startTipRotation(extraTips) {
    if (_tipInterval) return;
    var tips = _baseTips.concat(extraTips || []);
    function show() {
      var el = document.getElementById('tipText');
      if (!el || tips.length === 0) return;
      var idx;
      do { idx = Math.floor(Math.random() * tips.length); }
      while (tips[idx] === _lastTip && tips.length > 1);
      _lastTip = tips[idx];
      el.style.animation = 'none';
      void el.offsetWidth;
      el.innerHTML = tips[idx];
      el.style.animation = '';
    }
    show();
    _tipInterval = setInterval(show, 8000);
  }

  function stopTipRotation() {
    if (_tipInterval) { clearInterval(_tipInterval); _tipInterval = null; }
  }

  function showDisconnected() {
    if (document.querySelector('.disconnected-banner')) return;
    var header = document.querySelector('.header');
    if (!header) return;
    var banner = document.createElement('div');
    banner.className = 'disconnected-banner';
    banner.setAttribute('role', 'status');
    banner.setAttribute('aria-live', 'polite');
    var pill = document.createElement('div');
    pill.className = 'disconnected-pill';
    pill.innerHTML = '<svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true"><circle cx="7" cy="7" r="6" fill="currentColor" opacity="0.18"/><circle cx="7" cy="7" r="6" stroke="currentColor" stroke-width="1.25"/><path d="M4.5 7.1 L6.3 8.9 L9.5 5.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>Session complete';
    var text = document.createElement('span');
    text.className = 'disconnected-text';
    text.textContent = 'Server stopped — your review is now read only. Safe to close this tab.';
    banner.appendChild(pill);
    banner.appendChild(text);
    header.insertAdjacentElement('afterend', banner);
    var setHeaderVar = function () {
      document.documentElement.style.setProperty('--crit-header-height', header.offsetHeight + 'px');
    };
    setHeaderVar();
    if (typeof ResizeObserver !== 'undefined') {
      new ResizeObserver(setHeaderVar).observe(header);
    } else {
      window.addEventListener('resize', setHeaderVar);
    }
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
    updateCommentCountIndicator,
    showToast,
    runFinishReview,
    waitForSession,
    installSidebarResize,
    computeResizeDelta,
    attachImageUploads,
    showDisconnected,
    startTipRotation,
    stopTipRotation,
  };
})();
