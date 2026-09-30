(function() {
  'use strict';

  // Compatibility boundary for renderer-owned DOM. Keep all internal selectors
  // here and exercise them against the real dependency when upgrading.
  const PIERRE_QUOTE_HIGHLIGHT = 'crit-quote';
  // Styles injected into Pierre's shadow roots: quote highlights and on touch
  // (no hover, so no hover "+") the same "+" cue before commentable line
  // numbers as the classic views, with touch-action:none so the browser's
  // gesture recognizer can't cancel the tap handled by onLineNumberClick.
  const PIERRE_UNSAFE_CSS =
    // Files mode uses the same centered reading width for source and prose.
    ':host([data-crit-file-view]) pre[data-file] { max-width: min(var(--content-width), calc(100% - 32px));' +
    ' margin-inline: auto; --crit-comment-inset: 0px; }' +
    // File comments span the number gutter too, so line and file-level
    // cards share a reading width even when the viewport constrains it.
    // Wrap mode clears Pierre's column variables; syncNumberWidth measures
    // the gutter into --crit-number-width instead.
    ':host(:not([data-crit-document])) pre[data-file] [data-line-annotation] { margin-left: calc(-1 * var(--diffs-column-number-width, var(--crit-number-width, 0px)));' +
    ' width: var(--diffs-column-width); position: relative; z-index: 3; }' +
    ':host(:not([data-crit-document])) pre[data-file] [data-line-annotation] [data-annotation-content] { width: 100%; left: 0; }' +
    ':host(:not([data-crit-document])) pre[data-file] [data-line-annotation]:not([data-line-annotation="-1,-1"]) [data-annotation-content] {' +
    ' max-width: min(var(--crit-comment-width, var(--content-width, 1040px)), calc(100% - 2 * var(--crit-comment-inset, 16px))); margin-inline: auto; }' +
    // Comment rows (file-level and line) sit on the plain code background,
    // not Pierre's context tint. Same selectors Pierre uses to set the
    // variable; --diffs-bg-context-override would also hide the scrollbar
    // thumb, which shares that colour.
    '[data-line-annotation], [data-gutter-buffer=annotation] { --diffs-annotation-bg: var(--diffs-bg); }' +
    // Documented colour override only; keep Pierre's separator layout/controls.
    // Semantic gutter colours need more contrast than their line-tint colour.
    // Move them slightly toward the theme foreground while retaining their hue.
    ':host { --diffs-gap-block: 0px; --diffs-bg-separator-override: color-mix(in srgb, var(--diffs-fg) 5%, var(--diffs-bg));' +
    ' --diffs-fg-number-addition-override: color-mix(in srgb, var(--diffs-addition-base) 85%, var(--diffs-fg));' +
    ' --diffs-fg-number-deletion-override: color-mix(in srgb, var(--diffs-deletion-base) 85%, var(--diffs-fg)); }' +
    ':host([data-crit-stub]) [data-gutter], :host([data-crit-stub]) [data-content] > [data-line] { visibility: hidden; }' +
    // Rendered documents and placeholders (data-crit-document, see
    // isFileLevelOnly) have no code lines; do not reserve a gutter.
    ':host([data-crit-document]) [data-gutter] { display: none; }' +
    // The content grid inherits both columns once it spans them, so keep the
    // document on its own row and drop the empty line under it. Otherwise the
    // line sits beside the document at the same height and Pierre adds both
    // to the item height, which throws off every later file's position.
    ':host([data-crit-document]) [data-content] { grid-column: 1 / -1; }' +
    ':host([data-crit-document]) [data-content] > * { grid-column: 1 / -1; }' +
    ':host([data-crit-document]) [data-content] > [data-line] { display: none; }' +
    // Match Crit's quoted-text cue without wrapping renderer-owned tokens.
    // Highlight pseudos support text-decoration rather than border-bottom.
    '::highlight(' + PIERRE_QUOTE_HIGHLIGHT + ') { background-color: var(--crit-quote-highlight-bg);' +
    ' text-decoration: underline; text-decoration-color: var(--crit-quote-highlight-border); text-decoration-thickness: 1.5px; }' +
    '@media (pointer: coarse) {' +
    '  [data-gutter] > [data-column-number] { touch-action: none; }' +
    '  [data-gutter] > [data-column-number] [data-line-number-content]::before { content: "+"; display: inline-block; width: 1.2ch;' +
    '    color: var(--crit-editor-fg-muted); opacity: 0.6; font-weight: 700; pointer-events: none; }' +
    '  [data-utility-button] { display: none; }' +
    '}';

  function pierreSelectionForComment(selection) {
    // Chromium reports a selection inside a shadow root as collapsed on the
    // Selection itself; only the composed range knows its real extent.
    if (!selection || selection.rangeCount === 0 || typeof selection.getComposedRanges !== 'function') return null;
    // Every Pierre host: the review list and story chapter diffs.
    const hosts = Array.from(document.querySelectorAll('diffs-container'));
    const ranges = selection.getComposedRanges({ shadowRoots: hosts.map(function(h) { return h.shadowRoot; }).filter(Boolean) });
    if (!ranges || ranges.length === 0 || ranges[0].collapsed) return null;
    const r = ranges[0];
    const lineEl = function(node) {
      const el = node && (node.nodeType === 1 ? node : node.parentElement);
      return el ? el.closest('[data-line]') : null;
    };
    const startEl = lineEl(r.startContainer);
    const endEl = lineEl(r.endContainer);
    if (!startEl || !endEl) return null;
    const root = endEl.getRootNode();
    if (root !== startEl.getRootNode() || !root.host) return null;
    const filePath = root.host.dataset.critPath;
    if (!filePath) return null;
    const isOld = function(el) { return !!el.closest('code[data-deletions]') || el.dataset.lineType === 'change-deletion'; };
    const side = isOld(endEl) ? 'old' : '';
    const a = parseInt(startEl.dataset.line, 10);
    const b = parseInt(endEl.dataset.line, 10);
    const startLine = Math.min(a, b);
    const endLine = Math.max(a, b);

    // Line elements for the range on the chosen side, in order.
    const contentEls = Array.from(root.querySelectorAll('[data-line]')).filter(function(el) {
      const n = parseInt(el.dataset.line, 10);
      return n >= startLine && n <= endLine && isOld(el) === (side === 'old');
    });
    const live = document.createRange();
    live.setStart(r.startContainer, r.startOffset);
    live.setEnd(r.endContainer, r.endOffset);
    const selectedText = live.toString().trim();
    const fullText = contentEls.map(function(el) { return el.textContent.trim(); }).join('\n');
    let quote = null;
    let quoteOffset = null;
    if (selectedText && selectedText.replace(/\s+/g, ' ') !== fullText.trim().replace(/\s+/g, ' ') && selectedText.length <= 300) {
      quote = selectedText;
      let charsBefore = 0;
      for (let i = 0; i < contentEls.length; i++) {
        if (i > 0) charsBefore++;
        if (!contentEls[i].contains(r.startContainer)) { charsBefore += contentEls[i].textContent.length; continue; }
        const walker = document.createTreeWalker(contentEls[i], NodeFilter.SHOW_TEXT, null);
        let tn;
        while ((tn = walker.nextNode())) {
          if (tn === r.startContainer) { charsBefore += r.startOffset; break; }
          charsBefore += tn.textContent.length;
        }
        const rawAll = contentEls.map(function(el) { return el.textContent; }).join(' ');
        quoteOffset = rawAll.slice(0, charsBefore).replace(/\s+/g, ' ').trimStart().length;
        break;
      }
    }
    return { filePath: filePath, startLine: startLine, endLine: endLine, side: side, quote: quote, quoteOffset: quoteOffset };
  }

  function lineInRoot(root, line, side) {
    const column = side === 'old' ? 'code[data-deletions]' : 'code[data-additions]';
    const split = root.querySelector(column + ' [data-line="' + line + '"]');
    if (split) return split;
    // Unified context uses new-side data-line and old-side data-alt-line.
    // Either number may also belong to a change on the opposite side.
    return root.querySelector(side === 'old'
      ? 'code[data-unified] [data-line="' + line + '"][data-line-type="change-deletion"], code[data-unified] [data-alt-line="' + line + '"][data-line-type="context"]'
      : 'code:not([data-deletions]) [data-line="' + line + '"]:not([data-line-type="change-deletion"])');
  }

  function pierreLineElement(container, line, side) {
    const host = container && container.querySelector('diffs-container');
    const root = host && host.shadowRoot;
    return root ? lineInRoot(root, line, side) : null;
  }

  function pierreLineSelectors(file, ranges, unified, fileView, hunks) {
    const host = ':host([data-crit-path="' + CSS.escape(file.path) + '"]) ';
    const row = function(code, n, type) {
      return host + code + ' [data-content] > [data-line="' + n + '"]' + (type ? '[data-line-type="' + type + '"]' : '');
    };
    const out = [];
    if (fileView) {
      ranges.forEach(function(r) {
        for (let ln = r.start; ln <= r.end; ln++) out.push(row('code[data-code]', ln));
      });
      return out;
    }
    if (!unified) {
      ranges.forEach(function(r) {
        const code = r.old ? 'code[data-deletions]' : 'code[data-additions]';
        for (let ln = r.start; ln <= r.end; ln++) out.push(row(code, ln));
      });
      return out;
    }
    // Unified: walk the displayed rows in order (per hunk: context, then the
    // deletions of a change block, then its additions).
    const rows = [];
    for (let h = 0; h < hunks.length; h++) rows.push.apply(rows, hunks[h].Lines || []);
    ranges.forEach(function(r) {
      const inRange = function(l) {
        const n = r.old ? (l.Type === 'add' ? null : l.OldNum) : (l.Type === 'del' ? null : l.NewNum);
        return n !== null && n >= r.start && n <= r.end;
      };
      let first = -1;
      let last = -1;
      for (let i = 0; i < rows.length; i++) {
        if (inRange(rows[i])) { if (first < 0) first = i; last = i; }
      }
      for (let i = first; first >= 0 && i <= last; i++) {
        const l = rows[i];
        if (l.Type === 'del') out.push(row('code[data-unified]', l.OldNum, 'change-deletion'));
        else if (l.Type === 'add') { if (!r.old) out.push(row('code[data-unified]', l.NewNum, 'change-addition')); }
        else out.push(row('code[data-unified]', l.NewNum, 'context'));
      }
    });
    return out;
  }

  function rangeForQuote(lineEls, quote, offset) {
    if (lineEls.length === 0 || !quote) return null;
    const nodes = [];
    let text = '';
    for (let i = 0; i < lineEls.length; i++) {
      if (i > 0) text += '\n';
      const walker = document.createTreeWalker(lineEls[i], NodeFilter.SHOW_TEXT, null);
      let tn;
      while ((tn = walker.nextNode())) {
        nodes.push({ node: tn, start: text.length });
        text += tn.textContent;
      }
    }
    let at = -1;
    if (typeof offset === 'number') {
      let normalized = 0;
      let lastWasSpace = true;
      for (let i = 0; i < text.length && at < 0; i++) {
        if (normalized >= offset && text.startsWith(quote, i)) at = i;
        const space = /\s/.test(text[i]);
        if (!(space && lastWasSpace)) normalized++;
        lastWasSpace = space;
      }
    }
    if (at < 0) at = text.indexOf(quote);
    if (at < 0) return null;
    const end = at + quote.length;
    const locate = function(pos) {
      for (let i = nodes.length - 1; i >= 0; i--) {
        if (nodes[i].start <= pos) return { node: nodes[i].node, offset: Math.min(pos - nodes[i].start, nodes[i].node.textContent.length) };
      }
      return null;
    };
    const a = locate(at);
    const b = locate(end);
    if (!a || !b) return null;
    const range = new Range();
    range.setStart(a.node, a.offset);
    range.setEnd(b.node, b.offset);
    return range;
  }

  function labelPierreControls(root) {
    if (!root) return;
    root.querySelectorAll('[data-expand-button]:not([aria-label])').forEach(function(b) {
      b.setAttribute('aria-label', b.hasAttribute('data-expand-up') ? 'Expand up'
        : b.hasAttribute('data-expand-down') ? 'Expand down' : 'Expand all hidden lines');
    });
    root.querySelectorAll('[data-utility-button]:not([aria-label])').forEach(function(b) {
      b.setAttribute('aria-label', 'Add comment');
    });
  }


  // Empty file items whose only content is Crit's file-level annotation: a
  // rendered document or a placeholder (deleted, binary, renamed, orphaned).
  // Loading stubs are real diffs and keep their rows.
  function isFileLevelOnly(host) {
    return !!host.querySelector('.pierre-document, .diff-deleted-placeholder:not(.pierre-loading)');
  }

  // Pierre only publishes --diffs-column-number-width in scroll mode (it
  // watches the gutter there). In wrap mode, watch each side's gutter
  // ourselves so comments still span it and match the width of comments on
  // rendered documents. A ResizeObserver catches the first layout of a host
  // rendered while detached, font swaps and code-font changes, none of which
  // re-render the file, and reports sizes without forcing a layout.
  const numberWidthObservers = new WeakMap();
  function setNumberWidth(gutter, width) {
    const code = gutter.parentElement;
    if (!code) return;
    if (width > 0) code.style.setProperty('--crit-number-width', width + 'px');
    else code.style.removeProperty('--crit-number-width');
  }
  function unwatchNumberWidth(host) {
    const observer = numberWidthObservers.get(host);
    if (observer) observer.disconnect();
    numberWidthObservers.delete(host);
  }
  function syncNumberWidth(host) {
    const root = host.shadowRoot;
    if (!root) return;
    unwatchNumberWidth(host);
    const gutters = root.querySelectorAll('[data-code] > [data-gutter]');
    if (!root.querySelector('pre[data-overflow="wrap"]')) {
      gutters.forEach(function(gutter) { setNumberWidth(gutter, 0); });
      return;
    }
    if (typeof ResizeObserver !== 'function') {
      gutters.forEach(function(gutter) { setNumberWidth(gutter, gutter.getBoundingClientRect().width); });
      return;
    }
    const observer = new ResizeObserver(function(entries) {
      entries.forEach(function(entry) {
        const box = entry.borderBoxSize && entry.borderBoxSize[0];
        setNumberWidth(entry.target, box ? box.inlineSize : entry.target.getBoundingClientRect().width);
      });
    });
    gutters.forEach(function(gutter) { observer.observe(gutter); });
    numberWidthObservers.set(host, observer);
  }

  function hostFor(node) {
    return node && (node.shadowRoot ? node : (node.getRootNode && node.getRootNode().host));
  }
  function createDecorations() {
    const pierreRangeSheet = new CSSStyleSheet();
    const pierreQuoteRanges = new Map();
    let css = '';
    let quoteSyncPending = false;
    function schedulePierreQuoteHighlight() {
      if (quoteSyncPending) return;
      quoteSyncPending = true;
      // Cached inline renders finish before the caller attaches their host.
      // Publish after that DOM transaction, then prune hosts it removed.
      queueMicrotask(function() {
        quoteSyncPending = false;
        syncPierreQuoteHighlight();
      });
    }
    function adoptPierreRangeSheet(root) {
      if (root && root.adoptedStyleSheets.indexOf(pierreRangeSheet) === -1) {
        root.adoptedStyleSheets = root.adoptedStyleSheets.concat(pierreRangeSheet);
      }
    }
    function syncPierreQuoteHighlight() {
      if (typeof CSS === 'undefined' || !CSS.highlights || typeof Highlight === 'undefined') return;
      const all = [];
      pierreQuoteRanges.forEach(function(ranges, host) {
        if (!host.isConnected) { pierreQuoteRanges.delete(host); return; }
        for (let i = 0; i < ranges.length; i++) all.push(ranges[i]);
      });
      if (all.length === 0) CSS.highlights.delete(PIERRE_QUOTE_HIGHLIGHT);
      else CSS.highlights.set(PIERRE_QUOTE_HIGHLIGHT, new Highlight(...all));
    }
    function pierreQuoteRangesFor(host, quoted) {
      const root = host && host.shadowRoot;
      if (!root) return [];
      const ranges = [];
      for (let q = 0; q < quoted.length; q++) {
        const item = quoted[q];
        const lineEls = [];
        for (let ln = item.start; ln <= item.end; ln++) {
          const el = lineInRoot(root, ln, item.side);
          if (el) lineEls.push(el);
        }
        const range = rangeForQuote(lineEls, item.quote, item.offset);
        if (range) ranges.push(range);
      }
      return ranges;
    }
    return {
      setRangeCSS(value) { if (value !== css) { css = value; pierreRangeSheet.replaceSync(value); } },
      mount(host, path, quoted) {
        host.dataset.critPath = path;
        if (isFileLevelOnly(host)) host.dataset.critDocument = '1'; else delete host.dataset.critDocument;
        adoptPierreRangeSheet(host.shadowRoot);
        labelPierreControls(host.shadowRoot);
        syncNumberWidth(host);
        const had = pierreQuoteRanges.has(host);
        const ranges = pierreQuoteRangesFor(host, quoted);
        if (ranges.length) pierreQuoteRanges.set(host, ranges); else pierreQuoteRanges.delete(host);
        if (had || pierreQuoteRanges.size) schedulePierreQuoteHighlight();
      },
      unmount(host) {
        unwatchNumberWidth(host);
        if (pierreQuoteRanges.delete(host)) schedulePierreQuoteHighlight();
      },
    };
  }

  const api = { hostFor, isFileLevelOnly, createDecorations, unsafeCSS: PIERRE_UNSAFE_CSS, pierreSelectionForComment, pierreLineElement, pierreLineSelectors, rangeForQuote, labelPierreControls };
  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.pierreDOM = api;
  }
  if (typeof module === 'object' && module.exports) module.exports = api;
})();
