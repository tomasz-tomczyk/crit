(function () {
  'use strict';

  var DEFAULT_ESTIMATES = {
    line: 20,
    header: 34,
    gap: 22,
    'gap-double': 42,
    comment: 128,
    form: 190,
    outdated: 128,
  };

  function hunkKey(hunk) {
    return String(hunk.OldStart || 0) + ':' + String(hunk.NewStart || 0) + ':' +
      String(hunk.OldCount || 0) + ':' + String(hunk.NewCount || 0);
  }

  function commentAnchorKey(comment) {
    return String(comment.end_line || 0) + ':' + (comment.side || '');
  }

  // Flatten the unified diff into stable logical rows without creating DOM.
  // Crit-specific rendering stays in app.js; this model is also the source of
  // truth for offscreen line/comment navigation.
  function buildUnifiedRows(options) {
    var hunks = options.hunks || [];
    var commentsMap = options.commentsMap || {};
    var forms = options.forms || [];
    var hideResolved = !!options.hideResolved;
    var rows = [];
    var renderedCommentAnchors = new Set();
    var visualIdx = 0;

    if (hunks.length === 0) return rows;

    var first = hunks[0];
    var firstNewGap = first.NewCount > 0 ? first.NewStart - 1 : Infinity;
    var firstOldGap = first.OldCount > 0 ? first.OldStart - 1 : Infinity;
    var leadingGap = Math.min(firstNewGap, firstOldGap);
    var hasLeadingGap = leadingGap > 0 && leadingGap !== Infinity;
    if (hasLeadingGap) {
      rows.push({
        key: 'gap:leading:' + hunkKey(first),
        kind: 'gap',
        gapKind: 'leading',
        hunk: first,
        hunkIndex: 0,
        gap: leadingGap,
      });
    }

    for (var hi = 0; hi < hunks.length; hi++) {
      var hunk = hunks[hi];
      var hk = hunkKey(hunk);
      var spacerRendered = false;
      if (hi > 0) {
        var previous = hunks[hi - 1];
        var gap = hunk.NewStart - (previous.NewStart + previous.NewCount);
        if (gap > 0) {
          rows.push({
            key: 'gap:' + hunkKey(previous) + ':' + hk,
            kind: gap > 20 ? 'gap-double' : 'gap',
            gapKind: 'between',
            previousHunk: previous,
            hunk: hunk,
            previousHunkIndex: hi - 1,
            hunkIndex: hi,
            gap: gap,
          });
          spacerRendered = true;
        }
      }

      var contiguous = hi > 0 &&
        (hunks[hi - 1].NewStart + hunks[hi - 1].NewCount) >= hunk.NewStart;
      if (!spacerRendered && !(hi === 0 && hasLeadingGap) && !contiguous) {
        rows.push({ key: 'header:' + hk, kind: 'header', hunk: hunk, hunkIndex: hi });
      }

      var lines = hunk.Lines || [];
      for (var li = 0; li < lines.length; li++) {
        var line = lines[li];
        var lineNum = line.Type === 'del' ? line.OldNum : line.NewNum;
        var side = line.Type === 'del' ? 'old' : '';
        var anchor = String(lineNum || 0) + ':' + side;
        var row = {
          key: 'line:u:' + hk + ':' + line.Type + ':' +
            String(line.OldNum || 0) + ':' + String(line.NewNum || 0) + ':' + li,
          kind: 'line',
          hunk: hunk,
          hunkIndex: hi,
          line: line,
          lineIndex: li,
          lineNum: lineNum,
          side: side,
          visualIdx: visualIdx,
        };
        rows.push(row);

        if (lineNum) {
          renderedCommentAnchors.add(anchor);
          var lineComments = commentsMap[anchor] || [];
          for (var ci = 0; ci < lineComments.length; ci++) {
            var comment = lineComments[ci];
            if (comment.scope === 'file' || (hideResolved && comment.resolved)) continue;
            rows.push({
              key: 'comment:' + comment.id,
              kind: 'comment',
              comment: comment,
              lineKey: row.key,
              lineNum: lineNum,
              side: side,
            });
          }

          for (var fi = 0; fi < forms.length; fi++) {
            var form = forms[fi];
            if (form.editingId) continue;
            if (form.endLine === lineNum && (form.side || '') === side) {
              rows.push({
                key: 'form:' + form.formKey,
                kind: 'form',
                form: form,
                lineKey: row.key,
                lineNum: lineNum,
                side: side,
              });
            }
          }
        }
        visualIdx++;
      }
    }

    var last = hunks[hunks.length - 1];
    var totalLines = options.totalLines || 0;
    var trailingGap = totalLines - (last.NewStart + last.NewCount) + 1;
    if (trailingGap > 0) {
      rows.push({
        key: 'gap:trailing:' + hunkKey(last),
        kind: 'gap',
        gapKind: 'trailing',
        hunk: last,
        hunkIndex: hunks.length - 1,
        gap: trailingGap,
      });
    }

    Object.keys(commentsMap).forEach(function(anchor) {
      if (renderedCommentAnchors.has(anchor)) return;
      var comments = commentsMap[anchor] || [];
      for (var i = 0; i < comments.length; i++) {
        var comment = comments[i];
        if (comment.scope === 'file' || (hideResolved && comment.resolved)) continue;
        rows.push({
          key: 'outdated:' + comment.id,
          kind: 'outdated',
          comment: comment,
          lineNum: comment.end_line,
          side: comment.side || '',
        });
      }
    });

    return rows;
  }

  function estimateRowHeight(row) {
    return DEFAULT_ESTIMATES[row.kind] || DEFAULT_ESTIMATES.line;
  }

  function HeightIndex(rows, estimate) {
    this.rows = rows || [];
    this.estimate = estimate || estimateRowHeight;
    this.heights = new Array(this.rows.length);
    this.offsets = new Array(this.rows.length + 1);
    this.reset();
  }

  HeightIndex.prototype.reset = function() {
    this.offsets[0] = 0;
    for (var i = 0; i < this.rows.length; i++) {
      this.heights[i] = this.estimate(this.rows[i]);
      this.offsets[i + 1] = this.offsets[i] + this.heights[i];
    }
  };

  HeightIndex.prototype.total = function() {
    return this.offsets[this.rows.length] || 0;
  };

  HeightIndex.prototype.offset = function(index) {
    if (index <= 0) return 0;
    if (index >= this.offsets.length) return this.total();
    return this.offsets[index];
  };

  HeightIndex.prototype.height = function(index) {
    return this.heights[index] || 0;
  };

  HeightIndex.prototype.indexAt = function(offset) {
    if (this.rows.length === 0) return -1;
    if (offset <= 0) return 0;
    if (offset >= this.total()) return this.rows.length - 1;
    var lo = 0;
    var hi = this.rows.length;
    while (lo < hi) {
      var mid = (lo + hi) >> 1;
      if (this.offsets[mid + 1] <= offset) lo = mid + 1;
      else hi = mid;
    }
    return lo;
  };

  HeightIndex.prototype.updateMany = function(changes) {
    var first = this.rows.length;
    var changed = false;
    changes.forEach(function(height, index) {
      if (index < 0 || index >= this.heights.length || !(height >= 0)) return;
      if (Math.abs(this.heights[index] - height) < 0.5) return;
      this.heights[index] = height;
      first = Math.min(first, index);
      changed = true;
    }, this);
    if (!changed) return -1;
    for (var i = first; i < this.rows.length; i++) {
      this.offsets[i + 1] = this.offsets[i] + this.heights[i];
    }
    return first;
  };

  function overscanForViewport(viewportHeight) {
    return Math.max(800, Math.min(2400, viewportHeight * 1.5));
  }

  function mergeIntervals(intervals, rowCount) {
    var normalized = intervals.filter(function(interval) {
      return interval && interval[1] >= 0 && interval[0] < rowCount;
    }).map(function(interval) {
      return [Math.max(0, interval[0]), Math.min(rowCount - 1, interval[1])];
    }).sort(function(a, b) { return a[0] - b[0]; });
    var merged = [];
    for (var i = 0; i < normalized.length; i++) {
      var current = normalized[i];
      var previous = merged[merged.length - 1];
      if (previous && current[0] <= previous[1] + 1) {
        previous[1] = Math.max(previous[1], current[1]);
      } else {
        merged.push(current.slice());
      }
    }
    return merged;
  }

  function calculateWindow(heightIndex, localTop, viewportHeight) {
    if (!heightIndex || heightIndex.rows.length === 0) return null;
    var overscan = overscanForViewport(viewportHeight);
    var top = Math.max(0, localTop);
    var bottom = Math.min(heightIndex.total(), top + viewportHeight);
    return [
      heightIndex.indexAt(Math.max(0, top - overscan)),
      heightIndex.indexAt(Math.min(heightIndex.total(), bottom + overscan)),
    ];
  }

  function VirtualWindow(options) {
    this.surface = options.surface;
    this.rows = options.rows || [];
    this.renderRow = options.renderRow;
    this.estimateHeight = options.estimateHeight || estimateRowHeight;
    this.heightIndex = new HeightIndex(this.rows, this.estimateHeight);
    this.keyToIndex = new Map();
    this.nodes = new Map();
    this.pinnedKeys = new Set(options.pinnedKeys || []);
    this.selectionInterval = null;
    this.interactionIntervals = new Map();
    this.pendingMeasurements = new Map();
    this.measureFrame = 0;
    this.updateFrame = 0;
    this.disposed = false;
    this.started = false;
    this.widthBucket = 0;
    this.onRangeChange = options.onRangeChange || null;
    this._handleViewportChange = this.scheduleUpdate.bind(this);
    this._handleSelectionChange = this.handleSelectionChange.bind(this);
    this._handleCopy = this.handleCopy.bind(this);
    for (var i = 0; i < this.rows.length; i++) this.keyToIndex.set(this.rows[i].key, i);

    var self = this;
    this.resizeObserver = typeof ResizeObserver === 'function'
      ? new ResizeObserver(function(entries) { self.handleMeasurements(entries); })
      : null;

    this.surface.classList.add('diff-virtual-surface');
    this.surface._critVirtualWindow = this;
    // Give the disconnected surface a useful intrinsic height immediately.
    this.reconcile([[0, Math.min(this.rows.length - 1, 99)]]);
  }

  VirtualWindow.prototype.start = function() {
    if (this.started || this.disposed) return;
    this.started = true;
    window.addEventListener('scroll', this._handleViewportChange, { passive: true });
    window.addEventListener('resize', this._handleViewportChange, { passive: true });
    document.addEventListener('selectionchange', this._handleSelectionChange);
    document.addEventListener('copy', this._handleCopy);
    this.update();
  };

  VirtualWindow.prototype.dispose = function() {
    if (this.disposed) return;
    this.disposed = true;
    window.removeEventListener('scroll', this._handleViewportChange);
    window.removeEventListener('resize', this._handleViewportChange);
    document.removeEventListener('selectionchange', this._handleSelectionChange);
    document.removeEventListener('copy', this._handleCopy);
    if (this.updateFrame) cancelAnimationFrame(this.updateFrame);
    if (this.measureFrame) cancelAnimationFrame(this.measureFrame);
    if (this.resizeObserver) this.resizeObserver.disconnect();
    this.nodes.clear();
    if (this.surface._critVirtualWindow === this) delete this.surface._critVirtualWindow;
  };

  VirtualWindow.prototype.scheduleUpdate = function() {
    if (this.disposed || this.updateFrame) return;
    var self = this;
    this.updateFrame = requestAnimationFrame(function() {
      self.updateFrame = 0;
      self.update();
    });
  };

  VirtualWindow.prototype.viewportInterval = function() {
    if (this.rows.length === 0) return null;
    var rect = this.surface.getBoundingClientRect();
    var viewportHeight = window.innerHeight || document.documentElement.clientHeight || 0;
    var localTop = Math.max(0, -rect.top);
    return calculateWindow(this.heightIndex, localTop, viewportHeight);
  };

  VirtualWindow.prototype.update = function() {
    if (this.disposed) return;
    var widthBucket = Math.round((this.surface.clientWidth || 0) / 40);
    if (this.widthBucket && widthBucket && widthBucket !== this.widthBucket) {
      this.heightIndex.reset();
    }
    if (widthBucket) this.widthBucket = widthBucket;

    var interval = this.viewportInterval();
    var intervals = interval ? [interval] : [];
    this.pinnedKeys.forEach(function(key) {
      var index = this.keyToIndex.get(key);
      if (index !== undefined) intervals.push([index, index]);
    }, this);
    if (this.selectionInterval) intervals.push(this.selectionInterval);
    this.interactionIntervals.forEach(function(pinnedInterval) {
      intervals.push(pinnedInterval);
    });
    this.reconcile(mergeIntervals(intervals, this.rows.length));
  };

  VirtualWindow.prototype.reconcile = function(intervals) {
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
        desired.push({ key: this.rows[i].key, index: i, row: this.rows[i] });
      }
      cursor = interval[1] + 1;
    }
    if (cursor < this.rows.length) {
      desired.push({
        key: 'spacer:' + cursor + ':' + this.rows.length,
        spacer: true,
        height: this.heightIndex.total() - this.heightIndex.offset(cursor),
      });
    }

    var desiredKeys = new Set(desired.map(function(item) { return item.key; }));
    var children = Array.from(this.surface.children);
    for (var c = 0; c < children.length; c++) {
      var oldKey = children[c].dataset.virtualKey;
      if (desiredKeys.has(oldKey)) continue;
      if (this.resizeObserver && children[c].dataset.virtualRowIndex !== undefined) {
        this.resizeObserver.unobserve(children[c]);
      }
      if (oldKey) this.nodes.delete(oldKey);
      children[c].remove();
    }

    for (var di = 0; di < desired.length; di++) {
      var descriptor = desired[di];
      var node = this.nodes.get(descriptor.key);
      if (!node) {
        if (descriptor.spacer) {
          node = document.createElement('div');
          node.className = 'diff-virtual-spacer';
          node.setAttribute('aria-hidden', 'true');
        } else {
          node = this.renderRow(descriptor.row);
          if (!node || node.nodeType !== 1) throw new Error('Virtual diff rows must render one element');
          node.dataset.virtualRowIndex = String(descriptor.index);
          node.dataset.virtualRowKey = descriptor.row.key;
          if (this.resizeObserver) this.resizeObserver.observe(node);
        }
        node.dataset.virtualKey = descriptor.key;
        this.nodes.set(descriptor.key, node);
      }
      if (descriptor.spacer) node.style.height = Math.max(0, descriptor.height) + 'px';
      var current = this.surface.children[di];
      if (current !== node) this.surface.insertBefore(node, current || null);
    }
    while (this.surface.children.length > desired.length) {
      var extra = this.surface.lastElementChild;
      if (this.resizeObserver && extra.dataset.virtualRowIndex !== undefined) this.resizeObserver.unobserve(extra);
      this.nodes.delete(extra.dataset.virtualKey);
      extra.remove();
    }
    if (this.onRangeChange) this.onRangeChange(intervals);
  };

  VirtualWindow.prototype.handleMeasurements = function(entries) {
    for (var i = 0; i < entries.length; i++) {
      var index = parseInt(entries[i].target.dataset.virtualRowIndex, 10);
      if (isNaN(index)) continue;
      var height = entries[i].borderBoxSize && entries[i].borderBoxSize.length
        ? entries[i].borderBoxSize[0].blockSize
        : entries[i].target.getBoundingClientRect().height;
      this.pendingMeasurements.set(index, height);
    }
    if (this.measureFrame) return;
    var self = this;
    this.measureFrame = requestAnimationFrame(function() { self.flushMeasurements(); });
  };

  VirtualWindow.prototype.flushMeasurements = function() {
    this.measureFrame = 0;
    if (this.disposed || this.pendingMeasurements.size === 0) return;
    var rect = this.surface.getBoundingClientRect();
    var localTop = Math.max(0, -rect.top);
    var anchorIndex = this.heightIndex.indexAt(localTop);
    var deltaAbove = 0;
    this.pendingMeasurements.forEach(function(height, index) {
      if (index < anchorIndex) deltaAbove += height - this.heightIndex.height(index);
    }, this);
    this.heightIndex.updateMany(this.pendingMeasurements);
    this.pendingMeasurements.clear();
    if (Math.abs(deltaAbove) >= 0.5) window.scrollBy(0, deltaAbove);
    this.update();
  };

  VirtualWindow.prototype.pin = function(key) {
    if (!this.keyToIndex.has(key)) return false;
    this.pinnedKeys.add(key);
    this.update();
    return true;
  };

  VirtualWindow.prototype.unpin = function(key) {
    this.pinnedKeys.delete(key);
    this.scheduleUpdate();
  };

  VirtualWindow.prototype.resetEstimates = function() {
    var rect = this.surface.getBoundingClientRect();
    var viewportHeight = window.innerHeight || 0;
    var anchorY = Math.max(0, rect.top);
    var anchor = rect.bottom > 0 && rect.top < viewportHeight
      ? this.captureAnchor(anchorY)
      : null;
    this.heightIndex.reset();
    if (anchor) this.restoreAnchor(anchor, anchorY);
    this.update();
  };

  VirtualWindow.prototype.pinInterval = function(name, start, end) {
    if (this.rows.length === 0) return;
    this.interactionIntervals.set(name, [Math.min(start, end), Math.max(start, end)]);
    this.update();
  };

  VirtualWindow.prototype.pinVisualRange = function(name, startVisualIdx, endVisualIdx) {
    var first = -1;
    var last = -1;
    var low = Math.min(startVisualIdx, endVisualIdx);
    var high = Math.max(startVisualIdx, endVisualIdx);
    for (var i = 0; i < this.rows.length; i++) {
      var row = this.rows[i];
      if (row.kind !== 'line' || row.visualIdx < low || row.visualIdx > high) continue;
      if (first < 0) first = i;
      last = i;
    }
    if (first >= 0) this.pinInterval(name, first, last);
  };

  VirtualWindow.prototype.clearInterval = function(name) {
    if (!this.interactionIntervals.delete(name)) return;
    this.scheduleUpdate();
  };

  VirtualWindow.prototype.ensureMounted = function(key) {
    var self = this;
    if (!this.pin(key)) return Promise.resolve(null);
    return new Promise(function(resolve) {
      requestAnimationFrame(function() {
        resolve(self.nodes.get(key) || null);
      });
    });
  };

  VirtualWindow.prototype.scrollToRow = function(key, alignment) {
    var index = this.keyToIndex.get(key);
    if (index === undefined) return Promise.resolve(null);
    var rect = this.surface.getBoundingClientRect();
    var surfaceTop = window.scrollY + rect.top;
    var viewportHeight = window.innerHeight || 0;
    var rowHeight = this.heightIndex.height(index);
    var target = surfaceTop + this.heightIndex.offset(index);
    if (alignment === 'center') target -= Math.max(0, (viewportHeight - rowHeight) / 2);
    else if (alignment === 'end') target -= Math.max(0, viewportHeight - rowHeight);
    window.scrollTo({ top: target, left: window.scrollX, behavior: 'instant' });
    this.update();
    return this.ensureMounted(key);
  };

  VirtualWindow.prototype.captureAnchor = function(viewportY) {
    if (this.rows.length === 0) return null;
    var rect = this.surface.getBoundingClientRect();
    var local = Math.max(0, Math.min(
      this.heightIndex.total(),
      (viewportY === undefined ? 0 : viewportY) - rect.top
    ));
    var index = this.heightIndex.indexAt(local);
    if (index < 0) return null;
    return { rowKey: this.rows[index].key, intraRowOffset: local - this.heightIndex.offset(index) };
  };

  VirtualWindow.prototype.restoreAnchor = function(anchor, viewportY) {
    if (!anchor) return false;
    var index = this.keyToIndex.get(anchor.rowKey);
    if (index === undefined) return false;
    var rect = this.surface.getBoundingClientRect();
    var surfaceTop = window.scrollY + rect.top;
    var target = surfaceTop + this.heightIndex.offset(index) + (anchor.intraRowOffset || 0) - (viewportY || 0);
    window.scrollTo({ top: target, left: window.scrollX, behavior: 'instant' });
    this.update();
    return true;
  };

  VirtualWindow.prototype.rowKeyForLine = function(lineNum, side) {
    for (var i = 0; i < this.rows.length; i++) {
      var row = this.rows[i];
      if (row.kind === 'line' && row.lineNum === lineNum && (row.side || '') === (side || '')) return row.key;
    }
    return null;
  };

  VirtualWindow.prototype.rowKeyForComment = function(commentId) {
    var direct = 'comment:' + commentId;
    if (this.keyToIndex.has(direct)) return direct;
    var outdated = 'outdated:' + commentId;
    return this.keyToIndex.has(outdated) ? outdated : null;
  };

  VirtualWindow.prototype.handleSelectionChange = function() {
    var selection = document.getSelection && document.getSelection();
    if (!selection || selection.isCollapsed || !selection.anchorNode || !selection.focusNode) {
      this.selectionInterval = null;
      this.scheduleUpdate();
      return;
    }
    var anchorEl = selection.anchorNode.nodeType === 1 ? selection.anchorNode : selection.anchorNode.parentElement;
    var focusEl = selection.focusNode.nodeType === 1 ? selection.focusNode : selection.focusNode.parentElement;
    var anchorRow = anchorEl && anchorEl.closest ? anchorEl.closest('[data-virtual-row-index]') : null;
    var focusRow = focusEl && focusEl.closest ? focusEl.closest('[data-virtual-row-index]') : null;
    if (!anchorRow || !focusRow || !this.surface.contains(anchorRow) || !this.surface.contains(focusRow)) return;
    var a = parseInt(anchorRow.dataset.virtualRowIndex, 10);
    var b = parseInt(focusRow.dataset.virtualRowIndex, 10);
    this.selectionInterval = [Math.min(a, b), Math.max(a, b)];
    this.scheduleUpdate();
  };

  VirtualWindow.prototype.handleCopy = function() {
    var self = this;
    setTimeout(function() {
      self.selectionInterval = null;
      self.scheduleUpdate();
    }, 0);
  };

  var api = {
    DEFAULT_ESTIMATES: DEFAULT_ESTIMATES,
    buildUnifiedRows: buildUnifiedRows,
    estimateRowHeight: estimateRowHeight,
    HeightIndex: HeightIndex,
    overscanForViewport: overscanForViewport,
    mergeIntervals: mergeIntervals,
    calculateWindow: calculateWindow,
    VirtualWindow: VirtualWindow,
    commentAnchorKey: commentAnchorKey,
  };

  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.diffVirtualizer = api;
  }
  if (typeof module === 'object' && module.exports) module.exports = api;
})();
