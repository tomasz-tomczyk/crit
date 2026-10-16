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

  // Every match of `query` across `files`, in list order. `linesFor(file)`
  // returns the file's displayed rows as [{ side, line, text }] in display
  // order. Matching is case-insensitive unless `caseSensitive`.
  function collectMatches(files, query, linesFor, caseSensitive) {
    const out = [];
    if (!query) return out;
    const needle = caseSensitive ? query : query.toLowerCase();
    for (let f = 0; f < files.length; f++) {
      const rows = linesFor(files[f]) || [];
      for (let r = 0; r < rows.length; r++) {
        const text = caseSensitive ? rows[r].text : rows[r].text.toLowerCase();
        let at = text.indexOf(needle);
        let nth = 0;
        while (at !== -1) {
          out.push({ path: files[f].path, side: rows[r].side, line: rows[r].line, nth: nth++, col: at });
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
    const hay = caseSensitive ? text : text.toLowerCase();
    const needle = caseSensitive ? query : query.toLowerCase();
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

  function codeRows(root) {
    return root.querySelectorAll('code [data-content] > [data-line]');
  }

  function createFind(opts) {
    let query = '';
    let caseSensitive = false;
    let matches = [];
    let current = -1;
    let open = false;
    let revealSeq = 0;
    let loading = 0;

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
      else if (matches.length === 0) count.textContent = loading ? 'Searching…' : 'No results';
      else count.textContent = (current + 1) + ' of ' + matches.length + (matches.length >= MAX_MATCHES ? '+' : '') + (loading ? '…' : '');
      bar.classList.toggle('crit-find-empty', !!query && matches.length === 0 && !loading);
    }

    // Highlights: every match in the mounted rows, plus the current one.
    function hosts() {
      return Array.from(document.querySelectorAll(opts.hostSelector || 'diffs-container'));
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
      let currentRange = null;
      hosts().forEach(function(host) {
        const root = host.shadowRoot;
        const path = host.dataset.critPath;
        if (!root || !path) return;
        codeRows(root).forEach(function(el) {
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
      matches = collectMatches(opts.files(), query, opts.linesFor, caseSensitive);
      current = -1;
      if (keepPosition && prev) current = matches.findIndex(function(m) { return sameMatch(m, prev); });
      renderCount();
      syncHighlights();
    }

    // Lazy files have no rows until loaded. Load them in the background
    // while a query is active and fold their matches in as they land.
    function loadPending() {
      const pending = opts.files().filter(opts.isPending);
      if (pending.length === 0) return;
      loading += pending.length;
      renderCount();
      let next = 0;
      let researchQueued = false;
      const research = function() {
        if (researchQueued) return;
        researchQueued = true;
        setTimeout(function() {
          researchQueued = false;
          const hadCurrent = current >= 0;
          search(true);
          if (!hadCurrent && matches.length && open) go(opts.startIndex ? opts.startIndex(matches) : 0);
        }, 100);
      };
      const worker = function() {
        if (next >= pending.length) return Promise.resolve();
        const file = pending[next++];
        return Promise.resolve(opts.load(file.path)).catch(function() {}).then(function() {
          loading--;
          if (query) research(); else renderCount();
          return worker();
        });
      };
      for (let i = 0; i < 4; i++) worker();
    }

    function go(index) {
      if (matches.length === 0) return;
      current = (index + matches.length) % matches.length;
      renderCount();
      const m = matches[current];
      const seq = ++revealSeq;
      Promise.resolve(opts.reveal(m)).then(function() {
        // The row renders on a later frame; poll until it is mounted.
        let frames = 0;
        (function poll() {
          if (seq !== revealSeq) return;
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
      search(false);
      if (matches.length) go(opts.startIndex ? opts.startIndex(matches) : 0);
      if (query) loadPending();
    }

    function show() {
      open = true;
      bar.hidden = false;
      const selected = window.getSelection && String(window.getSelection()).trim();
      if (selected && selected.indexOf('\n') === -1 && selected.length <= 200) input.value = selected;
      input.focus();
      input.select();
      if (input.value !== query) setQuery(input.value);
      else { search(true); if (query) loadPending(); }
    }

    function hide() {
      open = false;
      revealSeq++;
      bar.hidden = true;
      syncHighlights();
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
      } else if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        hide();
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

    // Cmd/Ctrl+F opens the bar while the review list is up. Pressing it
    // again with the bar focused falls through to the browser's own find.
    // Cmd/Ctrl+G and Shift+Cmd/Ctrl+G step through matches.
    document.addEventListener('keydown', function(e) {
      const mod = e.metaKey || e.ctrlKey;
      if (!mod || e.altKey) return;
      const key = e.key.toLowerCase();
      if (key === 'f' && !e.shiftKey) {
        if (!opts.isActive()) return;
        if (open && document.activeElement === input) return;
        e.preventDefault();
        show();
      } else if (key === 'g' && open) {
        e.preventDefault();
        go(current + (e.shiftKey ? -1 : 1));
      }
    }, true);

    return {
      unsafeCSS: UNSAFE_CSS,
      // Call when Pierre mounts or unmounts rows (onPostRender).
      rowsChanged: queueSync,
      // Call when the file list or a file's rows change.
      filesChanged: function() { if (open && query) search(true); },
      isOpen: function() { return open; },
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
