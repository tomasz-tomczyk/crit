(function() {
  'use strict';

  // Find in the review list. Pierre virtualizes rows, so the browser's own
  // find (Cmd+F) only sees the lines near the viewport. This searches Crit's
  // file model instead, jumps with Pierre's scroll API and marks matches in
  // the mounted shadow roots with the CSS Custom Highlight API (the same
  // approach as quote highlights in crit-pierre-dom.js).
  //
  // The caller supplies the model (opts.files, opts.linesFor) and the
  // navigation (opts.reveal); this module owns the bar, the match list and
  // the highlights.

  const HIGHLIGHT_ALL = 'crit-find';
  const HIGHLIGHT_CURRENT = 'crit-find-current';
  const MAX_MATCHES = 10000;

  // Styles injected into Pierre's shadow roots (append to unsafeCSS).
  const UNSAFE_CSS =
    '::highlight(' + HIGHLIGHT_ALL + ') { background-color: var(--crit-find-bg); color: inherit; }' +
    '::highlight(' + HIGHLIGHT_CURRENT + ') { background-color: var(--crit-find-current-bg); color: var(--crit-find-current-fg); }';

  // Lower-case `text` without changing its length, so offsets found in the
  // folded text still point into the original. A character whose lower case
  // is longer (U+0130 becomes two code units) keeps its own case.
  function fold(text) {
    const low = text.toLowerCase();
    if (low.length === text.length) return low;
    let out = '';
    for (const ch of text) {
      const l = ch.toLowerCase();
      out += l.length === ch.length ? l : ch;
    }
    return out;
  }

  // Every match of `query` across `files`. `linesFor(file)` returns the
  // file's displayed rows as [{ side, line, text }] in display order (a row
  // may cache its folded text as `folded`). Files are walked from index
  // `startAt`, wrapping around, so a capped list starts where the reader is.
  // Matching is case-insensitive unless `caseSensitive`.
  function collectMatches(files, query, linesFor, caseSensitive, startAt) {
    const out = [];
    if (!query || files.length === 0) return out;
    const needle = caseSensitive ? query : fold(query);
    const first = startAt > 0 && startAt < files.length ? startAt : 0;
    for (let k = 0; k < files.length; k++) {
      const file = files[(first + k) % files.length];
      const rows = linesFor(file) || [];
      for (let r = 0; r < rows.length; r++) {
        const row = rows[r];
        const text = caseSensitive ? row.text : (row.folded !== undefined ? row.folded : (row.folded = fold(row.text)));
        let at = text.indexOf(needle);
        let nth = 0;
        while (at !== -1) {
          out.push({ path: file.path, side: row.side, line: row.line, nth: nth++, col: at });
          if (out.length >= MAX_MATCHES) return out;
          at = text.indexOf(needle, at + needle.length);
        }
      }
    }
    return out;
  }

  function sameMatch(a, b) {
    return !!a && !!b && a.path === b.path && a.side === b.side && a.line === b.line && a.nth === b.nth;
  }

  // Ranges for each occurrence of `query` in an element's text.
  function rangesInElement(el, query, caseSensitive) {
    const nodes = [];
    let text = '';
    const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT, null);
    let tn;
    while ((tn = walker.nextNode())) {
      nodes.push({ node: tn, start: text.length });
      text += tn.textContent;
    }
    const hay = caseSensitive ? text : fold(text);
    const needle = caseSensitive ? query : fold(query);
    const locate = function(pos) {
      for (let i = nodes.length - 1; i >= 0; i--) {
        if (nodes[i].start <= pos) return { node: nodes[i].node, offset: Math.min(pos - nodes[i].start, nodes[i].node.textContent.length) };
      }
      return null;
    };
    const ranges = [];
    let at = hay.indexOf(needle);
    while (at !== -1 && needle) {
      const a = locate(at);
      const b = locate(at + needle.length);
      if (a && b) {
        const range = new Range();
        range.setStart(a.node, a.offset);
        range.setEnd(b.node, b.offset);
        ranges.push(range);
      }
      at = hay.indexOf(needle, at + needle.length);
    }
    return ranges;
  }

  // Side of a rendered row, matching linesFor: 'old' for deletions, '' for
  // the new side, context and whole-file views.
  function rowSide(el) {
    return el.closest('code[data-deletions]') || el.dataset.lineType === 'change-deletion' ? 'old' : '';
  }

  function rowKey(path, side, line) {
    return path + '\u0000' + side + '\u0000' + line;
  }

  function codeRows(root) {
    return root.querySelectorAll('code [data-content] > [data-line]');
  }

  // Selected text, including a selection inside Pierre's shadow roots
  // (Chromium reports those as collapsed on the Selection itself).
  function selectedText(hostSelector) {
    const sel = window.getSelection && window.getSelection();
    if (!sel || sel.rangeCount === 0) return '';
    const plain = String(sel);
    if (plain.trim() || typeof sel.getComposedRanges !== 'function') return plain;
    const roots = Array.from(document.querySelectorAll(hostSelector)).map(function(h) { return h.shadowRoot; }).filter(Boolean);
    const r = sel.getComposedRanges({ shadowRoots: roots })[0];
    if (!r || r.collapsed) return '';
    const range = document.createRange();
    range.setStart(r.startContainer, r.startOffset);
    range.setEnd(r.endContainer, r.endOffset);
    return range.toString();
  }

  function createFind(opts) {
    const hostSelector = opts.hostSelector || 'diffs-container';
    let query = '';
    let caseSensitive = false;
    let matches = [];
    let current = -1;
    let open = false;
    let revealSeq = 0;
    let loadRun = null;
    let matchedRows = new Set();
    let returnFocus = null;
    let startPath = null; // file the current query started from

    const bar = document.createElement('div');
    bar.className = 'crit-find-bar';
    bar.hidden = true;
    bar.setAttribute('role', 'search');
    bar.innerHTML =
      '<input type="text" class="crit-find-input" placeholder="Find in diff" aria-label="Find in diff" spellcheck="false" autocomplete="off">' +
      '<span class="crit-find-count" aria-live="polite"></span>' +
      '<button type="button" class="crit-find-btn crit-find-case" aria-pressed="false" title="Match case (Alt+C)" aria-label="Match case">Aa</button>' +
      '<button type="button" class="crit-find-btn crit-find-prev" title="Previous (Shift+Enter)" aria-label="Previous match">&#8593;</button>' +
      '<button type="button" class="crit-find-btn crit-find-next" title="Next (Enter)" aria-label="Next match">&#8595;</button>' +
      '<button type="button" class="crit-find-btn crit-find-close" title="Close (Esc)" aria-label="Close find">&times;</button>';
    document.body.appendChild(bar);
    const input = bar.querySelector('.crit-find-input');
    const count = bar.querySelector('.crit-find-count');
    const caseBtn = bar.querySelector('.crit-find-case');

    function renderCount() {
      if (!query) count.textContent = '';
      else if (matches.length === 0) count.textContent = loadRun ? 'Searching…' : 'No results';
      else count.textContent = (current + 1) + ' of ' + matches.length + (matches.length >= MAX_MATCHES ? '+' : '') + (loadRun ? '…' : '');
      bar.classList.toggle('crit-find-empty', !!query && matches.length === 0 && !loadRun);
    }

    // Highlights: every match in the mounted rows, plus the current one.
    function hosts() {
      return Array.from(document.querySelectorAll(hostSelector));
    }

    function syncHighlights() {
      if (typeof CSS === 'undefined' || !CSS.highlights || typeof Highlight === 'undefined') return;
      if (!open || !query) {
        CSS.highlights.delete(HIGHLIGHT_ALL);
        CSS.highlights.delete(HIGHLIGHT_CURRENT);
        return;
      }
      const cur = matches[current];
      const all = [];
      // Only rows the match list counts, so the marks and the counter agree
      // (Pierre can show expanded context that the list does not search).
      let currentRange = null;
      hosts().forEach(function(host) {
        const root = host.shadowRoot;
        const path = host.dataset.critPath;
        if (!root || !path) return;
        codeRows(root).forEach(function(el) {
          if (!matchedRows.has(rowKey(path, rowSide(el), el.dataset.line))) return;
          const ranges = rangesInElement(el, query, caseSensitive);
          if (ranges.length === 0) return;
          if (cur && cur.path === path && String(cur.line) === el.dataset.line && cur.side === rowSide(el) && ranges[cur.nth]) {
            currentRange = ranges[cur.nth];
            ranges.splice(cur.nth, 1);
          }
          for (let i = 0; i < ranges.length; i++) all.push(ranges[i]);
        });
      });
      if (all.length) CSS.highlights.set(HIGHLIGHT_ALL, new Highlight(...all));
      else CSS.highlights.delete(HIGHLIGHT_ALL);
      if (currentRange) {
        const h = new Highlight(currentRange);
        h.priority = 1;
        CSS.highlights.set(HIGHLIGHT_CURRENT, h);
      } else {
        CSS.highlights.delete(HIGHLIGHT_CURRENT);
      }
      return currentRange;
    }

    // Pierre mounts rows on scroll; highlight them once per frame.
    let syncQueued = false;
    function queueSync() {
      if (syncQueued || !open) return;
      syncQueued = true;
      requestAnimationFrame(function() { syncQueued = false; syncHighlights(); });
    }

    // Recompute the match list, keeping the current match when it still exists.
    function search(keepPosition) {
      const prev = matches[current];
      const files = opts.files();
      const startAt = startPath ? files.findIndex(function(f) { return f.path === startPath; }) : 0;
      matches = collectMatches(files, query, opts.linesFor, caseSensitive, startAt);
      matchedRows = new Set(matches.map(function(m) { return rowKey(m.path, m.side, String(m.line)); }));
      current = -1;
      if (keepPosition && prev) current = matches.findIndex(function(m) { return sameMatch(m, prev); });
      renderCount();
      syncHighlights();
    }

    // Lazy files have no rows until loaded. Load them in the background
    // while the bar is open and fold their matches in as they land. One run
    // at a time; each step takes the next lazy file from the current list,
    // so a reload (new round, scope change) is picked up by the same run.
    function loadPending() {
      if (loadRun || !opts.files().some(opts.isPending)) return;
      const tried = new Set();
      let researchQueued = false;
      const research = function() {
        if (researchQueued) return;
        researchQueued = true;
        setTimeout(function() {
          researchQueued = false;
          if (!open || !query) return;
          const hadCurrent = current >= 0;
          search(true);
          if (!hadCurrent && matches.length) go(0);
        }, 300);
      };
      const worker = function() {
        if (!open) return Promise.resolve();
        const file = opts.files().find(function(f) { return opts.isPending(f) && !tried.has(f.path); });
        if (!file) return Promise.resolve();
        tried.add(file.path);
        return Promise.resolve(opts.load(file.path)).catch(function() {}).then(function() {
          if (query) research();
          return worker();
        });
      };
      const workers = [];
      for (let i = 0; i < 4; i++) workers.push(worker());
      loadRun = Promise.all(workers).then(function() {
        loadRun = null;
        renderCount();
      });
      renderCount();
    }

    function go(index) {
      if (!open || matches.length === 0) return;
      if (!opts.isActive()) { hide(); return; }
      current = (index + matches.length) % matches.length;
      renderCount();
      const m = matches[current];
      const seq = ++revealSeq;
      const isCurrent = function() { return open && seq === revealSeq; };
      Promise.resolve(opts.reveal(m, isCurrent)).then(function() {
        // The row renders on a later frame; poll until it is mounted.
        let frames = 0;
        (function poll() {
          if (!isCurrent()) return;
          const range = syncHighlights();
          if (range) { scrollRangeIntoView(range); return; }
          if (++frames < 60) requestAnimationFrame(poll);
        })();
      });
    }

    // Long lines scroll sideways inside Pierre; bring the match into view.
    function scrollRangeIntoView(range) {
      const el = range.startContainer.parentElement;
      const scroller = el && el.closest('[data-code]');
      if (!scroller || scroller.scrollWidth <= scroller.clientWidth) return;
      const r = range.getBoundingClientRect();
      const s = scroller.getBoundingClientRect();
      if (r.left < s.left + 60 || r.right > s.right - 20) {
        scroller.scrollLeft += r.left - s.left - s.width / 3;
      }
    }

    function setQuery(value) {
      query = value;
      startPath = opts.startFile ? opts.startFile() : null;
      search(false);
      if (matches.length) go(0);
      if (query) loadPending();
    }

    function show() {
      if (!open && document.activeElement !== input) returnFocus = document.activeElement;
      open = true;
      bar.hidden = false;
      const selected = selectedText(hostSelector).trim();
      if (selected && selected.indexOf('\n') === -1 && selected.length <= 200) input.value = selected;
      input.focus();
      input.select();
      if (input.value !== query) setQuery(input.value);
      else { search(true); if (query) loadPending(); }
    }

    function hide() {
      if (!open) return;
      open = false;
      clearTimeout(inputTimer);
      revealSeq++;
      bar.hidden = true;
      syncHighlights();
      const back = returnFocus;
      returnFocus = null;
      // Only hand focus back when it was in the bar: a close forced by
      // something else (a modal opening) must not pull focus out of it.
      const inBar = bar.contains(document.activeElement);
      if (inBar && back && back !== document.body && back.isConnected) back.focus({ preventScroll: true });
      if (opts.onClose) opts.onClose();
    }

    let inputTimer = null;
    input.addEventListener('input', function() {
      clearTimeout(inputTimer);
      inputTimer = setTimeout(function() { setQuery(input.value); }, 120);
    });
    input.addEventListener('keydown', function(e) {
      if (e.key === 'Enter') {
        e.preventDefault();
        if (input.value !== query) { clearTimeout(inputTimer); setQuery(input.value); return; }
        go(current + (e.shiftKey ? -1 : 1));
      } else if (e.altKey && e.code === 'KeyC') {
        e.preventDefault();
        toggleCase();
      }
    });
    function toggleCase() {
      caseSensitive = !caseSensitive;
      caseBtn.setAttribute('aria-pressed', String(caseSensitive));
      search(true);
      if (current < 0 && matches.length) go(0);
    }
    caseBtn.addEventListener('click', function() { toggleCase(); input.focus(); });
    bar.querySelector('.crit-find-prev').addEventListener('click', function() { go(current - 1); input.focus(); });
    bar.querySelector('.crit-find-next').addEventListener('click', function() { go(current + 1); input.focus(); });
    bar.querySelector('.crit-find-close').addEventListener('click', hide);

    // Cmd+F (Ctrl+F off macOS) opens the bar while the review list is up.
    // Pressing it again with the bar focused falls through to the browser's
    // own find, as does any press while find has nothing to search.
    // Cmd/Ctrl+G and Shift+Cmd/Ctrl+G step through matches. On macOS Ctrl+F
    // is left alone: it moves the caret in text fields.
    const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent || '');
    document.addEventListener('keydown', function(e) {
      const mod = isMac ? e.metaKey && !e.ctrlKey : e.ctrlKey && !e.metaKey;
      if (!mod || e.altKey) return;
      const key = e.key.toLowerCase();
      if (key === 'f' && !e.shiftKey) {
        if (!opts.isActive()) { hide(); return; }
        if (open && document.activeElement === input) return;
        e.preventDefault();
        show();
      } else if (key === 'g' && open) {
        if (!opts.isActive()) { hide(); return; }
        e.preventDefault();
        go(current + (e.shiftKey ? -1 : 1));
      }
    }, true);
    // Esc inside the bar (input or buttons) closes it.
    bar.addEventListener('keydown', function(e) {
      if (e.key !== 'Escape') return;
      e.preventDefault();
      e.stopPropagation();
      hide();
    });
    // Esc from the page or the review list. The page's keymap calls this
    // first in its Esc handling, so one Esc closes only the bar and the next
    // one reaches the keymap. Text fields keep their own Esc; focus elsewhere
    // (a dialog, the header) is left alone.
    const scopeSelector = opts.scopeSelector || '#filesContainer';
    function closeFromEscape(e) {
      if (!open || e.key !== 'Escape') return false;
      const t = e.target;
      if (!t || t.tagName === 'TEXTAREA' || t.tagName === 'INPUT' || t.tagName === 'SELECT' || t.isContentEditable) return false;
      const onPage = t === document.body || t === document.documentElement || t === document;
      if (!onPage && !(t.closest && t.closest(scopeSelector))) return false;
      hide();
      return true;
    }

    return {
      unsafeCSS: UNSAFE_CSS,
      // Call when Pierre mounts or unmounts rows (onPostRender).
      rowsChanged: queueSync,
      // Call when the file list or a file's rows change.
      filesChanged: function() {
        if (!open || !query) return;
        search(true);
        loadPending();
      },
      isOpen: function() { return open; },
      // Call from the page's Esc handling; true when it closed the bar.
      closeFromEscape: closeFromEscape,
      open: show,
      close: hide,
    };
  }

  const api = { createFind, collectMatches, sameMatch, unsafeCSS: UNSAFE_CSS };
  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.find = api;
  }
  if (typeof module === 'object' && module.exports) module.exports = api;
})();
