(function() {
  'use strict';

  // ===== Comment Markdown Renderer =====
  const commentMd = window.markdownit({ html: false, linkify: true, typographer: true });

  // ===== Document Markdown Renderer =====
  const documentMd = window.markdownit({
    html: true,
    typographer: true,
    linkify: true,
    highlight: function(str, lang) {
      if (lang && hljs.getLanguage(lang)) {
        try { return hljs.highlight(str, { language: lang }).value; } catch (_) {}
      }
      try { return hljs.highlightAuto(str).value; } catch (_) {}
      return '';
    }
  });

  // ===== State =====
  let session = {};       // { mode, branch, base_ref, review_round, files: [...] }
  let files = [];         // [{ path, status, fileType, content, diffHunks, comments, lineBlocks, tocItems, collapsed, viewMode }]
  let shareURL = '';
  let hostedURL = '';
  let deleteToken = '';
  let uiState = 'reviewing';
  let reviewRound = 1;

  let diffMode = localStorage.getItem('crit-diff-mode') || 'split'; // 'split' or 'unified'

  // Per-file active form state
  let activeFilePath = null;
  let activeForm = null;  // { filePath, afterBlockIndex, startLine, endLine, editingId, side }
  let selectionStart = null;
  let selectionEnd = null;
  var unifiedVisualStart = null; // visual index range for unified drag (cross-number-space)
  var unifiedVisualEnd = null;
  let focusedBlockIndex = null;
  let focusedFilePath = null;
  let focusedElement = null; // currently focused navigable element
  let navElements = []; // cached .kb-nav list, rebuilt on render

  const enc = encodeURIComponent;

  // ===== Viewed State =====
  function viewedStorageKey() {
    var paths = files.map(function(f) { return f.path; }).sort().join('\n');
    var hash = 0;
    for (var i = 0; i < paths.length; i++) {
      hash = ((hash << 5) - hash + paths.charCodeAt(i)) | 0;
    }
    return 'crit-viewed-' + (hash >>> 0).toString(36);
  }

  function saveViewedState() {
    var viewed = {};
    for (var i = 0; i < files.length; i++) {
      if (files[i].viewed) viewed[files[i].path] = true;
    }
    try { localStorage.setItem(viewedStorageKey(), JSON.stringify(viewed)); } catch (_) {}
  }

  function restoreViewedState() {
    try {
      var data = JSON.parse(localStorage.getItem(viewedStorageKey()) || '{}');
      for (var i = 0; i < files.length; i++) {
        files[i].viewed = !!data[files[i].path];
        if (files[i].viewed) files[i].collapsed = true;
      }
    } catch (_) {}
  }

  function clearViewedState() {
    for (var i = 0; i < files.length; i++) {
      files[i].viewed = false;
    }
    try { localStorage.removeItem(viewedStorageKey()); } catch (_) {}
  }

  function toggleViewed(filePath) {
    var file = getFileByPath(filePath);
    if (!file) return;
    file.viewed = !file.viewed;
    saveViewedState();
    updateViewedCount();
    updateTreeViewedState();
    // Update the checkbox in the file header
    var section = document.getElementById('file-section-' + filePath);
    if (section) {
      var cb = section.querySelector('.file-header-viewed input');
      if (cb) cb.checked = file.viewed;
      // Collapse when marking as viewed
      if (file.viewed && section.open) {
        if (section.getBoundingClientRect().top < 0) {
          section.scrollIntoView({ behavior: 'instant' });
        }
        section.open = false;
        file.collapsed = true;
      }
    }
  }

  // ===== Init =====
  async function init() {
    initTheme();

    // Measure actual header height and set CSS variable for sticky offsets
    function updateHeaderHeight() {
      var h = document.querySelector('.header');
      if (h) document.documentElement.style.setProperty('--header-height', h.getBoundingClientRect().height + 'px');
    }
    updateHeaderHeight();
    window.addEventListener('resize', updateHeaderHeight);

    const [sessionRes, configRes] = await Promise.all([
      fetch('/api/session').then(r => r.json()),
      fetch('/api/config').then(r => r.json()),
    ]);

    session = sessionRes;
    reviewRound = session.review_round || 1;

    // Config
    shareURL = configRes.share_url || '';
    hostedURL = configRes.hosted_url || '';
    deleteToken = configRes.delete_token || '';

    if (shareURL) document.getElementById('shareBtn').style.display = '';
    if (hostedURL) showSharedNotice(hostedURL);

    // Version check
    if (configRes.latest_version && configRes.version && configRes.latest_version !== configRes.version) {
      const el = document.getElementById('headerUpdate');
      el.style.display = '';
      document.getElementById('updateLink').textContent = configRes.latest_version + ' available';
    }

    // Branch context
    if (session.branch) {
      document.getElementById('branchContext').style.display = '';
      document.getElementById('branchName').textContent = session.branch;
    }

    // Show diff mode toggle in git mode (has diffs to show)
    if (session.mode === 'git') {
      document.getElementById('diffModeToggle').style.display = '';
      // Sync toggle buttons with persisted diffMode
      document.querySelectorAll('#diffModeToggle .toggle-btn').forEach(function(b) {
        b.classList.toggle('active', b.dataset.mode === diffMode);
      });
      document.getElementById('tocToggle').style.display = 'none';
    }

    updateHeaderRound();
    document.title = session.mode === 'git'
      ? 'Crit — ' + (session.branch || 'review')
      : 'Crit — ' + (session.files || []).map(f => f.path).join(', ');

    // Load all files in parallel
    const fileInfos = session.files || [];
    const filePromises = fileInfos.map(async (fi) => {
      const [fileRes, commentsRes] = await Promise.all([
        fetch('/api/file?path=' + enc(fi.path)).then(r => r.json()),
        fetch('/api/file/comments?path=' + enc(fi.path)).then(r => r.json()),
      ]);

      const f = {
        path: fi.path,
        status: fi.status,
        fileType: fi.file_type,
        content: fileRes.content || '',
        comments: Array.isArray(commentsRes) ? commentsRes : [],
        diffHunks: null,
        lineBlocks: null,
        tocItems: [],
        collapsed: fi.status === 'deleted',
        viewMode: (session.mode === 'git') ? 'diff' : 'document',
        additions: fi.additions || 0,
        deletions: fi.deletions || 0,
      };

      // Load diff hunks (always — code files get git diff, markdown may get inter-round diff)
      {
        try {
          const diffRes = await fetch('/api/file/diff?path=' + enc(fi.path)).then(r => r.json());
          f.diffHunks = diffRes.hunks || [];
        } catch (_) {
          f.diffHunks = [];
        }
      }

      // Mark large diffs for deferred rendering
      var diffLineCount = 0;
      if (f.diffHunks) {
        for (var h = 0; h < f.diffHunks.length; h++) {
          diffLineCount += (f.diffHunks[h].Lines || []).length;
        }
      }
      f.diffTooLarge = diffLineCount > 1000;
      f.diffLoaded = !f.diffTooLarge;

      // Pre-highlight code files for diff rendering
      if (f.fileType === 'code') {
        f.highlightCache = preHighlightFile(f);
        f.lang = langFromPath(f.path);
      }

      // Parse markdown content into line blocks
      if (f.fileType === 'markdown') {
        const parsed = parseMarkdown(f.content);
        f.lineBlocks = parsed.blocks;
        f.tocItems = parsed.tocItems;
      }

      return f;
    });

    files = await Promise.all(filePromises);

    // Sort files to match tree order: directories before files, then alphabetical
    files.sort(function(a, b) {
      var pa = a.path.split('/'), pb = b.path.split('/');
      var min = Math.min(pa.length, pb.length);
      for (var i = 0; i < min - 1; i++) {
        if (pa[i] !== pb[i]) return pa[i].localeCompare(pb[i]);
      }
      if (pa.length !== pb.length) return pb.length - pa.length;
      return pa[pa.length - 1].localeCompare(pb[pa.length - 1]);
    });

    restoreViewedState();
    renderFileTree();
    renderAllFiles();
    buildToc();
    updateCommentCount();
    updateViewedCount();
    restoreDrafts();
  }

  // ===== Syntax Highlighting for Diffs =====
  function langFromPath(filePath) {
    const ext = (filePath || '').split('.').pop().toLowerCase();
    const map = {
      js: 'javascript', jsx: 'javascript', ts: 'typescript', tsx: 'typescript',
      go: 'go', py: 'python', rb: 'ruby', rs: 'rust',
      sql: 'sql', sh: 'bash', bash: 'bash', zsh: 'bash',
      json: 'json', yaml: 'yaml', yml: 'yaml',
      html: 'xml', htm: 'xml', xml: 'xml', svg: 'xml',
      css: 'css', scss: 'css', less: 'css',
      ex: 'elixir', exs: 'elixir',
      md: 'markdown', java: 'java', kt: 'kotlin',
      c: 'c', h: 'c', cpp: 'cpp', hpp: 'cpp',
      cs: 'csharp', swift: 'swift', php: 'php',
      r: 'r', lua: 'lua', zig: 'zig', nim: 'nim',
      toml: 'ini', ini: 'ini', dockerfile: 'dockerfile',
      makefile: 'makefile', tf: 'hcl',
    };
    return map[ext] || null;
  }

  // Pre-highlight file content and return array of highlighted lines (1-indexed).
  // highlightedLines[lineNum] = highlighted HTML for that line.
  function preHighlightFile(file) {
    if (!file.content || file.fileType !== 'code') return null;
    const lang = langFromPath(file.path);
    if (!lang || !hljs.getLanguage(lang)) return null;
    try {
      const highlighted = hljs.highlight(file.content, { language: lang, ignoreIllegals: true }).value;
      const lines = splitHighlightedCode(highlighted);
      // Return 1-indexed: lines[1] = first line
      const result = [null]; // index 0 unused
      for (let i = 0; i < lines.length; i++) {
        result.push(lines[i]);
      }
      return result;
    } catch (_) {
      return null;
    }
  }

  // Get highlighted HTML for a single diff line.
  // Uses pre-highlighted cache for new-side lines, falls back to per-line for old-side.
  function highlightDiffLine(content, lineNum, side, highlightCache, lang) {
    // Try cache first (new-side lines: context and additions have NewNum mapped to file.content)
    if (highlightCache && lineNum > 0 && side !== 'old' && highlightCache[lineNum]) {
      return highlightCache[lineNum];
    }
    // Fallback: highlight individual line
    if (lang && hljs.getLanguage(lang)) {
      try {
        return hljs.highlight(content, { language: lang, ignoreIllegals: true }).value;
      } catch (_) {}
    }
    return escapeHtml(content);
  }

  // ===== Markdown Parsing =====
  function parseMarkdown(content) {
    const tokens = documentMd.parse(content, {});
    const blocks = buildLineBlocks(tokens, documentMd, content);
    const tocItems = extractTocItems(tokens);
    return { blocks, tocItems };
  }

  function extractTocItems(tokens) {
    const items = [];
    for (let i = 0; i < tokens.length; i++) {
      if (tokens[i].type === 'heading_open' && tokens[i].map) {
        const level = parseInt(tokens[i].tag.slice(1));
        const inline = tokens[i + 1];
        if (inline && inline.type === 'inline') {
          items.push({ level, text: inline.content, startLine: tokens[i].map[0] + 1 });
        }
      }
    }
    return items;
  }

  function splitHighlightedCode(html) {
    const result = [];
    let openSpans = [];
    const lines = html.split('\n');
    for (let i = 0; i < lines.length; i++) {
      let prefix = openSpans.map(s => s).join('');
      let line = lines[i];
      let fullLine = prefix + line;

      // Track open/close spans
      const opens = line.match(/<span[^>]*>/g) || [];
      const closes = line.match(/<\/span>/g) || [];
      for (const o of opens) openSpans.push(o);
      for (let c = 0; c < closes.length; c++) openSpans.pop();

      // Close any open spans at end of line
      let suffix = '</span>'.repeat(openSpans.length);
      result.push(fullLine + suffix);
    }
    return result;
  }

  function buildLineBlocks(tokens, md, content) {
    const sourceLines = content.split('\n');
    const totalLines = sourceLines.length;
    const blocks = [];
    let coveredUpTo = 0;

    function addGapLines(upTo) {
      while (coveredUpTo < upTo) {
        const lineText = sourceLines[coveredUpTo];
        blocks.push({
          startLine: coveredUpTo + 1,
          endLine: coveredUpTo + 1,
          html: lineText === '' ? '' : escapeHtml(lineText),
          isEmpty: lineText.trim() === ''
        });
        coveredUpTo++;
      }
    }

    function findClose(openIdx) {
      const openType = tokens[openIdx].type;
      const closeType = openType.replace('_open', '_close');
      let depth = 1;
      for (let j = openIdx + 1; j < tokens.length; j++) {
        if (tokens[j].type === openType) depth++;
        if (tokens[j].type === closeType) { depth--; if (depth === 0) return j; }
      }
      return openIdx;
    }

    let i = 0;
    while (i < tokens.length) {
      const token = tokens[i];
      if (token.hidden || !token.map) { i++; continue; }

      const blockStart = token.map[0];
      const blockEnd = token.map[1];

      addGapLines(blockStart);

      // === Code blocks (fence): split into per-line blocks ===
      if (token.type === 'fence') {
        const lang = token.info.trim().split(/\s+/)[0] || '';
        let highlighted = '';
        if (lang && hljs.getLanguage(lang)) {
          try { highlighted = hljs.highlight(token.content, { language: lang }).value; } catch (_) {}
        }
        if (!highlighted) {
          try { highlighted = hljs.highlightAuto(token.content).value; } catch (_) {}
        }
        if (!highlighted) highlighted = escapeHtml(token.content);

        const codeLines = splitHighlightedCode(highlighted);
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
        for (let ci = 0; ci < codeLines.length; ci++) {
          const ln = blockStart + 2 + ci;
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

        i++;
        addGapLines(blockEnd);
        continue;
      }

      // === Lists: split into per-item blocks ===
      if (token.type === 'bullet_list_open' || token.type === 'ordered_list_open') {
        const listCloseIdx = findClose(i);
        const listTag = token.type === 'bullet_list_open' ? 'ul' : 'ol';
        let j = i + 1;

        while (j < listCloseIdx) {
          if (tokens[j].type === 'list_item_open') {
            const itemMap = tokens[j].map;
            const itemCloseIdx = findClose(j);

            if (itemMap) {
              addGapLines(itemMap[0]);
              let effectiveEnd = itemMap[1];
              while (effectiveEnd > itemMap[0] + 1 && sourceLines[effectiveEnd - 1].trim() === '') {
                effectiveEnd--;
              }

              const itemTokens = tokens.slice(j, itemCloseIdx + 1);
              const startAttr = listTag === 'ol' && tokens[j].info ? ' start="' + tokens[j].info + '"' : '';
              const itemHtml = '<' + listTag + startAttr + '>' +
                md.renderer.render(itemTokens, md.options, {}) +
                '</' + listTag + '>';

              blocks.push({
                startLine: itemMap[0] + 1,
                endLine: effectiveEnd,
                html: itemHtml,
                isEmpty: false
              });
              coveredUpTo = effectiveEnd;
            }
            j = itemCloseIdx + 1;
          } else {
            j++;
          }
        }

        i = listCloseIdx + 1;
        addGapLines(blockEnd);
        continue;
      }

      // === Tables: split into per-row blocks ===
      if (token.type === 'table_open') {
        const tableCloseIdx = findClose(i);
        let colgroup = '';
        const aligns = [];
        for (let j = i + 1; j < tableCloseIdx; j++) {
          if (tokens[j].type === 'th_open') {
            aligns.push(tokens[j].attrGet('style') || '');
          }
        }
        if (aligns.length) {
          colgroup = '<colgroup>' +
            aligns.map(s => '<col' + (s ? ' style="' + s + '"' : '') + '>').join('') +
            '</colgroup>';
        }

        let j = i + 1;
        let inThead = false;
        let rowIndex = 0;
        let bodyRowIndex = 0;

        while (j < tableCloseIdx) {
          if (tokens[j].type === 'thead_open') { inThead = true; j++; continue; }
          if (tokens[j].type === 'thead_close') { inThead = false; j++; continue; }
          if (tokens[j].type === 'tbody_open' || tokens[j].type === 'tbody_close') { j++; continue; }

          if (tokens[j].type === 'tr_open') {
            const trCloseIdx = findClose(j);
            const trMap = tokens[j].map;

            if (trMap) {
              for (let ln = coveredUpTo; ln < trMap[0]; ln++) {
                const lineText = sourceLines[ln].trim();
                if (/^\|[\s\-:|]+\|$/.test(lineText) || /^[-:|][\s\-:|]*$/.test(lineText)) {
                  blocks.push({ startLine: ln + 1, endLine: ln + 1, html: '', isEmpty: false, cssClass: 'table-separator' });
                } else {
                  blocks.push({ startLine: ln + 1, endLine: ln + 1, html: lineText === '' ? '' : escapeHtml(lineText), isEmpty: lineText === '' });
                }
              }
              coveredUpTo = trMap[0];

              const trTokens = tokens.slice(j, trCloseIdx + 1);
              const section = inThead ? 'thead' : 'tbody';
              const rowHtml = '<table class="split-table">' + colgroup +
                '<' + section + '>' +
                md.renderer.render(trTokens, md.options, {}) +
                '</' + section + '></table>';

              let cls = 'table-row';
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
        if (blocks.length > 0 && blocks[blocks.length - 1].cssClass &&
            blocks[blocks.length - 1].cssClass.includes('table-row')) {
          blocks[blocks.length - 1].cssClass += ' table-last';
        }

        i = tableCloseIdx + 1;
        addGapLines(blockEnd);
        continue;
      }

      // === Blockquotes: split into child blocks ===
      if (token.type === 'blockquote_open') {
        const bqCloseIdx = findClose(i);
        let j = i + 1;
        let hasChildren = false;
        while (j < bqCloseIdx) {
          if (tokens[j].nesting === -1 || !tokens[j].map) { j++; continue; }
          hasChildren = true;
          const childMap = tokens[j].map;
          let childCloseIdx = j;
          if (tokens[j].nesting === 1) childCloseIdx = findClose(j);
          addGapLines(childMap[0]);
          const childTokens = tokens.slice(j, childCloseIdx + 1);
          const childHtml = '<blockquote>' +
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
          const bqTokens = tokens.slice(i, bqCloseIdx + 1);
          blocks.push({
            startLine: blockStart + 1, endLine: blockEnd,
            html: md.renderer.render(bqTokens, md.options, {}),
            isEmpty: false
          });
          coveredUpTo = blockEnd;
        }
        i = bqCloseIdx + 1;
        addGapLines(blockEnd);
        continue;
      }

      // === Default: render as single block ===
      let closeIdx = i;
      if (token.nesting === 1) closeIdx = findClose(i);

      const blockTokens = tokens.slice(i, closeIdx + 1);
      let html;
      try {
        html = md.renderer.render(blockTokens, md.options, {});
      } catch (e) {
        html = escapeHtml(blockTokens.map(t => t.content || '').join(''));
      }

      blocks.push({
        startLine: blockStart + 1, endLine: blockEnd,
        html: html, isEmpty: false
      });

      i = closeIdx + 1;
      coveredUpTo = blockEnd;
    }

    addGapLines(totalLines);
    return blocks;
  }

  // ===== Utility Functions =====
  function processTaskLists(html) {
    return html.replace(
      /(<li[^>]*class="task-list-item"[^>]*>)\s*<p>\[([ x])\]\s*/gi,
      function(match, liTag, checked) {
        const checkbox = checked === 'x'
          ? '<input type="checkbox" checked disabled>'
          : '<input type="checkbox" disabled>';
        return liTag + '<p>' + checkbox;
      }
    ).replace(
      /(<li[^>]*class="task-list-item"[^>]*>)\[([ x])\]\s*/gi,
      function(match, liTag, checked) {
        const checkbox = checked === 'x'
          ? '<input type="checkbox" checked disabled>'
          : '<input type="checkbox" disabled>';
        return liTag + checkbox;
      }
    );
  }

  function rewriteImageSrcs(html) {
    return html.replace(/(<img\s[^>]*src=")([^"]+)(")/gi, function(match, pre, src, post) {
      if (/^https?:\/\/|^data:|^\//.test(src)) return match;
      return pre + '/files/' + src + post;
    });
  }

  function escapeHtml(str) {
    return str.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
  }

  function formatTime(isoStr) {
    if (!isoStr) return '';
    const d = new Date(isoStr);
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }

  function getFileByPath(path) {
    return files.find(f => f.path === path);
  }

  // ===== File Tree Sidebar =====
  var activeTreePath = null;
  var treeObserver = null;
  var treeFolderState = {}; // { 'src': true, 'src/components': false } — true = collapsed

  function buildFileTree(fileList) {
    // Build a nested tree from flat paths
    var root = { children: {}, files: [] };
    for (var i = 0; i < fileList.length; i++) {
      var f = fileList[i];
      var parts = f.path.split('/');
      var node = root;
      for (var j = 0; j < parts.length - 1; j++) {
        var dirName = parts[j];
        if (!node.children[dirName]) {
          node.children[dirName] = { children: {}, files: [] };
        }
        node = node.children[dirName];
      }
      node.files.push(f);
    }
    return root;
  }

  function collapseCommonPrefixes(tree) {
    // Collapse single-child directories: src/ -> components/ -> Foo.tsx becomes src/components/
    var dirs = Object.keys(tree.children);
    var result = { children: {}, files: tree.files };
    for (var i = 0; i < dirs.length; i++) {
      var name = dirs[i];
      var child = tree.children[name];
      // Recursively collapse child first
      child = collapseCommonPrefixes(child);
      // If child has exactly one subdirectory and no files, merge
      var childDirs = Object.keys(child.children);
      while (childDirs.length === 1 && child.files.length === 0) {
        name = name + '/' + childDirs[0];
        child = child.children[childDirs[0]];
        child = collapseCommonPrefixes(child);
        childDirs = Object.keys(child.children);
      }
      result.children[name] = child;
    }
    return result;
  }

  function renderFileTree() {
    var panel = document.getElementById('fileTreePanel');
    if (files.length <= 1 && session.mode !== 'git') {
      panel.style.display = 'none';
      return;
    }
    panel.style.display = '';

    // Stats
    var totalAdd = 0, totalDel = 0;
    for (var i = 0; i < files.length; i++) { totalAdd += files[i].additions; totalDel += files[i].deletions; }
    var statsEl = document.getElementById('fileTreeStats');
    statsEl.innerHTML =
      '<span>' + files.length + '</span>' +
      (totalAdd ? ' <span class="tree-stat-add">+' + totalAdd + '</span>' : '') +
      (totalDel ? ' <span class="tree-stat-del">-' + totalDel + '</span>' : '');

    // Collapse/expand all button
    var existingBtn = document.querySelector('.file-tree-collapse-btn');
    if (existingBtn) existingBtn.remove();
    if (files.length > 1) {
      var collapseBtn = document.createElement('button');
      collapseBtn.className = 'file-tree-collapse-btn';
      collapseBtn.title = 'Collapse all files';
      // Stacked chevron SVG
      collapseBtn.innerHTML = '<svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor"><path d="M4.22 3.22a.75.75 0 0 1 1.06 0L8 5.94l2.72-2.72a.75.75 0 1 1 1.06 1.06l-3.25 3.25a.75.75 0 0 1-1.06 0L4.22 4.28a.75.75 0 0 1 0-1.06zm0 5a.75.75 0 0 1 1.06 0L8 10.94l2.72-2.72a.75.75 0 1 1 1.06 1.06l-3.25 3.25a.75.75 0 0 1-1.06 0L4.22 9.28a.75.75 0 0 1 0-1.06z"/></svg>';
      collapseBtn.addEventListener('click', function() {
        var anyExpanded = files.some(function(f) { return !f.collapsed; });
        for (var i = 0; i < files.length; i++) {
          files[i].collapsed = anyExpanded;
        }
        var sections = document.querySelectorAll('.file-section');
        for (var i = 0; i < sections.length; i++) {
          sections[i].open = !anyExpanded;
        }
        collapseBtn.title = anyExpanded ? 'Expand all files' : 'Collapse all files';
        collapseBtn.classList.toggle('all-collapsed', anyExpanded);
      });
      var headerEl = document.querySelector('.file-tree-header');
      headerEl.appendChild(collapseBtn);
    }

    // Build and render tree
    var tree = buildFileTree(files);
    tree = collapseCommonPrefixes(tree);
    var body = document.getElementById('fileTreeBody');
    body.innerHTML = '';
    renderTreeNode(body, tree, 0, '');

    // Set up intersection observer for active file tracking
    setupTreeObserver();
  }

  function fileStatusIcon(status) {
    // GitHub-style: document icon with colored +/- badge
    var doc = '<path fill-rule="evenodd" d="M3.75 1.5a.25.25 0 0 0-.25.25v12.5c0 .138.112.25.25.25h8.5a.25.25 0 0 0 .25-.25V6H9.75A1.75 1.75 0 0 1 8 4.25V1.5H3.75zm5.75.56v2.19c0 .138.112.25.25.25h2.19L9.5 2.06zM2 1.75C2 .784 2.784 0 3.75 0h5.086c.464 0 .909.184 1.237.513l3.414 3.414c.329.328.513.773.513 1.237v8.086A1.75 1.75 0 0 1 12.25 15h-8.5A1.75 1.75 0 0 1 2 13.25V1.75z"/>';
    if (status === 'added' || status === 'untracked') {
      return '<svg class="tree-file-status-icon added" viewBox="0 0 16 16">' + doc +
        '<rect x="8" y="8" width="7" height="7" rx="1.5" fill="var(--green)"/>' +
        '<path d="M11.5 10v1.5H13v1h-1.5V14h-1v-1.5H9v-1h1.5V10z" fill="var(--bg-secondary)"/></svg>';
    }
    if (status === 'deleted') {
      return '<svg class="tree-file-status-icon deleted" viewBox="0 0 16 16">' + doc +
        '<rect x="8" y="8" width="7" height="7" rx="1.5" fill="var(--red)"/>' +
        '<path d="M9.5 11.5h4v1h-4z" fill="var(--bg-secondary)"/></svg>';
    }
    if (status === 'modified') {
      return '<svg class="tree-file-status-icon modified" viewBox="0 0 16 16">' + doc +
        '<circle cx="11.5" cy="11.5" r="3.5" fill="var(--yellow)"/>' +
        '<circle cx="11.5" cy="11.5" r="1.5" fill="var(--bg-secondary)"/>' +
        '</svg>';
    }
    // renamed or other
    return '<svg class="tree-file-status-icon" viewBox="0 0 16 16">' + doc + '</svg>';
  }

  function renderTreeNode(container, node, depth, pathPrefix) {
    var folderSVG = '<svg viewBox="0 0 16 16" fill="currentColor"><path d="M1.75 1A1.75 1.75 0 0 0 0 2.75v10.5C0 14.216.784 15 1.75 15h12.5A1.75 1.75 0 0 0 16 13.25v-8.5A1.75 1.75 0 0 0 14.25 3H7.5a.25.25 0 0 1-.2-.1l-.9-1.2C6.07 1.26 5.55 1 5 1H1.75Z"/></svg>';

    // Render subdirectories
    var dirs = Object.keys(node.children).sort();
    for (var d = 0; d < dirs.length; d++) {
      var dirName = dirs[d];
      var fullPath = pathPrefix ? pathPrefix + '/' + dirName : dirName;
      var child = node.children[dirName];
      var isCollapsed = treeFolderState[fullPath] === true;

      var folder = document.createElement('div');
      folder.className = 'tree-folder' + (isCollapsed ? ' collapsed' : '');
      folder.dataset.folderPath = fullPath;

      var row = document.createElement('div');
      row.className = 'tree-folder-row';
      row.style.paddingLeft = (8 + depth * 16) + 'px';

      row.innerHTML =
        '<span class="tree-folder-chevron">&#9662;</span>' +
        '<span class="tree-folder-icon">' + folderSVG + '</span>' +
        '<span class="tree-folder-name">' + escapeHtml(dirName) + '</span>';

      (function(fp, folderEl) {
        row.addEventListener('click', function() {
          treeFolderState[fp] = !treeFolderState[fp];
          folderEl.classList.toggle('collapsed');
        });
      })(fullPath, folder);

      folder.appendChild(row);

      var childContainer = document.createElement('div');
      childContainer.className = 'tree-folder-children';
      renderTreeNode(childContainer, child, depth + 1, fullPath);
      folder.appendChild(childContainer);

      container.appendChild(folder);
    }

    // Render files
    var sortedFiles = node.files.slice().sort(function(a, b) { return a.path.localeCompare(b.path); });
    for (var fi = 0; fi < sortedFiles.length; fi++) {
      var f = sortedFiles[fi];
      var fileName = f.path.split('/').pop();
      var fileEl = document.createElement('div');
      fileEl.className = 'tree-file' + (activeTreePath === f.path ? ' active' : '') + (f.viewed ? ' viewed' : '');
      fileEl.dataset.treePath = f.path;
      fileEl.style.paddingLeft = (24 + depth * 16) + 'px';

      var innerHtml =
        '<span class="tree-file-icon">' + fileStatusIcon(f.status) + '</span>' +
        '<span class="tree-file-name">' + escapeHtml(fileName) + '</span>';

      if (f.viewed) {
        innerHtml += '<span class="tree-viewed-check" title="Viewed">&#10003;</span>';
      }
      if (f.comments.length > 0) {
        innerHtml += '<span class="tree-comment-badge">' + f.comments.length + '</span>';
      }

      fileEl.innerHTML = innerHtml;

      (function(path) {
        fileEl.addEventListener('click', function() {
          scrollToFile(path);
        });
      })(f.path);

      container.appendChild(fileEl);
    }
  }

  function updateTreeActive(filePath) {
    if (filePath === activeTreePath) return;
    activeTreePath = filePath;
    var allFiles = document.querySelectorAll('.tree-file');
    for (var i = 0; i < allFiles.length; i++) {
      allFiles[i].classList.toggle('active', allFiles[i].dataset.treePath === filePath);
    }
    // Scroll active item into view within the tree panel
    var activeEl = document.querySelector('.tree-file.active');
    if (activeEl) {
      var panel = document.getElementById('fileTreeBody');
      var rect = activeEl.getBoundingClientRect();
      var panelRect = panel.getBoundingClientRect();
      if (rect.top < panelRect.top || rect.bottom > panelRect.bottom) {
        activeEl.scrollIntoView({ block: 'nearest' });
      }
    }
  }

  function updateTreeCommentBadges() {
    var allFiles = document.querySelectorAll('.tree-file');
    for (var i = 0; i < allFiles.length; i++) {
      var el = allFiles[i];
      var path = el.dataset.treePath;
      var file = getFileByPath(path);
      if (!file) continue;
      var badge = el.querySelector('.tree-comment-badge');
      if (file.comments.length > 0) {
        if (badge) {
          badge.textContent = file.comments.length;
        } else {
          badge = document.createElement('span');
          badge.className = 'tree-comment-badge';
          badge.textContent = file.comments.length;
          el.appendChild(badge);
        }
      } else if (badge) {
        badge.remove();
      }
    }
  }

  function updateTreeViewedState() {
    var allFiles = document.querySelectorAll('.tree-file');
    for (var i = 0; i < allFiles.length; i++) {
      var el = allFiles[i];
      var path = el.dataset.treePath;
      var file = getFileByPath(path);
      if (!file) continue;
      el.classList.toggle('viewed', !!file.viewed);
      var check = el.querySelector('.tree-viewed-check');
      if (file.viewed) {
        if (!check) {
          check = document.createElement('span');
          check.className = 'tree-viewed-check';
          check.title = 'Viewed';
          check.textContent = '\u2713';
          // Insert before comment badge if present, else append
          var badge = el.querySelector('.tree-comment-badge');
          if (badge) el.insertBefore(check, badge);
          else el.appendChild(check);
        }
      } else if (check) {
        check.remove();
      }
    }
  }

  function setupTreeObserver() {
    if (treeObserver) treeObserver.disconnect();
    var sections = document.querySelectorAll('.file-section[id]');
    if (sections.length === 0) return;

    treeObserver = new IntersectionObserver(function(entries) {
      // Find the topmost visible section
      var bestPath = null;
      var bestTop = Infinity;
      for (var i = 0; i < entries.length; i++) {
        if (entries[i].isIntersecting) {
          var top = entries[i].boundingClientRect.top;
          if (top < bestTop) {
            bestTop = top;
            bestPath = entries[i].target.id.replace('file-section-', '');
          }
        }
      }
      if (bestPath) updateTreeActive(bestPath);
    }, { rootMargin: '-60px 0px -70% 0px' });

    for (var i = 0; i < sections.length; i++) {
      treeObserver.observe(sections[i]);
    }
  }

  // Alias for compatibility (called from submitComment, deleteComment, SSE handler)
  function renderFileSummary() {
    updateTreeCommentBadges();
  }

  function scrollToFile(filePath) {
    var sectionEl = document.getElementById('file-section-' + filePath);
    if (!sectionEl) return;
    // Uncollapse if collapsed
    var file = getFileByPath(filePath);
    if (file) file.collapsed = false;
    sectionEl.open = true;
    var headerEl = sectionEl.querySelector('.file-header');
    var mainHeader = document.querySelector('.header');
    var offset = (mainHeader ? mainHeader.offsetHeight : 49) + 8;
    var y = headerEl.getBoundingClientRect().top + window.scrollY - offset;
    window.scrollTo({ top: y, behavior: 'smooth' });
    updateTreeActive(filePath);
  }

  // ===== Render All File Sections =====
  function renderAllFiles() {
    const container = document.getElementById('filesContainer');
    container.innerHTML = '';

    for (const f of files) {
      container.appendChild(renderFileSection(f));
    }

    // Render mermaid diagrams
    renderMermaidBlocks();

    // Re-attach intersection observer for file tree active tracking
    setupTreeObserver();
    rebuildNavList();
  }

  function rebuildNavList() {
    navElements = Array.from(document.querySelectorAll('.kb-nav'));
  }

  // Re-render only a single file section (preserves scroll position)
  function renderFileByPath(filePath) {
    const file = getFileByPath(filePath);
    if (!file) return;
    const oldSection = document.getElementById('file-section-' + file.path);
    if (!oldSection) { renderAllFiles(); return; }
    oldSection.replaceWith(renderFileSection(file));
    rebuildNavList();
  }

  function renderFileSection(file) {
    // Use native <details>/<summary> for collapse — browser handles scroll natively
    const section = document.createElement('details');
    section.className = 'file-section';
    section.id = 'file-section-' + file.path;
    if (!file.collapsed) section.open = true;

    const header = document.createElement('summary');
    header.className = 'file-header';

    // Intercept click to fix scroll BEFORE collapse (avoids flicker)
    header.addEventListener('click', function(e) {
      if (e.target.closest('.file-header-toggle') || e.target.closest('.file-header-viewed')) {
        e.preventDefault();
        return;
      }
      if (section.open) {
        // Collapsing: correct scroll before content disappears
        e.preventDefault();
        if (section.getBoundingClientRect().top < 0) {
          section.scrollIntoView({ behavior: 'instant' });
        }
        section.open = false;
        file.collapsed = true;
      }
      // Expanding: let native <details> handle it
      header.blur(); // prevent <summary> from trapping keyboard focus
    });
    section.addEventListener('toggle', function() {
      file.collapsed = !section.open;
    });

    const dirParts = file.path.split('/');
    const fileName = dirParts.pop();
    const dirPath = dirParts.length > 0 ? dirParts.join('/') + '/' : '';

    let badgeLabel = file.status.charAt(0).toUpperCase() + file.status.slice(1);
    if (file.status === 'untracked') badgeLabel = 'New';
    if (file.status === 'added') badgeLabel = 'New File';

    header.innerHTML =
      '<div class="file-header-chevron"><svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor"><path d="M12.78 5.22a.749.749 0 0 1 0 1.06l-4.25 4.25a.749.749 0 0 1-1.06 0L3.22 6.28a.749.749 0 1 1 1.06-1.06L8 8.939l3.72-3.719a.749.749 0 0 1 1.06 0Z"/></svg></div>' +
      '<svg class="file-header-icon" viewBox="0 0 16 16" fill="var(--fg-dimmed)"><path fill-rule="evenodd" d="M3.75 1.5a.25.25 0 0 0-.25.25v12.5c0 .138.112.25.25.25h8.5a.25.25 0 0 0 .25-.25V6H9.75A1.75 1.75 0 0 1 8 4.25V1.5H3.75zm5.75.56v2.19c0 .138.112.25.25.25h2.19L9.5 2.06zM2 1.75C2 .784 2.784 0 3.75 0h5.086c.464 0 .909.184 1.237.513l3.414 3.414c.329.328.513.773.513 1.237v8.086A1.75 1.75 0 0 1 12.25 15h-8.5A1.75 1.75 0 0 1 2 13.25V1.75z"/></svg>' +
      '<span class="file-header-name"><span class="dir">' + escapeHtml(dirPath) + '</span>' + escapeHtml(fileName) + '</span>' +
      '<span class="file-header-badge ' + escapeHtml(file.status) + '">' + escapeHtml(badgeLabel) + '</span>' +
      (file.additions || file.deletions ? '<span class="file-header-stats">' +
        (file.additions ? '<span class="add">+' + file.additions + '</span>' : '') +
        (file.deletions ? '<span class="del">-' + file.deletions + '</span>' : '') +
      '</span>' : '') +
      (file.comments.length > 0 ? '<span class="file-header-comment-count">' +
        '<svg viewBox="0 0 16 16" fill="currentColor"><path d="M1 2.75C1 1.784 1.784 1 2.75 1h10.5c.966 0 1.75.784 1.75 1.75v7.5A1.75 1.75 0 0 1 13.25 12H9.06l-2.573 2.573A1.458 1.458 0 0 1 4 13.543V12H2.75A1.75 1.75 0 0 1 1 10.25Zm1.75-.25a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h2a.75.75 0 0 1 .75.75v2.19l2.72-2.72a.749.749 0 0 1 .53-.22h4.5a.25.25 0 0 0 .25-.25v-7.5a.25.25 0 0 0-.25-.25Z"/></svg>' +
        file.comments.length + '</span>' : '');

    // Add document/diff toggle for markdown files that have diff hunks
    if (file.fileType === 'markdown' && file.diffHunks && file.diffHunks.length > 0) {
      const toggle = document.createElement('div');
      toggle.className = 'file-header-toggle';
      toggle.innerHTML =
        '<button class="toggle-btn' + (file.viewMode === 'document' ? ' active' : '') + '" data-mode="document">Document</button>' +
        '<button class="toggle-btn' + (file.viewMode === 'diff' ? ' active' : '') + '" data-mode="diff">Diff</button>';
      toggle.addEventListener('click', function(e) {
        const btn = e.target.closest('.toggle-btn');
        if (!btn) return;
        e.preventDefault(); // Don't toggle the <details>
        if (activeForm && activeForm.filePath === file.path) {
          clearDraft();
          activeForm = null;
          activeFilePath = null;
          selectionStart = null;
          selectionEnd = null;
        }
        file.viewMode = btn.dataset.mode;
        renderFileByPath(file.path);
      });
      header.appendChild(toggle);
    }

    // Viewed checkbox
    var viewedLabel = document.createElement('label');
    viewedLabel.className = 'file-header-viewed';
    viewedLabel.title = 'Viewed';
    viewedLabel.innerHTML = '<input type="checkbox"' + (file.viewed ? ' checked' : '') + '><span>Viewed</span>';
    viewedLabel.addEventListener('click', function(e) {
      e.stopPropagation(); // Don't toggle the <details>
    });
    viewedLabel.querySelector('input').addEventListener('change', function() {
      toggleViewed(file.path);
    });
    header.appendChild(viewedLabel);

    section.appendChild(header);

    // File body
    const body = document.createElement('div');
    body.className = 'file-body';

    var showDiff = file.viewMode === 'diff' || file.fileType === 'code';

    if (file.status === 'deleted' && (!file.diffHunks || file.diffHunks.length === 0)) {
      const deleted = document.createElement('div');
      deleted.className = 'diff-deleted-placeholder';
      deleted.textContent = 'This file was deleted.';
      body.appendChild(deleted);
    } else if (showDiff && file.diffTooLarge && !file.diffLoaded) {
      var diffLineCount = 0;
      if (file.diffHunks) {
        for (var h = 0; h < file.diffHunks.length; h++) {
          diffLineCount += (file.diffHunks[h].Lines || []).length;
        }
      }
      const placeholder = document.createElement('div');
      placeholder.className = 'diff-large-placeholder';
      placeholder.innerHTML =
        '<p>Large diff not rendered by default.</p>' +
        '<p class="diff-large-meta">' + diffLineCount.toLocaleString() + ' lines changed</p>' +
        '<button class="btn btn-sm">Load diff</button>';
      placeholder.querySelector('button').addEventListener('click', function() {
        file.diffLoaded = true;
        renderFileByPath(file.path);
      });
      body.appendChild(placeholder);
    } else if (showDiff) {
      body.appendChild(renderDiffHunks(file));
    } else {
      body.appendChild(renderDocumentView(file));
    }

    section.appendChild(body);
    return section;
  }

  // ===== Document View (Markdown) =====
  function renderDocumentView(file) {
    const container = document.createElement('div');
    container.className = 'document-wrapper';
    if (!file.lineBlocks) return container;

    const commentsMap = buildCommentsMap(file.comments);
    const resolvedMap = buildResolvedMap(file);

    for (let bi = 0; bi < file.lineBlocks.length; bi++) {
      const block = file.lineBlocks[bi];

      const lineBlockEl = document.createElement('div');
      lineBlockEl.className = 'line-block kb-nav';
      lineBlockEl.dataset.blockIndex = bi;
      lineBlockEl.dataset.startLine = block.startLine;
      lineBlockEl.dataset.endLine = block.endLine;
      lineBlockEl.dataset.filePath = file.path;

      const blockComments = getCommentsForBlock(block, commentsMap);
      if (blockComments.length > 0) lineBlockEl.classList.add('has-comment');

      // Selection highlight (during drag or when form is open)
      if (activeFilePath === file.path && selectionStart !== null && selectionEnd !== null) {
        if (block.startLine >= selectionStart && block.endLine <= selectionEnd) {
          lineBlockEl.classList.add('selected');
        }
      }

      // Track hover for keyboard shortcuts
      (function(fp, idx, el) {
        lineBlockEl.addEventListener('mouseenter', function() {
          focusedFilePath = fp;
          focusedBlockIndex = idx;
          focusedElement = el;
        });
      })(file.path, bi, lineBlockEl);

      // Keyboard focus
      if (focusedFilePath === file.path && focusedBlockIndex === bi) {
        lineBlockEl.classList.add('focused');
      }

      // Line number gutter
      const gutter = document.createElement('div');
      gutter.className = 'line-gutter';

      const lineNum = document.createElement('span');
      lineNum.className = 'line-num';
      lineNum.textContent = block.startLine;
      gutter.appendChild(lineNum);

      // Comment gutter (separate column between line numbers and content)
      const commentGutter = document.createElement('div');
      commentGutter.className = 'line-comment-gutter';
      commentGutter.dataset.startLine = block.startLine;
      commentGutter.dataset.endLine = block.endLine;
      commentGutter.dataset.filePath = file.path;

      // Drag indicators: + at endpoints, blue line between
      if (dragState && dragState.filePath === file.path && selectionStart !== null && selectionEnd !== null) {
        var isAnchorBlock = block.startLine <= dragState.anchorEndLine && block.endLine >= dragState.anchorStartLine;
        var isCurrentBlock = block.startLine <= dragState.currentEndLine && block.endLine >= dragState.currentStartLine;
        var inRange = block.startLine >= selectionStart && block.endLine <= selectionEnd;
        if (isAnchorBlock || isCurrentBlock) commentGutter.classList.add('drag-endpoint');
        if (inRange) {
          commentGutter.classList.add('drag-range');
          if (block.startLine === selectionStart) commentGutter.classList.add('drag-range-start');
          if (block.endLine === selectionEnd) commentGutter.classList.add('drag-range-end');
        }
      }

      const lineAdd = document.createElement('span');
      lineAdd.className = 'line-add';
      lineAdd.textContent = '+';
      commentGutter.appendChild(lineAdd);
      commentGutter.addEventListener('mousedown', handleGutterMouseDown);

      // Content
      const content = document.createElement('div');
      let contentClasses = 'line-content';
      if (block.isEmpty) contentClasses += ' empty-line';
      if (block.cssClass) contentClasses += ' ' + block.cssClass;
      content.className = contentClasses;
      let html = block.html;
      html = processTaskLists(html);
      html = rewriteImageSrcs(html);
      content.innerHTML = html;

      lineBlockEl.appendChild(gutter);
      lineBlockEl.appendChild(commentGutter);
      lineBlockEl.appendChild(content);
      container.appendChild(lineBlockEl);

      // Comments after block
      for (const comment of blockComments) {
        container.appendChild(createCommentElement(comment, file.path));
      }

      // Resolved comments
      const blockResolved = getResolvedForBlock(block, resolvedMap);
      for (const rc of blockResolved) {
        container.appendChild(createResolvedElement(rc));
      }

      // Comment form
      if (activeForm && activeForm.filePath === file.path && !activeForm.editingId && activeForm.afterBlockIndex === bi) {
        container.appendChild(createCommentForm());
      }
    }

    return container;
  }

  // ===== Diff Hunk View (Code Files) =====
  function renderDiffHunks(file) {
    if (diffMode === 'split') return renderDiffSplit(file);
    return renderDiffUnified(file);
  }

  // ===== Diff Gutter Drag (multi-line comment selection) =====
  var diffDragState = null; // { filePath, side, anchorLine, currentLine }

  // Tag a diff line element with data attributes for drag detection + keyboard nav
  // For split mode, navEl (the row) gets kb-nav; el (the side) gets data attrs for drag.
  function tagDiffLine(el, filePath, lineNum, side, navEl) {
    el.dataset.diffFilePath = filePath;
    el.dataset.diffLineNum = lineNum;
    el.dataset.diffSide = side || '';
    // In split mode, kb-nav goes on the row; in unified, on the line itself
    var nav = navEl || el;
    if (!nav.classList.contains('kb-nav')) {
      nav.classList.add('kb-nav');
      nav.dataset.diffFilePath = filePath;
      nav.dataset.diffLineNum = lineNum;
      nav.dataset.diffSide = side || '';
    }
    el.addEventListener('mouseenter', function() {
      focusedElement = nav;
      focusedFilePath = filePath;
      focusedBlockIndex = null;
    });
  }

  // Creates a dedicated comment gutter column element with a + button.
  // Returns the element to insert between line numbers and content.
  function makeDiffCommentGutter(filePath, lineNum, side, visualIdx) {
    const col = document.createElement('div');
    col.className = 'diff-comment-gutter';
    if (!lineNum) return col; // empty placeholder for lines without numbers

    // During drag, show + at anchor and current line, blue line between
    var sideMatch = diffMode === 'split' ? diffDragState && diffDragState.side === (side || '') : true;
    if (diffDragState && diffDragState.filePath === filePath && sideMatch && selectionStart !== null && selectionEnd !== null) {
      var isAnchor, isCurrent, inRange, isRangeStart, isRangeEnd;
      if (diffMode !== 'split' && visualIdx !== undefined && unifiedVisualStart !== null) {
        // Unified mode: use visual indices (old/new line numbers are in different spaces)
        isAnchor = visualIdx === diffDragState.anchorVisualIdx;
        isCurrent = visualIdx === diffDragState.currentVisualIdx;
        inRange = visualIdx >= unifiedVisualStart && visualIdx <= unifiedVisualEnd;
        isRangeStart = visualIdx === unifiedVisualStart;
        isRangeEnd = visualIdx === unifiedVisualEnd;
      } else {
        isAnchor = lineNum === diffDragState.anchorLine;
        isCurrent = lineNum === diffDragState.currentLine;
        inRange = lineNum >= selectionStart && lineNum <= selectionEnd;
        isRangeStart = lineNum === selectionStart;
        isRangeEnd = lineNum === selectionEnd;
      }
      if (isAnchor || isCurrent) col.classList.add('drag-endpoint');
      if (inRange) {
        col.classList.add('drag-range');
        if (isRangeStart) col.classList.add('drag-range-start');
        if (isRangeEnd) col.classList.add('drag-range-end');
      }
    }

    const btn = document.createElement('button');
    btn.className = 'diff-comment-btn';
    btn.textContent = '+';
    btn.dataset.filePath = filePath;
    btn.dataset.lineNum = lineNum;
    btn.dataset.side = side || '';
    if (visualIdx !== undefined) btn.dataset.visualIdx = visualIdx;
    btn.addEventListener('mousedown', function(e) {
      e.preventDefault();
      e.stopPropagation();
      const fp = this.dataset.filePath;
      const ln = parseInt(this.dataset.lineNum);
      const s = this.dataset.side || '';
      const vi = this.dataset.visualIdx !== undefined ? parseInt(this.dataset.visualIdx) : undefined;

      diffDragState = { filePath: fp, side: s, anchorLine: ln, currentLine: ln, anchorVisualIdx: vi, currentVisualIdx: vi };
      activeFilePath = fp;
      selectionStart = ln;
      selectionEnd = ln;
      if (diffMode !== 'split' && vi !== undefined) {
        unifiedVisualStart = vi;
        unifiedVisualEnd = vi;
      }
      setActiveForm(null);
      renderFileByPath(fp);

      document.body.classList.add('dragging');
      document.addEventListener('mousemove', handleDiffDragMove);
      document.addEventListener('mouseup', handleDiffDragEnd);
    });
    col.appendChild(btn);
    return col;
  }

  function handleDiffDragMove(e) {
    if (!diffDragState) return;
    const el = document.elementFromPoint(e.clientX, e.clientY);
    if (!el) return;
    // Find the nearest diff line with data attributes
    const diffLine = el.closest('[data-diff-line-num]');
    if (!diffLine || diffLine.dataset.diffFilePath !== diffDragState.filePath) return;
    // In split mode, restrict to the same side; in unified, allow crossing add/del
    if (diffMode === 'split') {
      if ((diffLine.dataset.diffSide || '') !== diffDragState.side) return;
    }

    const hoverLine = parseInt(diffLine.dataset.diffLineNum);
    if (isNaN(hoverLine) || hoverLine === 0) return;

    diffDragState.currentLine = hoverLine;
    selectionStart = Math.min(diffDragState.anchorLine, hoverLine);
    selectionEnd = Math.max(diffDragState.anchorLine, hoverLine);

    // Unified mode: track visual indices for cross-number-space drag
    if (diffMode !== 'split' && diffLine.dataset.diffVisualIdx !== undefined) {
      var hoverVisualIdx = parseInt(diffLine.dataset.diffVisualIdx);
      diffDragState.currentVisualIdx = hoverVisualIdx;
      unifiedVisualStart = Math.min(diffDragState.anchorVisualIdx, hoverVisualIdx);
      unifiedVisualEnd = Math.max(diffDragState.anchorVisualIdx, hoverVisualIdx);
    }
    updateDragSelectionVisuals(diffDragState.filePath);
  }

  function handleDiffDragEnd() {
    document.removeEventListener('mousemove', handleDiffDragMove);
    document.removeEventListener('mouseup', handleDiffDragEnd);
    document.body.classList.remove('dragging');

    if (!diffDragState) return;
    const rangeStart = Math.min(diffDragState.anchorLine, diffDragState.currentLine);
    const rangeEnd = Math.max(diffDragState.anchorLine, diffDragState.currentLine);

    setActiveForm({
      filePath: diffDragState.filePath,
      afterBlockIndex: null,
      startLine: rangeStart,
      endLine: rangeEnd,
      editingId: null,
      side: diffDragState.side,
    });
    activeFilePath = diffDragState.filePath;
    selectionStart = rangeStart;
    selectionEnd = rangeEnd;
    var fp = diffDragState.filePath;
    diffDragState = null;
    unifiedVisualStart = null;
    unifiedVisualEnd = null;
    renderFileByPath(fp);
    focusCommentTextarea();
  }

  // Helper: render hunk spacer
  // prevIdx/nextIdx are indices into file.diffHunks so we can merge on expand
  function renderDiffSpacer(prevHunk, nextHunk, file, prevIdx, nextIdx) {
    const prevNewEnd = prevHunk.NewStart + prevHunk.NewCount;
    const prevOldEnd = prevHunk.OldStart + prevHunk.OldCount;
    const gap = nextHunk.NewStart - prevNewEnd;
    if (gap <= 0) return null;
    const spacer = document.createElement('div');
    spacer.className = 'diff-spacer';
    spacer.innerHTML =
      '<svg viewBox="0 0 16 16" fill="currentColor"><path d="M8 2a.75.75 0 0 1 .75.75v4.5h4.5a.75.75 0 0 1 0 1.5h-4.5v4.5a.75.75 0 0 1-1.5 0v-4.5h-4.5a.75.75 0 0 1 0-1.5h4.5v-4.5A.75.75 0 0 1 8 2z"/></svg>' +
      'Expand ' + gap + ' unchanged line' + (gap === 1 ? '' : 's');

    spacer.addEventListener('click', function() {
      if (!file.content) return;
      var contentLines = file.content.split('\n');

      // Build context lines to bridge the gap
      var contextLines = [];
      for (var i = 0; i < gap; i++) {
        var newLineNum = prevNewEnd + i;
        var oldLineNum = prevOldEnd + i;
        var text = newLineNum <= contentLines.length ? contentLines[newLineNum - 1] : '';
        contextLines.push({ Type: 'context', Content: text, OldNum: oldLineNum, NewNum: newLineNum });
      }

      // Merge: prev hunk + context lines + next hunk → single hunk
      var hunks = file.diffHunks;
      var merged = {
        OldStart: hunks[prevIdx].OldStart,
        NewStart: hunks[prevIdx].NewStart,
        Header: hunks[prevIdx].Header,
        Lines: hunks[prevIdx].Lines.concat(contextLines, hunks[nextIdx].Lines)
      };
      merged.OldCount = (hunks[nextIdx].OldStart + hunks[nextIdx].OldCount) - merged.OldStart;
      merged.NewCount = (hunks[nextIdx].NewStart + hunks[nextIdx].NewCount) - merged.NewStart;

      // Replace prevIdx with merged, remove nextIdx
      hunks.splice(prevIdx, 2, merged);

      // Re-render from data model so all lines get proper interaction
      renderFileByPath(file.path);
    });

    return spacer;
  }

  // Helper: render hunk header
  function renderDiffHunkHeader(hunk) {
    const hunkHeader = document.createElement('div');
    hunkHeader.className = 'diff-hunk-header';
    hunkHeader.innerHTML = '<div class="hunk-gutter"></div><span class="hunk-text">' + escapeHtml(hunk.Header) + '</span>';
    return hunkHeader;
  }

  // Helper: append comments for a given line number and side
  function appendDiffComments(container, filePath, lineNum, side, commentsMap) {
    const key = lineNum + ':' + (side || '');
    const lineComments = commentsMap[key] || [];
    for (const comment of lineComments) {
      var el = createCommentElement(comment, filePath);
      if (side === 'old') el.classList.add('diff-comment-left');
      else el.classList.add('diff-comment-right');
      container.appendChild(el);
    }
  }

  // Helper: append comment form if it targets this line and side
  function appendDiffForm(container, filePath, lineNum, side) {
    const formSide = (activeForm && activeForm.side) || '';
    if (activeForm && activeForm.filePath === filePath && !activeForm.editingId &&
        activeForm.endLine === lineNum && formSide === (side || '')) {
      var el = createCommentForm();
      if (formSide === 'old') el.classList.add('diff-comment-left');
      else el.classList.add('diff-comment-right');
      container.appendChild(el);
    }
  }

  // ===== Unified diff (interleaved lines, single pane) =====
  function renderDiffUnified(file) {
    const container = document.createElement('div');
    container.className = 'diff-container unified';

    const hunks = file.diffHunks || [];
    if (hunks.length === 0) {
      container.innerHTML = '<div style="padding: 16px 24px; color: var(--fg-muted); font-style: italic;">No changes</div>';
      return container;
    }

    const commentsMap = buildDiffCommentsMap(file.comments);
    var visualIdx = 0; // sequential index for unified drag (old/new nums are different spaces)

    for (let hi = 0; hi < hunks.length; hi++) {
      const hunk = hunks[hi];

      if (hi > 0) {
        const spacer = renderDiffSpacer(hunks[hi - 1], hunk, file, hi - 1, hi);
        if (spacer) container.appendChild(spacer);
      }

      container.appendChild(renderDiffHunkHeader(hunk));

      for (const line of hunk.Lines) {
        const lineEl = document.createElement('div');
        lineEl.className = 'diff-line';
        if (line.Type === 'add') lineEl.classList.add('addition');
        if (line.Type === 'del') lineEl.classList.add('deletion');
        lineEl.dataset.diffVisualIdx = visualIdx;

        var commentLineNum = line.Type === 'del' ? line.OldNum : line.NewNum;
        var lineSide = line.Type === 'del' ? 'old' : '';
        var commentKey = commentLineNum + ':' + lineSide;
        const lineComments = commentsMap[commentKey] || [];
        if (lineComments.length > 0) lineEl.classList.add('has-comment');

        // Tag for drag detection and selection highlighting
        if (commentLineNum) {
          tagDiffLine(lineEl, file.path, commentLineNum, lineSide);
          if (activeFilePath === file.path) {
            // During drag: use visual indices (old/new line numbers are different spaces)
            if (diffDragState && unifiedVisualStart !== null && unifiedVisualEnd !== null &&
                visualIdx >= unifiedVisualStart && visualIdx <= unifiedVisualEnd) {
              lineEl.classList.add('selected');
            }
            // After drag (form open): filter by side with actual line numbers
            else if (!diffDragState && selectionStart !== null && selectionEnd !== null) {
              var formSide = activeForm ? (activeForm.side || '') : '';
              if (lineSide === formSide && commentLineNum >= selectionStart && commentLineNum <= selectionEnd) {
                lineEl.classList.add('selected');
              }
            }
          }
        }

        const gutter = document.createElement('div');
        gutter.className = 'diff-gutter';

        const oldNum = document.createElement('div');
        oldNum.className = 'diff-gutter-num';
        oldNum.textContent = line.OldNum || '';

        const newNum = document.createElement('div');
        newNum.className = 'diff-gutter-num';
        newNum.textContent = line.NewNum || '';

        gutter.appendChild(oldNum);
        gutter.appendChild(newNum);

        const commentGutter = makeDiffCommentGutter(file.path, commentLineNum, lineSide, visualIdx);

        const sign = document.createElement('div');
        sign.className = 'diff-gutter-sign';
        sign.textContent = line.Type === 'add' ? '+' : line.Type === 'del' ? '-' : '';

        const contentEl = document.createElement('div');
        contentEl.className = 'diff-content';
        const hlLine = highlightDiffLine(line.Content, line.Type === 'del' ? line.OldNum : line.NewNum, line.Type === 'del' ? 'old' : '', file.highlightCache, file.lang);
        contentEl.innerHTML = hlLine;

        lineEl.appendChild(gutter);
        lineEl.appendChild(commentGutter);
        lineEl.appendChild(sign);
        lineEl.appendChild(contentEl);
        container.appendChild(lineEl);

        appendDiffComments(container, file.path, commentLineNum, lineSide, commentsMap);
        appendDiffForm(container, file.path, commentLineNum, lineSide);
        visualIdx++;
      }
    }

    return container;
  }

  // ===== Split diff (side-by-side: old on left, new on right) =====
  function renderDiffSplit(file) {
    const container = document.createElement('div');
    container.className = 'diff-container split';

    const hunks = file.diffHunks || [];
    if (hunks.length === 0) {
      container.innerHTML = '<div style="padding: 16px 24px; color: var(--fg-muted); font-style: italic;">No changes</div>';
      return container;
    }

    const commentsMap = buildDiffCommentsMap(file.comments);

    for (let hi = 0; hi < hunks.length; hi++) {
      const hunk = hunks[hi];

      if (hi > 0) {
        const spacer = renderDiffSpacer(hunks[hi - 1], hunk, file, hi - 1, hi);
        if (spacer) container.appendChild(spacer);
      }

      container.appendChild(renderDiffHunkHeader(hunk));

      // Group hunk lines into segments: runs of context, or runs of del+add (change pairs)
      const segments = [];
      let i = 0;
      const lines = hunk.Lines;
      while (i < lines.length) {
        if (lines[i].Type === 'context') {
          segments.push({ type: 'context', lines: [lines[i]] });
          i++;
        } else {
          // Collect consecutive dels then adds
          const dels = [];
          const adds = [];
          while (i < lines.length && lines[i].Type === 'del') { dels.push(lines[i]); i++; }
          while (i < lines.length && lines[i].Type === 'add') { adds.push(lines[i]); i++; }
          segments.push({ type: 'change', dels: dels, adds: adds });
        }
      }

      for (const seg of segments) {
        if (seg.type === 'context') {
          const line = seg.lines[0];
          const row = makeSplitRow(
            { num: line.OldNum, content: line.Content, type: 'context' },
            { num: line.NewNum, content: line.Content, type: 'context' },
            file, commentsMap
          );
          container.appendChild(row.el);
          // Context lines: form appears where clicked (left or right),
          // but submitted comments always render on the right, like GitHub
          var ctxComments = [
            ...(commentsMap[line.OldNum + ':old'] || []),
            ...(commentsMap[line.NewNum + ':'] || [])
          ];
          for (var ci = 0; ci < ctxComments.length; ci++) {
            var el = createCommentElement(ctxComments[ci], file.path);
            el.classList.add('diff-comment-right');
            container.appendChild(el);
          }
          appendDiffForm(container, file.path, line.OldNum, 'old');
          appendDiffForm(container, file.path, line.NewNum, '');
        } else {
          const maxLen = Math.max(seg.dels.length, seg.adds.length);
          for (let j = 0; j < maxLen; j++) {
            const del = seg.dels[j] || null;
            const add = seg.adds[j] || null;
            const row = makeSplitRow(
              del ? { num: del.OldNum, content: del.Content, type: 'del' } : null,
              add ? { num: add.NewNum, content: add.Content, type: 'add' } : null,
              file, commentsMap
            );
            container.appendChild(row.el);
            // Comments for both sides (different keys)
            if (del) appendDiffComments(container, file.path, del.OldNum, 'old', commentsMap);
            if (add) appendDiffComments(container, file.path, add.NewNum, '', commentsMap);
            // Form: render for whichever side was clicked
            if (del) appendDiffForm(container, file.path, del.OldNum, 'old');
            if (add) appendDiffForm(container, file.path, add.NewNum, '');
          }
        }
      }
    }

    return container;
  }

  // Build one split row: left (old) side + right (new) side
  // left/right: { num, content, type } or null for empty
  function makeSplitRow(left, right, file, commentsMap) {
    const row = document.createElement('div');
    row.className = 'diff-split-row';

    // Left side
    const leftEl = document.createElement('div');
    leftEl.className = 'diff-split-side left';
    if (left && left.type === 'del') leftEl.classList.add('deletion');

    const leftNum = document.createElement('div');
    leftNum.className = 'diff-gutter-num';
    leftNum.textContent = left ? (left.num || '') : '';

    var leftCommentGutter;
    if (left && left.num) {
      leftCommentGutter = makeDiffCommentGutter(file.path, left.num, 'old');
      tagDiffLine(leftEl, file.path, left.num, 'old', row);
      const lComments = commentsMap[left.num + ':old'] || [];
      if (lComments.length > 0) leftEl.classList.add('has-comment');
      var selSide = diffDragState ? diffDragState.side : (activeForm ? activeForm.side : null);
      if (activeFilePath === file.path && selectionStart !== null && selectionEnd !== null &&
          left.num >= selectionStart && left.num <= selectionEnd && selSide === 'old') {
        leftEl.classList.add('selected');
      }
    } else {
      leftCommentGutter = makeDiffCommentGutter(file.path, 0, '');
    }

    const leftContent = document.createElement('div');
    leftContent.className = 'diff-content';
    if (left) {
      leftContent.innerHTML = highlightDiffLine(left.content, left.num, 'old', file.highlightCache, file.lang);
    }
    if (!left) leftEl.classList.add('empty');

    leftEl.appendChild(leftNum);
    leftEl.appendChild(leftCommentGutter);
    leftEl.appendChild(leftContent);

    // Right side
    const rightEl = document.createElement('div');
    rightEl.className = 'diff-split-side right';
    if (right && right.type === 'add') rightEl.classList.add('addition');

    const rightNum = document.createElement('div');
    rightNum.className = 'diff-gutter-num';
    rightNum.textContent = right ? (right.num || '') : '';

    var rightCommentGutter;
    if (right && right.num) {
      if (right.type === 'add' || right.type === 'context') {
        rightCommentGutter = makeDiffCommentGutter(file.path, right.num, '');
      } else {
        rightCommentGutter = makeDiffCommentGutter(file.path, 0, '');
      }
      tagDiffLine(rightEl, file.path, right.num, '', row);
      const rComments = commentsMap[right.num + ':'] || [];
      if (rComments.length > 0) rightEl.classList.add('has-comment');
      var selSideR = diffDragState ? diffDragState.side : (activeForm ? activeForm.side : null);
      if (activeFilePath === file.path && selectionStart !== null && selectionEnd !== null &&
          right.num >= selectionStart && right.num <= selectionEnd && (selSideR || '') === '') {
        rightEl.classList.add('selected');
      }
    } else {
      rightCommentGutter = makeDiffCommentGutter(file.path, 0, '');
    }

    const rightContent = document.createElement('div');
    rightContent.className = 'diff-content';
    if (right) {
      rightContent.innerHTML = highlightDiffLine(right.content, right.num, right.type === 'del' ? 'old' : '', file.highlightCache, file.lang);
    }
    if (!right) rightEl.classList.add('empty');

    rightEl.appendChild(rightNum);
    rightEl.appendChild(rightCommentGutter);
    rightEl.appendChild(rightContent);

    row.appendChild(leftEl);
    row.appendChild(rightEl);

    return { el: row };
  }

  // ===== Comment Helpers =====
  function buildCommentsMap(comments) {
    const map = {};
    for (const c of comments) {
      const key = c.end_line;
      if (!map[key]) map[key] = [];
      map[key].push(c);
    }
    return map;
  }

  function buildDiffCommentsMap(comments) {
    // Key by "line:side" to distinguish old-side vs new-side comments on the same line number
    const map = {};
    for (const c of comments) {
      const key = c.end_line + ':' + (c.side || '');
      if (!map[key]) map[key] = [];
      map[key].push(c);
    }
    return map;
  }

  function getCommentsForBlock(block, commentsMap) {
    const result = [];
    for (let ln = block.startLine; ln <= block.endLine; ln++) {
      if (commentsMap[ln]) result.push(...commentsMap[ln]);
    }
    return result;
  }

  function buildResolvedMap(file) {
    const map = {};
    const prev = file.PreviousComments || [];
    for (const c of prev) {
      if (!c.resolved || !c.resolution_lines || c.resolution_lines.length === 0) continue;
      const targetLine = Math.max(...c.resolution_lines);
      if (!map[targetLine]) map[targetLine] = [];
      map[targetLine].push(c);
    }
    return map;
  }

  function getResolvedForBlock(block, resolvedMap) {
    const result = [];
    for (let ln = block.startLine; ln <= block.endLine; ln++) {
      if (resolvedMap[ln]) result.push(...resolvedMap[ln]);
    }
    return result;
  }

  // ===== Gutter Drag Selection =====
  let dragState = null;

  function handleGutterMouseDown(e) {
    e.preventDefault();
    const gutter = e.currentTarget;
    const startLine = parseInt(gutter.dataset.startLine);
    const endLine = parseInt(gutter.dataset.endLine);
    const filePath = gutter.dataset.filePath;
    const blockEl = gutter.parentElement;
    const blockIndex = parseInt(blockEl.dataset.blockIndex);

    // Shift+click: extend selection
    if (e.shiftKey && selectionStart !== null && activeFilePath === filePath) {
      const rangeStart = Math.min(selectionStart, startLine);
      const rangeEnd = Math.max(selectionEnd, endLine);
      const file = getFileByPath(filePath);
      if (!file) return;
      let lastBlockIndex = 0;
      for (let i = 0; i < file.lineBlocks.length; i++) {
        if (file.lineBlocks[i].startLine >= rangeStart && file.lineBlocks[i].endLine <= rangeEnd) {
          lastBlockIndex = i;
        }
      }
      setActiveForm({ filePath, afterBlockIndex: lastBlockIndex, startLine: rangeStart, endLine: rangeEnd, editingId: null });
      activeFilePath = filePath;
      selectionStart = rangeStart;
      selectionEnd = rangeEnd;
      renderFileByPath(filePath);
      focusCommentTextarea();
      return;
    }

    dragState = {
      filePath,
      anchorStartLine: startLine, anchorEndLine: endLine,
      anchorBlockIndex: blockIndex,
      currentStartLine: startLine, currentEndLine: endLine,
      currentBlockIndex: blockIndex,
    };

    activeFilePath = filePath;
    selectionStart = startLine;
    selectionEnd = endLine;
    setActiveForm(null);
    renderFileByPath(filePath);

    document.body.classList.add('dragging');
    document.addEventListener('mousemove', handleDragMove);
    document.addEventListener('mouseup', handleDragEnd);
  }

  // Update drag selection CSS classes on existing DOM without full re-render.
  // Handles both markdown line blocks and diff gutter elements.
  function updateDragSelectionVisuals(filePath) {
    var section = document.getElementById('file-section-' + filePath);
    if (!section) return;

    // Markdown line blocks: toggle .selected on line-block, update comment gutter drag classes
    var lineBlocks = section.querySelectorAll('.line-block[data-file-path="' + filePath + '"]');
    for (var i = 0; i < lineBlocks.length; i++) {
      var lb = lineBlocks[i];
      var startLine = parseInt(lb.dataset.startLine);
      var endLine = parseInt(lb.dataset.endLine);
      var inRange = activeFilePath === filePath && selectionStart !== null && selectionEnd !== null &&
                    startLine >= selectionStart && endLine <= selectionEnd;
      lb.classList.toggle('selected', inRange);

      // Update the comment gutter within this line block
      var gutter = lb.querySelector('.line-comment-gutter');
      if (gutter && dragState && dragState.filePath === filePath && selectionStart !== null) {
        var isAnchorBlock = startLine <= dragState.anchorEndLine && endLine >= dragState.anchorStartLine;
        var isCurrentBlock = startLine <= dragState.currentEndLine && endLine >= dragState.currentStartLine;
        var gutterInRange = startLine >= selectionStart && endLine <= selectionEnd;
        gutter.classList.toggle('drag-endpoint', isAnchorBlock || isCurrentBlock);
        gutter.classList.toggle('drag-range', gutterInRange);
        gutter.classList.toggle('drag-range-start', gutterInRange && startLine === selectionStart);
        gutter.classList.toggle('drag-range-end', gutterInRange && endLine === selectionEnd);
      }
    }

    // Diff line elements: toggle .selected on diff lines and drag-range on gutters
    if (diffDragState && diffDragState.filePath === filePath) {
      // Unified mode: toggle .selected on .diff-line elements
      var unifiedLines = section.querySelectorAll('.diff-container.unified .diff-line[data-diff-visual-idx]');
      for (var ui = 0; ui < unifiedLines.length; ui++) {
        var uLine = unifiedLines[ui];
        var uVisualIdx = parseInt(uLine.dataset.diffVisualIdx);
        var uSelected = unifiedVisualStart !== null && unifiedVisualEnd !== null &&
                        uVisualIdx >= unifiedVisualStart && uVisualIdx <= unifiedVisualEnd;
        uLine.classList.toggle('selected', uSelected);
      }

      // Split mode: toggle .selected on .diff-split-side elements
      var splitSides = section.querySelectorAll('.diff-container.split .diff-split-side[data-diff-line-num]');
      for (var si = 0; si < splitSides.length; si++) {
        var sSide = splitSides[si];
        var sLineNum = parseInt(sSide.dataset.diffLineNum);
        var sSideVal = sSide.dataset.diffSide || '';
        var sSideMatch = diffDragState.side === sSideVal;
        var sSelected = sSideMatch && selectionStart !== null && selectionEnd !== null &&
                        sLineNum >= selectionStart && sLineNum <= selectionEnd;
        sSide.classList.toggle('selected', sSelected);
      }
    }

    // Diff gutter elements: toggle drag-range classes
    var diffGutters = section.querySelectorAll('.diff-comment-gutter');
    for (var j = 0; j < diffGutters.length; j++) {
      var col = diffGutters[j];
      var btn = col.querySelector('.diff-comment-btn');
      if (!btn) continue;
      var lineNum = parseInt(btn.dataset.lineNum);
      var side = btn.dataset.side || '';
      var visualIdx = btn.dataset.visualIdx !== undefined ? parseInt(btn.dataset.visualIdx) : undefined;
      if (!lineNum) continue;

      var sideMatch = diffMode === 'split' ? (diffDragState && diffDragState.side === side) : true;
      var isActive = diffDragState && diffDragState.filePath === filePath && sideMatch && selectionStart !== null && selectionEnd !== null;

      if (isActive) {
        var isAnchor, isCurrent, dgInRange, isRangeStart, isRangeEnd;
        if (diffMode !== 'split' && visualIdx !== undefined && unifiedVisualStart !== null) {
          isAnchor = visualIdx === diffDragState.anchorVisualIdx;
          isCurrent = visualIdx === diffDragState.currentVisualIdx;
          dgInRange = visualIdx >= unifiedVisualStart && visualIdx <= unifiedVisualEnd;
          isRangeStart = visualIdx === unifiedVisualStart;
          isRangeEnd = visualIdx === unifiedVisualEnd;
        } else {
          isAnchor = lineNum === diffDragState.anchorLine;
          isCurrent = lineNum === diffDragState.currentLine;
          dgInRange = lineNum >= selectionStart && lineNum <= selectionEnd;
          isRangeStart = lineNum === selectionStart;
          isRangeEnd = lineNum === selectionEnd;
        }
        col.classList.toggle('drag-endpoint', isAnchor || isCurrent);
        col.classList.toggle('drag-range', dgInRange);
        col.classList.toggle('drag-range-start', dgInRange && isRangeStart);
        col.classList.toggle('drag-range-end', dgInRange && isRangeEnd);
      } else {
        col.classList.remove('drag-endpoint', 'drag-range', 'drag-range-start', 'drag-range-end');
      }
    }
  }

  function handleDragMove(e) {
    if (!dragState) return;
    const el = document.elementFromPoint(e.clientX, e.clientY);
    if (!el) return;
    const lineBlock = el.closest('.line-block');
    if (!lineBlock || lineBlock.dataset.filePath !== dragState.filePath) return;

    const hoverStartLine = parseInt(lineBlock.dataset.startLine);
    const hoverEndLine = parseInt(lineBlock.dataset.endLine);
    const hoverBlockIndex = parseInt(lineBlock.dataset.blockIndex);

    dragState.currentStartLine = hoverStartLine;
    dragState.currentEndLine = hoverEndLine;
    dragState.currentBlockIndex = hoverBlockIndex;

    selectionStart = Math.min(dragState.anchorStartLine, hoverStartLine);
    selectionEnd = Math.max(dragState.anchorEndLine, hoverEndLine);
    updateDragSelectionVisuals(dragState.filePath);
  }

  function handleDragEnd() {
    document.removeEventListener('mousemove', handleDragMove);
    document.removeEventListener('mouseup', handleDragEnd);
    document.body.classList.remove('dragging');

    if (!dragState) return;
    const rangeStart = Math.min(dragState.anchorStartLine, dragState.currentStartLine);
    const rangeEnd = Math.max(dragState.anchorEndLine, dragState.currentEndLine);

    const file = getFileByPath(dragState.filePath);
    let lastBlockIndex = dragState.currentBlockIndex;
    if (file && file.lineBlocks) {
      for (let i = 0; i < file.lineBlocks.length; i++) {
        if (file.lineBlocks[i].startLine >= rangeStart && file.lineBlocks[i].endLine <= rangeEnd) {
          lastBlockIndex = i;
        }
      }
    }

    setActiveForm({
      filePath: dragState.filePath,
      afterBlockIndex: lastBlockIndex,
      startLine: rangeStart,
      endLine: rangeEnd,
      editingId: null,
    });
    activeFilePath = dragState.filePath;
    selectionStart = rangeStart;
    selectionEnd = rangeEnd;
    var fp = dragState.filePath;
    dragState = null;
    renderFileByPath(fp);
    focusCommentTextarea();
  }

  function setActiveForm(newForm) {
    var prevPath = activeForm ? activeForm.filePath : null;
    activeForm = newForm;
    if (prevPath && (!newForm || newForm.filePath !== prevPath)) {
      renderFileByPath(prevPath);
    }
  }

  function focusCommentTextarea() {
    requestAnimationFrame(() => {
      const ta = document.querySelector('.comment-form textarea');
      if (ta) ta.focus();
    });
  }

  // ===== Comment Form =====
  function createCommentForm() {
    const wrapper = document.createElement('div');
    wrapper.className = 'comment-form-wrapper';

    const form = document.createElement('div');
    form.className = 'comment-form';

    const header = document.createElement('div');
    header.className = 'comment-form-header';
    const lineRef = activeForm.startLine === activeForm.endLine
      ? 'Line ' + activeForm.startLine
      : 'Lines ' + activeForm.startLine + '-' + activeForm.endLine;
    header.textContent = activeForm.editingId ? 'Editing comment on ' + lineRef : 'Comment on ' + lineRef;

    const textarea = document.createElement('textarea');
    textarea.placeholder = 'Leave a review comment... (Ctrl+Enter to submit, Escape to cancel)';
    if (activeForm.editingId) {
      const file = getFileByPath(activeForm.filePath);
      if (file) {
        const existing = file.comments.find(c => c.id === activeForm.editingId);
        if (existing) textarea.value = existing.body;
      }
    }

    textarea.addEventListener('keydown', function(e) {
      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        submitComment(textarea.value);
      } else if (e.key === 'Escape') {
        e.preventDefault();
        cancelComment();
      }
    });

    textarea.addEventListener('input', function() { debouncedSaveDraft(textarea.value); });

    const actions = document.createElement('div');
    actions.className = 'comment-form-actions';

    const suggestBtn = document.createElement('button');
    suggestBtn.className = 'btn btn-sm';
    suggestBtn.textContent = '\u00B1 Suggest';
    suggestBtn.title = 'Insert the selected lines as a suggestion';
    suggestBtn.style.marginRight = 'auto';
    suggestBtn.addEventListener('click', () => insertSuggestion(textarea));

    const cancelBtn = document.createElement('button');
    cancelBtn.className = 'btn btn-sm';
    cancelBtn.textContent = 'Cancel';
    cancelBtn.addEventListener('click', cancelComment);

    const submitBtn = document.createElement('button');
    submitBtn.className = 'btn btn-sm btn-primary';
    submitBtn.textContent = activeForm.editingId ? 'Update Comment' : 'Add Comment';
    submitBtn.addEventListener('click', () => submitComment(textarea.value));

    actions.appendChild(suggestBtn);
    actions.appendChild(cancelBtn);
    actions.appendChild(submitBtn);

    form.appendChild(header);
    form.appendChild(textarea);
    form.appendChild(actions);
    wrapper.appendChild(form);
    return wrapper;
  }

  function insertSuggestion(textarea) {
    if (!activeForm) return;
    const file = getFileByPath(activeForm.filePath);
    if (!file) return;
    const lines = file.content.split('\n').slice(activeForm.startLine - 1, activeForm.endLine);
    const suggestion = '```suggestion\n' + lines.join('\n') + '\n```';
    const start = textarea.selectionStart;
    const end = textarea.selectionEnd;
    textarea.value = textarea.value.substring(0, start) + suggestion + textarea.value.substring(end);
    const cursorPos = start + '```suggestion\n'.length;
    textarea.selectionStart = cursorPos;
    textarea.selectionEnd = cursorPos + lines.join('\n').length;
    textarea.focus();
  }

  async function submitComment(body) {
    if (!body.trim() || !activeForm) return;
    clearDraft();
    const filePath = activeForm.filePath;
    const file = getFileByPath(filePath);
    if (!file) return;

    try {
      if (activeForm.editingId) {
        const res = await fetch('/api/comment/' + activeForm.editingId + '?path=' + enc(filePath), {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ body: body.trim() })
        });
        const updated = await res.json();
        const idx = file.comments.findIndex(c => c.id === activeForm.editingId);
        if (idx >= 0) file.comments[idx] = updated;
      } else {
        const payload = {
          start_line: activeForm.startLine,
          end_line: activeForm.endLine,
          body: body.trim()
        };
        if (activeForm.side) payload.side = activeForm.side;
        const res = await fetch('/api/file/comments?path=' + enc(filePath), {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
        const newComment = await res.json();
        file.comments.push(newComment);
      }
    } catch (err) {
      console.error('Error saving comment:', err);
    }

    var rerenderPath = filePath;
    activeForm = null;
    activeFilePath = null;
    selectionStart = null;
    selectionEnd = null;
    renderFileByPath(rerenderPath);
    renderFileSummary();
    updateCommentCount();
  }

  function cancelComment() {
    clearDraft();
    var rerenderPath = activeForm ? activeForm.filePath : null;
    activeForm = null;
    activeFilePath = null;
    selectionStart = null;
    selectionEnd = null;
    if (rerenderPath) {
      renderFileByPath(rerenderPath);
    } else {
      renderAllFiles();
    }
  }

  // ===== Draft Autosave =====
  let draftTimer = null;

  function getDraftKey() {
    if (!activeForm) return null;
    return 'crit-draft-' + (activeForm.filePath || '');
  }

  function saveDraft(body) {
    if (!activeForm) return;
    const key = getDraftKey();
    if (!key) return;
    try {
      localStorage.setItem(key, JSON.stringify({
        filePath: activeForm.filePath,
        startLine: activeForm.startLine,
        endLine: activeForm.endLine,
        afterBlockIndex: activeForm.afterBlockIndex,
        editingId: activeForm.editingId,
        side: activeForm.side || '',
        body: body,
        savedAt: Date.now()
      }));
    } catch (_) {}
  }

  function debouncedSaveDraft(body) {
    clearTimeout(draftTimer);
    draftTimer = setTimeout(function() { saveDraft(body); }, 500);
  }

  function clearDraft() {
    clearTimeout(draftTimer);
    const key = getDraftKey();
    if (key) {
      try { localStorage.removeItem(key); } catch (_) {}
    }
  }

  window.addEventListener('beforeunload', function() {
    if (!activeForm) return;
    const ta = document.querySelector('.comment-form textarea');
    if (ta) saveDraft(ta.value);
  });

  function restoreDrafts() {
    // Check all files for saved drafts
    for (const file of files) {
      const key = 'crit-draft-' + file.path;
      try {
        var raw = localStorage.getItem(key);
        if (!raw) continue;
        var draft = JSON.parse(raw);

        // Discard drafts older than 24 hours
        if (Date.now() - draft.savedAt > 24 * 60 * 60 * 1000) {
          localStorage.removeItem(key);
          continue;
        }

        // Verify line range exists in current file
        if (file.fileType === 'markdown' && file.content) {
          var totalLines = file.content.split('\n').length;
          if (draft.startLine < 1 || draft.endLine > totalLines) {
            localStorage.removeItem(key);
            continue;
          }
        }

        // If editing, verify comment still exists
        if (draft.editingId) {
          if (!file.comments.find(function(c) { return c.id === draft.editingId; })) {
            localStorage.removeItem(key);
            continue;
          }
        }

        // Restore activeForm and re-render
        activeForm = {
          filePath: file.path,
          afterBlockIndex: draft.afterBlockIndex,
          startLine: draft.startLine,
          endLine: draft.endLine,
          editingId: draft.editingId,
          side: draft.side || ''
        };
        activeFilePath = file.path;
        selectionStart = draft.startLine;
        selectionEnd = draft.endLine;
        renderFileByPath(file.path);

        // Populate textarea with saved body
        requestAnimationFrame(function() {
          var ta = document.querySelector('.comment-form textarea');
          if (ta && draft.body) {
            ta.value = draft.body;
            ta.focus();
          }
        });

        showMiniToast('Draft restored');
        break; // Only restore one draft at a time
      } catch (_) {
        localStorage.removeItem(key);
      }
    }
  }

  function showMiniToast(message) {
    var t = document.createElement('div');
    t.className = 'mini-toast';
    t.textContent = message;
    document.body.appendChild(t);
    requestAnimationFrame(function() { t.classList.add('mini-toast-visible'); });
    setTimeout(function() {
      t.classList.remove('mini-toast-visible');
      setTimeout(function() { t.remove(); }, 300);
    }, 3000);
  }

  // ===== Comment Display =====
  function createCommentElement(comment, filePath) {
    if (activeForm && activeForm.editingId === comment.id) {
      return createInlineEditor(comment);
    }

    const wrapper = document.createElement('div');
    wrapper.className = 'comment-block';

    const card = document.createElement('div');
    card.className = 'comment-card' + (comment.carried_forward ? ' carried-forward' : '');

    const header = document.createElement('div');
    header.className = 'comment-header';

    const lineRef = document.createElement('span');
    lineRef.className = 'comment-line-ref';
    lineRef.textContent = comment.start_line === comment.end_line
      ? 'Line ' + comment.start_line
      : 'Lines ' + comment.start_line + '-' + comment.end_line;

    const time = document.createElement('span');
    time.className = 'comment-time';
    time.textContent = formatTime(comment.created_at);

    const headerLeft = document.createElement('div');
    headerLeft.style.cssText = 'display:flex;align-items:center;gap:10px';
    headerLeft.appendChild(lineRef);
    if (comment.carried_forward) {
      const label = document.createElement('span');
      label.className = 'carried-forward-label';
      label.textContent = 'Unresolved';
      headerLeft.appendChild(label);
    }
    headerLeft.appendChild(time);

    const actions = document.createElement('div');
    actions.className = 'comment-actions';

    const editBtn = document.createElement('button');
    editBtn.textContent = 'Edit';
    editBtn.addEventListener('click', () => editComment(comment, filePath));

    const deleteBtn = document.createElement('button');
    deleteBtn.className = 'delete-btn';
    deleteBtn.textContent = 'Delete';
    deleteBtn.addEventListener('click', () => deleteComment(comment.id, filePath));

    actions.appendChild(editBtn);
    actions.appendChild(deleteBtn);

    header.appendChild(headerLeft);
    header.appendChild(actions);

    const bodyEl = document.createElement('div');
    bodyEl.className = 'comment-body';
    bodyEl.innerHTML = commentMd.render(comment.body);

    card.appendChild(header);
    card.appendChild(bodyEl);
    wrapper.appendChild(card);
    return wrapper;
  }

  function createInlineEditor(comment) {
    const wrapper = document.createElement('div');
    wrapper.className = 'comment-form-wrapper';

    const form = document.createElement('div');
    form.className = 'comment-form';

    const header = document.createElement('div');
    header.className = 'comment-form-header';
    const lineRef = comment.start_line === comment.end_line
      ? 'Line ' + comment.start_line
      : 'Lines ' + comment.start_line + '-' + comment.end_line;
    header.textContent = 'Editing comment on ' + lineRef;

    const textarea = document.createElement('textarea');
    textarea.placeholder = 'Leave a review comment... (Ctrl+Enter to submit, Escape to cancel)';
    textarea.value = comment.body;

    textarea.addEventListener('keydown', function(e) {
      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        submitComment(textarea.value);
      } else if (e.key === 'Escape') {
        e.preventDefault();
        cancelComment();
      }
    });

    textarea.addEventListener('input', function() { debouncedSaveDraft(textarea.value); });

    const actions = document.createElement('div');
    actions.className = 'comment-form-actions';

    const suggestBtn = document.createElement('button');
    suggestBtn.className = 'btn btn-sm';
    suggestBtn.textContent = '\u00B1 Suggest';
    suggestBtn.style.marginRight = 'auto';
    suggestBtn.addEventListener('click', () => insertSuggestion(textarea));

    const cancelBtn = document.createElement('button');
    cancelBtn.className = 'btn btn-sm';
    cancelBtn.textContent = 'Cancel';
    cancelBtn.addEventListener('click', cancelComment);

    const submitBtn = document.createElement('button');
    submitBtn.className = 'btn btn-sm btn-primary';
    submitBtn.textContent = 'Update Comment';
    submitBtn.addEventListener('click', () => submitComment(textarea.value));

    actions.appendChild(suggestBtn);
    actions.appendChild(cancelBtn);
    actions.appendChild(submitBtn);

    form.appendChild(header);
    form.appendChild(textarea);
    form.appendChild(actions);
    wrapper.appendChild(form);

    requestAnimationFrame(() => textarea.focus());
    return wrapper;
  }

  function editComment(comment, filePath) {
    setActiveForm({
      filePath,
      afterBlockIndex: null,
      startLine: comment.start_line,
      endLine: comment.end_line,
      editingId: comment.id,
    });
    activeFilePath = filePath;
    renderFileByPath(filePath);
  }

  async function deleteComment(id, filePath) {
    const file = getFileByPath(filePath);
    if (!file) return;
    try {
      await fetch('/api/comment/' + id + '?path=' + enc(filePath), { method: 'DELETE' });
      file.comments = file.comments.filter(c => c.id !== id);
    } catch (err) {
      console.error('Error deleting comment:', err);
    }
    renderFileByPath(filePath);
    renderFileSummary();
    updateCommentCount();
  }

  function createResolvedElement(comment) {
    const el = document.createElement('div');
    el.className = 'resolved-comment';
    el.innerHTML =
      '<div class="resolved-comment-header">' +
        '<span class="resolved-check">\u2713</span>' +
        '<span class="resolved-body">' + escapeHtml(comment.body) + '</span>' +
      '</div>' +
      (comment.resolution_note ? '<span class="resolved-note">' + escapeHtml(comment.resolution_note) + '</span>' : '');
    el.addEventListener('click', function() { el.classList.toggle('expanded'); });
    return el;
  }

  // ===== Comment Count =====
  function updateCommentCount() {
    let total = 0;
    for (const f of files) total += f.comments.length;
    const el = document.getElementById('commentCount');
    el.innerHTML = total === 0 ? '' : '<strong>' + total + '</strong> comment' + (total === 1 ? '' : 's');
  }

  function updateViewedCount() {
    var viewed = 0;
    for (var i = 0; i < files.length; i++) {
      if (files[i].viewed) viewed++;
    }
    var el = document.getElementById('viewedCount');
    if (files.length <= 1) { el.textContent = ''; return; }
    el.textContent = viewed + ' / ' + files.length + ' files viewed';
    el.classList.toggle('all-viewed', viewed === files.length);
  }

  // ===== UI State =====
  function updateHeaderRound() {
    const el = document.getElementById('headerNotify');
    if (reviewRound > 1) {
      el.textContent = 'Round #' + reviewRound;
    }
  }

  function setUIState(state) {
    uiState = state;
    const finishBtn = document.getElementById('finishBtn');
    const waitingOverlay = document.getElementById('waitingOverlay');

    switch (state) {
      case 'reviewing':
        finishBtn.textContent = 'Finish Review';
        finishBtn.disabled = false;
        finishBtn.classList.add('btn-primary');
        document.getElementById('waitingEdits').textContent = '';
        waitingOverlay.classList.remove('active');
        break;
      case 'waiting':
        finishBtn.textContent = 'Waiting...';
        finishBtn.disabled = true;
        finishBtn.classList.remove('btn-primary');
        document.getElementById('waitingEdits').textContent = '';
        waitingOverlay.classList.add('active');
        break;
    }
  }

  // ===== Finish Review =====
  document.getElementById('finishBtn').addEventListener('click', async function() {
    if (uiState !== 'reviewing') return;

    try {
      const resp = await fetch('/api/finish', { method: 'POST' });
      const data = await resp.json();
      const hasComments = !!data.prompt;
      const prompt = data.prompt || 'I reviewed the changes, no feedback, good to go!';

      document.getElementById('waitingPrompt').textContent = prompt;

      if (hasComments) {
        document.getElementById('waitingMessage').innerHTML =
          'Paste the prompt below to your agent, then wait for updates.';
        const clipEl = document.getElementById('waitingClipboard');
        clipEl.textContent = '\u2713 Copied to clipboard';
        clipEl.classList.remove('clipboard-confirm');
        void clipEl.offsetWidth;
        clipEl.classList.add('clipboard-confirm');
      } else {
        document.getElementById('waitingMessage').textContent =
          'You can close this browser tab, or leave it open for another round.';
        document.getElementById('waitingClipboard').textContent = '';
      }

      try { await navigator.clipboard.writeText(prompt); } catch (_) {}
    } catch (_) {}

    setUIState('waiting');
  });

  document.getElementById('backToEditing').addEventListener('click', function() {
    setUIState('reviewing');
  });

  // ===== SSE Client =====
  function connectSSE() {
    const source = new EventSource('/api/events');

    source.addEventListener('file-changed', async function() {
      try {
        // Capture per-file user state before rebuilding
        var prevState = {};
        for (var pi = 0; pi < files.length; pi++) {
          prevState[files[pi].path] = {
            viewMode: files[pi].viewMode,
            collapsed: files[pi].collapsed,
            diffLoaded: files[pi].diffLoaded,
          };
        }

        // Re-fetch everything on file-changed (round complete)
        const sessionRes = await fetch('/api/session').then(r => r.json());
        session = sessionRes;
        reviewRound = session.review_round || reviewRound + 1;

        // Reload all files
        const fileInfos = session.files || [];
        const filePromises = fileInfos.map(async (fi) => {
          const [fileRes, commentsRes] = await Promise.all([
            fetch('/api/file?path=' + enc(fi.path)).then(r => r.json()),
            fetch('/api/file/comments?path=' + enc(fi.path)).then(r => r.json()),
          ]);

          const f = {
            path: fi.path,
            status: fi.status,
            fileType: fi.file_type,
            content: fileRes.content || '',
            comments: Array.isArray(commentsRes) ? commentsRes : [],
            diffHunks: null,
            lineBlocks: null,
            tocItems: [],
            collapsed: fi.status === 'deleted',
            viewMode: (session.mode === 'git') ? 'diff' : 'document',
            additions: fi.additions || 0,
            deletions: fi.deletions || 0,
          };

          // Load diff hunks (always — code files get git diff, markdown may get inter-round diff)
          {
            try {
              const diffRes = await fetch('/api/file/diff?path=' + enc(fi.path)).then(r => r.json());
              f.diffHunks = diffRes.hunks || [];
            } catch (_) { f.diffHunks = []; }
          }

          // Mark large diffs for deferred rendering
          var diffLineCount = 0;
          if (f.diffHunks) {
            for (var h = 0; h < f.diffHunks.length; h++) {
              diffLineCount += (f.diffHunks[h].Lines || []).length;
            }
          }
          f.diffTooLarge = diffLineCount > 1000;
          f.diffLoaded = !f.diffTooLarge;

          // Pre-highlight code files for diff rendering
          if (f.fileType === 'code') {
            f.highlightCache = preHighlightFile(f);
            f.lang = langFromPath(f.path);
          }

          if (f.fileType === 'markdown') {
            const parsed = parseMarkdown(f.content);
            f.lineBlocks = parsed.blocks;
            f.tocItems = parsed.tocItems;
          }

          // Restore user state from previous round
          var prev = prevState[fi.path];
          if (prev) {
            f.viewMode = prev.viewMode;
            f.collapsed = prev.collapsed;
            if (prev.diffLoaded) f.diffLoaded = prev.diffLoaded;
          }

          return f;
        });

        files = await Promise.all(filePromises);

        files.sort(function(a, b) {
          var pa = a.path.split('/'), pb = b.path.split('/');
          var min = Math.min(pa.length, pb.length);
          for (var i = 0; i < min - 1; i++) {
            if (pa[i] !== pb[i]) return pa[i].localeCompare(pb[i]);
          }
          if (pa.length !== pb.length) return pb.length - pa.length;
          return pa[pa.length - 1].localeCompare(pb[pa.length - 1]);
        });

        activeForm = null;
        activeFilePath = null;
        selectionStart = null;
        selectionEnd = null;
        focusedBlockIndex = null;
        focusedFilePath = null;
        focusedElement = null;

        clearViewedState();
        updateHeaderRound();
        renderFileTree();
        renderAllFiles();
        updateCommentCount();
        updateViewedCount();
        updateTreeViewedState();
        setUIState('reviewing');
      } catch (err) {
        console.error('Error handling file-changed:', err);
      }
    });

    source.addEventListener('edit-detected', function(e) {
      try {
        const data = JSON.parse(e.data);
        const count = parseInt(data.content, 10);
        const el = document.getElementById('waitingEdits');
        if (el && uiState === 'waiting') {
          el.innerHTML = '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:-3px;margin-right:4px"><rect x="3" y="11" width="18" height="10" rx="2"/><circle cx="12" cy="5" r="2"/><line x1="12" y1="7" x2="12" y2="11"/><line x1="8" y1="16" x2="8" y2="16"/><line x1="16" y1="16" x2="16" y2="16"/></svg>Your agent made ' + count + ' edit' + (count === 1 ? '' : 's');
        }
      } catch (_) {}
    });

    source.addEventListener('server-shutdown', function() {
      source.close();
      showDisconnected();
    });

    source.onerror = function() {};
  }

  function showDisconnected() {
    var overlay = document.createElement('div');
    overlay.style.cssText = 'position:fixed;inset:0;background:rgba(0,0,0,0.6);display:flex;align-items:center;justify-content:center;z-index:10000';
    var box = document.createElement('div');
    box.style.cssText = 'background:var(--bg-primary,#1e1e2e);border:1px solid var(--border-color,#45475a);border-radius:12px;padding:32px 40px;text-align:center;color:var(--text-primary,#cdd6f4);font-family:inherit';
    box.innerHTML = '<div style="font-size:20px;font-weight:600;margin-bottom:8px">Server stopped</div><div style="color:var(--text-secondary,#a6adc8)">You can close this tab.</div>';
    overlay.appendChild(box);
    document.body.appendChild(overlay);
  }

  // ===== Share =====
  function showSharedNotice(url) {
    const unpublishBtn = deleteToken
      ? '<button class="toast-btn toast-btn-danger" id="shareUnpublishBtn">Unpublish</button>'
      : '';
    const el = showToast('share', 'success', '' +
      '<span>Shared! <span class="toast-url">' + escapeHtml(url) + '</span></span>' +
      '<div class="toast-actions">' +
        '<button class="toast-btn toast-btn-filled" id="shareCopyBtn">Copy link</button>' +
        unpublishBtn +
        '<button class="toast-btn toast-btn-ghost" onclick="dismissToast(\'share\')">Dismiss</button>' +
      '</div>');
    el.querySelector('#shareCopyBtn').addEventListener('click', async function() {
      await navigator.clipboard.writeText(url).catch(() => {});
      this.textContent = '\u2713 Copied';
      setTimeout(() => { this.textContent = 'Copy link'; }, 2000);
    });
    if (deleteToken) {
      el.querySelector('#shareUnpublishBtn').addEventListener('click', handleUnpublish);
    }
  }

  async function handleUnpublish() {
    const btn = document.getElementById('shareUnpublishBtn');
    if (btn) { btn.textContent = 'Unpublishing\u2026'; btn.disabled = true; }
    try {
      const resp = await fetch(shareURL + '/api/reviews', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ delete_token: deleteToken }),
      });
      const alreadyDeleted = resp.status === 404;
      if (!alreadyDeleted && !resp.ok) throw new Error('Server error ' + resp.status);
      hostedURL = '';
      deleteToken = '';
      fetch('/api/share-url', { method: 'DELETE' }).catch(() => {});
      const message = alreadyDeleted ? 'Already deleted.' : 'Review unpublished.';
      showToast('share', 'success',
        '<span>' + message + '</span>' +
        '<div class="toast-actions"><button class="toast-btn toast-btn-ghost" onclick="dismissToast(\'share\')">Dismiss</button></div>');
    } catch (err) {
      const el = showToast('share', 'error',
        '<span>Unpublish failed: ' + escapeHtml(err.message) + '</span>' +
        '<div class="toast-actions">' +
          '<button class="toast-btn toast-btn-filled" id="shareUnpublishRetryBtn">Retry</button>' +
          '<button class="toast-btn toast-btn-ghost" onclick="dismissToast(\'share\')">Dismiss</button>' +
        '</div>');
      el.querySelector('#shareUnpublishRetryBtn').addEventListener('click', () => {
        dismissToast('share');
        handleUnpublish();
      });
    }
  }

  document.getElementById('shareBtn').addEventListener('click', async function() {
    if (hostedURL) { showSharedNotice(hostedURL); return; }
    const btn = this;
    btn.textContent = 'Sharing\u2026';
    btn.disabled = true;
    dismissToast('share');

    // Build payload from all files
    const payload = {
      content: files.map(f => f.content).join('\n'),
      filename: files.length === 1 ? files[0].path : session.branch || 'review',
      comments: [],
    };
    for (const f of files) {
      for (const c of f.comments) {
        payload.comments.push({ file: f.path, start_line: c.start_line, end_line: c.end_line, body: c.body });
      }
    }

    try {
      const resp = await fetch(shareURL + '/api/reviews', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      if (!resp.ok) {
        const err = await resp.json().catch(() => ({}));
        throw new Error(err.error || 'Server error ' + resp.status);
      }
      const { url, delete_token } = await resp.json();
      hostedURL = url;
      deleteToken = delete_token || '';
      showSharedNotice(url);
      fetch('/api/share-url', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url, delete_token: deleteToken }),
      }).catch(() => {});
    } catch (err) {
      const el = showToast('share', 'error',
        '<span>Share failed: ' + escapeHtml(err.message) + '</span>' +
        '<div class="toast-actions">' +
          '<button class="toast-btn toast-btn-filled" id="shareRetryBtn">Retry</button>' +
          '<button class="toast-btn toast-btn-ghost" onclick="dismissToast(\'share\')">Dismiss</button>' +
        '</div>');
      el.querySelector('#shareRetryBtn').addEventListener('click', () => {
        dismissToast('share');
        document.getElementById('shareBtn').click();
      });
    } finally {
      btn.textContent = 'Share';
      btn.disabled = false;
    }
  });

  // ===== Toast System =====
  function showToast(id, type, content) {
    dismissToast(id);
    const container = document.getElementById('toastContainer');
    const el = document.createElement('div');
    el.className = 'toast toast-' + type;
    el.id = 'toast-' + id;
    el.innerHTML = content;
    container.appendChild(el);
    return el;
  }

  // Global for onclick handlers in toast HTML
  window.dismissToast = function(id) {
    const el = document.getElementById('toast-' + id);
    if (el) el.remove();
  };

  // ===== Table of Contents =====
  function buildToc() {
    const tocEl = document.getElementById('toc');
    const listEl = tocEl.querySelector('.toc-list');
    const toggleBtn = document.getElementById('tocToggle');
    listEl.innerHTML = '';

    // TOC only for single-file markdown reviews
    if (session.mode === 'git' || files.length > 1) {
      toggleBtn.style.display = 'none';
      return;
    }

    // Gather TOC from all markdown files
    let allItems = [];
    for (const f of files) {
      if (f.tocItems && f.tocItems.length > 0) {
        for (const item of f.tocItems) {
          allItems.push({ ...item, filePath: f.path });
        }
      }
    }

    if (allItems.length === 0) {
      toggleBtn.style.display = 'none';
      return;
    }
    toggleBtn.style.display = '';

    const minLevel = Math.min(...allItems.map(i => i.level));
    for (const item of allItems) {
      const li = document.createElement('li');
      const a = document.createElement('a');
      a.href = '#';
      a.textContent = item.text;
      a.dataset.startLine = item.startLine;
      a.dataset.filePath = item.filePath;
      a.style.paddingLeft = (12 + (item.level - minLevel) * 10) + 'px';
      a.addEventListener('click', function(e) {
        e.preventDefault();
        scrollToFile(item.filePath);
      });
      li.appendChild(a);
      listEl.appendChild(li);
    }
  }

  // ===== Mermaid =====
  function renderMermaidBlocks() {
    if (typeof mermaid === 'undefined') return;
    mermaid.initialize({ startOnLoad: false, theme: 'dark' });
    const codes = document.querySelectorAll('code.language-mermaid');
    codes.forEach(function(code) {
      const pre = code.parentElement;
      if (!pre || pre.tagName !== 'PRE') return;
      const container = document.createElement('div');
      container.className = 'mermaid';
      container.textContent = code.textContent;
      pre.replaceWith(container);
    });
    try { mermaid.run(); } catch (_) {}
  }

  // ===== Theme =====
  function initTheme() {
    const saved = localStorage.getItem('crit-theme') || 'system';
    applyTheme(saved);
  }

  window.applyTheme = function(choice) {
    localStorage.setItem('crit-theme', choice);
    if (choice === 'light') document.documentElement.setAttribute('data-theme', 'light');
    else if (choice === 'dark') document.documentElement.setAttribute('data-theme', 'dark');
    else document.documentElement.removeAttribute('data-theme');

    document.querySelectorAll('.theme-pill-btn').forEach(function(btn) {
      const forTheme = btn.getAttribute('data-for-theme');
      btn.classList.toggle('active', forTheme === choice);
    });

    const indicator = document.querySelector('.theme-pill-indicator');
    if (indicator) {
      if (choice === 'system') indicator.style.left = '0%';
      else if (choice === 'light') indicator.style.left = '33.333%';
      else indicator.style.left = '66.666%';
    }
  };

  document.querySelectorAll('.theme-pill-btn').forEach(function(btn) {
    btn.addEventListener('click', function() {
      applyTheme(btn.getAttribute('data-for-theme'));
    });
  });

  // ===== Update Dismiss =====
  window.dismissUpdate = function() {
    document.getElementById('headerUpdate').style.display = 'none';
  };

  // ===== Diff Mode Toggle (Split / Unified) =====
  document.querySelectorAll('#diffModeToggle .toggle-btn').forEach(function(btn) {
    btn.addEventListener('click', function() {
      const mode = btn.dataset.mode;
      if (mode === diffMode) return;
      diffMode = mode;
      localStorage.setItem('crit-diff-mode', mode);
      document.querySelectorAll('#diffModeToggle .toggle-btn').forEach(function(b) {
        b.classList.toggle('active', b.dataset.mode === mode);
      });
      renderAllFiles();
    });
  });

  // ===== TOC Toggle =====
  document.getElementById('tocToggle').addEventListener('click', function() {
    document.getElementById('toc').classList.toggle('toc-hidden');
    buildToc();
  });

  document.querySelector('.toc-close').addEventListener('click', function() {
    document.getElementById('toc').classList.add('toc-hidden');
  });

  // ===== Keyboard Shortcuts =====
  function toggleShortcutsOverlay() {
    document.getElementById('shortcutsOverlay').classList.toggle('active');
  }

  document.getElementById('shortcutsToggle').addEventListener('click', toggleShortcutsOverlay);
  document.getElementById('shortcutsOverlay').addEventListener('click', function(e) {
    if (e.target === this) toggleShortcutsOverlay();
  });

  document.addEventListener('keydown', function(e) {
    const tag = document.activeElement.tagName;
    if (tag === 'TEXTAREA' || tag === 'INPUT' || document.activeElement.isContentEditable) {
      if (e.key === 'Escape' && activeForm) {
        e.preventDefault();
        cancelComment();
      }
      return;
    }

    if (document.getElementById('shortcutsOverlay').classList.contains('active')) {
      if (e.key === 'Escape' || e.key === '?') {
        e.preventDefault();
        toggleShortcutsOverlay();
      }
      return;
    }

    if (e.metaKey || e.ctrlKey || e.altKey) return;

    switch (e.key) {
      case 'j': case 'k': {
        e.preventDefault();
        var allNav = navElements;
        if (allNav.length === 0) return;
        var curIdx = focusedElement ? allNav.indexOf(focusedElement) : -1;
        if (curIdx === -1 && focusedElement) {
          // Stale ref after re-render — find nearest match by data attributes
          var fp = focusedElement.dataset.filePath || focusedElement.dataset.diffFilePath;
          var bi = focusedElement.dataset.blockIndex;
          var dln = focusedElement.dataset.diffLineNum;
          for (var ni = 0; ni < allNav.length; ni++) {
            var n = allNav[ni];
            if (fp && bi != null && n.dataset.filePath === fp && n.dataset.blockIndex === bi) { curIdx = ni; break; }
            if (fp && dln && n.dataset.diffFilePath === fp && n.dataset.diffLineNum === dln) { curIdx = ni; break; }
          }
        }
        if (curIdx === -1) {
          curIdx = e.key === 'j' ? 0 : allNav.length - 1;
        } else {
          if (e.key === 'j' && curIdx < allNav.length - 1) curIdx++;
          if (e.key === 'k' && curIdx > 0) curIdx--;
        }
        document.querySelectorAll('.kb-nav.focused').forEach(function(el) { el.classList.remove('focused'); });
        focusedElement = allNav[curIdx];
        focusedElement.classList.add('focused');
        focusedElement.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
        // Sync legacy state
        if (focusedElement.dataset.filePath) {
          focusedFilePath = focusedElement.dataset.filePath;
          focusedBlockIndex = parseInt(focusedElement.dataset.blockIndex);
        } else if (focusedElement.dataset.diffFilePath) {
          focusedFilePath = focusedElement.dataset.diffFilePath;
          focusedBlockIndex = null;
        }
        break;
      }
      case 'c': {
        e.preventDefault();
        if (!focusedElement) return;
        // Markdown line block
        if (focusedElement.dataset.filePath && focusedElement.dataset.blockIndex != null) {
          var fp = focusedElement.dataset.filePath;
          var bi = parseInt(focusedElement.dataset.blockIndex);
          var file = getFileByPath(fp);
          if (!file || !file.lineBlocks) return;
          var block = file.lineBlocks[bi];
          selectionStart = block.startLine;
          selectionEnd = block.endLine;
          setActiveForm({ filePath: fp, afterBlockIndex: bi, startLine: block.startLine, endLine: block.endLine, editingId: null });
          activeFilePath = fp;
          renderFileByPath(fp);
          focusCommentTextarea();
        }
        // Diff line
        else if (focusedElement.dataset.diffFilePath && focusedElement.dataset.diffLineNum) {
          var dfp = focusedElement.dataset.diffFilePath;
          var lineNum = parseInt(focusedElement.dataset.diffLineNum);
          var side = focusedElement.dataset.diffSide || '';
          selectionStart = lineNum;
          selectionEnd = lineNum;
          setActiveForm({ filePath: dfp, afterBlockIndex: null, startLine: lineNum, endLine: lineNum, editingId: null, side: side || undefined });
          activeFilePath = dfp;
          renderFileByPath(dfp);
          focusCommentTextarea();
        }
        break;
      }
      case 'e':
      case 'd': {
        e.preventDefault();
        if (!focusedElement) return;
        var fp = focusedElement.dataset.filePath || focusedElement.dataset.diffFilePath;
        if (!fp) return;
        var file = getFileByPath(fp);
        if (!file || !file.comments || file.comments.length === 0) return;
        // Find comments for the focused line
        var comment = null;
        if (focusedElement.dataset.blockIndex != null) {
          var block = file.lineBlocks[parseInt(focusedElement.dataset.blockIndex)];
          if (block) {
            comment = file.comments.find(function(c) { return c.end_line >= block.startLine && c.end_line <= block.endLine; });
          }
        } else if (focusedElement.dataset.diffLineNum) {
          var ln = parseInt(focusedElement.dataset.diffLineNum);
          var sd = focusedElement.dataset.diffSide || '';
          comment = file.comments.find(function(c) { return c.end_line === ln && (c.side || '') === sd; });
        }
        if (!comment) return;
        if (e.key === 'e') editComment(comment, fp);
        else deleteComment(comment.id, fp);
        break;
      }
      case 'F': {
        e.preventDefault();
        if (uiState !== 'reviewing') return;
        document.getElementById('finishBtn').click();
        break;
      }
      case 't': {
        e.preventDefault();
        document.getElementById('tocToggle').click();
        break;
      }
      case 'v': {
        e.preventDefault();
        // Toggle viewed on the file that owns the focused element
        var vfp = focusedElement && (focusedElement.dataset.filePath || focusedElement.dataset.diffFilePath);
        if (vfp) toggleViewed(vfp);
        break;
      }
      case '?': {
        e.preventDefault();
        toggleShortcutsOverlay();
        break;
      }
      case 'Escape': {
        e.preventDefault();
        if (activeForm) cancelComment();
        else if (selectionStart !== null) {
          var clearPath = activeFilePath;
          selectionStart = null;
          selectionEnd = null;
          activeFilePath = null;
          if (clearPath) renderFileByPath(clearPath);
        } else if (focusedElement) {
          document.querySelectorAll('.kb-nav.focused').forEach(function(el) { el.classList.remove('focused'); });
          focusedBlockIndex = null;
          focusedFilePath = null;
          focusedElement = null;
        }
        break;
      }
    }
  });

  // ===== Start =====
  init();
  connectSSE();

})();
