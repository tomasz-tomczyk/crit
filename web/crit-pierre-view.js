(function () {
  'use strict';

  // Crit's multi-file diff surface on @pierre/diffs CodeView.
  //
  // Pierre owns rendering, virtualization, syntax highlighting (Shiki in the
  // worker pool), word diffs, sticky headers and hunk expansion. This module
  // owns the Crit side of that boundary:
  //   - Crit files → CodeView items (full diffs via old/new contents; lazy
  //     files start as line-count-sized stubs and hydrate when rendered)
  //   - comment threads / forms → line annotations, backed by a per-id
  //     element cache so drafts survive virtualization unmounts
  //   - Crit's file header (viewed, collapse, comment button) → custom header
  //   - gutter "+" / selection → Crit form ranges
  //   - scroll/jump helpers used by the tree, comments panel and j/k
  //
  // Reads: window.crit.pierreAdapter (pure mapping). Pierre is injected as
  // opts.pierre (window.PierreDiffs in the page) so tests can pass a fake.

  var adapter = (typeof window !== 'undefined' && window.crit && window.crit.pierreAdapter) ||
    (typeof require === 'function' ? require('./crit-pierre-adapter.js') : null);

  var HYDRATE_CONCURRENCY = 4;
  var nextViewId = 0;

  function createPierreView(opts) {
    var P = opts.pierre;
    var root = opts.root;
    var versions = new Map();          // path → item version
    var stubs = new Set();             // paths rendered as line-count stubs
    var hydrating = new Map();         // path → Promise
    var hydrateQueue = [];
    var elements = new Map();          // "kind:id" → HTMLElement (annotation cache)
    var metadataCache = new Map();     // "kind:id" → metadata object (stable identity)
    var itemsByPath = new Map();     // path → last published item
    var order = [];                  // paths in list order
    var diffStyle = opts.diffStyle || 'split';
    var themeType = opts.themeType || 'system';
    var disposed = false;
    var generation = 0;

    function nextVersion(path) {
      var v = (versions.get(path) || 0) + 1;
      versions.set(path, v);
      return v;
    }

    function metadataFor(kind, id) {
      var key = kind + ':' + id;
      var m = metadataCache.get(key);
      if (!m) {
        m = { kind: kind, id: id };
        metadataCache.set(key, m);
      }
      return m;
    }

    function annotationsFor(file) {
      var list = opts.annotations(file) || [];
      // Stable metadata objects: Pierre re-creates an annotation's DOM when
      // its metadata reference changes, so reuse them until invalidated.
      return list.map(function(a) {
        return { side: a.side, lineNumber: a.lineNumber, metadata: metadataFor(a.metadata.kind, a.metadata.id) };
      });
    }

    // Parsed diffs and file contents, reused while what they were built from
    // is unchanged. Each parsed result has a stable Pierre cacheKey, so its
    // worker keeps highlighting too, and a stable object keeps whatever Pierre has
    // hydrated onto it. Publishing for comments, forms or collapse therefore
    // costs no re-parse or re-highlight.
    var viewId = ++nextViewId;
    var parseVersion = 0;
    var parsed = new Map(); // path → { inputs, value }
    function memoParsed(path, inputs, build) {
      var hit = parsed.get(path);
      if (hit && inputs.length === hit.inputs.length && inputs.every(function(value, i) { return value === hit.inputs[i]; })) return hit.value;
      // Compare the actual inputs exactly; hashes of only the new content or
      // hunk geometry miss changes to the old side after a new review round.
      // Worker cache IDs stay small and unique without hashing source text.
      var value = build('crit-view:' + viewId + ':' + (++parseVersion));
      parsed.set(path, { inputs: inputs, value: value });
      return value;
    }

    function fileDiffFor(file) {
      var hunks = opts.prepareHunks ? opts.prepareHunks(file) : (file.diffHunks || []);
      var inputs = ['diff', file.path, file.oldPath || file.old_path || '', file.status,
        file.content || '', JSON.stringify(hunks)];
      return memoParsed(file.path, inputs, function(k) { return adapter.buildFileDiff(P, file, hunks, k); });
    }

    // CodeView estimates off-screen height from lines, not annotation DOM.
    // Keep this adapter until unloaded items support explicit size estimates.
    function stubDiffFor(file) {
      var n = adapter.estimatedLineCount(file), p = file.path;
      return memoParsed(p, ['stub', p, n], function(k) {
        var patch = 'diff --git a/' + p + ' b/' + p + '\n--- a/' + p + '\n+++ b/' + p +
          '\n@@ -1,' + n + ' +1,' + n + ' @@\n' + ' \n'.repeat(n);
        return P.processFile(patch, { cacheKey: k });
      });
    }

    function itemFor(file) {
      var isStub = !!file.lazy;
      if (isStub) stubs.add(file.path); else stubs.delete(file.path);
      if (isStub) return {
        id: file.path, type: 'diff', fileDiff: stubDiffFor(file),
        annotations: [{ side: 'additions', lineNumber: 0, metadata: metadataFor('loading', file.path) }],
        collapsed: !!file.collapsed, version: nextVersion(file.path),
      };
      // Markdown "Document" view in git mode: an empty file item whose
      // file-level annotation is Crit's rendered document (see app.js
      // buildPierreDocument). Pierre still owns header, collapse and scroll.
      if (!isStub && opts.isDocumentView && opts.isDocumentView(file)) {
        return fileItem(file, emptyContents(file.path));
      }
      // Files-mode code file: the whole file, comments per line.
      if (!isStub && opts.isFileView && opts.isFileView(file)) {
        return fileItem(file, memoParsed(file.path, ['file', file.path, file.content || ''], function(k) { return adapter.buildFileContents(P, file, k); }));
      }
      return {
        id: file.path,
        type: 'diff',
        fileDiff: fileDiffFor(file),
        annotations: annotationsFor(file),
        collapsed: !!file.collapsed,
        version: nextVersion(file.path),
      };
    }

    // The empty file behind a rendered document / placeholder. One object per
    // path: Pierre prepares a collapsed item's layout against the file object
    // and throws if a later render hands it a different one.
    var emptyFiles = new Map();
    function emptyContents(path) {
      var f = emptyFiles.get(path);
      if (!f) {
        f = { name: path, contents: '', cacheKey: 'doc:' + path };
        emptyFiles.set(path, f);
      }
      return f;
    }

    // File items are single-sided: annotations carry a line number only.
    function fileItem(file, contents) {
      return {
        id: file.path,
        type: 'file',
        file: contents,
        annotations: annotationsFor(file).map(function(a) { return { lineNumber: a.lineNumber, metadata: a.metadata }; }),
        collapsed: !!file.collapsed,
        version: nextVersion(file.path),
      };
    }

    // Publish one file's item. updateItem can't change an item's type
    // (diff ↔ file for markdown Document view); setItems swaps the record in
    // place instead, with the other items passed back unchanged. Pierre's
    // scroll anchor skips the replaced record, so if the reader was on this
    // file its top is put back where it was.
    function publish(file) {
      var item = itemFor(file);
      var prev = itemsByPath.get(file.path);
      itemsByPath.set(file.path, item);
      if (!prev || prev.type === item.type) {
        viewer.updateItem(item);
        return;
      }
      var before = renderedTop(file.path);
      viewer.setItems(order.map(function(p) { return itemsByPath.get(p); }));
      if (before === null) return;
      viewer.render(true);
      var after = renderedTop(file.path);
      if (after !== null && Math.abs(after - before) >= 1) {
        viewer.scrollTo({ type: 'position', position: Math.max(0, viewer.getScrollTop() + after - before) });
      }
    }

    // On-screen top of a mounted item's element, or null when not rendered.
    function renderedTop(path) {
      var r = viewer.getRenderedItems().find(function(x) { return x.id === path; });
      return r && r.element ? r.element.getBoundingClientRect().top : null;
    }

    // Pierre appends a changed annotation's wrapper at the end of the host,
    // and annotations sharing a slot (all file-level ones) show in DOM order.
    // A thread added above a rendered document would land below it. Put the
    // wrappers back in annotation order, walking backwards so the last one
    // (usually the large document) never moves.
    function orderAnnotationWrappers(node, path) {
      var item = itemsByPath.get(path);
      if (!item || !item.annotations || item.annotations.length < 2) return;
      var nextInSlot = new Map();
      for (var i = item.annotations.length - 1; i >= 0; i--) {
        var m = item.annotations[i].metadata;
        var el = elements.get(m.kind + ':' + m.id);
        var wrapper = el && el.parentElement;
        if (!wrapper || wrapper.parentNode !== node) continue;
        var next = nextInSlot.get(wrapper.slot);
        if (next && (wrapper.compareDocumentPosition(next) & 2 /* DOCUMENT_POSITION_PRECEDING */)) {
          // Moving a node blurs whatever it contains; keep a composer's focus.
          var active = wrapper.ownerDocument && wrapper.ownerDocument.activeElement;
          var keep = active && wrapper.contains(active) ? active : null;
          node.insertBefore(wrapper, next);
          if (keep && keep.ownerDocument.activeElement !== keep) keep.focus({ preventScroll: true });
        }
        nextInSlot.set(wrapper.slot, wrapper);
      }
    }

    function pathOf(context) {
      return context && context.item ? context.item.id : null;
    }

    function renderAnnotation(annotation, context) {
      var m = annotation.metadata;
      var key = m.kind + ':' + m.id;
      var el = elements.get(key);
      if (!el) {
        el = opts.buildAnnotation(m.kind, pathOf(context), m.id);
        if (!el) return undefined;
        elements.set(key, el);
      }
      return el;
    }

    var options = Object.assign(adapter.baseOptions(themeType, diffStyle), {
      stickyHeaders: true,
      // Pierre drops pointer events for ~120ms after each scroll by default;
      // measured no scroll cost with them on, and clicks right after a
      // scroll (tree jump, then click a line) land.
      pointerEventsOnScroll: true,
      itemMetrics: opts.itemMetrics,
      layout: opts.layout,
      unsafeCSS: opts.unsafeCSS,
      theme: opts.theme || adapter.THEME,
      overflow: opts.overflow || 'scroll',
      hunkSeparators: opts.hunkSeparators || 'line-info',
      lineDiffType: opts.lineDiffType || 'word-alt',
      diffIndicators: opts.diffIndicators || 'bars',
      expandUnchanged: !!opts.expandUnchanged,
      disableLineNumbers: !!opts.disableLineNumbers,
      renderCustomHeader: function(fileDiff, context) {
        return opts.buildHeader(pathOf(context));
      },
      renderAnnotation: renderAnnotation,
      renderCodeViewHeader: opts.buildListHeader,
      onLineNumberClick: function(props, context) {
        if (opts.onLineNumberClick) opts.onLineNumberClick(props, pathOf(context));
      },
      onLineEnter: opts.onLineEnter,
      onGutterUtilityClick: function(range, context) {
        var r = adapter.formRangeFromSelection(range);
        // File items (document view) have one side; comments are new-side.
        if (r && context && context.type === 'file') r.side = '';
        opts.onGutterUtilityClick(pathOf(context), r);
        // Pierre keeps the clicked range selected, and its next gutter press
        // extends that selection instead of starting at the hovered line.
        // The form now marks the range; drop the selection once the click
        // has finished.
        var clear = function() { if (!disposed) viewer.clearSelectedLines(); };
        if (typeof requestAnimationFrame === 'function') requestAnimationFrame(clear); else clear();
      },
      onPostRender: function(node, instance, phase, context) {
        var path = pathOf(context);
        if (node && phase !== 'unmount') orderAnnotationWrappers(node, path);
        if (node && node.dataset) {
          if (stubs.has(path)) node.dataset.critStub = '1'; else delete node.dataset.critStub;
        }
        if (opts.onPostRender) opts.onPostRender(path, node, phase);
        // Scroll subscribers run before CodeView renders its new viewport.
        // Hydrate from the mounted items too, so a single scroll cannot leave
        // the newly revealed files as stubs until the reader scrolls again.
        if (phase !== 'unmount') queueHydrate();
      },
    });
    // setOptions replaces the whole options object, so keep the source of truth here.
    function updateOptions(patch) {
      options = Object.assign({}, options, patch);
      viewer.setOptions(options);
    }
    var viewer = new P.CodeView(options, opts.workerPool);
    viewer.setup(root);

    function hydrateVisible() {
      if (disposed) return;
      var rendered = viewer.getRenderedItems();
      for (var i = 0; i < rendered.length; i++) {
        var path = rendered[i].id;
        if (stubs.has(path) && !hydrating.has(path)) hydrateQueue.push(path), hydrating.set(path, null);
      }
      pumpHydration();
    }

    var active = 0;
    function pumpHydration() {
      while (active < HYDRATE_CONCURRENCY && hydrateQueue.length) {
        var path = hydrateQueue.shift();
        active++;
        hydrate(path).finally(function() { active--; pumpHydration(); });
      }
    }

    // Load a lazy file's real diff and swap the stub in place. CodeView keeps
    // the reading position anchored across the height change.
    function hydrate(path) {
      var existing = hydrating.get(path);
      if (existing) return existing;
      var started = generation;
      var p = Promise.resolve(opts.loadFile(path)).then(function(file) {
        if (disposed || started !== generation || !file || !itemsByPath.has(path)) return;
        publish(file);
      });
      hydrating.set(path, p);
      return p.finally(function() { if (hydrating.get(path) === p) hydrating.delete(path); });
    }

    // Scroll events come faster than frames; look for stubs once per frame.
    var hydrateQueued = false;
    function queueHydrate() {
      if (hydrateQueued) return;
      hydrateQueued = true;
      var run = function() { hydrateQueued = false; hydrateVisible(); };
      if (typeof requestAnimationFrame === 'function') requestAnimationFrame(run); else setTimeout(run, 0);
    }
    var unsubscribe = viewer.subscribeToScroll(queueHydrate);

    function setFiles(files) {
      generation++;
      // Loads started for the previous generation are dropped when they land;
      // forget them so hydrateVisible below starts fresh ones.
      hydrating.clear();
      hydrateQueue.length = 0;
      // A full render follows a reload, round or view toggle: rebuild
      // annotations from the current model, except open forms (they hold
      // the reader's typing) and rendered documents (the caller re-renders
      // those in place, keeping the element Pierre has measured).
      Array.from(elements.keys()).forEach(function(key) {
        if (key.indexOf('form:') !== 0 && key.indexOf('document:') !== 0) forgetAnnotation(key);
      });
      order = files.map(function(f) { return f.path; });
      itemsByPath = new Map();
      var items = files.map(function(f) {
        var item = itemFor(f);
        itemsByPath.set(f.path, item);
        return item;
      });
      viewer.setItems(items);
      viewer.render(true);
      hydrateVisible();
    }

    // Re-publish one file (comments/forms/hunks changed). Pass a thread or
    // form key in `invalidate` to rebuild just those annotation elements.
    function refreshFile(file, invalidate) {
      (invalidate || []).forEach(forgetAnnotation);
      publish(file);
      viewer.render(true);
    }

    function forgetAnnotation(key) {
      elements.delete(key);
      metadataCache.delete(key);
    }

    function annotationElement(key) {
      return elements.get(key) || null;
    }

    // Make sure a file's real diff is loaded (for jumps to lazy files).
    function ensureLoaded(path) {
      return stubs.has(path) ? hydrate(path) : Promise.resolve();
    }

    function scrollToFile(path, align) {
      return ensureLoaded(path).then(function() {
        viewer.scrollTo({ type: 'item', id: path, align: align || 'start' });
      });
    }

    function scrollToLine(path, lineNumber, side, align) {
      // A rendered document / placeholder is an empty file item: there is no
      // line to target, and Pierre would keep sticking to a bogus position.
      var item = itemsByPath.get(path);
      if (item && item.type === 'file' && item.file.contents === '') return scrollToFile(path, 'start');
      return ensureLoaded(path).then(function() {
        viewer.scrollTo({
          type: 'line', id: path, lineNumber: lineNumber,
          side: side === 'old' ? 'deletions' : 'additions',
          align: align || 'center',
        });
      });
    }

    function setCollapsed(file, isCollapsed) {
      file.collapsed = !!isCollapsed;
      publish(file);
    }

    // Collapse or expand every file, then render once.
    function setAllCollapsed(files, isCollapsed) {
      files.forEach(function(f) {
        f.collapsed = !!isCollapsed;
        if (itemsByPath.has(f.path)) publish(f);
      });
      viewer.render(true);
    }

    function setDiffStyle(style, files) {
      if (style === diffStyle) return;
      diffStyle = style;
      updateOptions({ diffStyle: style });
      if (files) setFiles(files);
    }

    function setThemeType(type) {
      if (type === themeType) return;
      themeType = type;
      updateOptions({ themeType: type });
    }

    function setSelectedLine(path, lineNumber, side) {
      if (!path) {
        viewer.clearSelectedLines();
        return;
      }
      var s = side === 'old' ? 'deletions' : 'additions';
      viewer.setSelectedLines({ id: path, range: { start: lineNumber, end: lineNumber, side: s, endSide: s } });
    }

    function renderedPaths() {
      return viewer.getRenderedItems().map(function(r) { return r.id; });
    }

    function destroy() {
      disposed = true;
      generation++;
      if (unsubscribe) unsubscribe();
      viewer.cleanUp();
      elements.clear();
      metadataCache.clear();
    }

    return {
      viewer: viewer,
      setFiles: setFiles,
      refreshFile: refreshFile,
      forgetAnnotation: forgetAnnotation,
      annotationElement: annotationElement,
      ensureLoaded: ensureLoaded,
      scrollToFile: scrollToFile,
      scrollToLine: scrollToLine,
      setCollapsed: setCollapsed,
      setAllCollapsed: setAllCollapsed,
      setDiffStyle: setDiffStyle,
      setThemeType: setThemeType,
      setRenderOptions: updateOptions,
      setSelectedLine: setSelectedLine,
      renderedPaths: renderedPaths,
      isStub: function(path) { return stubs.has(path); },
      destroy: destroy,
    };
  }

  var api = { createPierreView: createPierreView };

  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.pierreView = api;
  }
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
