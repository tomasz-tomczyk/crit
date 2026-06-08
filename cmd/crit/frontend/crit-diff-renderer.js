(function () {
  'use strict';

  var escapeHtml = (typeof window !== 'undefined' && window.crit && window.crit.commentCardHelpers)
    ? window.crit.commentCardHelpers.escapeHtml
    : function(s) { return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;'); };
  var DiffMatchPatch = (typeof window !== 'undefined') ? window.DiffMatchPatch : null;

  // Compute similarity between two strings using token multiset Dice coefficient.
  // Returns 0–1 (1 = identical tokens, 0 = nothing in common).
  // Only counts word tokens (identifiers, numbers) — single punctuation characters
  // like ", :, {, }, etc. are structural noise that inflates similarity between
  // unrelated JSON/code lines.
  function lineSimilarity(a, b) {
    if (a === b) return 1;
    if (!a || !b) return 0;
    // \w+ matches word tokens directly — no need for a separate tokenize pass
    // followed by a filter (the previous custom LCS path used tokenize for
    // more, but after the @sanity/diff-match-patch swap this is the only call
    // site, so it is inlined).
    var tokA = a.match(/\w+/g) || [];
    var tokB = b.match(/\w+/g) || [];
    if (tokA.length === 0 && tokB.length === 0) return 1;
    if (tokA.length === 0 || tokB.length === 0) return 0;
    var counts = {};
    for (var i = 0; i < tokA.length; i++) {
      counts[tokA[i]] = (counts[tokA[i]] || 0) + 1;
    }
    var common = 0;
    for (var i = 0; i < tokB.length; i++) {
      if (counts[tokB[i]] > 0) {
        common++;
        counts[tokB[i]]--;
      }
    }
    return (2 * common) / (tokA.length + tokB.length);
  }

  // Find best similarity-based pairing between del and add lines for word diff.
  // Returns array of [delIdx, addIdx] pairs. Unpaired lines get no word diff.
  function bestWordDiffPairing(delTexts, addTexts) {
    var delCount = delTexts.length;
    var addCount = addTexts.length;
    var pairCount = Math.min(delCount, addCount);
    if (pairCount === 0) return [];
    // Large blocks are code rewrites, not line edits — skip word-diff entirely.
    // This matches GitHub's behavior of not highlighting large del/add blocks.
    if (delCount + addCount > 8) return [];
    // 1:1 — pair directly if similar enough (most common case)
    if (delCount === 1 && addCount === 1) {
      return lineSimilarity(delTexts[0], addTexts[0]) >= 0.4 ? [[0, 0]] : [];
    }
    // Compute all similarity scores
    var candidates = [];
    for (var d = 0; d < delCount; d++) {
      for (var a = 0; a < addCount; a++) {
        candidates.push({ d: d, a: a, score: lineSimilarity(delTexts[d], addTexts[a]) });
      }
    }
    candidates.sort(function(x, y) { return y.score - x.score; });
    // Greedy assignment: pick highest similarity first
    var usedDels = {};
    var usedAdds = {};
    var pairs = [];
    for (var i = 0; i < candidates.length; i++) {
      var c = candidates[i];
      if (usedDels[c.d] || usedAdds[c.a]) continue;
      if (c.score < 0.4) break;
      pairs.push([c.d, c.a]);
      usedDels[c.d] = true;
      usedAdds[c.a] = true;
      if (pairs.length === pairCount) break;
    }
    return pairs;
  }

  // Compute word-level diff between two lines using diff-match-patch.
  // Runs character-level diff with semantic cleanup to produce word-aligned highlights.
  // Returns { oldRanges, newRanges } where each range is [startCharIdx, endCharIdx] in the raw text.
  // Returns null if lines are too long, identical, or completely different.
  function wordDiff(oldLine, newLine) {
    // Skip for very long lines (perf guard)
    if (oldLine.length > 500 || newLine.length > 500) return null;
    // Skip for lines with no spaces and >200 chars (likely minified/binary)
    if (oldLine.length > 200 && !oldLine.includes(' ')) return null;
    if (newLine.length > 200 && !newLine.includes(' ')) return null;
    // Identical lines — no diff needed
    if (oldLine === newLine) return null;

    var dmp = DiffMatchPatch;
    if (!dmp) return null;
    var diffs = dmp.cleanupSemantic(dmp.makeDiff(oldLine, newLine));

    // Build character ranges from diff tuples.
    // DIFF_DELETE (-1) tuples advance old position, DIFF_INSERT (1) advance new,
    // DIFF_EQUAL (0) advances both.
    var oldRanges = [];
    var newRanges = [];
    var oldIdx = 0;
    var newIdx = 0;

    for (var i = 0; i < diffs.length; i++) {
      var op = diffs[i][0];
      var text = diffs[i][1];
      var len = text.length;

      if (op === dmp.DIFF_EQUAL) {
        oldIdx += len;
        newIdx += len;
      } else if (op === dmp.DIFF_DELETE) {
        oldRanges.push([oldIdx, oldIdx + len]);
        oldIdx += len;
      } else if (op === dmp.DIFF_INSERT) {
        newRanges.push([newIdx, newIdx + len]);
        newIdx += len;
      }
    }

    if (oldRanges.length === 0 && newRanges.length === 0) return null;

    // If most of the line changed, the lines probably don't correspond —
    // skip word-diff to avoid noisy highlights on unrelated lines.
    var oldChanged = oldRanges.reduce(function(s, r) { return s + r[1] - r[0]; }, 0);
    var newChanged = newRanges.reduce(function(s, r) { return s + r[1] - r[0]; }, 0);
    if (oldLine.length > 0 && oldChanged / oldLine.length > 0.5) return null;
    if (newLine.length > 0 && newChanged / newLine.length > 0.5) return null;

    return { oldRanges: oldRanges, newRanges: newRanges };
  }

  // Overlay word-diff highlight ranges onto syntax-highlighted HTML.
  // Walks the HTML string, tracking visible character position (skipping HTML tags),
  // and inserts <span class="cssClass"> wrappers around the character ranges.
  // ranges: array of [startCharIdx, endCharIdx] in the raw text.
  function applyWordDiffToHtml(html, ranges, cssClass) {
    if (!ranges || ranges.length === 0) return html;

    var result = '';
    var charIdx = 0;       // visible character index
    var rangeIdx = 0;      // which range we're processing
    var inRange = false;   // currently inside a word-diff span
    var i = 0;             // position in html string

    while (i < html.length) {
      // Skip HTML tags (don't count them as visible characters)
      if (html[i] === '<') {
        // If we're in a word-diff range, close it before the tag, reopen after
        if (inRange) result += '</span>';
        var tagEnd = html.indexOf('>', i);
        if (tagEnd === -1) { result += html.slice(i); break; }
        result += html.slice(i, tagEnd + 1);
        i = tagEnd + 1;
        if (inRange) result += '<span class="' + cssClass + '">';
        continue;
      }

      // Handle HTML entities (e.g., &amp; &lt; &gt; &quot;) as single visible characters
      var visibleChar;
      if (html[i] === '&') {
        var semiIdx = html.indexOf(';', i);
        if (semiIdx !== -1 && semiIdx - i < 10) {
          visibleChar = html.slice(i, semiIdx + 1);
          i = semiIdx + 1;
        } else {
          visibleChar = html[i];
          i++;
        }
      } else {
        visibleChar = html[i];
        i++;
      }

      // Check if we need to open a word-diff span
      if (!inRange && rangeIdx < ranges.length && charIdx >= ranges[rangeIdx][0]) {
        result += '<span class="' + cssClass + '">';
        inRange = true;
      }

      result += visibleChar;
      charIdx++;

      // Check if we need to close a word-diff span
      if (inRange && rangeIdx < ranges.length && charIdx >= ranges[rangeIdx][1]) {
        result += '</span>';
        inRange = false;
        rangeIdx++;
        // Check if immediately entering next range
        if (rangeIdx < ranges.length && charIdx >= ranges[rangeIdx][0]) {
          result += '<span class="' + cssClass + '">';
          inRange = true;
        }
      }
    }

    if (inRange) result += '</span>';
    return result;
  }

  // Strip HTML tags and decode entities to get visible text for word-diff comparison.
  function htmlToText(html) {
    return html.replace(/<[^>]*>/g, '').replace(/&amp;/g, '&').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"').replace(/&#39;/g, "'");
  }

  // Apply word-level diffs to a pair of old/new blocks if they are sufficiently similar.
  // Skips pairs where >70% of characters changed (blocks probably don't correspond).
  function applyWordDiffPair(oldBlock, newBlock) {
    // Normalize newlines to spaces so paragraph re-wrapping doesn't create false diffs.
    // In markdown, soft line breaks within a paragraph are just whitespace.
    // Both \n and ' ' are single chars, so word-diff ranges remain valid for applyWordDiffToHtml.
    var oldText = htmlToText(oldBlock.html).replace(/\n/g, ' ');
    var newText = htmlToText(newBlock.html).replace(/\n/g, ' ');
    var wd = wordDiff(oldText, newText);
    if (!wd) return;
    var oldChangedChars = wd.oldRanges.reduce(function(s, r) { return s + r[1] - r[0]; }, 0);
    var newChangedChars = wd.newRanges.reduce(function(s, r) { return s + r[1] - r[0]; }, 0);
    if (oldText.length > 0 && oldChangedChars / oldText.length > 0.7) return;
    if (newText.length > 0 && newChangedChars / newText.length > 0.7) return;
    oldBlock.wordDiffHtml = applyWordDiffToHtml(oldBlock.html, wd.oldRanges, 'diff-word-del');
    newBlock.wordDiffHtml = applyWordDiffToHtml(newBlock.html, wd.newRanges, 'diff-word-add');
  }

  // Pre-compute word diffs for all paired del/add runs in a hunk.
  // Returns a Map<lineIndex, { ranges, cssClass }> mapping hunk line indices to word-diff info.
  function buildHunkWordDiffs(hunk) {
    var wordDiffMap = new Map();
    var lines = hunk.Lines;
    var i = 0;
    while (i < lines.length) {
      if (lines[i].Type === 'del') {
        // Collect consecutive dels
        var delStart = i;
        while (i < lines.length && lines[i].Type === 'del') i++;
        // Collect consecutive adds
        var addStart = i;
        while (i < lines.length && lines[i].Type === 'add') i++;
        // Pair by similarity so word diffs highlight the right counterpart
        var delCount = addStart - delStart;
        var addCount = i - addStart;
        var delTexts = [];
        for (var d = 0; d < delCount; d++) delTexts.push(lines[delStart + d].Content);
        var addTexts = [];
        for (var a = 0; a < addCount; a++) addTexts.push(lines[addStart + a].Content);
        var pairs = bestWordDiffPairing(delTexts, addTexts);
        for (var p = 0; p < pairs.length; p++) {
          var dIdx = delStart + pairs[p][0];
          var aIdx = addStart + pairs[p][1];
          var wd = wordDiff(lines[dIdx].Content, lines[aIdx].Content);
          if (wd) {
            wordDiffMap.set(dIdx, { ranges: wd.oldRanges, cssClass: 'diff-word-del' });
            wordDiffMap.set(aIdx, { ranges: wd.newRanges, cssClass: 'diff-word-add' });
          }
        }
      } else {
        i++;
      }
    }
    return wordDiffMap;
  }

  var api = {
    lineSimilarity: lineSimilarity,
    bestWordDiffPairing: bestWordDiffPairing,
    wordDiff: wordDiff,
    applyWordDiffToHtml: applyWordDiffToHtml,
    htmlToText: htmlToText,
    applyWordDiffPair: applyWordDiffPair,
    buildHunkWordDiffs: buildHunkWordDiffs,
  };
  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.diffRenderer = api;
  }
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
