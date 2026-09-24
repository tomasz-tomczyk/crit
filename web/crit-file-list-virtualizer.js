(function () {
  'use strict';

  // Pierre CodeView-style multi-file list virtualization for Crit.
  // Off-screen files are height placeholders; near-viewport files mount a real
  // .file-section (whose body may still use crit-diff-virtualizer row windows).
  // Height refinements use getScrollAnchor / resolveAnchoredScrollTop so mounts
  // and collapse toggles do not shove the reading position.

  var diffV = (typeof window !== 'undefined' && window.crit && window.crit.diffVirtualizer)
    ? window.crit.diffVirtualizer
    : (typeof module === 'object' && typeof require === 'function'
      ? require('./crit-diff-virtualizer.js')
      : null);

  // In the browser, load after crit-diff-virtualizer.js. Soft-fail so a missing
  // dependency cannot blank the whole review UI.
  if (!diffV) {
    var emptyApi = {
      FILE_HEADER_ESTIMATE: 44,
      estimateFileSectionHeight: function() { return 44; },
      getScrollAnchor: function() { return null; },
      resolveAnchoredScrollTop: function() { return null; },
      applyScrollFix: function() {},
      FileListVirtualizer: null,
    };
    if (typeof window !== 'undefined') {
      window.crit = window.crit || {};
      window.crit.fileListVirtualizer = emptyApi;
    }
    if (typeof module === 'object' && module.exports) module.exports = emptyApi;
    return;
  }

  // Approximate <summary.file-header> — Pierre DEFAULT_VIRTUAL_FILE_METRICS.diffHeaderHeight.
  // Prefer a live measure once a real header is in the DOM.
  var FILE_HEADER_ESTIMATE = 44;
  var _measuredFileHeader = null;

  // Pierre CodeView.js scroll-rebase constants (only engage above THRESHOLD).
  var SCROLL_REBASE_CONTAINER_HEIGHT = 12e6;
  var SCROLL_REBASE_TRIGGER_TOP = 1e6;
  var SCROLL_REBASE_TARGET_TOP = 2e6;
  var SCROLL_REBASE_TARGET_BOTTOM = 1e7;
  var SCROLL_REBASE_THRESHOLD = 11e6;

  function roundToDevicePixel(value) {
    if (diffV && typeof diffV.roundToDevicePixel === 'function') {
      return diffV.roundToDevicePixel(value);
    }
    var dpr = (typeof window !== 'undefined' && window.devicePixelRatio) || 1;
    return Math.round(value * dpr) / dpr;
  }

  function fileHeaderEstimate() {
    if (_measuredFileHeader != null) return _measuredFileHeader;
    if (typeof document !== 'undefined') {
      try {
        var el = document.querySelector('.file-section > summary.file-header, summary.file-header');
        if (el && typeof el.getBoundingClientRect === 'function') {
          var h = el.getBoundingClientRect().height;
          if (h > 0) {
            _measuredFileHeader = h;
            return h;
          }
        }
      } catch (e) { /* ignore */ }
    }
    return FILE_HEADER_ESTIMATE;
  }

  function estimateFileSectionHeight(item) {
    item = item || {};
    var body = item.bodyHeight || 0;
    if (typeof item.estimateBodyHeight === 'function') {
      body = item.estimateBodyHeight(item);
    }
    var header = fileHeaderEstimate();
    if (item.collapsed) return header;
    return header + Math.max(0, body);
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
      var se = document.scrollingElement || document.documentElement;
      return se.scrollTop || window.pageYOffset || 0;
    }
    return scrollParent.scrollTop || 0;
  }

  // Assign scrollTop under scroll-behavior:auto. CSS `html { scroll-behavior:
  // smooth }` (if present) animates even raw scrollTop writes in Chromium and
  // turns deep-file pins into multi-second drifts.
  function withInstantScroll(fn) {
    var root = typeof document !== 'undefined' ? document.documentElement : null;
    var prev = root ? root.style.scrollBehavior : '';
    if (root) root.style.scrollBehavior = 'auto';
    try {
      fn();
    } finally {
      if (root) root.style.scrollBehavior = prev;
    }
  }

  function scrollParentScrollTo(scrollParent, top) {
    withInstantScroll(function() {
      if (!scrollParent || scrollParent === window) {
        var se = document.scrollingElement || document.documentElement;
        se.scrollTop = top;
        return;
      }
      scrollParent.scrollTop = top;
    });
  }

  function scrollParentScrollBy(scrollParent, delta) {
    if (!(Math.abs(delta) >= 0.5)) return;
    withInstantScroll(function() {
      if (!scrollParent || scrollParent === window) {
        var se = document.scrollingElement || document.documentElement;
        se.scrollTop += delta;
        return;
      }
      scrollParent.scrollTop += delta;
    });
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
    this._scrollLockKey = null;
    this._scrollLockUntil = 0;
    this._stickKey = null;
    this._stickIgnoreScroll = false;
    this._stickTargetTop = null;
    this._pinPaddingPx = 0;
    this.scrollPageOffset = 0;
    this._lastWindowScrollTop = null;
    this._forceFitPerfectly = false;
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
    // Pierre CodeView keeps pendingScrollTarget through layout; it does NOT
    // drop the anchor when its own scrollFix writes scrollTop. Clearing stick
    // on the generic 'scroll' event raced with pinKeyToViewportTop and left
    // app.js unstuck while neighbor heights were still refining.
    this._onScroll = function() {
      if (self.shouldRebaseScroll()) {
        var logical = scrollParentScrollTop(self.scrollParent || window) + (self.scrollPageOffset || 0);
        if (self.needsScrollPageUpdate(logical)) {
          var resolved = self.resolvePagedScrollPosition(logical);
          self.scrollPageOffset = resolved.scrollPageOffset;
          self.update();
          scrollParentScrollTo(self.scrollParent || window, resolved.pagedScrollTop);
          return;
        }
      } else if (self.scrollPageOffset) {
        self.scrollPageOffset = 0;
      }
      self.scheduleUpdate();
    };
    this._onUserScrollIntent = function() {
      // Always release pending target + lock + pin padding. After settle we
      // clear stick but keep lock for max-scroll room; without clearing lock
      // on wheel, restoreAfterHeightChange kept re-pinning and fought scroll.
      self.clearStickToKey();
    };
    this._onResize = function() { self.scheduleUpdate(); };
    var target = this.scrollParent === window ? window : this.scrollParent;
    target.addEventListener('scroll', this._onScroll, { passive: true });
    // Pierre CodeView.clearPendingScroll: wheel / touchstart / pointerdown / keydown.
    // Only real user intents release stick (not programmatic scrollTop writes).
    // Keydown is filtered to scroll keys so Crit j/k nav does not clear stick.
    target.addEventListener('wheel', this._onUserScrollIntent, { passive: true });
    target.addEventListener('touchstart', this._onUserScrollIntent, { passive: true });
    target.addEventListener('touchmove', this._onUserScrollIntent, { passive: true });
    target.addEventListener('pointerdown', this._onUserScrollIntent, { passive: true });
    if (typeof window !== 'undefined') {
      window.addEventListener('keydown', this._onUserKeyScrollIntent = function(e) {
        var k = e.key;
        if (k === 'PageDown' || k === 'PageUp' || k === 'Home' || k === 'End' ||
            k === 'ArrowDown' || k === 'ArrowUp' || k === ' ') {
          self._onUserScrollIntent();
        }
      }, true);
    }
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
    if (this._onUserScrollIntent && target) {
      target.removeEventListener('wheel', this._onUserScrollIntent);
      target.removeEventListener('touchstart', this._onUserScrollIntent);
      target.removeEventListener('touchmove', this._onUserScrollIntent);
      target.removeEventListener('pointerdown', this._onUserScrollIntent);
    }
    if (this._onUserKeyScrollIntent && typeof window !== 'undefined') {
      window.removeEventListener('keydown', this._onUserKeyScrollIntent, true);
    }
    if (this._onResize) window.removeEventListener('resize', this._onResize);
    this._stickKey = null;
    this.clearPinScrollRoom();
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
    // Pierre: windowing uses logical scroll (pagedScrollTop + scrollPageOffset).
    if (this.shouldRebaseScroll()) {
      localTop = localTop + (this.scrollPageOffset || 0);
    }
    // Pierre CodeView: overscrollSize 200; fitPerfectly on first paint or large jumps
    // (|Δscroll| > viewport + overscroll*2), and while a pending scroll target is set.
    var overscrollSize = diffV.OVERSCROLL_SIZE != null ? diffV.OVERSCROLL_SIZE : 200;
    var fitPerfectly = !!this._forceFitPerfectly || !!this._stickKey;
    if (!fitPerfectly && this._lastWindowScrollTop != null) {
      fitPerfectly = Math.abs(localTop - this._lastWindowScrollTop) >
        viewportHeight + overscrollSize * 2;
    }
    if (this._lastWindowScrollTop == null) fitPerfectly = true; // first paint
    this._lastWindowScrollTop = localTop;
    this._forceFitPerfectly = false;
    // gap(8) + diffHeaderHeight(44) — Pierre getFitPerfectlyOverscroll()
    var fitPerfectlyOverscroll = 8 + 44;
    return diffV.calculateWindow(this.heightIndex, localTop, viewportHeight, {
      overscrollSize: overscrollSize,
      fitPerfectly: fitPerfectly,
      fitPerfectlyOverscroll: fitPerfectlyOverscroll,
    });
  };

  FileListVirtualizer.prototype.getViewportHeight = function() {
    if (this._fixedViewportHeight != null) return this._fixedViewportHeight;
    var scrollParent = this.scrollParent || window;
    if (!scrollParent || scrollParent === window) {
      return window.innerHeight ||
        (document.documentElement && document.documentElement.clientHeight) || 0;
    }
    return scrollParent.getBoundingClientRect().height;
  };

  FileListVirtualizer.prototype.getScrollHeight = function() {
    return this.heightIndex.total() + (this._pinPaddingPx || 0);
  };

  FileListVirtualizer.prototype.getMaxScrollTopForHeight = function(scrollHeight) {
    return Math.max((scrollHeight || 0) - this.getViewportHeight(), 0);
  };

  FileListVirtualizer.prototype.getMaxScrollTop = function() {
    return this.getMaxScrollTopForHeight(this.getScrollHeight());
  };

  // Pierre CodeView.shouldRebaseScroll — only above SCROLL_REBASE_THRESHOLD.
  FileListVirtualizer.prototype.shouldRebaseScroll = function() {
    return this.getMaxScrollTop() > SCROLL_REBASE_THRESHOLD;
  };

  FileListVirtualizer.prototype.getPagedScrollHeight = function() {
    return this.shouldRebaseScroll()
      ? Math.min(this.getScrollHeight(), SCROLL_REBASE_CONTAINER_HEIGHT)
      : this.getScrollHeight();
  };

  FileListVirtualizer.prototype.getMaxPagedScrollTop = function() {
    return this.getMaxScrollTopForHeight(this.getPagedScrollHeight());
  };

  FileListVirtualizer.prototype.clampPagedScrollTop = function(value) {
    var maxScroll = this.getMaxPagedScrollTop();
    return Math.max(0, Math.min(value, maxScroll));
  };

  FileListVirtualizer.prototype.getMaxScrollPageOffset = function() {
    return Math.max(this.getMaxScrollTop() - this.getMaxPagedScrollTop(), 0);
  };

  FileListVirtualizer.prototype.clampScrollPageOffset = function(value) {
    var maxOffset = this.getMaxScrollPageOffset();
    return Math.max(0, Math.min(value, maxOffset));
  };

  FileListVirtualizer.prototype.resolveScrollPageWindow = function(scrollTop, preferredPagedScrollTop) {
    var pagedScrollTop = roundToDevicePixel(this.clampPagedScrollTop(preferredPagedScrollTop));
    var scrollPageOffset = this.clampScrollPageOffset(scrollTop - pagedScrollTop);
    pagedScrollTop = roundToDevicePixel(this.clampPagedScrollTop(scrollTop - scrollPageOffset));
    scrollPageOffset = this.clampScrollPageOffset(scrollTop - pagedScrollTop);
    return { pagedScrollTop: pagedScrollTop, scrollPageOffset: scrollPageOffset };
  };

  FileListVirtualizer.prototype.resolvePagedScrollPosition = function(logicalScrollTop) {
    if (!this.shouldRebaseScroll()) {
      return {
        pagedScrollTop: this.clampPagedScrollTop(logicalScrollTop),
        scrollPageOffset: 0,
      };
    }
    var currentPageOffset = this.clampScrollPageOffset(this.scrollPageOffset || 0);
    var pagedScrollTop = logicalScrollTop - currentPageOffset;
    var pagedMaxScrollTop = this.getMaxPagedScrollTop();
    var maxRebaseOffset = this.getMaxScrollPageOffset();
    var shouldMoveDown = pagedScrollTop > SCROLL_REBASE_THRESHOLD && currentPageOffset < maxRebaseOffset;
    var shouldMoveUp = pagedScrollTop < SCROLL_REBASE_TRIGGER_TOP && currentPageOffset > 0;
    if (pagedScrollTop < 0 || pagedScrollTop > pagedMaxScrollTop || shouldMoveDown || shouldMoveUp) {
      return this.resolveScrollPageWindow(
        logicalScrollTop,
        shouldMoveUp ? Math.min(SCROLL_REBASE_TARGET_BOTTOM, pagedMaxScrollTop) : SCROLL_REBASE_TARGET_TOP
      );
    }
    return {
      pagedScrollTop: roundToDevicePixel(this.clampPagedScrollTop(pagedScrollTop)),
      scrollPageOffset: currentPageOffset,
    };
  };

  FileListVirtualizer.prototype.needsScrollPageUpdate = function(logicalScrollTop) {
    var rounded = roundToDevicePixel(Math.max(0, Math.min(logicalScrollTop, this.getMaxScrollTop())));
    var resolved = this.resolvePagedScrollPosition(rounded);
    return resolved.scrollPageOffset !== (this.scrollPageOffset || 0);
  };

  FileListVirtualizer.prototype.getPagedLayoutTop = function(logicalTop) {
    if (!this.shouldRebaseScroll()) return logicalTop;
    return Math.max(logicalTop - (this.scrollPageOffset || 0), 0);
  };

  FileListVirtualizer.prototype.update = function() {
    if (this.disposed) return;
    // Keep pin padding while stuck to a sidebar jump target — clearing it on
    // lock expiry was shoving deep files (e.g. app.js) out of view.
    if (!this._stickKey && !this.isScrollLocked() && this._pinPaddingPx) {
      this.clearPinScrollRoom();
    }
    if (!this.shouldRebaseScroll() && this.scrollPageOffset) {
      this.scrollPageOffset = 0;
    }
    var interval = this.viewportInterval();
    var intervals = interval ? [interval] : [];
    this.pinnedKeys.forEach(function(key) {
      var index = this.keyToIndex.get(key);
      if (index !== undefined) intervals.push([index, index]);
    }, this);
    this.reconcile(diffV.mergeIntervals(intervals, this.items.length));
    // Pierre: after every layout pass while a scroll target is pending, re-apply
    // scrollFix so height refine cannot leave the clicked file mid-viewport.
    if (this._stickKey) {
      this.pinKeyToViewportTop(this._stickKey);
      if (this.isPendingTargetSettled()) {
        // Drop pending target only (Pierre clears pendingScrollTarget). Keep pin
        // padding / lock until user scroll — clearing padding early re-clamps
        // max-scroll and parks deep files mid-viewport.
        this.releasePendingScrollTarget();
      }
    }
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

    // Pierre paged scaffold: when rebasing, DOM height is capped — leading
    // spacer is layout-top within the page; trailing fills paged height.
    if (this.shouldRebaseScroll() && desired.length > 0) {
      var firstLogical = null;
      for (var fi = 0; fi < desired.length; fi++) {
        if (!desired[fi].spacer && desired[fi].index !== undefined) {
          firstLogical = this.heightIndex.offset(desired[fi].index);
          break;
        }
      }
      if (firstLogical != null) {
        var lead = this.getPagedLayoutTop(firstLogical);
        if (desired[0].spacer) {
          desired[0].height = lead;
        } else if (lead > 0) {
          desired.unshift({
            key: 'spacer:page-lead',
            spacer: true,
            height: lead,
          });
        }
      }
      // Drop the full-logical trailing spacer (if any); replace with page trail.
      var lastDes = desired[desired.length - 1];
      if (lastDes && lastDes.spacer && lastDes.key !== 'spacer:page-lead') {
        desired.pop();
      }
      var sum = 0;
      for (var si = 0; si < desired.length; si++) {
        if (desired[si].spacer) sum += Math.max(0, desired[si].height || 0);
        else if (desired[si].index !== undefined) sum += this.heightIndex.height(desired[si].index);
      }
      var trail = Math.max(0, this.getPagedScrollHeight() - sum);
      if (trail > 0) {
        desired.push({ key: 'spacer:page-trail', spacer: true, height: trail });
      }
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
    // Pierre-style: pin a real on-screen node, not a reconstructed heightIndex
    // offset. Index math drifts when spacer estimates disagree with DOM.
    var domAnchor = this.captureDomAnchor();
    if (this.onBeforeHeightChange) this.onBeforeHeightChange(domAnchor);
    var first = this.heightIndex.updateMany(this.pendingMeasurements);
    this.pendingMeasurements.clear();
    if (first < 0) return;
    // Reconcile spacers synchronously, then restore — scheduling update for
    // next frame lets spacer heights change after restore and shove the view.
    this.update();
    this.restoreAfterHeightChange(domAnchor);
  };

  // Prefer a mounted file section's getBoundingClientRect over heightIndex
  // absolute offsets (Pierre Virtualizer.getScrollAnchor / scrollFix).
  // When scrolled into a file (header above viewport), prefer a line/row anchor
  // — Pierre CodeView getScrollAnchor → getNumericScrollAnchor.
  FileListVirtualizer.prototype.captureDomAnchor = function() {
    var scrollParent = this.scrollParent || window;
    var vpTop;
    var vpBottom;
    if (!scrollParent || scrollParent === window) {
      vpTop = 0;
      vpBottom = window.innerHeight || 0;
    } else {
      var parentRect = scrollParent.getBoundingClientRect();
      vpTop = parentRect.top;
      vpBottom = parentRect.bottom;
    }
    var best = null;
    var bestFallback = null;
    this.nodes.forEach(function(node, key) {
      if (!node || typeof node.getBoundingClientRect !== 'function') return;
      if (node.classList && (
        node.classList.contains('file-list-virtual-spacer') ||
        node.classList.contains('diff-virtual-spacer')
      )) return;
      var rect = node.getBoundingClientRect();
      if (rect.bottom <= vpTop || rect.top >= vpBottom) return;
      var candidate = { key: key, top: rect.top, scrollParent: scrollParent, node: node };
      if (!bestFallback || rect.top < bestFallback.top) bestFallback = candidate;
      if (rect.top < vpTop) return;
      if (!best || rect.top < best.top) best = candidate;
    });
    var picked = best || bestFallback;
    if (!picked) return null;

    // File header still in view → item-level anchor (Pierre type: "item").
    if (picked.top >= vpTop - 0.5) {
      return {
        type: 'item',
        key: picked.key,
        top: picked.top,
        scrollParent: picked.scrollParent,
      };
    }

    // Scrolled into the file → line/row anchor (Pierre type: "line").
    var lineAnchor = this.captureLineAnchorInSection(picked.node, vpTop);
    if (lineAnchor) {
      return {
        type: 'line',
        key: picked.key,
        top: picked.top,
        rowKey: lineAnchor.rowKey,
        rowTop: lineAnchor.rowTop,
        scrollParent: picked.scrollParent,
      };
    }
    return {
      type: 'item',
      key: picked.key,
      top: picked.top,
      scrollParent: picked.scrollParent,
    };
  };

  FileListVirtualizer.prototype.captureLineAnchorInSection = function(section, vpTop) {
    if (!section) return null;
    var surface = section.querySelector
      ? section.querySelector('.diff-virtual-surface')
      : null;
    var vw = surface && surface._critVirtualWindow;
    if (vw && vw.nodes) {
      var bestRow = null;
      var bestTop = Infinity;
      vw.nodes.forEach(function(rowNode, rowKey) {
        if (!rowNode || typeof rowNode.getBoundingClientRect !== 'function') return;
        if (rowNode.classList && rowNode.classList.contains('diff-virtual-spacer')) return;
        var top = rowNode.getBoundingClientRect().top;
        if (top < vpTop) return;
        if (top < bestTop) {
          bestTop = top;
          bestRow = { rowKey: rowKey, rowTop: top };
        }
      });
      if (bestRow) return bestRow;
    }
    // Fallback: first [data-virtual-row-key] at or below viewport top.
    if (!section.querySelectorAll) return null;
    var rows = section.querySelectorAll('[data-virtual-row-key]');
    var fallback = null;
    for (var i = 0; i < rows.length; i++) {
      var r = rows[i];
      if (r.classList && r.classList.contains('diff-virtual-spacer')) continue;
      var rt = r.getBoundingClientRect().top;
      if (rt < vpTop) continue;
      if (!fallback || rt < fallback.rowTop) {
        fallback = { rowKey: r.dataset.virtualRowKey, rowTop: rt };
      }
    }
    return fallback;
  };

  FileListVirtualizer.prototype.restoreDomAnchor = function(anchor) {
    if (!anchor || !anchor.key) return false;
    var scrollParent = anchor.scrollParent || this.scrollParent || window;

    if (anchor.type === 'line' && anchor.rowKey) {
      var section = this.nodes.get(anchor.key);
      var rowNode = null;
      if (section) {
        var surface = section.querySelector
          ? section.querySelector('.diff-virtual-surface')
          : null;
        var vw = surface && surface._critVirtualWindow;
        if (vw && vw.nodes) rowNode = vw.nodes.get(anchor.rowKey) || null;
        if (!rowNode && section.querySelector) {
          var nodes = section.querySelectorAll('[data-virtual-row-key]');
          for (var i = 0; i < nodes.length; i++) {
            if (nodes[i].dataset.virtualRowKey === anchor.rowKey) {
              rowNode = nodes[i];
              break;
            }
          }
        }
      }
      if (rowNode && typeof rowNode.getBoundingClientRect === 'function' &&
          anchor.rowTop != null) {
        var rowDelta = rowNode.getBoundingClientRect().top - anchor.rowTop;
        if (Math.abs(rowDelta) >= 1) scrollParentScrollBy(scrollParent, rowDelta);
        return true;
      }
    }

    var node = this.nodes.get(anchor.key);
    if (!node || typeof node.getBoundingClientRect !== 'function') return false;
    var delta = node.getBoundingClientRect().top - anchor.top;
    if (!(Math.abs(delta) >= 1)) return true;
    scrollParentScrollBy(scrollParent, delta);
    return true;
  };

  FileListVirtualizer.prototype.lockScrollToKey = function(key, ms) {
    this._scrollLockKey = key || null;
    this._scrollLockUntil = key ? (Date.now() + (ms || 2000)) : 0;
    if (key) this.ensurePinScrollRoom(key);
  };

  FileListVirtualizer.prototype.isScrollLocked = function() {
    return !!(this._scrollLockKey && Date.now() < (this._scrollLockUntil || 0));
  };

  FileListVirtualizer.prototype.scrollLockKey = function() {
    if (this._stickKey) return this._stickKey;
    return this.isScrollLocked() ? this._scrollLockKey : null;
  };

  // Keep the clicked file stuck under the header across height refine (neighbor
  // mounts, row-virtualizer measure, prefetch estimate updates) — same role as
  // Pierre CodeView.pendingScrollTarget. Cleared when device-pixel settled
  // (Pierre isPendingTargetSettled) or via wheel/touch/page keys.
  FileListVirtualizer.prototype.stickToKey = function(key) {
    if (!key) {
      this.clearStickToKey();
      return;
    }
    if (this._stickKey && this._stickKey !== key) {
      this.unpin(this._stickKey);
    }
    this._stickKey = key;
    this._stickTargetTop = null;
    this._stickSince = Date.now();
    this._forceFitPerfectly = true;
    this.pin(key);
    this.lockScrollToKey(key, 60000);
    this.ensurePinScrollRoom(key);
  };

  FileListVirtualizer.prototype.clearStickToKey = function() {
    var prev = this._stickKey;
    this._stickKey = null;
    this._stickTargetTop = null;
    this._scrollLockKey = null;
    this._scrollLockUntil = 0;
    this.clearPinScrollRoom();
    if (prev) this.unpin(prev);
  };

  // Pierre: pendingScrollTarget = undefined after settle — stop forcing scrollFix.
  FileListVirtualizer.prototype.releasePendingScrollTarget = function() {
    var prev = this._stickKey;
    this._stickKey = null;
    this._stickTargetTop = null;
    if (prev) this.unpin(prev);
  };

  FileListVirtualizer.prototype.stickKey = function() {
    return this._stickKey || null;
  };

  FileListVirtualizer.prototype.stickTargetTop = function(node) {
    var headerH = 49;
    try {
      if (typeof getComputedStyle === 'function' && typeof document !== 'undefined') {
        var raw = getComputedStyle(document.documentElement).getPropertyValue('--header-height');
        var parsed = parseFloat(raw);
        if (!isNaN(parsed) && parsed > 0) headerH = parsed;
      }
    } catch (e) { /* ignore */ }
    var targetTop = headerH + 8;
    try {
      if (node) {
        var margin = parseFloat(getComputedStyle(node).scrollMarginTop);
        if (!isNaN(margin) && margin > 0 && margin < 200) targetTop = margin;
      }
    } catch (e2) { /* ignore */ }
    return targetTop;
  };

  // Pierre CodeView.isPendingTargetSettled — device-pixel equality.
  FileListVirtualizer.prototype.isPendingTargetSettled = function() {
    if (!this._stickKey) return true;
    var node = this.nodes.get(this._stickKey);
    if (!node || typeof node.getBoundingClientRect !== 'function') return false;
    var targetTop = this.stickTargetTop(node);
    this._stickTargetTop = targetTop;
    var top = node.getBoundingClientRect().top;
    return roundToDevicePixel(top) === roundToDevicePixel(targetTop);
  };

  // Row-virtualizer height refine inside a mounted file — remeasure the section
  // and restore via stick / DOM anchor (Pierre's single applyScrollFix pipeline).
  FileListVirtualizer.prototype.noteChildHeightChange = function(surfaceOrSection) {
    if (this.disposed) return;
    var section = surfaceOrSection;
    if (section && section.classList && !section.classList.contains('file-section')) {
      section = section.closest ? section.closest('.file-section') : null;
    }
    if (!section) return;
    var key = section.dataset && (section.dataset.filePath || section.dataset.fileKey || section.dataset.virtualKey);
    if (!key) {
      // id="file-section-<path>"
      var id = section.id || '';
      if (id.indexOf('file-section-') === 0) key = id.slice('file-section-'.length);
    }
    if (!key || !this.keyToIndex.has(key)) return;
    if (this.nodes.get(key) !== section) this.adoptNode(key, section);
    var height = section.getBoundingClientRect().height;
    this.setItemHeight(key, height);
  };

  // Guarantee enough document height below `key` that it can sit under the
  // sticky app header. Without this, accurate (short) estimates for trailing
  // files leave max-scroll with the target parked mid-viewport — common after
  // background prefetch replaces oversized lazy stubs with real heights.
  FileListVirtualizer.prototype.ensurePinScrollRoom = function(key) {
    if (!this.surface) return;
    var viewportHeight = 0;
    if (this._fixedViewportHeight != null) {
      viewportHeight = this._fixedViewportHeight;
    } else {
      var scrollParent = this.scrollParent || window;
      if (!scrollParent || scrollParent === window) {
        viewportHeight = window.innerHeight || 0;
      } else {
        viewportHeight = scrollParent.getBoundingClientRect().height;
      }
    }
    var pad = Math.max(0, Math.round(viewportHeight));
    if (this._pinPaddingPx === pad) return;
    this._pinPaddingPx = pad;
    this.surface.style.paddingBottom = pad ? pad + 'px' : '';
  };

  FileListVirtualizer.prototype.clearPinScrollRoom = function() {
    this._pinPaddingPx = 0;
    if (this.surface) this.surface.style.paddingBottom = '';
  };

  // Pin a mounted file so its top sits just under the sticky app header
  // (matches .file-section scroll-margin-top). Prefer this over scrollIntoView
  // during height refine — absolute scrollTop adjustments fight less with
  // spacer reflow.
  FileListVirtualizer.prototype.pinKeyToViewportTop = function(key) {
    var node = this.nodes.get(key);
    if (!node || typeof node.getBoundingClientRect !== 'function') return false;
    this.ensurePinScrollRoom(key);
    var targetTop = this.stickTargetTop(node);
    var top = node.getBoundingClientRect().top;
    var delta = top - targetTop;
    if (!(Math.abs(delta) >= 0.5)) return true;
    scrollParentScrollBy(this.scrollParent || window, delta);
    return true;
  };

  FileListVirtualizer.prototype.restoreAfterHeightChange = function(domAnchor) {
    // Only force file-top pin while a pending scroll target is active (Pierre
    // pendingScrollTarget). A leftover scroll-lock must NOT re-pin — that
    // fought the user when they tried to scroll away after a sidebar jump.
    if (this._stickKey) {
      if (this.pinKeyToViewportTop(this._stickKey)) return true;
    }
    return this.restoreDomAnchor(domAnchor);
  };

  // heightIndex anchor — used for indexAtScroll / tests. Measurement paths use
  // captureDomAnchor instead.
  FileListVirtualizer.prototype.captureAnchor = function() {
    if (!this.surface) {
      var top = scrollParentScrollTop(this.scrollParent || window);
      if (this.shouldRebaseScroll()) top += (this.scrollPageOffset || 0);
      return getScrollAnchor(this.heightIndex, this.items, top);
    }
    var scrollParent = this.scrollParent || window;
    var metrics = diffV.viewportMetrics(scrollParent, this.surface);
    var localTop = metrics.localTop;
    if (this.shouldRebaseScroll()) localTop += (this.scrollPageOffset || 0);
    return getScrollAnchor(this.heightIndex, this.items, localTop);
  };

  FileListVirtualizer.prototype.restoreAnchor = function(anchor) {
    if (!anchor) return false;
    // If this looks like a DOM anchor, use the DOM path.
    if (anchor.key != null && anchor.top != null && anchor.index == null) {
      return this.restoreDomAnchor(anchor);
    }
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
    var domAnchor = this.captureDomAnchor();
    var changes = new Map();
    changes.set(index, height);
    var first = this.heightIndex.updateMany(changes);
    if (first < 0) return false;
    this.update();
    this.restoreAfterHeightChange(domAnchor);
    return true;
  };

  // Re-bind a key to a live DOM node after an in-place remount. Used when
  // loadLazyFile must rebuild a section outside the virtualizer's renderItem.
  FileListVirtualizer.prototype.adoptNode = function(key, node) {
    if (!key || !node) return false;
    var prev = this.nodes.get(key);
    if (prev && prev !== node && this.resizeObserver) {
      try { this.resizeObserver.unobserve(prev); } catch (e) { /* ignore */ }
    }
    this.nodes.set(key, node);
    this.mountedKeys.add(key);
    var index = this.keyToIndex.get(key);
    if (index !== undefined) {
      node.dataset.fileListIndex = String(index);
      node.dataset.virtualKey = key;
      if (this.resizeObserver) this.resizeObserver.observe(node);
    }
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
    this.pin(key);
    this.update();
    return Promise.resolve(this.nodes.get(key) || null);
  };

  FileListVirtualizer.prototype.scrollToItem = function(key, alignment) {
    var index = this.keyToIndex.get(key);
    if (index === undefined) return Promise.resolve(null);
    var self = this;
    this._forceFitPerfectly = true;
    this.lockScrollToKey(key, 2000);
    this.pin(key);
    // Synchronous mount + scroll before the next rAF reconcile can fight us.
    this.update();
    var node = this.nodes.get(key) || null;
    if (node && alignment !== 'center' && typeof this.pinKeyToViewportTop === 'function') {
      this.pinKeyToViewportTop(key);
    } else if (node && typeof node.scrollIntoView === 'function') {
      node.scrollIntoView({
        block: alignment === 'center' ? 'center' : 'start',
        behavior: 'instant',
      });
    } else {
      var scrollParent = self.scrollParent || window;
      var itemTop = self.heightIndex.offset(index);
      if (self.surface && (scrollParent === window || !scrollParent)) {
        var rect = self.surface.getBoundingClientRect();
        var surfaceTop = rect.top + (window.pageYOffset || document.documentElement.scrollTop || 0);
        applyScrollFix(window, surfaceTop + Math.max(0, itemTop));
      } else {
        applyScrollFix(scrollParent, Math.max(0, itemTop));
      }
    }
    return Promise.resolve(node);
  };

  FileListVirtualizer.prototype.indexAtScroll = function() {
    var anchor = this.captureAnchor();
    return anchor ? anchor.index : -1;
  };

  FileListVirtualizer.prototype.totalHeight = function() {
    return this.shouldRebaseScroll() ? this.getPagedScrollHeight() : this.heightIndex.total();
  };

  var api = {
    FILE_HEADER_ESTIMATE: FILE_HEADER_ESTIMATE,
    fileHeaderEstimate: fileHeaderEstimate,
    estimateFileSectionHeight: estimateFileSectionHeight,
    getScrollAnchor: getScrollAnchor,
    resolveAnchoredScrollTop: resolveAnchoredScrollTop,
    applyScrollFix: applyScrollFix,
    roundToDevicePixel: roundToDevicePixel,
    SCROLL_REBASE_THRESHOLD: SCROLL_REBASE_THRESHOLD,
    SCROLL_REBASE_CONTAINER_HEIGHT: SCROLL_REBASE_CONTAINER_HEIGHT,
    FileListVirtualizer: FileListVirtualizer,
  };

  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.fileListVirtualizer = api;
  }
  if (typeof module === 'object' && module.exports) module.exports = api;
})();
