(function () {
  'use strict';

  // Pure data mapping between Crit's review model and @pierre/diffs.
  // No DOM, no Pierre calls: app.js hands the results to window.PierreDiffs.
  //
  // Crit hunks (internal/diff DiffHunk, PascalCase JSON):
  //   { OldStart, OldCount, NewStart, NewCount, Header, Lines: [{ Type, Content, OldNum, NewNum }] }
  //   Type is "context" | "add" | "del".

  // Pierre's ChangeTypes for Crit's session file status.
  var CHANGE_TYPE = {
    added: 'new',
    untracked: 'new',
    deleted: 'deleted',
    removed: 'deleted',
    renamed: 'rename-changed',
    modified: 'change',
  };

  function linePrefix(type) {
    if (type === 'add') return '+';
    if (type === 'del') return '-';
    return ' ';
  }

  function hunkRange(start, count) {
    // git omits ",count" when it is 1; start is 0 for an empty side.
    return count === 1 ? String(start) : start + ',' + count;
  }

  // One file's unified diff in git format, from Crit hunks. Pierre parses
  // this with processFile(); oldFile/newFile make it a full (expandable) diff.
  function hunksToPatch(file) {
    var path = file.path;
    var oldPath = file.old_path || file.oldPath || path;
    var status = file.status;
    var isNew = status === 'added' || status === 'untracked';
    var isDeleted = status === 'deleted' || status === 'removed';
    var out = ['diff --git a/' + oldPath + ' b/' + path];
    if (isNew) out.push('new file mode 100644');
    if (isDeleted) out.push('deleted file mode 100644');
    if (oldPath !== path) {
      out.push('rename from ' + oldPath);
      out.push('rename to ' + path);
    }
    out.push('--- ' + (isNew ? '/dev/null' : 'a/' + oldPath));
    out.push('+++ ' + (isDeleted ? '/dev/null' : 'b/' + path));
    var hunks = file.diffHunks || [];
    for (var i = 0; i < hunks.length; i++) {
      var h = hunks[i];
      var header = '@@ -' + hunkRange(h.OldStart, h.OldCount) + ' +' + hunkRange(h.NewStart, h.NewCount) + ' @@';
      var context = (h.Header || '').replace(/^@@[^@]*@@\s?/, '');
      out.push(context ? header + ' ' + context : header);
      var lines = h.Lines || [];
      for (var j = 0; j < lines.length; j++) {
        out.push(linePrefix(lines[j].Type) + lines[j].Content);
      }
    }
    return out.join('\n') + '\n';
  }

  // Rebuild the old side of a file from its new contents and the hunks.
  // Lines outside hunks are identical on both sides, so only hunk ranges
  // differ. Returns null when the hunks do not line up with newContent (stale
  // content vs diff); callers then fall back to a partial (patch-only) diff.
  function reconstructOldContent(newContent, hunks) {
    if (typeof newContent !== 'string') return null;
    var trailingNewline = newContent.length === 0 || newContent.endsWith('\n');
    var newLines = newContent.length === 0 ? [] : newContent.replace(/\n$/, '').split('\n');
    var oldLines = [];
    var cursor = 1; // 1-based index into newLines
    for (var i = 0; i < (hunks || []).length; i++) {
      var h = hunks[i];
      var hunkNewStart = h.NewCount === 0 ? h.NewStart + 1 : h.NewStart;
      if (hunkNewStart < cursor || hunkNewStart - 1 > newLines.length) return null;
      for (; cursor < hunkNewStart; cursor++) oldLines.push(newLines[cursor - 1]);
      var lines = h.Lines || [];
      for (var j = 0; j < lines.length; j++) {
        var line = lines[j];
        if (line.Type === 'add' || line.Type === 'context') {
          if (newLines[cursor - 1] !== line.Content) return null;
          cursor++;
        }
        if (line.Type === 'del' || line.Type === 'context') oldLines.push(line.Content);
      }
    }
    for (; cursor <= newLines.length; cursor++) oldLines.push(newLines[cursor - 1]);
    if (oldLines.length === 0) return '';
    return oldLines.join('\n') + (trailingNewline ? '\n' : '');
  }

  // Extensions Pierre's filename detection leaves as plain text that Crit
  // highlighted before (see docs/frontend-js.md, "Language coverage").
  var LANGUAGE_OVERRIDES = {
    heex: 'html',
    leex: 'html',
    svg: 'xml',
    gradle: 'groovy',
  };

  function languageOverride(path) {
    var base = String(path || '').split('/').pop().toLowerCase();
    var dot = base.lastIndexOf('.');
    return dot > 0 ? LANGUAGE_OVERRIDES[base.slice(dot + 1)] || null : null;
  }

  // Pierre FileContents for a whole file (files-mode code view).
  function buildFileContents(P, file, cacheKey) {
    var contents = { name: file.path, contents: file.content || '', cacheKey: cacheKey };
    var lang = languageOverride(file.path);
    return lang ? P.setLanguageOverride(contents, lang) : contents;
  }

  // Does `all` hold a hunk that `shown` leaves out, before the last shown one?
  // A hunk counts as shown when a shown hunk covers its new-side range
  // (shown hunks may have been merged with neighbours).
  function omitsHunkBeforeShown(shown, all) {
    if (!all || !shown || shown.length === 0) return false;
    var covered = function(h) {
      return shown.some(function(s) {
        return h.NewStart >= s.NewStart && h.NewStart + h.NewCount <= s.NewStart + s.NewCount;
      });
    };
    var lastShown = Math.max.apply(null, shown.map(function(s) { return s.NewStart; }));
    return all.some(function(h) { return h.NewStart < lastShown && !covered(h); });
  }

  // Pierre FileDiffMetadata for a Crit file and the hunks to show. With both
  // sides reconstructed Pierre gets a full (expandable) diff; if the content
  // and hunks disagree (file changed after the diff was computed) it falls
  // back to the patch alone, where context expansion is unavailable.
  //
  // `allHunks` (optional) is every hunk of the file when `hunks` is a subset
  // (a story chapter). Pierre treats the text between shown hunks as
  // unchanged, so full contents only work when no omitted hunk sits before a
  // shown one; otherwise the old-side line numbers would not line up and the
  // diff falls back to the patch alone (correct numbers, no expansion).
  // P is window.PierreDiffs (processFile, setLanguageOverride).
  function buildFileDiff(P, file, hunks, cacheKey, allHunks) {
    var model = { path: file.path, old_path: file.oldPath || file.old_path, status: file.status, diffHunks: hunks };
    var patch = hunksToPatch(model);
    var newContent = file.content || '';
    var oldContent = omitsHunkBeforeShown(hunks, allHunks) ? null : reconstructOldContent(newContent, hunks);
    var diff = oldContent === null
      ? P.processFile(patch, { cacheKey: cacheKey })
      : P.processFile(patch, {
        cacheKey: cacheKey,
        oldFile: { name: model.old_path || file.path, contents: oldContent },
        newFile: { name: file.path, contents: newContent },
      });
    var lang = languageOverride(file.path);
    return lang ? P.setLanguageOverride(diff, lang) : diff;
  }

  // Estimated line count for a file whose hunks are not loaded yet (lazy).
  // numstat additions+deletions plus a hunk header; used to size stub items
  // so the scroll height is close before the real diff arrives.
  function estimatedLineCount(file) {
    var changed = (file.additions || 0) + (file.deletions || 0);
    return Math.max(1, changed) + 1;
  }

  // Crit comment → Pierre DiffLineAnnotation. Comments anchor on their end
  // line; old-side comments sit on the deletions side. File-level comments use
  // lineNumber 0 (Pierre renders those above the first hunk).
  function annotationForComment(comment) {
    if (comment.scope === 'file') {
      return { side: 'additions', lineNumber: 0, metadata: { kind: 'thread', id: comment.id } };
    }
    return {
      side: comment.side === 'old' ? 'deletions' : 'additions',
      lineNumber: comment.end_line,
      metadata: { kind: 'thread', id: comment.id },
    };
  }

  // Crit open comment form → annotation after its end line.
  function annotationForForm(form) {
    if (form.scope === 'file') {
      return { side: 'additions', lineNumber: 0, metadata: { kind: 'form', id: form.formKey } };
    }
    return {
      side: form.side === 'old' ? 'deletions' : 'additions',
      lineNumber: form.endLine,
      metadata: { kind: 'form', id: form.formKey },
    };
  }

  // Pierre SelectedLineRange → Crit form range. Pierre numbers lines per side;
  // a range that ends on the deletions side is an old-side comment.
  function formRangeFromSelection(range) {
    if (!range) return null;
    var start = Math.min(range.start, range.end);
    var end = Math.max(range.start, range.end);
    var side = (range.endSide || range.side) === 'deletions' ? 'old' : '';
    return { startLine: start, endLine: end, side: side };
  }

  // Keyboard navigation rows for one file, in visual order. Unified: every
  // line is a row. Split: a change block pairs deletions with additions row
  // by row (like the rendered split view), and a paired row focuses its
  // new-side line. Each row is the line j/k focuses and `c` comments on.
  function navRowsForHunks(hunks, style) {
    var rows = [];
    for (var i = 0; i < (hunks || []).length; i++) {
      var lines = hunks[i].Lines || [];
      var dels = [];
      var adds = [];
      var flush = function() {
        if (style === 'split') {
          var n = Math.max(dels.length, adds.length);
          for (var k = 0; k < n; k++) {
            if (adds[k]) rows.push({ line: adds[k].NewNum, side: '' });
            else rows.push({ line: dels[k].OldNum, side: 'old' });
          }
        } else {
          dels.forEach(function(d) { rows.push({ line: d.OldNum, side: 'old' }); });
          adds.forEach(function(a) { rows.push({ line: a.NewNum, side: '' }); });
        }
        dels = [];
        adds = [];
      };
      for (var j = 0; j < lines.length; j++) {
        var line = lines[j];
        if (line.Type === 'del') { if (adds.length) flush(); dels.push(line); continue; }
        if (line.Type === 'add') { adds.push(line); continue; }
        flush();
        rows.push({ line: line.NewNum, side: '' });
      }
      flush();
    }
    return rows;
  }

  // Unmodified Shiki defaults; Settings can choose another bundled theme.
  var THEME = { dark: 'tokyo-night', light: 'github-light-default' };

  // Rendering options every Crit Pierre surface shares (the review list's
  // CodeView and story chapter FileDiffs).
  function baseOptions(themeType, diffStyle) {
    return {
      theme: THEME,
      themeType: themeType,
      diffStyle: diffStyle,
      lineDiffType: 'word-alt',
      expansionLineCount: 20,
      enableGutterUtility: true,
      lineHoverHighlight: 'number',
    };
  }

  // Shared renderer settings for the review list, story diffs and previews.
  // Theme registration stays with the caller, which owns the Pierre instance.
  function displayOptions(getSetting) {
    var granularity = getSetting('inlineDiff', 'word-alt');
    var indicators = getSetting('changeIndicators', 'bars');
    return {
      overflow: getSetting('codeOverflow', 'scroll') === 'wrap' ? 'wrap' : 'scroll',
      hunkSeparators: 'line-info',
      lineDiffType: ['word-alt', 'word', 'char', 'none'].indexOf(granularity) >= 0 ? granularity : 'word-alt',
      diffIndicators: ['classic', 'bars', 'none'].indexOf(indicators) >= 0 ? indicators : 'bars',
      expandUnchanged: getSetting('unchangedContext', 'collapsed') === 'expanded',
      disableLineNumbers: getSetting('lineNumbers', 'on') === 'off',
    };
  }

  // Crit theme setting → Pierre themeType ('system' follows the OS).
  function themeTypeFor(setting) {
    return setting === 'light' || setting === 'dark' ? setting : 'system';
  }

  var api = {
    CHANGE_TYPE: CHANGE_TYPE,
    THEME: THEME,
    baseOptions: baseOptions,
    displayOptions: displayOptions,
    hunksToPatch: hunksToPatch,
    reconstructOldContent: reconstructOldContent,
    estimatedLineCount: estimatedLineCount,
    annotationForComment: annotationForComment,
    annotationForForm: annotationForForm,
    formRangeFromSelection: formRangeFromSelection,
    themeTypeFor: themeTypeFor,
    navRowsForHunks: navRowsForHunks,
    buildFileDiff: buildFileDiff,
    buildFileContents: buildFileContents,
    omitsHunkBeforeShown: omitsHunkBeforeShown,
    languageOverride: languageOverride,
  };

  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.pierreAdapter = api;
  }
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
