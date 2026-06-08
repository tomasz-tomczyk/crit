// crit-line-blocks.js — Line block building for markdown and code files.
// Dependencies: window.crit.commentCardHelpers.escapeHtml, window.hljs
(function () {
  'use strict';

  // Dependencies from previously-loaded modules
  var escapeHtml = (typeof window !== 'undefined' && window.crit && window.crit.commentCardHelpers)
    ? window.crit.commentCardHelpers.escapeHtml
    : function(s) { return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;'); };
  var hljs = (typeof window !== 'undefined') ? window.hljs : null;

  function slugifyHeading(text) {
    return text
      .toLowerCase()
      .replace(/[^\p{L}\p{N}\s-]/gu, '')
      .replace(/[\s-]+/g, '-')
      .replace(/^-+|-+$/g, '');
  }

  // Split highlighted HTML into per-line strings, preserving open spans across lines.
  function splitHighlightedCode(html) {
    var result = [];
    var openSpans = [];
    var lines = html.split('\n');
    for (var i = 0; i < lines.length; i++) {
      var prefix = openSpans.map(function(s) { return s; }).join('');
      var line = lines[i];
      var fullLine = prefix + line;

      // Track open/close spans
      var opens = line.match(/<span[^>]*>/g) || [];
      var closes = line.match(/<\/span>/g) || [];
      for (var oi = 0; oi < opens.length; oi++) openSpans.push(opens[oi]);
      for (var ci = 0; ci < closes.length; ci++) openSpans.pop();

      // Close any open spans at end of line
      var suffix = '';
      for (var si = 0; si < openSpans.length; si++) suffix += '</span>';
      result.push(fullLine + suffix);
    }
    return result;
  }

  // Build line blocks for code files in file mode (document view)
  function buildCodeLineBlocks(file) {
    var lines = file.content.split('\n');
    var blocks = [];
    for (var i = 0; i < lines.length; i++) {
      var lineNum = i + 1;
      var html;
      var cacheEntry = file.highlightCache ? file.highlightCache[lineNum] : null;
      if (cacheEntry && cacheEntry.raw === (lines[i] || '')) {
        html = '<code class="hljs">' + cacheEntry.html + '</code>';
      } else {
        html = '<code class="hljs">' + escapeHtml(lines[i] || '') + '</code>';
      }
      blocks.push({
        startLine: lineNum,
        endLine: lineNum,
        html: html,
        isEmpty: lines[i].trim() === '',
        cssClass: 'code-line'
      });
    }
    return blocks;
  }

  // ===== buildLineBlocks helpers =====

  // Find the matching close token for an open token at openIdx.
  function findCloseToken(tokens, openIdx) {
    var openType = tokens[openIdx].type;
    var closeType = openType.replace('_open', '_close');
    var depth = 1;
    for (var j = openIdx + 1; j < tokens.length; j++) {
      if (tokens[j].type === openType) depth++;
      if (tokens[j].type === closeType) { depth--; if (depth === 0) return j; }
    }
    return openIdx;
  }

  // Emit gap-line blocks for uncovered source lines up to (but not including) `upTo`.
  function addGapLineBlocks(blocks, sourceLines, coveredUpTo, upTo) {
    while (coveredUpTo < upTo) {
      var lineText = sourceLines[coveredUpTo];
      blocks.push({
        startLine: coveredUpTo + 1,
        endLine: coveredUpTo + 1,
        html: lineText === '' ? '' : escapeHtml(lineText),
        isEmpty: lineText.trim() === ''
      });
      coveredUpTo++;
    }
    return coveredUpTo;
  }

  // Handle a fence (code block) token — split into per-line blocks.
  function handleFenceToken(token, blocks, sourceLines, coveredUpTo, blockStart, blockEnd) {
    var lang = token.info.trim().split(/\s+/)[0] || '';

    // Mermaid diagrams: render as a single block (not split per-line)
    if (lang === 'mermaid') {
      blocks.push({
        startLine: blockStart + 1, endLine: blockEnd,
        html: '<pre><code class="language-mermaid">' + escapeHtml(token.content) + '</code></pre>',
        isEmpty: false, cssClass: 'mermaid-block'
      });
      return addGapLineBlocks(blocks, sourceLines, blockEnd, blockEnd);
    }

    var highlighted = '';
    if (lang && hljs && hljs.getLanguage(lang)) {
      try { highlighted = hljs.highlight(token.content, { language: lang }).value; } catch (e) {}
    }
    if (!highlighted) highlighted = escapeHtml(token.content);

    var codeLines = splitHighlightedCode(highlighted);
    // Remove trailing empty line from fence
    if (codeLines.length > 0 && codeLines[codeLines.length - 1] === '') codeLines.pop();

    // Opening fence line
    blocks.push({
      startLine: blockStart + 1, endLine: blockStart + 1,
      html: '<span class="fence-marker">' + escapeHtml(sourceLines[blockStart]) + '</span>',
      isEmpty: false, cssClass: 'code-line code-first'
    });
    coveredUpTo = blockStart + 1;

    // Code content lines
    for (var ci = 0; ci < codeLines.length; ci++) {
      var ln = blockStart + 2 + ci;
      if (ln > blockEnd) break;
      var isLast = (ci === codeLines.length - 1 && blockEnd <= ln);
      blocks.push({
        startLine: ln, endLine: ln,
        html: '<code class="hljs">' + (codeLines[ci] || '&nbsp;') + '</code>',
        isEmpty: false, cssClass: 'code-line' + (isLast ? ' code-last' : '')
      });
      coveredUpTo = ln;
    }

    // Closing fence line
    if (blockEnd > coveredUpTo) {
      blocks.push({
        startLine: blockEnd, endLine: blockEnd,
        html: '<span class="fence-marker">' + escapeHtml(sourceLines[blockEnd - 1]) + '</span>',
        isEmpty: false, cssClass: 'code-line code-last'
      });
      coveredUpTo = blockEnd;
    }

    coveredUpTo = addGapLineBlocks(blocks, sourceLines, coveredUpTo, blockEnd);
    return coveredUpTo;
  }

  // Handle a list token (bullet or ordered) — split into per-item blocks.
  function handleListToken(tokens, i, _token, md, blocks, sourceLines, coveredUpTo, blockEnd) {
    var listCloseIdx = findCloseToken(tokens, i);

    splitListInto(tokens, i, listCloseIdx, md, blocks, sourceLines, coveredUpTo, function(html) {
      return html;
    });

    if (blocks.length > 0) {
      coveredUpTo = Math.max(coveredUpTo, blocks[blocks.length - 1].endLine);
    }

    coveredUpTo = addGapLineBlocks(blocks, sourceLines, coveredUpTo, blockEnd);
    return { nextIndex: listCloseIdx + 1, coveredUpTo: coveredUpTo };
  }

  // Recursively split a list into blocks.
  function splitListInto(tokens, listOpenIdx, listCloseIdx, md, blocks, sourceLines, coveredUpTo, wrap) {
    var listOpen = tokens[listOpenIdx];
    var listTag = listOpen.type === 'bullet_list_open' ? 'ul' : 'ol';
    var j = listOpenIdx + 1;

    while (j < listCloseIdx) {
      if (tokens[j].type !== 'list_item_open') { j++; continue; }
      var itemOpenIdx = j;
      var itemCloseIdx = findCloseToken(tokens, j);
      var itemMap = tokens[itemOpenIdx].map;

      if (!itemMap) { j = itemCloseIdx + 1; continue; }

      addGapLineBlocks(blocks, sourceLines, coveredUpTo, itemMap[0]);
      coveredUpTo = itemMap[0];

      var nestedRanges = findDirectNestedLists(tokens, itemOpenIdx, itemCloseIdx);

      var itemStartAttr = (listTag === 'ol' && tokens[itemOpenIdx].info)
        ? ' start="' + tokens[itemOpenIdx].info + '"'
        : '';

      var firstNested = nestedRanges.length > 0 ? nestedRanges[0] : null;
      var leadEndTokenIdx = firstNested ? firstNested.openIdx : itemCloseIdx;

      var leadStartLine = itemMap[0] + 1;
      var leadEndLine;
      if (firstNested) {
        var nestedFirstMap = tokens[firstNested.openIdx].map;
        leadEndLine = nestedFirstMap ? nestedFirstMap[0] : itemMap[1];
      } else {
        leadEndLine = itemMap[1];
        while (leadEndLine > leadStartLine && sourceLines[leadEndLine - 1].trim() === '') {
          leadEndLine--;
        }
      }

      var leadInnerTokens = tokens.slice(itemOpenIdx + 1, leadEndTokenIdx);
      var leadInnerHtml = md.renderer.render(leadInnerTokens, md.options, {});
      var leadLiClass = tokens[itemOpenIdx].attrGet && tokens[itemOpenIdx].attrGet('class');
      var leadLiAttr = leadLiClass ? ' class="' + escapeAttr(leadLiClass) + '"' : '';
      var leadInnerWrapped = '<' + listTag + itemStartAttr + '>' +
        '<li' + leadLiAttr + '>' + leadInnerHtml + '</li>' +
        '</' + listTag + '>';

      if (leadEndLine > leadStartLine - 1) {
        blocks.push({
          startLine: leadStartLine,
          endLine: leadEndLine,
          html: wrap(leadInnerWrapped),
          isEmpty: false
        });
        coveredUpTo = leadEndLine;
      }

      for (var n = 0; n < nestedRanges.length; n++) {
        var nested = nestedRanges[n];
        var childWrap = (function(outerWrap, outerListTag) {
          return function(innerHtml) {
            return outerWrap(
              '<' + outerListTag + ' class="crit-list-wrapper">' +
              '<li class="crit-list-wrapper">' + innerHtml + '</li>' +
              '</' + outerListTag + '>'
            );
          };
        })(wrap, listTag);
        coveredUpTo = splitListInto(tokens, nested.openIdx, nested.closeIdx, md, blocks, sourceLines, coveredUpTo, childWrap);
      }

      if (nestedRanges.length > 0) {
        var lastNested = nestedRanges[nestedRanges.length - 1];
        var trailStartTokenIdx = lastNested.closeIdx + 1;
        if (trailStartTokenIdx < itemCloseIdx) {
          var trailStartLine = coveredUpTo;
          var trailEndLine = itemMap[1];
          while (trailEndLine > trailStartLine && sourceLines[trailEndLine - 1].trim() === '') {
            trailEndLine--;
          }
          if (trailEndLine > trailStartLine) {
            var trailInnerTokens = tokens.slice(trailStartTokenIdx, itemCloseIdx);
            var trailInnerHtml = md.renderer.render(trailInnerTokens, md.options, {});
            var trailWrapped = '<' + listTag + '>' +
              '<li>' + trailInnerHtml + '</li>' +
              '</' + listTag + '>';
            blocks.push({
              startLine: trailStartLine + 1,
              endLine: trailEndLine,
              html: wrap(trailWrapped),
              isEmpty: false
            });
            coveredUpTo = trailEndLine;
          }
        }
      }

      j = itemCloseIdx + 1;
    }

    return coveredUpTo;
  }

  // Find direct-child nested lists within an item.
  function findDirectNestedLists(tokens, itemOpenIdx, itemCloseIdx) {
    var result = [];
    var depth = 0;
    for (var k = itemOpenIdx + 1; k < itemCloseIdx; k++) {
      var t = tokens[k];
      if (t.nesting === 1) {
        if (depth === 0 && (t.type === 'bullet_list_open' || t.type === 'ordered_list_open')) {
          var closeIdx = findCloseToken(tokens, k);
          result.push({ openIdx: k, closeIdx: closeIdx });
          k = closeIdx;
          continue;
        }
        depth++;
      } else if (t.nesting === -1) {
        if (depth > 0) depth--;
      }
    }
    return result;
  }

  function escapeAttr(s) {
    return String(s).replace(/&/g, '&amp;').replace(/"/g, '&quot;');
  }

  // Handle a table token — split into per-row blocks.
  function handleTableToken(tokens, i, md, blocks, sourceLines, coveredUpTo, blockEnd) {
    var tableCloseIdx = findCloseToken(tokens, i);

    // Build colgroup from header cell alignments
    var colgroup = '';
    var aligns = [];
    for (var j = i + 1; j < tableCloseIdx; j++) {
      if (tokens[j].type === 'th_open') {
        aligns.push(tokens[j].attrGet('style') || '');
      }
    }
    if (aligns.length) {
      colgroup = '<colgroup>' +
        aligns.map(function(s) { return '<col' + (s ? ' style="' + s + '"' : '') + '>'; }).join('') +
        '</colgroup>';
    }

    j = i + 1;
    var inThead = false;
    var rowIndex = 0;
    var bodyRowIndex = 0;

    while (j < tableCloseIdx) {
      if (tokens[j].type === 'thead_open') { inThead = true; j++; continue; }
      if (tokens[j].type === 'thead_close') { inThead = false; j++; continue; }
      if (tokens[j].type === 'tbody_open' || tokens[j].type === 'tbody_close') { j++; continue; }

      if (tokens[j].type === 'tr_open') {
        var trCloseIdx = findCloseToken(tokens, j);
        var trMap = tokens[j].map;

        if (trMap) {
          for (var ln = coveredUpTo; ln < trMap[0]; ln++) {
            var lineText = sourceLines[ln].trim();
            if (/^\|[\s\-:|]+\|$/.test(lineText) || /^[-:|][\s\-:|]*$/.test(lineText)) {
              blocks.push({ startLine: ln + 1, endLine: ln + 1, html: '', isEmpty: false, cssClass: 'table-separator' });
            } else {
              blocks.push({ startLine: ln + 1, endLine: ln + 1, html: lineText === '' ? '' : escapeHtml(lineText), isEmpty: lineText === '' });
            }
          }

          var trTokens = tokens.slice(j, trCloseIdx + 1);
          var section = inThead ? 'thead' : 'tbody';
          var rowHtml = '<table class="split-table">' + colgroup +
            '<' + section + '>' +
            md.renderer.render(trTokens, md.options, {}) +
            '</' + section + '></table>';

          var cls = 'table-row';
          if (rowIndex === 0) cls += ' table-first';
          if (!inThead && bodyRowIndex % 2 === 1) cls += ' table-even';
          blocks.push({
            startLine: trMap[0] + 1, endLine: trMap[1],
            html: rowHtml, isEmpty: false, cssClass: cls
          });
          coveredUpTo = trMap[1];
          rowIndex++;
          if (!inThead) bodyRowIndex++;
        }
        j = trCloseIdx + 1;
      } else {
        j++;
      }
    }

    // Mark the last table row
    if (blocks.length > 0 && blocks[blocks.length - 1].cssClass &&
        blocks[blocks.length - 1].cssClass.indexOf('table-row') !== -1) {
      blocks[blocks.length - 1].cssClass += ' table-last';
    }

    coveredUpTo = addGapLineBlocks(blocks, sourceLines, coveredUpTo, blockEnd);
    return { nextIndex: tableCloseIdx + 1, coveredUpTo: coveredUpTo };
  }

  // Handle a blockquote token — split into child blocks.
  function handleBlockquoteToken(tokens, i, md, blocks, sourceLines, coveredUpTo, blockStart, blockEnd) {
    var bqCloseIdx = findCloseToken(tokens, i);
    var j = i + 1;
    var hasChildren = false;

    while (j < bqCloseIdx) {
      if (tokens[j].nesting === -1 || !tokens[j].map) { j++; continue; }
      hasChildren = true;
      var childMap = tokens[j].map;
      var childCloseIdx = j;
      if (tokens[j].nesting === 1) childCloseIdx = findCloseToken(tokens, j);
      addGapLineBlocks(blocks, sourceLines, coveredUpTo, childMap[0]);
      var childTokens = tokens.slice(j, childCloseIdx + 1);
      var childHtml = '<blockquote>' +
        md.renderer.render(childTokens, md.options, {}) +
        '</blockquote>';
      blocks.push({
        startLine: childMap[0] + 1, endLine: childMap[1],
        html: childHtml, isEmpty: false
      });
      coveredUpTo = childMap[1];
      j = childCloseIdx + 1;
    }

    if (!hasChildren) {
      var bqTokens = tokens.slice(i, bqCloseIdx + 1);
      blocks.push({
        startLine: blockStart + 1, endLine: blockEnd,
        html: md.renderer.render(bqTokens, md.options, {}),
        isEmpty: false
      });
      coveredUpTo = blockEnd;
    }

    coveredUpTo = addGapLineBlocks(blocks, sourceLines, coveredUpTo, blockEnd);
    return { nextIndex: bqCloseIdx + 1, coveredUpTo: coveredUpTo };
  }

  // ===== buildLineBlocks =====
  // Parses markdown tokens into a flat array of commentable line blocks.
  function buildLineBlocks(tokens, md, content) {
    var sourceLines = content.split('\n');
    var totalLines = sourceLines.length;
    var blocks = [];
    var coveredUpTo = 0;

    var i = 0;
    while (i < tokens.length) {
      var token = tokens[i];
      if (token.hidden || !token.map) { i++; continue; }

      var blockStart = token.map[0];
      var blockEnd = token.map[1];

      coveredUpTo = addGapLineBlocks(blocks, sourceLines, coveredUpTo, blockStart);

      // Code blocks (fence): split into per-line blocks
      if (token.type === 'fence') {
        coveredUpTo = handleFenceToken(token, blocks, sourceLines, coveredUpTo, blockStart, blockEnd);
        i++;
        continue;
      }

      // Lists: split into per-item blocks
      if (token.type === 'bullet_list_open' || token.type === 'ordered_list_open') {
        var listResult = handleListToken(tokens, i, token, md, blocks, sourceLines, coveredUpTo, blockEnd);
        i = listResult.nextIndex;
        coveredUpTo = listResult.coveredUpTo;
        continue;
      }

      // Tables: split into per-row blocks
      if (token.type === 'table_open') {
        var tableResult = handleTableToken(tokens, i, md, blocks, sourceLines, coveredUpTo, blockEnd);
        i = tableResult.nextIndex;
        coveredUpTo = tableResult.coveredUpTo;
        continue;
      }

      // Blockquotes: split into child blocks
      if (token.type === 'blockquote_open') {
        var bqResult = handleBlockquoteToken(tokens, i, md, blocks, sourceLines, coveredUpTo, blockStart, blockEnd);
        i = bqResult.nextIndex;
        coveredUpTo = bqResult.coveredUpTo;
        continue;
      }

      // Default: render as single block
      var closeIdx = i;
      if (token.nesting === 1) closeIdx = findCloseToken(tokens, i);

      var blockTokens = tokens.slice(i, closeIdx + 1);
      var html;
      try {
        html = md.renderer.render(blockTokens, md.options, {});
      } catch (e) {
        html = escapeHtml(blockTokens.map(function(t) { return t.content || ''; }).join(''));
      }

      blocks.push({
        startLine: blockStart + 1, endLine: blockEnd,
        html: html, isEmpty: false
      });

      i = closeIdx + 1;
      coveredUpTo = blockEnd;
    }

    addGapLineBlocks(blocks, sourceLines, coveredUpTo, totalLines);
    return blocks;
  }

  var api = {
    splitHighlightedCode: splitHighlightedCode,
    buildCodeLineBlocks: buildCodeLineBlocks,
    buildLineBlocks: buildLineBlocks,
    findCloseToken: findCloseToken,
    addGapLineBlocks: addGapLineBlocks,
    handleFenceToken: handleFenceToken,
    handleListToken: handleListToken,
    handleTableToken: handleTableToken,
    handleBlockquoteToken: handleBlockquoteToken,
    slugifyHeading: slugifyHeading
  };
  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.lineBlocks = api;
  }
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
