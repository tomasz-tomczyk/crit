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

    function fileDiffFor(file) {
      var hunks = opts.prepareHunks ? opts.prepareHunks(file) : (file.diffHunks || []);
      var cacheKey = file.path + ':' + (file.fileHash || '') + ':' + (versions.get(file.path) || 0);
      return adapter.buildFileDiff(P, file, hunks, cacheKey);
    }

    function stubDiffFor(file) {
      var n = adapter.estimatedLineCount(file);
      var p = file.path;
      var patch = 'diff --git a/' + p + ' b/' + p + '\n--- a/' + p + '\n+++ b/' + p +
        '\n@@ -1,' + n + ' +1,' + n + ' @@\n' + ' \n'.repeat(n);
      return P.processFile(patch, { cacheKey: 'stub:' + p });
    }

    function itemFor(file) {
      var isStub = !!file.lazy;
      if (isStub) stubs.add(file.path); else stubs.delete(file.path);
      // Markdown "Document" view in git mode: the full source as a Pierre
      // file item (Pierre renders source, not rendered markdown). Comments
      // attach per source line, like the diff view.
      if (!isStub && opts.isDocumentView && opts.isDocumentView(file)) {
        return {
          id: file.path,
          type: 'file',
          file: { name: file.path, contents: file.content || '', cacheKey: 'doc:' + file.path + ':' + (file.fileHash || '') },
          annotations: annotationsFor(file).map(function(a) { return { lineNumber: a.lineNumber, metadata: a.metadata }; }),
          collapsed: !!file.collapsed,
          version: nextVersion(file.path),
        };
      }
      return {
        id: file.path,
        type: 'diff',
        fileDiff: isStub ? stubDiffFor(file) : fileDiffFor(file),
        annotations: isStub ? [] : annotationsFor(file),
        collapsed: !!file.collapsed,
        version: nextVersion(file.path),
      };
    }

    // Publish one file's item. CodeView won't change an item's type in
    // place (diff ↔ file for markdown Document view), so a type change is a
    // remove + reinsert at the same position; other items are passed back
    // unchanged and reconcile without re-rendering.
    function publish(file) {
      var item = itemFor(file);
      var prev = itemsByPath.get(file.path);
      itemsByPath.set(file.path, item);
      if (prev && prev.type !== item.type) {
        // Removing the record drops the scroll anchor on it; if the reader
        // was on this file, put its top back where it was.
        var before = renderedTop(file.path);
        viewer.removeItem(file.path);
        viewer.setItems(order.map(function(p) { return itemsByPath.get(p); }));
        if (before !== null) {
          viewer.render(true);
          viewer.scrollTo({ type: 'item', id: file.path, align: 'start' });
          viewer.render(true);
          var after = renderedTop(file.path);
          if (after !== null && Math.abs(after - before) >= 1) {
            viewer.scrollTo({ type: 'position', position: Math.max(0, viewer.getScrollTop() + after - before) });
          }
        }
        return;
      }
      viewer.updateItem(item);
    }

    // On-screen top of a mounted item's element, or null when not rendered.
    function renderedTop(path) {
      var r = viewer.getRenderedItems().find(function(x) { return x.id === path; });
      return r && r.element ? r.element.getBoundingClientRect().top : null;
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

    var options = {
      theme: { dark: 'pierre-dark', light: 'pierre-light' },
      themeType: themeType,
      diffStyle: diffStyle,
      lineDiffType: 'word-alt',
      stickyHeaders: true,
      expansionLineCount: 20,
      enableGutterUtility: true,
      lineHoverHighlight: 'number',
      itemMetrics: opts.itemMetrics,
      unsafeCSS: opts.unsafeCSS,
      renderCustomHeader: function(fileDiff, context) {
        return opts.buildHeader(pathOf(context));
      },
      renderAnnotation: renderAnnotation,
      renderCodeViewHeader: opts.buildListHeader,
      onGutterUtilityClick: function(range, context) {
        var r = adapter.formRangeFromSelection(range);
        // File items (document view) have one side; comments are new-side.
        if (r && context && context.type === 'file') r.side = '';
        opts.onGutterUtilityClick(pathOf(context), r);
      },
      onPostRender: function(node, instance, phase, context) {
        if (opts.onPostRender) opts.onPostRender(pathOf(context), node, phase);
      },
    };
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
      var p = Promise.resolve(opts.loadFile(path)).then(function(file) {
        if (disposed || !file) return;
        publish(file);
      });
      hydrating.set(path, p);
      return p.finally(function() { hydrating.delete(path); });
    }

    var unsubscribe = viewer.subscribeToScroll(hydrateVisible);

    function setFiles(files) {
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
      (invalidate || []).forEach(function(key) {
        elements.delete(key);
        metadataCache.delete(key);
      });
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
      setDiffStyle: setDiffStyle,
      setThemeType: setThemeType,
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
