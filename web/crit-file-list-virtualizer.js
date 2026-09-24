(function () {
  'use strict';

  // Pierre CodeView-style multi-file list virtualization for Crit.
  // Off-screen files are height placeholders; near-viewport files mount a real
  // .file-section (whose body may still use crit-diff-virtualizer row windows).
  // Height refinements use getScrollAnchor / resolveAnchoredScrollTop so mounts
  // and collapse toggles do not shove the reading position.

  var diffV = (typeof window !== 'undefined' && window.crit && window.crit.diffVirtualizer)
    ? window.crit.diffVirtualizer
    : (typeof require === 'function' ? require('./crit-diff-virtualizer.js') : null);

  if (!diffV) {
    throw new Error('crit-file-list-virtualizer requires crit-diff-virtualizer');
  }

  // Approximate <summary.file-header> block (padding + one text line).
  var FILE_HEADER_ESTIMATE = 40;

  function estimateFileSectionHeight(item) {
    item = item || {};
    var body = item.bodyHeight || 0;
    if (typeof item.estimateBodyHeight === 'function') {
      body = item.estimateBodyHeight(item);
    }
    if (item.collapsed) return FILE_HEADER_ESTIMATE;
    return FILE_HEADER_ESTIMATE + Math.max(0, body);
  }

  function getScrollAnchor(heightIndex, items, scrollTop) {
    if (!heightIndex || !items || items.length === 0) return null;
    var top = Math.max(0, scrollTop || 0);
    var index = heightIndex.indexAt(top);
    if (index < 0) return null;
    return {
      index: index,
      key: items[index].key,
      intraOffset: top - heightIndex.offset(index),
    };
  }

  function resolveAnchoredScrollTop(heightIndex, items, anchor) {
    if (!anchor || !heightIndex || !items) return null;
    var index = anchor.index;
    if (anchor.key != null) {
      for (var i = 0; i < items.length; i++) {
        if (items[i].key === anchor.key) {
          index = i;
          break;
        }
      }
    }
    if (index < 0 || index >= items.length) return null;
    return heightIndex.offset(index) + (anchor.intraOffset || 0);
  }

  function scrollParentScrollTop(scrollParent) {
    if (!scrollParent || scrollParent === window) {
      return window.pageYOffset || document.documentElement.scrollTop || 0;
    }
    return scrollParent.scrollTop || 0;
  }

  function scrollParentScrollTo(scrollParent, top) {
    if (!scrollParent || scrollParent === window) {
      window.scrollTo({ top: top, left: 0, behavior: 'instant' });
      return;
    }
    scrollParent.scrollTop = top;
  }

  function applyScrollFix(scrollParent, targetScrollTop) {
    if (targetScrollTop == null || isNaN(targetScrollTop)) return;
    var current = scrollParentScrollTop(scrollParent);
    if (Math.abs(current - targetScrollTop) < 0.5) return;
    scrollParentScrollTo(scrollParent, targetScrollTop);
  }

  function FileListVirtualizer(options) {
    options = options || {};
    this.surface = options.surface;
    this.items = options.items || [];
    this.estimateHeight = options.estimateHeight || estimateFileSectionHeight;
    this.renderPlaceholder = options.renderPlaceholder;
    this.renderMounted = options.renderMounted;
    this.onRangeChange = options.onRangeChange || null;
    this.onBeforeHeightChange = options.onBeforeHeightChange || null;
    this.onUnmount = options.onUnmount || null;
    this.scrollParent = options.scrollParent || null;
    // Test hooks: when set, skip live viewportMetrics.
    this._fixedLocalTop = options.localTop;
    this._fixedViewportHeight = options.viewportHeight;
    this.heightIndex = new diffV.HeightIndex(this.items, this.estimateHeight);
    this.keyToIndex = new Map();
    this.rebuildKeyMap();
    this.nodes = new Map();
    this.pinnedKeys = new Set();
    this.mountedKeys = new Set();
    this.disposed = false;
    this.updateFrame = 0;
    this.measureFrame = 0;
    this.pendingMeasurements = new Map();
    this._onScroll = null;
    this._onResize = null;
    var self = this;
    this.resizeObserver = typeof ResizeObserver === 'function'
      ? new ResizeObserver(function(entries) { self.handleMeasurements(entries); })
      : null;
    if (this.surface) this.surface._critFileListVirtualizer = this;
  }

  FileListVirtualizer.prototype.rebuildKeyMap = function() {
    this.keyToIndex = new Map();
    for (var i = 0; i < this.items.length; i++) {
      this.keyToIndex.set(this.items[i].key, i);
    }
  };

  FileListVirtualizer.prototype.start = function() {
    if (this.disposed) return;
    if (!this.scrollParent) {
      this.scrollParent = diffV.findScrollParent(this.surface) || window;
    }
    var self = this;
    this._onScroll = function() { self.scheduleUpdate(); };
    this._onResize = function() { self.scheduleUpdate(); };
    var target = this.scrollParent === window ? window : this.scrollParent;
    target.addEventListener('scroll', this._onScroll, { passive: true });
    window.addEventListener('resize', this._onResize);
    this.update();
  };

  FileListVirtualizer.prototype.dispose = function() {
    this.disposed = true;
    if (this.updateFrame) cancelAnimationFrame(this.updateFrame);
    if (this.measureFrame) cancelAnimationFrame(this.measureFrame);
    if (this.resizeObserver) this.resizeObserver.disconnect();
    var target = this.scrollParent === window ? window : this.scrollParent;
    if (this._onScroll && target) target.removeEventListener('scroll', this._onScroll);
    if (this._onResize) window.removeEventListener('resize', this._onResize);
    this.nodes.clear();
    this.mountedKeys.clear();
    this.pinnedKeys.clear();
    if (this.surface && this.surface._critFileListVirtualizer === this) {
      delete this.surface._critFileListVirtualizer;
    }
  };

  FileListVirtualizer.prototype.setItems = function(items) {
    var anchor = this.captureAnchor();
    this.items = items || [];
    this.rebuildKeyMap();
    this.heightIndex = new diffV.HeightIndex(this.items, this.estimateHeight);
    this.nodes.clear();
    this.mountedKeys.clear();
    if (this.surface) {
      while (this.surface.firstChild) this.surface.removeChild(this.surface.firstChild);
    }
    this.update();
    if (anchor) this.restoreAnchor(anchor);
  };

  FileListVirtualizer.prototype.scheduleUpdate = function() {
    if (this.disposed || this.updateFrame) return;
    var self = this;
    this.updateFrame = requestAnimationFrame(function() {
      self.updateFrame = 0;
      self.update();
    });
  };

  FileListVirtualizer.prototype.viewportInterval = function() {
    if (this.items.length === 0) return null;
    var localTop;
    var viewportHeight;
    if (this._fixedViewportHeight != null) {
      localTop = this._fixedLocalTop || 0;
      viewportHeight = this._fixedViewportHeight;
    } else {
      var metrics = diffV.viewportMetrics(this.scrollParent || window, this.surface);
      localTop = metrics.localTop;
      viewportHeight = metrics.viewportHeight;
    }
    return diffV.calculateWindow(this.heightIndex, localTop, viewportHeight);
  };

  FileListVirtualizer.prototype.update = function() {
    if (this.disposed) return;
    var interval = this.viewportInterval();
    var intervals = interval ? [interval] : [];
    this.pinnedKeys.forEach(function(key) {
      var index = this.keyToIndex.get(key);
      if (index !== undefined) intervals.push([index, index]);
    }, this);
    this.reconcile(diffV.mergeIntervals(intervals, this.items.length));
  };

  FileListVirtualizer.prototype.reconcile = function(intervals) {
    // Accept a single [start, end] or an array of intervals.
    if (intervals && intervals.length === 2 &&
        typeof intervals[0] === 'number' && typeof intervals[1] === 'number') {
      intervals = [intervals];
    }
    intervals = intervals || [];
    var desired = [];
    var cursor = 0;
    for (var ii = 0; ii < intervals.length; ii++) {
      var interval = intervals[ii];
      if (interval[0] > cursor) {
        desired.push({
          key: 'spacer:' + cursor + ':' + interval[0],
          spacer: true,
          height: this.heightIndex.offset(interval[0]) - this.heightIndex.offset(cursor),
        });
      }
      for (var i = interval[0]; i <= interval[1]; i++) {
        desired.push({ key: this.items[i].key, index: i, item: this.items[i] });
      }
      cursor = interval[1] + 1;
    }
    if (cursor < this.items.length) {
      desired.push({
        key: 'spacer:' + cursor + ':' + this.items.length,
        spacer: true,
        height: this.heightIndex.total() - this.heightIndex.offset(cursor),
      });
    }

    var desiredKeys = new Set(desired.map(function(d) { return d.key; }));
    var children = Array.from(this.surface.children);
    for (var c = 0; c < children.length; c++) {
      var oldKey = children[c].dataset.virtualKey;
      if (desiredKeys.has(oldKey)) continue;
      if (this.resizeObserver && children[c].dataset.fileListIndex !== undefined) {
        this.resizeObserver.unobserve(children[c]);
      }
      // Drop row-virtualizer controllers before detaching the section.
      if (typeof this.onUnmount === 'function' && this.mountedKeys.has(oldKey)) {
        try { this.onUnmount(oldKey, children[c]); } catch (err) { /* ignore */ }
      }
      this.nodes.delete(oldKey);
      this.mountedKeys.delete(oldKey);
      children[c].remove();
    }

    for (var di = 0; di < desired.length; di++) {
      var descriptor = desired[di];
      var node = this.nodes.get(descriptor.key);
      if (!node) {
        if (descriptor.spacer) {
          node = document.createElement('div');
          node.className = 'file-list-virtual-spacer';
          node.setAttribute('aria-hidden', 'true');
        } else {
          var preferMount = this.pinnedKeys.has(descriptor.key) ||
            !descriptor.item.placeholderOnly;
          // Mount real sections for the ordinary window + pins. Placeholders
          // only when renderMounted is absent or item asks for placeholderOnly.
          if (preferMount && this.renderMounted) {
            node = this.renderMounted(descriptor.item, descriptor.index);
            this.mountedKeys.add(descriptor.key);
          } else if (this.renderPlaceholder) {
            node = this.renderPlaceholder(descriptor.item, descriptor.index);
          } else {
            node = document.createElement('div');
            node.className = 'file-section-placeholder';
          }
          if (!node || node.nodeType !== 1) {
            throw new Error('File list items must render one element');
          }
          node.dataset.fileListIndex = String(descriptor.index);
          node.dataset.fileKey = descriptor.item.key;
          if (this.resizeObserver) this.resizeObserver.observe(node);
        }
        node.dataset.virtualKey = descriptor.key;
        this.nodes.set(descriptor.key, node);
      }
      if (descriptor.spacer) {
        node.style.height = Math.max(0, descriptor.height) + 'px';
      }
      var current = this.surface.children[di];
      if (current !== node) this.surface.insertBefore(node, current || null);
    }
    while (this.surface.children.length > desired.length) {
      var extra = this.surface.lastElementChild;
      if (this.resizeObserver && extra.dataset.fileListIndex !== undefined) {
        this.resizeObserver.unobserve(extra);
      }
      this.nodes.delete(extra.dataset.virtualKey);
      this.mountedKeys.delete(extra.dataset.virtualKey);
      extra.remove();
    }
    if (this.onRangeChange) this.onRangeChange(intervals);
  };

  FileListVirtualizer.prototype.handleMeasurements = function(entries) {
    for (var i = 0; i < entries.length; i++) {
      var index = parseInt(entries[i].target.dataset.fileListIndex, 10);
      if (isNaN(index)) continue;
      var height = entries[i].borderBoxSize && entries[i].borderBoxSize.length
        ? entries[i].borderBoxSize[0].blockSize
        : entries[i].target.getBoundingClientRect().height;
      this.pendingMeasurements.set(index, height);
    }
    if (this.measureFrame) return;
    var self = this;
    this.measureFrame = requestAnimationFrame(function() {
      self.measureFrame = 0;
      self.flushMeasurements();
    });
  };

  FileListVirtualizer.prototype.flushMeasurements = function() {
    if (this.pendingMeasurements.size === 0) return;
    var anchor = this.captureAnchor();
    if (this.onBeforeHeightChange) this.onBeforeHeightChange(anchor);
    var first = this.heightIndex.updateMany(this.pendingMeasurements);
    this.pendingMeasurements.clear();
    if (first < 0) return;
    this.restoreAnchor(anchor);
    this.scheduleUpdate();
  };

  FileListVirtualizer.prototype.captureAnchor = function() {
    if (!this.surface) {
      return getScrollAnchor(this.heightIndex, this.items, scrollParentScrollTop(this.scrollParent || window));
    }
    var scrollParent = this.scrollParent || window;
    var metrics = diffV.viewportMetrics(scrollParent, this.surface);
    return getScrollAnchor(this.heightIndex, this.items, metrics.localTop);
  };

  FileListVirtualizer.prototype.restoreAnchor = function(anchor) {
    if (!anchor) return false;
    var target = resolveAnchoredScrollTop(this.heightIndex, this.items, anchor);
    if (target == null) return false;
    var scrollParent = this.scrollParent || window;
    if (!this.surface) {
      applyScrollFix(scrollParent, target);
      return true;
    }
    var metrics = diffV.viewportMetrics(scrollParent, this.surface);
    var surfaceOffset = scrollParentScrollTop(scrollParent) +
      (metrics.surfaceRect.top - metrics.vpTop);
    applyScrollFix(scrollParent, surfaceOffset + target);
    return true;
  };

  FileListVirtualizer.prototype.setItemHeight = function(keyOrIndex, height) {
    var index = typeof keyOrIndex === 'number'
      ? keyOrIndex
      : this.keyToIndex.get(keyOrIndex);
    if (index === undefined || index < 0) return false;
    var anchor = this.captureAnchor();
    var changes = new Map();
    changes.set(index, height);
    var first = this.heightIndex.updateMany(changes);
    if (first < 0) return false;
    this.restoreAnchor(anchor);
    this.scheduleUpdate();
    return true;
  };

  FileListVirtualizer.prototype.setCollapsed = function(key, collapsed) {
    var index = this.keyToIndex.get(key);
    if (index === undefined) return false;
    var item = this.items[index];
    item.collapsed = !!collapsed;
    var next = this.estimateHeight(item);
    return this.setItemHeight(index, next);
  };

  FileListVirtualizer.prototype.pin = function(key) {
    this.pinnedKeys.add(key);
    this.scheduleUpdate();
  };

  FileListVirtualizer.prototype.unpin = function(key) {
    this.pinnedKeys.delete(key);
    this.scheduleUpdate();
  };

  FileListVirtualizer.prototype.ensureMounted = function(key) {
    var self = this;
    this.pin(key);
    this.update();
    return Promise.resolve(this.nodes.get(key)).then(function(node) {
      // Caller may unpin after scroll settles.
      return node || null;
    });
  };

  FileListVirtualizer.prototype.scrollToItem = function(key, alignment) {
    var index = this.keyToIndex.get(key);
    if (index === undefined) return Promise.resolve(null);
    var self = this;
    return this.ensureMounted(key).then(function(node) {
      // Prefer element.scrollIntoView after mount — absolute heightIndex math
      // drifts when estimates refine, which left deep sidebar jumps off-screen.
      if (node && typeof node.scrollIntoView === 'function') {
        node.scrollIntoView({
          block: alignment === 'center' ? 'center' : 'start',
          behavior: 'instant',
        });
      } else {
        var scrollParent = self.scrollParent || window;
        var itemTop = self.heightIndex.offset(index);
        var itemHeight = self.heightIndex.height(index);
        var target = itemTop;
        if (alignment === 'center') {
          var vh = scrollParent === window
            ? (window.innerHeight || 0)
            : (scrollParent.clientHeight || 0);
          target = itemTop - Math.max(0, (vh - itemHeight) / 2);
        }
        if (self.surface && scrollParent === window) {
          var rect = self.surface.getBoundingClientRect();
          var surfaceTop = rect.top + (window.pageYOffset || 0);
          applyScrollFix(scrollParent, surfaceTop + Math.max(0, target));
        } else if (self.surface) {
          var metrics = diffV.viewportMetrics(scrollParent, self.surface);
          var surfaceOffset = scrollParentScrollTop(scrollParent) +
            (metrics.surfaceRect.top - metrics.vpTop);
          applyScrollFix(scrollParent, surfaceOffset + Math.max(0, target));
        } else {
          applyScrollFix(scrollParent, Math.max(0, target));
        }
      }
      self.scheduleUpdate();
      return node;
    });
  };

  FileListVirtualizer.prototype.indexAtScroll = function() {
    var anchor = this.captureAnchor();
    return anchor ? anchor.index : -1;
  };

  FileListVirtualizer.prototype.totalHeight = function() {
    return this.heightIndex.total();
  };

  var api = {
    FILE_HEADER_ESTIMATE: FILE_HEADER_ESTIMATE,
    estimateFileSectionHeight: estimateFileSectionHeight,
    getScrollAnchor: getScrollAnchor,
    resolveAnchoredScrollTop: resolveAnchoredScrollTop,
    applyScrollFix: applyScrollFix,
    FileListVirtualizer: FileListVirtualizer,
  };

  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.fileListVirtualizer = api;
  }
  if (typeof module === 'object' && module.exports) module.exports = api;
})();
