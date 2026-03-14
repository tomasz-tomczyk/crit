(function() {
  'use strict';

  // ===== Comment Markdown Renderer =====
  const commentMd = window.markdownit({
    html: false,
    linkify: true,
    typographer: true,
    highlight: function(str, lang) {
      if (lang && hljs.getLanguage(lang)) {
        try { return hljs.highlight(str, { language: lang }).value; } catch (_) {}
      }
      try { return hljs.highlightAuto(str).value; } catch (_) {}
      return '';
    }
  });

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

  // ===== Cookie helpers (persist across random ports on 127.0.0.1) =====
  function setCookie(name, value) {
    document.cookie = name + '=' + encodeURIComponent(value) + '; path=/; max-age=31536000; SameSite=Strict';
  }
  function getCookie(name) {
    var match = document.cookie.match('(?:^|; )' + name + '=([^;]*)');
    return match ? decodeURIComponent(match[1]) : null;
  }

  // ===== State =====
  let session = {};       // { mode, branch, base_ref, review_round, files: [...] }
  let files = [];         // [{ path, status, fileType, content, diffHunks, comments, lineBlocks, tocItems, collapsed, viewMode }]
  let shareURL = '';
  let hostedURL = '';
  let deleteToken = '';
  let configAuthor = '';
  let uiState = 'reviewing';

  let diffMode = getCookie('crit-diff-mode') || 'split'; // 'split' or 'unified'
  let diffScope = getCookie('crit-diff-scope') || 'all'; // 'all', 'branch', 'staged', or 'unstaged'
  let diffActive = false; // rendered diff view toggle for file mode

  // Per-file active form state
  let activeFilePath = null;
  let activeForms = [];  // Array of { formKey, filePath, afterBlockIndex, startLine, endLine, editingId, side }

  function formKey(form) {
    if (form.editingId) return form.filePath + ':edit:' + form.editingId;
    return form.filePath + ':' + form.startLine + ':' + form.endLine + ':' + (form.side || '');
  }

  function addForm(form) {
    form.formKey = formKey(form);
    var idx = activeForms.findIndex(function(f) { return f.formKey === form.formKey; });
    if (idx >= 0) {
      activeForms[idx] = form;
    } else {
      activeForms.push(form);
    }
  }

  function removeForm(key) {
    activeForms = activeForms.filter(function(f) { return f.formKey !== key; });
  }

  function getFormsForFile(filePath) {
    return activeForms.filter(function(f) { return f.filePath === filePath; });
  }

  function findFormForEdit(commentId) {
    return activeForms.find(function(f) { return f.editingId === commentId; });
  }
  let selectionStart = null;
  let selectionEnd = null;
  var unifiedVisualStart = null; // visual index range for unified drag (cross-number-space)
  var unifiedVisualEnd = null;
  let focusedBlockIndex = null;
  let focusedFilePath = null;
  let focusedElement = null; // currently focused navigable element
  let navElements = []; // cached .kb-nav list, rebuilt on render
  let changeGroups = [];      // [{elements: [DOM], filePath: string}]
  let currentChangeIdx = -1;

  const enc = encodeURIComponent;

  // Author color-coding for multi-reviewer comments
  const AUTHOR_COLORS = [
    { bg: 'rgba(74, 144, 217, 0.15)', border: 'rgba(74, 144, 217, 0.4)', text: '#4a90d9' },
    { bg: 'rgba(217, 74, 74, 0.15)', border: 'rgba(217, 74, 74, 0.4)', text: '#d94a4a' },
    { bg: 'rgba(74, 180, 100, 0.15)', border: 'rgba(74, 180, 100, 0.4)', text: '#4ab464' },
    { bg: 'rgba(217, 166, 74, 0.15)', border: 'rgba(217, 166, 74, 0.4)', text: '#d9a64a' },
    { bg: 'rgba(155, 74, 217, 0.15)', border: 'rgba(155, 74, 217, 0.4)', text: '#9b4ad9' },
    { bg: 'rgba(74, 195, 195, 0.15)', border: 'rgba(74, 195, 195, 0.4)', text: '#4ac3c3' },
  ];

  function authorColor(name) {
    let hash = 0;
    for (const ch of name) hash = ((hash << 5) - hash + ch.charCodeAt(0)) | 0;
    return AUTHOR_COLORS[Math.abs(hash) % AUTHOR_COLORS.length];
  }

  // Sort comparator: directories before files at each depth, then alphabetical
  function fileSortComparator(a, b) {
    var pa = a.path.split('/'), pb = b.path.split('/');
    var min = Math.min(pa.length, pb.length);
    for (var i = 0; i < min - 1; i++) {
      if (pa[i] !== pb[i]) return pa[i].localeCompare(pb[i]);
    }
    if (pa.length !== pb.length) return pb.length - pa.length;
    return pa[pa.length - 1].localeCompare(pb[pa.length - 1]);
  }

  // Fetch and build file objects from the API for a list of file infos.
  async function loadAllFileData(fileInfos, scope) {
    return Promise.all(fileInfos.map(async (fi) => {
      var diffUrl = '/api/file/diff?path=' + enc(fi.path);
      if (scope && scope !== 'all') {
        diffUrl += '&scope=' + enc(scope);
      }
      const [fileRes, commentsRes, diffRes] = await Promise.all([
        fetch('/api/file?path=' + enc(fi.path)).then(function(r) { return r.ok ? r.json() : { content: '' }; }).catch(function() { return { content: '' }; }),
        fetch('/api/file/comments?path=' + enc(fi.path)).then(function(r) { return r.ok ? r.json() : []; }).catch(function() { return []; }),
        fetch(diffUrl).then(function(r) { return r.ok ? r.json() : { hunks: [] }; }).catch(function() { return { hunks: [] }; }),
      ]);

      const f = {
        path: fi.path,
        status: fi.status,
        fileType: fi.file_type,
        content: fileRes.content || '',
        previousContent: diffRes.previous_content || '',
        comments: Array.isArray(commentsRes) ? commentsRes : [],
        diffHunks: diffRes.hunks || [],
        lineBlocks: null,
        previousLineBlocks: null,
        tocItems: [],
        collapsed: fi.status === 'deleted',
        viewMode: (session.mode === 'git') ? 'diff' : 'document',
        additions: fi.additions || 0,
        deletions: fi.deletions || 0,
      };

      // Mark large diffs for deferred rendering
      var diffLineCount = 0;
      for (var h = 0; h < f.diffHunks.length; h++) {
        diffLineCount += (f.diffHunks[h].Lines || []).length;
      }
      f.diffTooLarge = diffLineCount > 1000;
      f.diffLoaded = !f.diffTooLarge;

      // Pre-highlight code files for diff rendering
      if (f.fileType === 'code') {
        f.highlightCache = preHighlightFile(f);
        f.lang = langFromPath(f.path);

        // In file mode, build line blocks so code files render as document view
        if (session.mode !== 'git') {
          f.lineBlocks = buildCodeLineBlocks(f);
        }
      }

      // Parse markdown content into line blocks
      if (f.fileType === 'markdown') {
        const parsed = parseMarkdown(f.content);
        f.lineBlocks = parsed.blocks;
        f.tocItems = parsed.tocItems;
        if (f.previousContent) {
          f.previousLineBlocks = parseMarkdown(f.previousContent).blocks;
        }
      }

      return f;
    }));
  }

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
      fetch('/api/session?scope=' + enc(diffScope)).then(r => r.json()),
      fetch('/api/config').then(r => r.json()),
    ]);

    session = sessionRes;

    // Config
    shareURL = configRes.share_url || '';
    hostedURL = configRes.hosted_url || '';
    deleteToken = configRes.delete_token || '';
    configAuthor = configRes.author || '';

    if (shareURL && session.mode !== 'git') document.getElementById('shareBtn').style.display = '';
    if (hostedURL) showSharedNotice(hostedURL);

    // Version check
    if (configRes.latest_version && configRes.version && configRes.latest_version !== configRes.version) {
      const el = document.getElementById('headerUpdate');
      el.style.display = '';
      document.getElementById('updateLink').textContent = configRes.latest_version + ' available';
    }

    // Header context: branch name in git mode, filename in single-file file mode
    if (session.mode === 'git' && session.branch) {
      document.getElementById('branchContext').style.display = '';
      document.getElementById('branchName').textContent = session.branch;
    } else if (session.mode !== 'git' && session.files && session.files.length === 1) {
      document.getElementById('branchContext').style.display = '';
      document.querySelector('.branch-icon').innerHTML = '<svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor"><path fill-rule="evenodd" d="M3.75 1.5a.25.25 0 0 0-.25.25v12.5c0 .138.112.25.25.25h8.5a.25.25 0 0 0 .25-.25V6H9.75A1.75 1.75 0 0 1 8 4.25V1.5H3.75zm5.75.56v2.19c0 .138.112.25.25.25h2.19L9.5 2.06zM2 1.75C2 .784 2.784 0 3.75 0h5.086c.464 0 .909.184 1.237.513l3.414 3.414c.329.328.513.773.513 1.237v8.086A1.75 1.75 0 0 1 12.25 15h-8.5A1.75 1.75 0 0 1 2 13.25V1.75z"/></svg>';
      document.getElementById('branchName').textContent = session.files[0].path.split('/').pop();
    }

    // Show diff mode toggle in git mode (always has diffs)
    // In file mode, it gets shown later via updateDiffModeToggle() once diffs exist
    if (session.mode === 'git') {
      document.getElementById('diffModeToggle').style.display = '';
      document.querySelectorAll('#diffModeToggle .toggle-btn').forEach(function(b) {
        b.classList.toggle('active', b.dataset.mode === diffMode);
      });
      document.getElementById('tocToggle').style.display = 'none';

      // Show scope toggle and hide unavailable scopes
      var scopeToggle = document.getElementById('scopeToggle');
      scopeToggle.style.display = '';
      var scopes = session.available_scopes || ['all', 'staged', 'unstaged'];
      scopeToggle.querySelectorAll('.toggle-btn').forEach(function(b) {
        if (b.dataset.scope !== 'all' && scopes.indexOf(b.dataset.scope) === -1) {
          b.disabled = true;
          b.classList.add('disabled');
        }
      });
      if (scopes.indexOf(diffScope) === -1) {
        diffScope = 'all';
        setCookie('crit-diff-scope', 'all');
      }
      scopeToggle.querySelectorAll('.toggle-btn').forEach(function(b) {
        b.classList.toggle('active', b.dataset.scope === diffScope);
      });
    }

    // Hide file-mode-only shortcuts in git mode
    if (session.mode === 'git') {
      document.querySelectorAll('.shortcut-filemode-only').forEach(function(el) { el.style.display = 'none'; });
    }

    updateHeaderRound();
    document.title = session.mode === 'git'
      ? 'Crit — ' + (session.branch || 'review')
      : 'Crit — ' + (session.files || []).map(f => f.path).join(', ');

    files = await loadAllFileData(session.files || [], diffScope);

    files.sort(fileSortComparator);

    restoreViewedState();
    updateDiffModeToggle();
    renderFileTree();
    renderAllFiles();
    buildToc();
    updateCommentCount();
    updateViewedCount();
    restoreDrafts();
  }

  // Show/hide the Toggle Diff button and Split/Unified toggle in file mode
  function updateDiffModeToggle() {
    if (session.mode === 'git') return; // git mode handles this in init
    var hasDiffs = files.some(function(f) {
      return f.fileType === 'markdown' && f.previousLineBlocks && f.previousLineBlocks.length > 0;
    });
    var diffToggleBtn = document.getElementById('diffToggle');
    if (diffToggleBtn) {
      diffToggleBtn.style.display = hasDiffs ? '' : 'none';
      diffToggleBtn.classList.toggle('active', diffActive);
    }
    // Show Split/Unified toggle only when diff view is active
    document.getElementById('diffModeToggle').style.display = (hasDiffs && diffActive) ? '' : 'none';
    if (hasDiffs && diffActive) {
      document.querySelectorAll('#diffModeToggle .toggle-btn').forEach(function(b) {
        b.classList.toggle('active', b.dataset.mode === diffMode);
      });
    }
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

  // Build line blocks for code files in file mode (document view)
  function buildCodeLineBlocks(file) {
    const lines = file.content.split('\n');
    const blocks = [];
    for (let i = 0; i < lines.length; i++) {
      const lineNum = i + 1;
      let html;
      if (file.highlightCache && file.highlightCache[lineNum]) {
        html = '<code class="hljs">' + file.highlightCache[lineNum] + '</code>';
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

        // Mermaid diagrams: render as a single block (not split per-line)
        if (lang === 'mermaid') {
          blocks.push({
            startLine: blockStart + 1, endLine: blockEnd,
            html: '<pre><code class="language-mermaid">' + escapeHtml(token.content) + '</code></pre>',
            isEmpty: false, cssClass: 'mermaid-block'
          });
          i++;
          coveredUpTo = blockEnd;
          addGapLines(blockEnd);
          continue;
        }

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
  var ignoreTreeObserverUntil = 0;
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

      // In file mode, show plain file icon (no git status badge)
      var iconHtml = session.mode === 'git' ? fileStatusIcon(f.status) : fileStatusIcon('');
      var innerHtml =
        '<span class="tree-file-icon">' + iconHtml + '</span>' +
        '<span class="tree-file-name">' + escapeHtml(fileName) + '</span>';

      if (f.viewed) {
        innerHtml += '<span class="tree-viewed-check" title="Viewed">&#10003;</span>';
      }
      var unresolvedCount = f.comments.filter(function(c) { return !c.resolved; }).length;
      if (unresolvedCount > 0) {
        innerHtml += '<span class="tree-comment-badge">' + unresolvedCount + '</span>';
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
    // Scroll active item into view within the tree panel (manual scroll
    // to avoid scrollIntoView affecting ancestor scroll containers)
    var activeEl = document.querySelector('.tree-file.active');
    if (activeEl) {
      var panel = document.getElementById('fileTreeBody');
      var rect = activeEl.getBoundingClientRect();
      var panelRect = panel.getBoundingClientRect();
      if (rect.top < panelRect.top) {
        panel.scrollTop += rect.top - panelRect.top;
      } else if (rect.bottom > panelRect.bottom) {
        panel.scrollTop += rect.bottom - panelRect.bottom;
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
      var count = file.comments.filter(function(c) { return !c.resolved; }).length;
      if (count > 0) {
        if (badge) {
          badge.textContent = count;
        } else {
          badge = document.createElement('span');
          badge.className = 'tree-comment-badge';
          badge.textContent = count;
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
      // Skip observer updates briefly after a manual scrollToFile click
      if (Date.now() < ignoreTreeObserverUntil) return;
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
    // Suppress IntersectionObserver for 200ms so it doesn't override our manual active state
    ignoreTreeObserverUntil = Date.now() + 200;
    sectionEl.scrollIntoView({ block: 'start', behavior: 'instant' });
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
    buildChangeGroups();
  }

  function buildChangeGroups() {
    changeGroups = [];
    // Document view: color-coded change blocks + deletion markers
    var docEls = document.querySelectorAll('.line-block-added, .line-block-modified, .deletion-marker');
    // Diff view: diff-added and diff-removed blocks in rendered diff (file mode)
    var diffEls = document.querySelectorAll('.diff-view .line-block.diff-added, .diff-view .line-block.diff-removed, .diff-view-unified .line-block.diff-added, .diff-view-unified .line-block.diff-removed');
    var all = docEls.length > 0 ? docEls : diffEls;
    if (all.length === 0) { currentChangeIdx = -1; updateChangeCounters(); return; }
    var group = null;
    for (var i = 0; i < all.length; i++) {
      var el = all[i];
      var fp = el.dataset.filePath;
      // Start new group if file changes or elements aren't consecutive siblings
      if (!group || group.filePath !== fp || !isConsecutiveSibling(group.elements[group.elements.length - 1], el)) {
        group = { elements: [el], filePath: fp };
        changeGroups.push(group);
      } else {
        group.elements.push(el);
      }
    }
    currentChangeIdx = -1;
    updateChangeCounters();
  }

  function isConsecutiveSibling(a, b) {
    // Check if b immediately follows a, skipping comment elements between them
    var node = a.nextElementSibling;
    while (node && node !== b) {
      // A non-changed line-block in between breaks the group
      if (node.classList.contains('line-block') &&
          !node.classList.contains('line-block-added') &&
          !node.classList.contains('line-block-modified') &&
          !node.classList.contains('diff-added') &&
          !node.classList.contains('diff-removed')) return false;
      // Deletion markers don't break the group
      if (node.classList.contains('deletion-marker')) { node = node.nextElementSibling; continue; }
      node = node.nextElementSibling;
    }
    return node === b;
  }

  function navigateToChange(dir) {
    if (changeGroups.length === 0) return;
    // Remove previous flash
    document.querySelectorAll('.change-flash').forEach(function(el) { el.classList.remove('change-flash'); });

    var viewCenter = window.innerHeight / 2;
    var threshold = 50;
    var targetIdx = -1;

    // Check if the previously navigated change is still near viewport center
    // (i.e. user hasn't scrolled away manually)
    var currentIsCentered = false;
    if (currentChangeIdx >= 0 && currentChangeIdx < changeGroups.length) {
      var curRect = changeGroups[currentChangeIdx].elements[0].getBoundingClientRect();
      var curCenter = (curRect.top + curRect.bottom) / 2;
      currentIsCentered = Math.abs(curCenter - viewCenter) < threshold * 3;
    }

    if (currentIsCentered) {
      // User hasn't scrolled away — use index-based next/prev with wrapping
      if (dir > 0) {
        targetIdx = (currentChangeIdx + 1) % changeGroups.length;
      } else {
        targetIdx = (currentChangeIdx - 1 + changeGroups.length) % changeGroups.length;
      }
    } else {
      // User scrolled manually — find next/prev relative to viewport position
      if (dir > 0) {
        for (var i = 0; i < changeGroups.length; i++) {
          var rect = changeGroups[i].elements[0].getBoundingClientRect();
          var elCenter = (rect.top + rect.bottom) / 2;
          if (elCenter > viewCenter + threshold) { targetIdx = i; break; }
        }
        if (targetIdx === -1) targetIdx = 0;
      } else {
        for (var i = changeGroups.length - 1; i >= 0; i--) {
          var rect = changeGroups[i].elements[0].getBoundingClientRect();
          var elCenter = (rect.top + rect.bottom) / 2;
          if (elCenter < viewCenter - threshold) { targetIdx = i; break; }
        }
        if (targetIdx === -1) targetIdx = changeGroups.length - 1;
      }
    }

    currentChangeIdx = targetIdx;
    var group = changeGroups[currentChangeIdx];
    group.elements[0].scrollIntoView({ block: 'center', behavior: 'instant' });
    group.elements.forEach(function(el) { el.classList.add('change-flash'); });
    focusedElement = group.elements[0];
    focusedFilePath = group.filePath;
    var bi = parseInt(group.elements[0].dataset.blockIndex);
    if (!isNaN(bi)) focusedBlockIndex = bi;
    updateChangeCounters();
  }

  function updateChangeCounters() {
    var labels = document.querySelectorAll('.change-nav-label');
    labels.forEach(function(label) {
      var fp = label.dataset.filePath;
      // Count groups for this file
      var fileGroups = changeGroups.filter(function(g) { return g.filePath === fp; });
      var total = fileGroups.length;
      // Find current index within this file's groups
      var current = 0;
      if (currentChangeIdx >= 0) {
        var globalGroup = changeGroups[currentChangeIdx];
        if (globalGroup.filePath === fp) {
          current = fileGroups.indexOf(globalGroup) + 1;
        }
      }
      label.textContent = (current || '-') + ' / ' + total + ' change' + (total !== 1 ? 's' : '');
    });
  }

  // Re-render only a single file section (preserves scroll position)
  function saveOpenFormContent(filePath) {
    var fileForms = getFormsForFile(filePath);
    for (var i = 0; i < fileForms.length; i++) {
      var ta = document.querySelector('.comment-form[data-form-key="' + fileForms[i].formKey + '"] textarea');
      if (ta) fileForms[i].draftBody = ta.value;
    }
  }

  function renderFileByPath(filePath) {
    const file = getFileByPath(filePath);
    if (!file) return;
    saveOpenFormContent(filePath);
    const oldSection = document.getElementById('file-section-' + file.path);
    if (!oldSection) { renderAllFiles(); return; }
    oldSection.replaceWith(renderFileSection(file));
    renderMermaidBlocks();
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

    const fileUnresolvedCount = file.comments.filter(function(c) { return !c.resolved; }).length;
    const dirParts = file.path.split('/');
    const fileName = dirParts.pop();
    const dirPath = dirParts.length > 0 ? dirParts.join('/') + '/' : '';

    // In file mode, hide the badge (status like "modified" is only meaningful in git mode)
    var showBadge = session.mode === 'git';
    let badgeLabel = file.status.charAt(0).toUpperCase() + file.status.slice(1);
    if (file.status === 'untracked') badgeLabel = 'New';
    if (file.status === 'added') badgeLabel = 'New File';

    // In single-file file mode, hide the file header (filename is shown in the header bar)
    var singleFileMode = session.mode !== 'git' && files.length === 1;
    if (singleFileMode) header.style.display = 'none';

    header.innerHTML =
      '<div class="file-header-chevron"><svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor"><path d="M12.78 5.22a.749.749 0 0 1 0 1.06l-4.25 4.25a.749.749 0 0 1-1.06 0L3.22 6.28a.749.749 0 1 1 1.06-1.06L8 8.939l3.72-3.719a.749.749 0 0 1 1.06 0Z"/></svg></div>' +
      '<svg class="file-header-icon" viewBox="0 0 16 16" fill="var(--fg-dimmed)"><path fill-rule="evenodd" d="M3.75 1.5a.25.25 0 0 0-.25.25v12.5c0 .138.112.25.25.25h8.5a.25.25 0 0 0 .25-.25V6H9.75A1.75 1.75 0 0 1 8 4.25V1.5H3.75zm5.75.56v2.19c0 .138.112.25.25.25h2.19L9.5 2.06zM2 1.75C2 .784 2.784 0 3.75 0h5.086c.464 0 .909.184 1.237.513l3.414 3.414c.329.328.513.773.513 1.237v8.086A1.75 1.75 0 0 1 12.25 15h-8.5A1.75 1.75 0 0 1 2 13.25V1.75z"/></svg>' +
      '<span class="file-header-name"><span class="dir">' + escapeHtml(dirPath) + '</span>' + escapeHtml(fileName) + '</span>' +
      (showBadge ? '<span class="file-header-badge ' + escapeHtml(file.status) + '">' + escapeHtml(badgeLabel) + '</span>' : '') +
      (file.additions || file.deletions ? '<span class="file-header-stats">' +
        (file.additions ? '<span class="add">+' + file.additions + '</span>' : '') +
        (file.deletions ? '<span class="del">-' + file.deletions + '</span>' : '') +
      '</span>' : '') +
      (fileUnresolvedCount > 0 ? '<span class="file-header-comment-count">' +
        '<svg viewBox="0 0 16 16" fill="currentColor"><path d="M1 2.75C1 1.784 1.784 1 2.75 1h10.5c.966 0 1.75.784 1.75 1.75v7.5A1.75 1.75 0 0 1 13.25 12H9.06l-2.573 2.573A1.458 1.458 0 0 1 4 13.543V12H2.75A1.75 1.75 0 0 1 1 10.25Zm1.75-.25a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h2a.75.75 0 0 1 .75.75v2.19l2.72-2.72a.749.749 0 0 1 .53-.22h4.5a.25.25 0 0 0 .25-.25v-7.5a.25.25 0 0 0-.25-.25Z"/></svg>' +
        fileUnresolvedCount + '</span>' : '');

    // Add document/diff toggle for markdown files that have diff hunks
    // Hide when diffActive is on (header-level rendered diff overrides per-file toggle)
    if (file.fileType === 'markdown' && file.diffHunks && file.diffHunks.length > 0 && !diffActive) {
      const toggle = document.createElement('div');
      toggle.className = 'file-header-toggle';
      toggle.innerHTML =
        '<button class="toggle-btn' + (file.viewMode === 'document' ? ' active' : '') + '" data-mode="document">Document</button>' +
        '<button class="toggle-btn' + (file.viewMode === 'diff' ? ' active' : '') + '" data-mode="diff">Diff</button>';
      toggle.addEventListener('click', function(e) {
        const btn = e.target.closest('.toggle-btn');
        if (!btn) return;
        e.preventDefault(); // Don't toggle the <details>
        var fileForms = getFormsForFile(file.path);
        fileForms.forEach(function(f) { removeForm(f.formKey); });
        if (activeFilePath === file.path) {
          selectionStart = null;
          selectionEnd = null;
        }
        file.viewMode = btn.dataset.mode;
        renderFileByPath(file.path);
      });
      header.appendChild(toggle);

      // Change navigation widget (file mode, both document and diff view)
      if (session.mode !== 'git') {
        var changeNav = document.createElement('div');
        changeNav.className = 'change-nav';
        changeNav.innerHTML =
          '<button class="change-nav-btn" data-dir="-1" title="Previous change (N)">&#9650;</button>' +
          '<span class="change-nav-label" data-file-path="' + escapeHtml(file.path) + '"></span>' +
          '<button class="change-nav-btn" data-dir="1" title="Next change (n)">&#9660;</button>';
        changeNav.addEventListener('click', function(e) {
          var btn = e.target.closest('.change-nav-btn');
          if (!btn) return;
          e.preventDefault();
          e.stopPropagation();
          navigateToChange(parseInt(btn.dataset.dir));
        });
        header.appendChild(changeNav);
      }
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

    var showDiff = file.viewMode === 'diff' || (file.fileType === 'code' && session.mode === 'git');

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
    } else if (diffActive && file.previousLineBlocks && file.previousLineBlocks.length > 0) {
      body.appendChild(diffMode === 'split' ? renderRenderedDiffSplit(file) : renderRenderedDiffUnified(file));
    } else {
      body.appendChild(renderDocumentView(file));
    }

    section.appendChild(body);
    return section;
  }

  // ===== Rendered Diff View (Markdown, file mode) =====

  // Build sets of added/removed line numbers from diff hunks
  function buildDiffLineSetFromHunks(hunks) {
    var added = new Set();
    var removed = new Set();
    for (var h = 0; h < hunks.length; h++) {
      var lines = hunks[h].Lines || [];
      for (var l = 0; l < lines.length; l++) {
        if (lines[l].Type === 'add' && lines[l].NewNum) added.add(lines[l].NewNum);
        if (lines[l].Type === 'del' && lines[l].OldNum) removed.add(lines[l].OldNum);
      }
    }
    return { added: added, removed: removed };
  }

  // Classify a block as diff-added, diff-removed, or unchanged
  function classifyBlock(block, changedLines) {
    for (var ln = block.startLine; ln <= block.endLine; ln++) {
      if (changedLines.has(ln)) return true;
    }
    return false;
  }

  // Render a single side of the rendered diff (reuses document view block pattern)
  function renderRenderedDiffBlocks(blocks, diffClass, file, enableComments) {
    var container = document.createElement('div');
    container.className = 'diff-view-blocks';

    var commentsMap = enableComments ? buildCommentsMap(file.comments) : {};
    var commentRangeSet = enableComments ? buildCommentedRangeSet(file.comments) : new Set();

    for (var bi = 0; bi < blocks.length; bi++) {
      var block = blocks[bi];

      var lineBlockEl = document.createElement('div');
      lineBlockEl.className = 'line-block';
      lineBlockEl.dataset.filePath = file.path;
      if (enableComments) {
        lineBlockEl.classList.add('kb-nav');
        lineBlockEl.dataset.blockIndex = bi;
        lineBlockEl.dataset.startLine = block.startLine;
        lineBlockEl.dataset.endLine = block.endLine;
      }

      if (block.isDiff) lineBlockEl.classList.add(diffClass);

      if (enableComments) {
        var blockComments = getCommentsForBlock(block, commentsMap);
        var blockInCommentRange = false;
        for (var ln = block.startLine; ln <= block.endLine; ln++) {
          if (commentRangeSet.has(ln + ':')) { blockInCommentRange = true; break; }
        }
        if (blockInCommentRange) lineBlockEl.classList.add('has-comment');

        var fileForms1 = getFormsForFile(file.path);
        var hasFormForBlock1 = fileForms1.some(function(f) {
          return !f.editingId && block.startLine >= f.startLine && block.endLine <= f.endLine;
        });
        var inCurrentSelection1 = activeFilePath === file.path && selectionStart !== null && selectionEnd !== null &&
          block.startLine >= selectionStart && block.endLine <= selectionEnd;
        if (inCurrentSelection1) { lineBlockEl.classList.add('selected'); }
        if (hasFormForBlock1 && !inCurrentSelection1) { lineBlockEl.classList.add('form-selected'); }

        (function(fp, idx, el) {
          lineBlockEl.addEventListener('mouseenter', function() {
            focusedFilePath = fp;
            focusedBlockIndex = idx;
            focusedElement = el;
          });
        })(file.path, bi, lineBlockEl);

        if (focusedFilePath === file.path && focusedBlockIndex === bi) {
          lineBlockEl.classList.add('focused');
        }
      }

      // Line number gutter
      var gutter = document.createElement('div');
      gutter.className = 'line-gutter';
      var lineNum = document.createElement('span');
      lineNum.className = 'line-num';
      lineNum.textContent = block.startLine;
      gutter.appendChild(lineNum);
      lineBlockEl.appendChild(gutter);

      // Comment gutter
      var commentGutter = document.createElement('div');
      commentGutter.className = 'line-comment-gutter';
      if (enableComments) {
        commentGutter.dataset.startLine = block.startLine;
        commentGutter.dataset.endLine = block.endLine;
        commentGutter.dataset.filePath = file.path;
        var lineAdd = document.createElement('span');
        lineAdd.className = 'line-add';
        lineAdd.textContent = '+';
        commentGutter.appendChild(lineAdd);
        commentGutter.addEventListener('mousedown', handleGutterMouseDown);
      } else {
        commentGutter.classList.add('diff-no-comment');
      }
      lineBlockEl.appendChild(commentGutter);

      // Content
      var content = document.createElement('div');
      var contentClasses = 'line-content';
      if (block.isEmpty) contentClasses += ' empty-line';
      if (block.cssClass) contentClasses += ' ' + block.cssClass;
      content.className = contentClasses;
      var html = block.html;
      html = processTaskLists(html);
      html = rewriteImageSrcs(html);
      content.innerHTML = html;

      lineBlockEl.appendChild(content);
      container.appendChild(lineBlockEl);

      // Comments after block (only on current/right side)
      if (enableComments && blockComments) {
        for (var ci = 0; ci < blockComments.length; ci++) {
          if (blockComments[ci].resolved) {
            container.appendChild(createResolvedElement(blockComments[ci]));
          } else {
            container.appendChild(createCommentElement(blockComments[ci], file.path));
          }
        }
        var fileForms = getFormsForFile(file.path);
        for (var fi = 0; fi < fileForms.length; fi++) {
          if (!fileForms[fi].editingId && fileForms[fi].afterBlockIndex === bi) {
            container.appendChild(createCommentForm(fileForms[fi]));
          }
        }
      }
    }
    return container;
  }

  // Annotate blocks with isDiff flag based on changed line numbers
  function annotateBlocks(blocks, changedLines) {
    return blocks.map(function(b) {
      return Object.assign({}, b, { isDiff: classifyBlock(b, changedLines) });
    });
  }

  function renderRenderedDiffSplit(file) {
    var container = document.createElement('div');
    container.className = 'diff-view';

    var lineSets = buildDiffLineSetFromHunks(file.diffHunks);
    var prevBlocks = annotateBlocks(file.previousLineBlocks, lineSets.removed);
    var currBlocks = annotateBlocks(file.lineBlocks, lineSets.added);

    // Left side (previous round — removed highlighting, no comments)
    var leftSide = document.createElement('div');
    leftSide.className = 'diff-view-side';
    var leftLabel = document.createElement('div');
    leftLabel.className = 'diff-view-side-label';
    leftLabel.textContent = 'Previous round';
    leftSide.appendChild(leftLabel);
    leftSide.appendChild(renderRenderedDiffBlocks(prevBlocks, 'diff-removed', file, false));

    // Right side (current round — added highlighting, comments enabled)
    var rightSide = document.createElement('div');
    rightSide.className = 'diff-view-side';
    var rightLabel = document.createElement('div');
    rightLabel.className = 'diff-view-side-label';
    rightLabel.textContent = 'Current round';
    rightSide.appendChild(rightLabel);
    rightSide.appendChild(renderRenderedDiffBlocks(currBlocks, 'diff-added', file, true));

    container.appendChild(leftSide);
    container.appendChild(rightSide);
    return container;
  }

  // Render a single block for the unified diff view.
  // When commentable=true, includes gutter, keyboard nav, comments. Otherwise read-only.
  function renderUnifiedBlock(block, diffClass, file, commentable, blockIndex, commentsMap, commentRangeSet) {
    var frag = document.createDocumentFragment();

    var lineBlockEl = document.createElement('div');
    lineBlockEl.className = 'line-block';
    lineBlockEl.dataset.filePath = file.path;
    if (commentable) {
      lineBlockEl.classList.add('kb-nav');
      lineBlockEl.dataset.blockIndex = blockIndex;
      lineBlockEl.dataset.startLine = block.startLine;
      lineBlockEl.dataset.endLine = block.endLine;
    }
    if (diffClass) lineBlockEl.classList.add(diffClass);

    var blockComments = null;
    if (commentable) {
      blockComments = getCommentsForBlock(block, commentsMap);
      var blockInCommentRange = false;
      for (var ln = block.startLine; ln <= block.endLine; ln++) {
        if (commentRangeSet.has(ln + ':')) { blockInCommentRange = true; break; }
      }
      if (blockInCommentRange) lineBlockEl.classList.add('has-comment');

      var fileForms2 = getFormsForFile(file.path);
      var hasFormForBlock2 = fileForms2.some(function(f) {
        return !f.editingId && block.startLine >= f.startLine && block.endLine <= f.endLine;
      });
      var inCurrentSelection2 = activeFilePath === file.path && selectionStart !== null && selectionEnd !== null &&
        block.startLine >= selectionStart && block.endLine <= selectionEnd;
      if (inCurrentSelection2) { lineBlockEl.classList.add('selected'); }
      if (hasFormForBlock2 && !inCurrentSelection2) { lineBlockEl.classList.add('form-selected'); }

      (function(fp, idx, el) {
        lineBlockEl.addEventListener('mouseenter', function() {
          focusedFilePath = fp;
          focusedBlockIndex = idx;
          focusedElement = el;
        });
      })(file.path, blockIndex, lineBlockEl);

      if (focusedFilePath === file.path && focusedBlockIndex === blockIndex) {
        lineBlockEl.classList.add('focused');
      }

      var commentGutter = document.createElement('div');
      commentGutter.className = 'line-comment-gutter';
      commentGutter.dataset.startLine = block.startLine;
      commentGutter.dataset.endLine = block.endLine;
      commentGutter.dataset.filePath = file.path;
      var lineAdd = document.createElement('span');
      lineAdd.className = 'line-add';
      lineAdd.textContent = '+';
      commentGutter.appendChild(lineAdd);
      commentGutter.addEventListener('mousedown', handleGutterMouseDown);
      lineBlockEl.appendChild(commentGutter);
    } else {
      // Non-commentable block: still add gutter but mark as read-only
      var roGutter = document.createElement('div');
      roGutter.className = 'line-comment-gutter diff-no-comment';
      lineBlockEl.appendChild(roGutter);
    }

    // Line number gutter
    var gutter = document.createElement('div');
    gutter.className = 'line-gutter';
    var lineNum = document.createElement('span');
    lineNum.className = 'line-num';
    lineNum.textContent = block.startLine;
    gutter.appendChild(lineNum);
    lineBlockEl.insertBefore(gutter, lineBlockEl.firstChild);

    var contentEl = document.createElement('div');
    var contentClasses = 'line-content';
    if (block.isEmpty) contentClasses += ' empty-line';
    if (block.cssClass) contentClasses += ' ' + block.cssClass;
    contentEl.className = contentClasses;
    var html = block.html;
    html = processTaskLists(html);
    html = rewriteImageSrcs(html);
    contentEl.innerHTML = html;
    lineBlockEl.appendChild(contentEl);

    frag.appendChild(lineBlockEl);

    // Comments after block (only on commentable/new side)
    if (commentable && blockComments) {
      for (var ci = 0; ci < blockComments.length; ci++) {
        if (blockComments[ci].resolved) {
          frag.appendChild(createResolvedElement(blockComments[ci]));
        } else {
          frag.appendChild(createCommentElement(blockComments[ci], file.path));
        }
      }
      var fileForms = getFormsForFile(file.path);
      for (var fi = 0; fi < fileForms.length; fi++) {
        if (!fileForms[fi].editingId && fileForms[fi].afterBlockIndex === blockIndex) {
          frag.appendChild(createCommentForm(fileForms[fi]));
        }
      }
    }

    return frag;
  }

  function renderRenderedDiffUnified(file) {
    var container = document.createElement('div');
    container.className = 'diff-view-unified';

    var lineSets = buildDiffLineSetFromHunks(file.diffHunks);
    var oldBlocks = file.previousLineBlocks;
    var newBlocks = file.lineBlocks;

    var commentsMap = buildCommentsMap(file.comments);
    var commentRangeSet = buildCommentedRangeSet(file.comments);

    // Two-pointer merge: walk both block lists simultaneously
    var oldIdx = 0;
    var newIdx = 0;

    while (oldIdx < oldBlocks.length || newIdx < newBlocks.length) {
      if (oldIdx >= oldBlocks.length) {
        // Old exhausted — remaining new blocks are additions
        container.appendChild(renderUnifiedBlock(newBlocks[newIdx], 'diff-added', file, true, newIdx, commentsMap, commentRangeSet));
        newIdx++;
      } else if (newIdx >= newBlocks.length) {
        // New exhausted — remaining old blocks are deletions
        container.appendChild(renderUnifiedBlock(oldBlocks[oldIdx], 'diff-removed', file, false, oldIdx, null, null));
        oldIdx++;
      } else if (classifyBlock(oldBlocks[oldIdx], lineSets.removed)) {
        // Old block is removed — emit with strikethrough
        container.appendChild(renderUnifiedBlock(oldBlocks[oldIdx], 'diff-removed', file, false, oldIdx, null, null));
        oldIdx++;
      } else if (classifyBlock(newBlocks[newIdx], lineSets.added)) {
        // New block is added — emit with green highlight + comments
        container.appendChild(renderUnifiedBlock(newBlocks[newIdx], 'diff-added', file, true, newIdx, commentsMap, commentRangeSet));
        newIdx++;
      } else {
        // Both unchanged — emit new block once (with comments), advance both
        container.appendChild(renderUnifiedBlock(newBlocks[newIdx], null, file, true, newIdx, commentsMap, commentRangeSet));
        newIdx++;
        oldIdx++;
      }
    }

    return container;
  }

  // ===== Change Detection (for inter-round diffs in document view) =====
  // Returns { added: Set<NewNum>, modified: Set<NewNum>, deletionPoints: [{afterLine, count}] }
  // added = pure additions (green), modified = changed lines (amber), deletionPoints = where lines were removed (red)
  function getChangeInfo(file) {
    if (!file.diffHunks || file.diffHunks.length === 0) return null;
    var added = new Set();
    var modified = new Set();
    var deletionPoints = [];

    for (var h = 0; h < file.diffHunks.length; h++) {
      var lines = file.diffHunks[h].Lines || [];
      var lastContextNewNum = file.diffHunks[h].NewStart > 0 ? file.diffHunks[h].NewStart - 1 : 0;
      var i = 0;
      while (i < lines.length) {
        if (lines[i].Type === 'context') {
          lastContextNewNum = lines[i].NewNum;
          i++;
        } else {
          // Collect consecutive change group (dels then adds, or interleaved)
          var dels = [], adds = [];
          while (i < lines.length && lines[i].Type !== 'context') {
            if (lines[i].Type === 'del') dels.push(lines[i]);
            if (lines[i].Type === 'add') adds.push(lines[i]);
            i++;
          }
          if (dels.length > 0 && adds.length > 0) {
            // Modification: mark add lines as modified (amber)
            for (var a = 0; a < adds.length; a++) {
              if (adds[a].NewNum) modified.add(adds[a].NewNum);
            }
          } else if (adds.length > 0) {
            // Pure addition (green)
            for (var a = 0; a < adds.length; a++) {
              if (adds[a].NewNum) added.add(adds[a].NewNum);
            }
          } else if (dels.length > 0) {
            // Pure deletion — record where marker should appear
            deletionPoints.push({ afterLine: lastContextNewNum, count: dels.length });
          }
          // Update last context position if we saw adds
          if (adds.length > 0) {
            lastContextNewNum = adds[adds.length - 1].NewNum;
          }
        }
      }
    }
    if (added.size === 0 && modified.size === 0 && deletionPoints.length === 0) return null;
    return { added: added, modified: modified, deletionPoints: deletionPoints };
  }

  // ===== Document View (Markdown) =====
  function renderDocumentView(file) {
    const container = document.createElement('div');
    container.className = 'document-wrapper' + (file.fileType === 'code' ? ' code-document' : '');
    if (!file.lineBlocks) return container;

    const commentsMap = buildCommentsMap(file.comments);

    const commentRangeSet = buildCommentedRangeSet(file.comments);

    const changeInfo = (file.viewMode === 'document' && session.mode !== 'git') ? getChangeInfo(file) : null;
    // Build a map of afterLine -> deletion marker for quick lookup
    var deletionMarkerMap = {};
    if (changeInfo) {
      for (var dp = 0; dp < changeInfo.deletionPoints.length; dp++) {
        var pt = changeInfo.deletionPoints[dp];
        deletionMarkerMap[pt.afterLine] = pt;
      }
    }

    for (let bi = 0; bi < file.lineBlocks.length; bi++) {
      const block = file.lineBlocks[bi];

      const lineBlockEl = document.createElement('div');
      lineBlockEl.className = 'line-block kb-nav';
      lineBlockEl.dataset.blockIndex = bi;
      lineBlockEl.dataset.startLine = block.startLine;
      lineBlockEl.dataset.endLine = block.endLine;
      lineBlockEl.dataset.filePath = file.path;

      const blockComments = getCommentsForBlock(block, commentsMap);
      // Highlight all blocks in the comment's line range
      var blockInCommentRange = false;
      for (let ln = block.startLine; ln <= block.endLine; ln++) {
        if (commentRangeSet.has(ln + ':')) { blockInCommentRange = true; break; }
      }
      if (blockInCommentRange) lineBlockEl.classList.add('has-comment');

      // Mark blocks that overlap inter-round changes (color-coded)
      if (changeInfo) {
        var blockChangeType = null;
        for (let ln = block.startLine; ln <= block.endLine; ln++) {
          if (changeInfo.modified.has(ln)) { blockChangeType = 'modified'; break; }
          if (changeInfo.added.has(ln)) { blockChangeType = 'added'; }
        }
        if (blockChangeType === 'modified') lineBlockEl.classList.add('line-block-modified');
        else if (blockChangeType === 'added') lineBlockEl.classList.add('line-block-added');
      }

      // Selection highlight (during drag or when form is open)
      var fileForms3 = getFormsForFile(file.path);
      var hasFormForBlock3 = fileForms3.some(function(f) {
        return !f.editingId && block.startLine >= f.startLine && block.endLine <= f.endLine;
      });
      var inCurrentSelection3 = activeFilePath === file.path && selectionStart !== null && selectionEnd !== null &&
        block.startLine >= selectionStart && block.endLine <= selectionEnd;
      if (inCurrentSelection3) { lineBlockEl.classList.add('selected'); }
      if (hasFormForBlock3 && !inCurrentSelection3) { lineBlockEl.classList.add('form-selected'); }

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

      gutter.appendChild(commentGutter);
      lineBlockEl.appendChild(gutter);
      lineBlockEl.appendChild(content);

      // Insert deletion marker before this block if deletions occurred before it
      if (changeInfo && bi === 0 && deletionMarkerMap[0]) {
        var marker0 = document.createElement('div');
        marker0.className = 'deletion-marker';
        marker0.dataset.filePath = file.path;
        marker0.textContent = '\u2212' + deletionMarkerMap[0].count + ' line' + (deletionMarkerMap[0].count !== 1 ? 's' : '');
        container.appendChild(marker0);
      }

      container.appendChild(lineBlockEl);

      // Insert deletion marker after this block if deletions occurred after it
      if (changeInfo && deletionMarkerMap[block.endLine]) {
        var marker = document.createElement('div');
        marker.className = 'deletion-marker';
        marker.dataset.filePath = file.path;
        marker.textContent = '\u2212' + deletionMarkerMap[block.endLine].count + ' line' + (deletionMarkerMap[block.endLine].count !== 1 ? 's' : '');
        container.appendChild(marker);
      }

      // Comments after block
      for (const comment of blockComments) {
        if (comment.resolved) {
          container.appendChild(createResolvedElement(comment));
        } else {
          container.appendChild(createCommentElement(comment, file.path));
        }
      }

      // Comment form
      var fileForms = getFormsForFile(file.path);
      for (var fi = 0; fi < fileForms.length; fi++) {
        if (!fileForms[fi].editingId && fileForms[fi].afterBlockIndex === bi) {
          container.appendChild(createCommentForm(fileForms[fi]));
        }
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

    var fp = diffDragState.filePath;
    var side = diffDragState.side;
    diffDragState = null;
    unifiedVisualStart = null;
    unifiedVisualEnd = null;
    openForm({
      filePath: fp,
      afterBlockIndex: null,
      startLine: rangeStart,
      endLine: rangeEnd,
      editingId: null,
      side: side,
    });
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
      var el = comment.resolved
        ? createResolvedElement(comment)
        : createCommentElement(comment, filePath);
      if (side === 'old') el.classList.add('diff-comment-left');
      else el.classList.add('diff-comment-right');
      container.appendChild(el);
    }
  }

  // Helper: append comment form if it targets this line and side
  function appendDiffForm(container, filePath, lineNum, side) {
    var fileForms = getFormsForFile(filePath);
    for (var fi = 0; fi < fileForms.length; fi++) {
      var form = fileForms[fi];
      var formSide = form.side || '';
      if (!form.editingId && form.endLine === lineNum && formSide === (side || '')) {
        var el = createCommentForm(form);
        if (formSide === 'old') el.classList.add('diff-comment-left');
        else el.classList.add('diff-comment-right');
        container.appendChild(el);
      }
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
    const commentVisualSet = buildUnifiedCommentVisualSet(hunks, file.comments);
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
        if (commentVisualSet.has(visualIdx)) lineEl.classList.add('has-comment');

        // Tag for drag detection and selection highlighting
        if (commentLineNum) {
          tagDiffLine(lineEl, file.path, commentLineNum, lineSide);
          if (activeFilePath === file.path) {
            var inCurrentDrag = diffDragState && unifiedVisualStart !== null && unifiedVisualEnd !== null &&
                visualIdx >= unifiedVisualStart && visualIdx <= unifiedVisualEnd;
            var formSide = activeForms.length > 0 ? (activeForms[activeForms.length - 1].side || '') : '';
            var inCurrentForm = !diffDragState && selectionStart !== null && selectionEnd !== null &&
                lineSide === formSide && commentLineNum >= selectionStart && commentLineNum <= selectionEnd;
            var inCurrentSelUnified = inCurrentDrag || inCurrentForm;
            var hasFormUnified = getFormsForFile(file.path).some(function(f) {
              return !f.editingId && commentLineNum >= f.startLine && commentLineNum <= f.endLine && (f.side || '') === lineSide;
            });
            if (inCurrentSelUnified) { lineEl.classList.add('selected'); }
            if (hasFormUnified && !inCurrentSelUnified) { lineEl.classList.add('form-selected'); }
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
    const commentRangeSet = buildCommentedRangeSet(file.comments);

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
            file, commentRangeSet
          );
          container.appendChild(row.el);
          // Context lines: form appears where clicked (left or right),
          // but submitted comments always render on the right, like GitHub
          var ctxComments = [
            ...(commentsMap[line.OldNum + ':old'] || []),
            ...(commentsMap[line.NewNum + ':'] || [])
          ];
          for (var ci = 0; ci < ctxComments.length; ci++) {
            var el = ctxComments[ci].resolved
              ? createResolvedElement(ctxComments[ci])
              : createCommentElement(ctxComments[ci], file.path);
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
              file, commentRangeSet
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
  function makeSplitRow(left, right, file, commentRangeSet) {
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
      if (commentRangeSet.has(left.num + ':old')) leftEl.classList.add('has-comment');
      var selSide = diffDragState ? diffDragState.side : (activeForms.length > 0 ? activeForms[activeForms.length - 1].side : null);
      var inCurrentSelLeft = activeFilePath === file.path && selectionStart !== null && selectionEnd !== null &&
          left.num >= selectionStart && left.num <= selectionEnd && selSide === 'old';
      var hasFormLeft = getFormsForFile(file.path).some(function(f) {
        return !f.editingId && left.num >= f.startLine && left.num <= f.endLine && (f.side || '') === 'old';
      });
      if (inCurrentSelLeft) { leftEl.classList.add('selected'); }
      if (hasFormLeft && !inCurrentSelLeft) { leftEl.classList.add('form-selected'); }
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
      if (commentRangeSet.has(right.num + ':')) rightEl.classList.add('has-comment');
      var selSideR = diffDragState ? diffDragState.side : (activeForms.length > 0 ? activeForms[activeForms.length - 1].side : null);
      var inCurrentSelRight = activeFilePath === file.path && selectionStart !== null && selectionEnd !== null &&
          right.num >= selectionStart && right.num <= selectionEnd && (selSideR || '') === '';
      var hasFormRight = getFormsForFile(file.path).some(function(f) {
        return !f.editingId && right.num >= f.startLine && right.num <= f.endLine && (f.side || '') === '';
      });
      if (inCurrentSelRight) { rightEl.classList.add('selected'); }
      if (hasFormRight && !inCurrentSelRight) { rightEl.classList.add('form-selected'); }
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

  function buildCommentedRangeSet(comments) {
    const set = new Set();
    for (const c of comments) {
      if (c.resolved) continue;
      const side = c.side || '';
      for (let ln = c.start_line; ln <= c.end_line; ln++) set.add(ln + ':' + side);
    }
    return set;
  }

  // For unified diff: build a Set of visual indices that should have has-comment.
  // This handles interleaved add/del lines correctly by using sequential position.
  function buildUnifiedCommentVisualSet(hunks, comments) {
    if (!comments.length) return new Set();
    // Flatten all hunk lines with their line numbers
    const lines = [];
    for (const hunk of hunks) {
      for (const line of hunk.Lines) {
        lines.push({ oldNum: line.OldNum, newNum: line.NewNum });
      }
    }
    const set = new Set();
    for (const c of comments) {
      const side = c.side || '';
      let startIdx = -1, endIdx = -1;
      for (let i = 0; i < lines.length; i++) {
        // startIdx: match either OldNum or NewNum so deletions adjacent to
        // the comment boundary are included in the visual range
        if (startIdx === -1 && (lines[i].oldNum === c.start_line || lines[i].newNum === c.start_line)) {
          startIdx = i;
        }
        // endIdx: match only the comment's side to avoid overshooting
        const endNum = side === 'old' ? lines[i].oldNum : lines[i].newNum;
        if (endNum === c.end_line) endIdx = i;
      }
      if (startIdx !== -1 && endIdx !== -1) {
        for (let i = startIdx; i <= endIdx; i++) set.add(i);
      }
    }
    return set;
  }

  function getCommentsForBlock(block, commentsMap) {
    const result = [];
    for (let ln = block.startLine; ln <= block.endLine; ln++) {
      if (commentsMap[ln]) result.push(...commentsMap[ln]);
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
    const blockEl = gutter.closest('.line-block') || gutter.closest('.diff-split-side') || gutter.parentElement;
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
      openForm({ filePath: filePath, afterBlockIndex: lastBlockIndex, startLine: rangeStart, endLine: rangeEnd, editingId: null });
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
      var fileForms = getFormsForFile(filePath);
      var hasFormForBlock = fileForms.some(function(f) {
        return !f.editingId && startLine >= f.startLine && endLine <= f.endLine;
      });
      lb.classList.toggle('selected', inRange);
      lb.classList.toggle('form-selected', hasFormForBlock && !inRange);

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
        var uLineNum = parseInt(uLine.dataset.diffLineNum);
        var uSide = uLine.dataset.diffSide || '';
        var uHasForm = getFormsForFile(filePath).some(function(f) {
          return !f.editingId && uLineNum >= f.startLine && uLineNum <= f.endLine && (f.side || '') === uSide;
        });
        uLine.classList.toggle('selected', uSelected);
        uLine.classList.toggle('form-selected', uHasForm && !uSelected);
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
        var sHasForm = getFormsForFile(filePath).some(function(f) {
          return !f.editingId && sLineNum >= f.startLine && sLineNum <= f.endLine && (f.side || '') === sSideVal;
        });
        sSide.classList.toggle('selected', sSelected);
        sSide.classList.toggle('form-selected', sHasForm && !sSelected);
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

    var fp = dragState.filePath;
    dragState = null;
    openForm({
      filePath: fp,
      afterBlockIndex: lastBlockIndex,
      startLine: rangeStart,
      endLine: rangeEnd,
      editingId: null,
    });
  }

  function openForm(newForm) {
    var fk = formKey(newForm);
    var existing = activeForms.find(function(f) { return f.formKey === fk; });
    if (existing) {
      activeFilePath = newForm.filePath;
      selectionStart = newForm.startLine;
      selectionEnd = newForm.endLine;
      renderFileByPath(newForm.filePath);
      focusCommentTextarea(existing.formKey);
      return;
    }
    addForm(newForm);
    activeFilePath = newForm.filePath;
    selectionStart = newForm.startLine;
    selectionEnd = newForm.endLine;
    renderFileByPath(newForm.filePath);
    focusCommentTextarea(newForm.formKey);
  }


  function focusCommentTextarea(targetFormKey) {
    requestAnimationFrame(() => {
      if (targetFormKey) {
        var ta = document.querySelector('.comment-form[data-form-key="' + targetFormKey + '"] textarea');
        if (ta) { ta.focus(); return; }
      }
      var forms = document.querySelectorAll('.comment-form textarea');
      if (forms.length > 0) forms[forms.length - 1].focus();
    });
  }

  // ===== Comment Templates =====
  function getTemplates() {
    try {
      var raw = getCookie('crit-templates');
      if (raw) {
        var parsed = JSON.parse(raw);
        if (Array.isArray(parsed)) return parsed;
      }
    } catch (_) {}
    return [];
  }

  function saveTemplates(templates) {
    setCookie('crit-templates', JSON.stringify(templates));
  }

  function populateTemplateBar(bar, textarea) {
    bar.innerHTML = '';
    var templates = getTemplates();
    if (templates.length === 0) {
      bar.style.display = 'none';
      return;
    }
    bar.style.display = '';
    templates.forEach(function(tmpl, i) {
      var chip = document.createElement('button');
      chip.className = 'template-chip';
      chip.title = tmpl;
      var label = document.createElement('span');
      label.className = 'template-chip-label';
      label.textContent = tmpl;
      chip.appendChild(label);
      var del = document.createElement('span');
      del.className = 'template-chip-delete';
      del.textContent = '\u00d7';
      del.title = 'Remove template';
      del.addEventListener('click', function(e) {
        e.preventDefault();
        e.stopPropagation();
        var t = getTemplates();
        t.splice(i, 1);
        saveTemplates(t);
        populateTemplateBar(bar, textarea);
      });
      chip.appendChild(del);
      chip.addEventListener('click', function(e) {
        e.preventDefault();
        var start = textarea.selectionStart;
        var end = textarea.selectionEnd;
        textarea.value = textarea.value.substring(0, start) + tmpl + textarea.value.substring(end);
        textarea.selectionStart = textarea.selectionEnd = start + tmpl.length;
        textarea.focus();
        textarea.dispatchEvent(new Event('input'));
      });
      bar.appendChild(chip);
    });
  }

  function createTemplateBar(textarea) {
    var bar = document.createElement('div');
    bar.className = 'comment-template-bar';
    populateTemplateBar(bar, textarea);
    return bar;
  }

  function attachTemplateUI(form, textarea, actions) {
    var templateBar = createTemplateBar(textarea);

    var saveTemplateBtn = document.createElement('button');
    saveTemplateBtn.className = 'btn btn-sm';
    saveTemplateBtn.textContent = '+ Save as template';
    saveTemplateBtn.addEventListener('click', function(e) {
      e.preventDefault();
      showSaveTemplateDialog(textarea, templateBar);
    });

    var suggestBtn = document.createElement('button');
    suggestBtn.className = 'btn btn-sm';
    suggestBtn.textContent = '\u00B1 Suggest';
    suggestBtn.title = 'Insert the selected lines as a suggestion';
    suggestBtn.addEventListener('click', function() { insertSuggestion(textarea); });

    var leftGroup = document.createElement('div');
    leftGroup.className = 'comment-form-actions-left';
    leftGroup.appendChild(suggestBtn);
    leftGroup.appendChild(saveTemplateBtn);
    leftGroup.style.marginRight = 'auto';

    actions.insertBefore(leftGroup, actions.firstChild);
    form.insertBefore(templateBar, form.querySelector('textarea'));
  }

  function showSaveTemplateDialog(textarea, templateBar) {
    var text = textarea.value.trim();
    if (!text) {
      textarea.focus();
      return;
    }
    var overlay = document.createElement('div');
    overlay.className = 'save-template-overlay active';

    var dialog = document.createElement('div');
    dialog.className = 'save-template-dialog';

    var title = document.createElement('h3');
    title.textContent = 'Save as template';
    dialog.appendChild(title);

    var desc = document.createElement('p');
    desc.textContent = 'Edit the template text, then save.';
    dialog.appendChild(desc);

    var input = document.createElement('textarea');
    input.className = 'save-template-input';
    input.value = text;
    input.rows = 3;
    dialog.appendChild(input);

    var btns = document.createElement('div');
    btns.className = 'save-template-actions';

    var cancelBtn = document.createElement('button');
    cancelBtn.className = 'btn btn-sm';
    cancelBtn.textContent = 'Cancel';
    cancelBtn.addEventListener('click', function() { overlay.remove(); textarea.focus(); });

    var saveBtn = document.createElement('button');
    saveBtn.className = 'btn btn-sm btn-primary';
    saveBtn.textContent = 'Save';
    saveBtn.addEventListener('click', function() {
      var val = input.value.trim();
      if (!val) return;
      var t = getTemplates();
      t.push(val);
      saveTemplates(t);
      overlay.remove();
      populateTemplateBar(templateBar, textarea);
      textarea.focus();
    });

    btns.appendChild(cancelBtn);
    btns.appendChild(saveBtn);
    dialog.appendChild(btns);

    input.addEventListener('keydown', function(e) {
      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        saveBtn.click();
      } else if (e.key === 'Escape') {
        e.preventDefault();
        cancelBtn.click();
      }
    });

    overlay.appendChild(dialog);
    overlay.addEventListener('click', function(e) {
      if (e.target === overlay) { overlay.remove(); textarea.focus(); }
    });
    document.body.appendChild(overlay);
    requestAnimationFrame(function() { input.focus(); input.select(); });
  }

  // ===== Comment Form =====
  function createCommentForm(formObj) {
    const wrapper = document.createElement('div');
    wrapper.className = 'comment-form-wrapper';

    const form = document.createElement('div');
    form.className = 'comment-form';
    form.dataset.formKey = formObj.formKey;

    const header = document.createElement('div');
    header.className = 'comment-form-header';
    const lineRef = formObj.startLine === formObj.endLine
      ? 'Line ' + formObj.startLine
      : 'Lines ' + formObj.startLine + '-' + formObj.endLine;
    header.textContent = formObj.editingId ? 'Editing comment on ' + lineRef : 'Comment on ' + lineRef;

    const textarea = document.createElement('textarea');
    textarea.placeholder = 'Leave a review comment... (Ctrl+Enter to submit, Escape to cancel)';
    textarea.dataset.formKey = formObj.formKey;
    if (formObj.editingId) {
      const file = getFileByPath(formObj.filePath);
      if (file) {
        const existing = file.comments.find(c => c.id === formObj.editingId);
        if (existing) textarea.value = existing.body;
      }
    } else if (formObj.draftBody) {
      textarea.value = formObj.draftBody;
    }

    textarea.addEventListener('keydown', function(e) {
      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        e.stopPropagation();
        submitComment(textarea.value, formObj);
      } else if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        cancelComment(formObj);
      }
    });

    textarea.addEventListener('input', function() { debouncedSaveDraft(textarea.value, formObj); });

    const actions = document.createElement('div');
    actions.className = 'comment-form-actions';

    const cancelBtn = document.createElement('button');
    cancelBtn.className = 'btn btn-sm';
    cancelBtn.textContent = 'Cancel';
    cancelBtn.addEventListener('click', function() { cancelComment(formObj); });

    const submitBtn = document.createElement('button');
    submitBtn.className = 'btn btn-sm btn-primary';
    submitBtn.textContent = formObj.editingId ? 'Update Comment' : 'Add Comment';
    submitBtn.addEventListener('click', function() { submitComment(textarea.value, formObj); });

    actions.appendChild(cancelBtn);
    actions.appendChild(submitBtn);

    form.appendChild(header);
    form.appendChild(textarea);
    form.appendChild(actions);
    attachTemplateUI(form, textarea, actions);
    wrapper.appendChild(form);
    return wrapper;
  }

  function insertSuggestion(textarea) {
    var key = textarea.dataset.formKey;
    var formObj = activeForms.find(function(f) { return f.formKey === key; });
    if (!formObj) return;
    const file = getFileByPath(formObj.filePath);
    if (!file) return;
    const lines = file.content.split('\n').slice(formObj.startLine - 1, formObj.endLine);
    const suggestion = '```suggestion\n' + lines.join('\n') + '\n```';
    const start = textarea.selectionStart;
    const end = textarea.selectionEnd;
    textarea.value = textarea.value.substring(0, start) + suggestion + textarea.value.substring(end);
    const cursorPos = start + '```suggestion\n'.length;
    textarea.selectionStart = cursorPos;
    textarea.selectionEnd = cursorPos + lines.join('\n').length;
    textarea.focus();
  }

  async function submitComment(body, formObj) {
    if (!formObj) {
      // Legacy fallback: find the most recent form (will be removed when all callers migrated)
      formObj = activeForms.length > 0 ? activeForms[activeForms.length - 1] : null;
    }
    if (!body.trim() || !formObj) return;
    clearDraft(formObj);
    const filePath = formObj.filePath;
    const file = getFileByPath(filePath);
    if (!file) return;

    try {
      if (formObj.editingId) {
        const res = await fetch('/api/comment/' + formObj.editingId + '?path=' + enc(filePath), {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ body: body.trim() })
        });
        const updated = await res.json();
        const idx = file.comments.findIndex(c => c.id === formObj.editingId);
        if (idx >= 0) file.comments[idx] = updated;
      } else {
        const payload = {
          start_line: formObj.startLine,
          end_line: formObj.endLine,
          body: body.trim()
        };
        if (formObj.side) payload.side = formObj.side;
        if (configAuthor) payload.author = configAuthor;
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

    removeForm(formObj.formKey);
    if (getFormsForFile(filePath).length === 0) {
      if (activeFilePath === filePath) {
        activeFilePath = null;
        selectionStart = null;
        selectionEnd = null;
      }
      focusedFilePath = null;
      focusedBlockIndex = null;
      focusedElement = null;
    }
    renderFileByPath(filePath);
    renderFileSummary();
    updateCommentCount();
  }

  function cancelComment(formObj) {
    if (!formObj) {
      // Legacy fallback: find the most recent form (will be removed when all callers migrated)
      formObj = activeForms.length > 0 ? activeForms[activeForms.length - 1] : null;
    }
    if (!formObj) return;
    clearDraft(formObj);
    removeForm(formObj.formKey);
    if (getFormsForFile(formObj.filePath).length === 0) {
      if (activeFilePath === formObj.filePath) {
        activeFilePath = null;
        selectionStart = null;
        selectionEnd = null;
      }
      focusedFilePath = null;
      focusedBlockIndex = null;
      focusedElement = null;
    }
    renderFileByPath(formObj.filePath);
  }

  // ===== Draft Autosave =====
  let draftTimers = {};

  function getDraftKey(formObj) {
    if (!formObj) return null;
    return 'crit-draft-' + formObj.formKey;
  }

  function saveDraft(body, formObj) {
    if (!formObj) return;
    var key = getDraftKey(formObj);
    if (!key) return;
    try {
      localStorage.setItem(key, JSON.stringify({
        filePath: formObj.filePath,
        startLine: formObj.startLine,
        endLine: formObj.endLine,
        afterBlockIndex: formObj.afterBlockIndex,
        editingId: formObj.editingId,
        side: formObj.side || '',
        body: body,
        savedAt: Date.now()
      }));
    } catch (_) {}
  }

  function debouncedSaveDraft(body, formObj) {
    if (!formObj) return;
    var key = formObj.formKey;
    clearTimeout(draftTimers[key]);
    draftTimers[key] = setTimeout(function() { saveDraft(body, formObj); }, 500);
  }

  function clearDraft(formObj) {
    if (!formObj) return;
    var key = formObj.formKey;
    if (draftTimers[key]) {
      clearTimeout(draftTimers[key]);
      delete draftTimers[key];
    }
    var draftKey = getDraftKey(formObj);
    if (draftKey) {
      try { localStorage.removeItem(draftKey); } catch (_) {}
    }
  }

  window.addEventListener('beforeunload', function() {
    activeForms.forEach(function(formObj) {
      var el = document.querySelector('.comment-form[data-form-key="' + formObj.formKey + '"] textarea');
      if (el) saveDraft(el.value, formObj);
    });
  });

  function restoreDrafts() {
    var restored = false;
    var keysToProcess = [];
    for (var i = 0; i < localStorage.length; i++) {
      var k = localStorage.key(i);
      if (k && k.startsWith('crit-draft-')) keysToProcess.push(k);
    }
    for (var ki = 0; ki < keysToProcess.length; ki++) {
      var key = keysToProcess[ki];
      try {
        var raw = localStorage.getItem(key);
        if (!raw) continue;
        var draft = JSON.parse(raw);

        if (Date.now() - draft.savedAt > 24 * 60 * 60 * 1000) {
          localStorage.removeItem(key);
          continue;
        }

        var file = getFileByPath(draft.filePath);
        if (!file) { localStorage.removeItem(key); continue; }

        if (file.fileType === 'markdown' && file.content) {
          var totalLines = file.content.split('\n').length;
          if (draft.startLine < 1 || draft.endLine > totalLines) {
            localStorage.removeItem(key);
            continue;
          }
        }

        if (draft.editingId) {
          if (!file.comments.find(function(c) { return c.id === draft.editingId; })) {
            localStorage.removeItem(key);
            continue;
          }
        }

        var formObj = {
          filePath: file.path,
          afterBlockIndex: draft.afterBlockIndex,
          startLine: draft.startLine,
          endLine: draft.endLine,
          editingId: draft.editingId,
          side: draft.side || '',
          draftBody: draft.body || ''
        };
        formObj.formKey = formKey(formObj);
        addForm(formObj);

        restored = true;
        localStorage.removeItem(key);
      } catch (_) {
        localStorage.removeItem(key);
      }
    }
    if (restored) {
      // Render all files that have restored forms (deduplicated)
      var renderedFiles = {};
      activeForms.forEach(function(f) {
        if (!renderedFiles[f.filePath]) {
          renderedFiles[f.filePath] = true;
          renderFileByPath(f.filePath);
        }
      });
      showMiniToast('Draft restored');
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
    if (findFormForEdit(comment.id)) {
      return createInlineEditor(comment);
    }

    const wrapper = document.createElement('div');
    wrapper.className = 'comment-block';

    const card = document.createElement('div');
    card.className = 'comment-card' + (comment.carried_forward ? ' carried-forward' : '');
    card.dataset.commentId = comment.id;

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
    if (comment.author) {
      const authorBadge = document.createElement('span');
      authorBadge.className = 'comment-author-badge';
      const colors = authorColor(comment.author);
      authorBadge.style.cssText = 'background:' + colors.bg + ';border-color:' + colors.border + ';color:' + colors.text;
      authorBadge.textContent = '@' + comment.author;
      headerLeft.appendChild(authorBadge);
    }
    if (comment.review_round >= 1) {
      const roundBadge = document.createElement('span');
      var rc = comment.review_round === session.review_round ? ' round-current' : comment.review_round === session.review_round - 1 ? ' round-latest' : '';
      roundBadge.className = 'comment-round-badge' + rc;
      roundBadge.textContent = 'R' + comment.review_round;
      headerLeft.appendChild(roundBadge);
    }
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
    editBtn.title = 'Edit';
    editBtn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17 3a2.85 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/><path d="m15 5 4 4"/></svg>';
    editBtn.addEventListener('click', () => editComment(comment, filePath));

    const deleteBtn = document.createElement('button');
    deleteBtn.className = 'delete-btn';
    deleteBtn.title = 'Delete';
    deleteBtn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/></svg>';
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
    var formObj = findFormForEdit(comment.id);
    if (!formObj) return null;

    const wrapper = document.createElement('div');
    wrapper.className = 'comment-form-wrapper';

    const form = document.createElement('div');
    form.className = 'comment-form';
    form.dataset.formKey = formObj.formKey;

    const header = document.createElement('div');
    header.className = 'comment-form-header';
    const lineRef = comment.start_line === comment.end_line
      ? 'Line ' + comment.start_line
      : 'Lines ' + comment.start_line + '-' + comment.end_line;
    header.textContent = 'Editing comment on ' + lineRef;

    const textarea = document.createElement('textarea');
    textarea.placeholder = 'Leave a review comment... (Ctrl+Enter to submit, Escape to cancel)';
    textarea.dataset.formKey = formObj.formKey;
    textarea.value = comment.body;

    textarea.addEventListener('keydown', function(e) {
      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        e.stopPropagation();
        submitComment(textarea.value, formObj);
      } else if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        cancelComment(formObj);
      }
    });

    textarea.addEventListener('input', function() { debouncedSaveDraft(textarea.value, formObj); });

    const actions = document.createElement('div');
    actions.className = 'comment-form-actions';

    const cancelBtn = document.createElement('button');
    cancelBtn.className = 'btn btn-sm';
    cancelBtn.textContent = 'Cancel';
    cancelBtn.addEventListener('click', function() { cancelComment(formObj); });

    const submitBtn = document.createElement('button');
    submitBtn.className = 'btn btn-sm btn-primary';
    submitBtn.textContent = 'Update Comment';
    submitBtn.addEventListener('click', function() { submitComment(textarea.value, formObj); });

    actions.appendChild(cancelBtn);
    actions.appendChild(submitBtn);

    form.appendChild(header);
    form.appendChild(textarea);
    form.appendChild(actions);
    attachTemplateUI(form, textarea, actions);
    wrapper.appendChild(form);

    requestAnimationFrame(() => textarea.focus());
    return wrapper;
  }

  function editComment(comment, filePath) {
    openForm({
      filePath: filePath,
      afterBlockIndex: null,
      startLine: comment.start_line,
      endLine: comment.end_line,
      editingId: comment.id,
    });
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
    el.dataset.commentId = comment.id;

    const header = document.createElement('div');
    header.className = 'resolved-comment-header';

    const check = document.createElement('span');
    check.className = 'resolved-check';
    check.textContent = '\u2713';

    const body = document.createElement('div');
    body.className = 'resolved-body';
    body.innerHTML = commentMd.render(comment.body);

    header.appendChild(check);
    if (comment.review_round >= 1) {
      const roundBadge = document.createElement('span');
      var rc = comment.review_round === session.review_round ? ' round-current' : comment.review_round === session.review_round - 1 ? ' round-latest' : '';
      roundBadge.className = 'comment-round-badge' + rc;
      roundBadge.textContent = 'R' + comment.review_round;
      header.appendChild(roundBadge);
    }
    header.appendChild(body);
    el.appendChild(header);

    if (comment.resolution_note) {
      const note = document.createElement('span');
      note.className = 'resolved-note';
      note.textContent = comment.resolution_note;
      el.appendChild(note);
    }

    el.addEventListener('click', function() { el.classList.toggle('expanded'); });
    return el;
  }

  // ===== Comment Count =====
  function updateCommentCount() {
    let unresolved = 0, resolved = 0;
    for (const f of files) {
      for (const c of f.comments) {
        if (c.resolved) resolved++; else unresolved++;
      }
    }
    const total = unresolved + resolved;
    const el = document.getElementById('commentCount');
    const numEl = document.getElementById('commentCountNumber');
    if (total === 0) {
      el.style.display = 'none';
      el.title = 'Toggle comments panel';
      numEl.textContent = '';
    } else if (unresolved > 0) {
      el.style.display = '';
      el.classList.remove('comment-count-resolved');
      el.title = unresolved + ' unresolved comment' + (unresolved === 1 ? '' : 's') + ' — toggle panel';
      numEl.textContent = unresolved;
    } else {
      el.style.display = '';
      el.classList.add('comment-count-resolved');
      el.title = total + ' resolved comment' + (total === 1 ? '' : 's') + ' — toggle panel';
      numEl.textContent = total;
    }
    renderCommentsPanel();
  }

  function updateTocPosition() {
    var toc = document.getElementById('toc');
    var panel = document.getElementById('commentsPanel');
    if (!toc || !panel) return;
    var panelOpen = !panel.classList.contains('comments-panel-hidden');
    var tocBaseRight = 16; // matches the default right: 16px in CSS
    toc.style.right = panelOpen ? (panel.offsetWidth + tocBaseRight) + 'px' : '';
  }

  function toggleCommentsPanel() {
    var panel = document.getElementById('commentsPanel');
    var isHidden = panel.classList.contains('comments-panel-hidden');
    panel.classList.toggle('comments-panel-hidden');
    if (isHidden) {
      renderCommentsPanel();
    }
    updateTocPosition();
  }

  function renderCommentsPanel() {
    var panel = document.getElementById('commentsPanel');
    if (panel.classList.contains('comments-panel-hidden')) return;

    var showResolved = document.getElementById('showResolvedToggle').checked;
    var body = document.getElementById('commentsPanelBody');
    body.innerHTML = '';

    // Show/hide the filter bar only when resolved comments exist
    var hasResolved = files.some(function(f) { return f.comments.some(function(c) { return c.resolved; }); });
    document.getElementById('commentsPanelFilter').style.display = hasResolved ? '' : 'none';

    var hasComments = false;

    for (var i = 0; i < files.length; i++) {
      var file = files[i];
      var visibleComments = file.comments.filter(function(c) {
        return showResolved ? true : !c.resolved;
      });
      if (visibleComments.length === 0) continue;
      hasComments = true;

      // Sort by start_line
      visibleComments.sort(function(a, b) { return a.start_line - b.start_line; });

      var group = document.createElement('div');
      group.className = 'comments-panel-file-group';

      // File name header (only in multi-file mode)
      if (files.length > 1) {
        var fileName = document.createElement('div');
        fileName.className = 'comments-panel-file-name';
        fileName.textContent = file.path;
        fileName.title = file.path;
        group.appendChild(fileName);
      }

      for (var j = 0; j < visibleComments.length; j++) {
        var comment = visibleComments[j];
        var card = document.createElement('div');
        card.className = 'comments-panel-card' + (comment.resolved ? ' comments-panel-card-resolved' : '');
        card.dataset.commentId = comment.id;
        card.dataset.filePath = file.path;

        var lineRef = document.createElement('div');
        lineRef.className = 'comments-panel-card-line';
        lineRef.textContent = comment.start_line === comment.end_line
          ? 'Line ' + comment.start_line
          : 'Lines ' + comment.start_line + '-' + comment.end_line;
        if (comment.carried_forward) {
          var badge = document.createElement('span');
          if (comment.resolved) {
            badge.className = 'comments-panel-badge comments-panel-badge-resolved';
            badge.textContent = 'Resolved';
          } else {
            badge.className = 'comments-panel-badge comments-panel-badge-unresolved';
            badge.textContent = 'Unresolved';
          }
          lineRef.appendChild(badge);
        }
        if (comment.review_round >= 1) {
          var roundBadge = document.createElement('span');
          var rc = comment.review_round === session.review_round ? ' round-current' : comment.review_round === session.review_round - 1 ? ' round-latest' : '';
      roundBadge.className = 'comment-round-badge' + rc;
          roundBadge.textContent = 'R' + comment.review_round;
          lineRef.appendChild(roundBadge);
        }

        var bodyEl = document.createElement('div');
        bodyEl.className = 'comments-panel-card-body';
        bodyEl.innerHTML = commentMd.render(comment.body);

        card.appendChild(lineRef);
        card.appendChild(bodyEl);
        card.addEventListener('click', (function(commentId, filePath) {
          return function() { scrollToComment(commentId, filePath); };
        })(comment.id, file.path));

        group.appendChild(card);
      }

      body.appendChild(group);
    }

    if (!hasComments) {
      var empty = document.createElement('div');
      empty.className = 'comments-panel-empty';
      empty.textContent = showResolved ? 'No comments yet' : 'No unresolved comments';
      body.appendChild(empty);
    }
  }

  function scrollToComment(commentId, filePath) {
    // 1. Find the file section and expand if collapsed
    var section = document.getElementById('file-section-' + filePath);
    if (!section) return;
    if (!section.open) section.open = true;

    // 2. Find the inline comment card by comment ID
    var commentCard = section.querySelector('.comment-card[data-comment-id="' + CSS.escape(commentId) + '"]')
      || section.querySelector('.resolved-comment[data-comment-id="' + CSS.escape(commentId) + '"]');
    if (!commentCard) return;

    // 3. Scroll into view
    commentCard.scrollIntoView({ behavior: 'smooth', block: 'center' });

    // 4. Flash highlight
    commentCard.classList.remove('comment-card-highlight');
    void commentCard.offsetWidth;
    commentCard.classList.add('comment-card-highlight');
    commentCard.addEventListener('animationend', function() {
      commentCard.classList.remove('comment-card-highlight');
    }, { once: true });
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
    if (session.review_round > 1) {
      el.textContent = 'Round #' + session.review_round;
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
        document.getElementById('waitingPrompt').style.display = '';
        document.getElementById('waitingClipboard').style.display = '';
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
            viewed: files[pi].viewed,
          };
        }

        // Re-fetch everything on file-changed (round complete)
        const sessionRes = await fetch('/api/session?scope=' + enc(diffScope)).then(r => r.json());
        session = sessionRes;

        // Reload all files
        files = await loadAllFileData(session.files || [], diffScope);

        // Restore per-file user state from previous round
        for (var fi = 0; fi < files.length; fi++) {
          var prev = prevState[files[fi].path];
          if (prev) {
            files[fi].viewMode = prev.viewMode;
            files[fi].collapsed = prev.collapsed;
            if (prev.diffLoaded) files[fi].diffLoaded = prev.diffLoaded;
            if (prev.viewed) files[fi].viewed = true;
          }
        }

        files.sort(fileSortComparator);

        activeForms = [];
        activeFilePath = null;
        selectionStart = null;
        selectionEnd = null;
        focusedBlockIndex = null;
        focusedFilePath = null;
        focusedElement = null;
        diffActive = false;

        saveViewedState();
        updateHeaderRound();
        updateDiffModeToggle();
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
          // Hide prompt and clipboard once agent starts making edits
          var promptEl = document.getElementById('waitingPrompt');
          var clipEl = document.getElementById('waitingClipboard');
          if (promptEl) promptEl.style.display = 'none';
          if (clipEl) clipEl.style.display = 'none';
          document.getElementById('waitingMessage').textContent = 'Waiting for your agent to finish...';
        }
      } catch (_) {}
    });

    source.addEventListener('comments-changed', async function() {
      try {
        for (var i = 0; i < files.length; i++) {
          var f = files[i];
          var commentsRes = await fetch('/api/file/comments?path=' + enc(f.path))
            .then(function(r) { return r.ok ? r.json() : []; })
            .catch(function() { return []; });
          f.comments = Array.isArray(commentsRes) ? commentsRes : [];
        }
        renderAllFiles();
        updateTreeCommentBadges();
      } catch (err) {
        console.error('Error handling comments-changed:', err);
      }
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
    box.style.cssText = 'background:var(--bg-primary,#1e1e2e);border:1px solid var(--border,#292e42);border-radius:12px;padding:32px 40px;text-align:center;color:var(--fg-primary,#c0caf5);font-family:inherit';
    box.innerHTML = '<div style="font-size:20px;font-weight:600;margin-bottom:8px">Server stopped</div><div style="color:var(--fg-secondary,#a9b1d6)">You can close this tab.</div>';
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

    // Build payload — multi-file sends individual files, single-file sends content for backward compat
    const payload = files.length === 1
      ? {
          content: files[0].content,
          filename: files[0].path,
          review_round: session.review_round || 1,
          comments: [],
        }
      : {
          files: files.map(f => ({ path: f.path, content: f.content })),
          review_round: session.review_round || 1,
          comments: [],
        };

    for (const f of files) {
      for (const c of f.comments) {
        const shared = { file: f.path, start_line: c.start_line, end_line: c.end_line, body: c.body };
        if (c.author) shared.author_display_name = c.author;
        if (c.review_round >= 1) shared.review_round = c.review_round;
        payload.comments.push(shared);
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
    const tocShortcut = document.querySelector('.shortcut-toc-only');
    listEl.innerHTML = '';

    function hideToc() {
      toggleBtn.style.display = 'none';
      if (tocShortcut) tocShortcut.style.display = 'none';
    }

    // TOC only for single-file markdown reviews
    if (session.mode === 'git' || files.length > 1) {
      hideToc();
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
      hideToc();
      return;
    }
    toggleBtn.style.display = '';
    if (tocShortcut) tocShortcut.style.display = '';

    // Restore TOC open/closed state from cookie
    if (getCookie('crit-toc') === 'open') {
      tocEl.classList.remove('toc-hidden');
    }

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
        // Uncollapse the file section first
        var sectionEl = document.getElementById('file-section-' + item.filePath);
        if (sectionEl) {
          var file = getFileByPath(item.filePath);
          if (file) file.collapsed = false;
          sectionEl.open = true;
        }
        // Find the line block matching this heading's start line
        var target = sectionEl && sectionEl.querySelector('.line-block[data-start-line="' + item.startLine + '"]');
        if (target) {
          var mainHeader = document.querySelector('.header');
          var offset = (mainHeader ? mainHeader.offsetHeight : 49) + 8;
          var y = target.getBoundingClientRect().top + window.scrollY - offset;
          window.scrollTo({ top: y, behavior: 'smooth' });
        } else {
          scrollToFile(item.filePath);
        }
      });
      li.appendChild(a);
      listEl.appendChild(li);
    }

    // Scrollspy: highlight current heading in TOC
    setupTocScrollspy(allItems);
  }

  let tocScrollHandler = null;
  function setupTocScrollspy(items) {
    if (tocScrollHandler) {
      window.removeEventListener('scroll', tocScrollHandler);
      tocScrollHandler = null;
    }
    if (!items || items.length === 0) return;

    tocScrollHandler = function() {
      const headerHeight = (document.querySelector('.header')?.offsetHeight || 49) + 16;
      let activeItem = null;

      for (const item of items) {
        var sectionEl = document.getElementById('file-section-' + item.filePath);
        var block = sectionEl && sectionEl.querySelector('.line-block[data-start-line="' + item.startLine + '"]');
        if (!block) continue;
        var rect = block.getBoundingClientRect();
        if (rect.top <= headerHeight) {
          activeItem = item;
        }
      }

      const tocLinks = document.querySelectorAll('.toc-list a');
      for (const link of tocLinks) {
        var isActive = activeItem &&
          link.dataset.startLine === String(activeItem.startLine) &&
          link.dataset.filePath === activeItem.filePath;
        link.classList.toggle('toc-active', !!isActive);
      }
    };

    window.addEventListener('scroll', tocScrollHandler, { passive: true });
    tocScrollHandler();
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
    const saved = getCookie('crit-theme') || 'system';
    applyTheme(saved);
  }

  window.applyTheme = function(choice) {
    setCookie('crit-theme', choice);
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
      setCookie('crit-diff-mode', mode);
      document.querySelectorAll('#diffModeToggle .toggle-btn').forEach(function(b) {
        b.classList.toggle('active', b.dataset.mode === mode);
      });
      renderAllFiles();
    });
  });

  // ===== Toggle Diff (rendered diff view for file mode) =====
  document.getElementById('diffToggle').addEventListener('click', function() {
    diffActive = !diffActive;
    updateDiffModeToggle();
    renderAllFiles();
  });

  // ===== Scope Toggle (All / Branch / Staged / Unstaged) =====
  document.getElementById('scopeToggle').addEventListener('click', async function(e) {
    var btn = e.target.closest('.toggle-btn');
    if (!btn || btn.disabled || btn.classList.contains('active')) return;
    var scope = btn.dataset.scope;
    diffScope = scope;
    setCookie('crit-diff-scope', scope);
    document.querySelectorAll('#scopeToggle .toggle-btn').forEach(function(b) {
      b.classList.toggle('active', b.dataset.scope === scope);
    });
    await reloadForScope();
  });

  async function reloadForScope() {
    document.getElementById('filesContainer').innerHTML =
      '<div class="loading" style="padding: 40px; text-align: center; color: var(--fg-muted);">Loading...</div>';

    const sessionRes = await fetch('/api/session?scope=' + enc(diffScope)).then(function(r) { return r.json(); });
    session = sessionRes;

    if (!session.files || session.files.length === 0) {
      document.getElementById('filesContainer').innerHTML =
        '<div class="loading" style="padding: 40px; text-align: center; color: var(--fg-muted);">No ' + diffScope + ' changes</div>';
      files = [];
      renderFileTree();
      updateCommentCount();
      updateViewedCount();
      return;
    }

    files = await loadAllFileData(session.files, diffScope);
    files.sort(fileSortComparator);
    restoreViewedState();
    renderFileTree();
    renderAllFiles();
    buildToc();
    updateCommentCount();
    updateViewedCount();
  }

  // ===== TOC Toggle =====
  document.getElementById('tocToggle').addEventListener('click', function() {
    var tocEl = document.getElementById('toc');
    tocEl.classList.toggle('toc-hidden');
    setCookie('crit-toc', tocEl.classList.contains('toc-hidden') ? 'closed' : 'open');
    buildToc();
  });

  document.querySelector('.toc-close').addEventListener('click', function() {
    document.getElementById('toc').classList.add('toc-hidden');
    setCookie('crit-toc', 'closed');
  });

  // ===== Comments Panel Toggle =====
  document.getElementById('commentCount').addEventListener('click', function() {
    toggleCommentsPanel();
  });
  document.getElementById('commentCount').addEventListener('keydown', function(e) {
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggleCommentsPanel(); }
  });

  document.querySelector('.comments-panel-close').addEventListener('click', function() {
    document.getElementById('commentsPanel').classList.add('comments-panel-hidden');
    updateTocPosition();
  });

  document.getElementById('showResolvedToggle').addEventListener('change', function() {
    renderCommentsPanel();
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
      if (e.key === 'Escape' && activeForms.length > 0) {
        e.preventDefault();
        var ta = document.activeElement;
        if (ta && ta.dataset && ta.dataset.formKey) {
          var form = activeForms.find(function(f) { return f.formKey === ta.dataset.formKey; });
          if (form) cancelComment(form);
        }
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
          openForm({ filePath: fp, afterBlockIndex: bi, startLine: block.startLine, endLine: block.endLine, editingId: null });
        }
        // Diff line
        else if (focusedElement.dataset.diffFilePath && focusedElement.dataset.diffLineNum) {
          var dfp = focusedElement.dataset.diffFilePath;
          var lineNum = parseInt(focusedElement.dataset.diffLineNum);
          var side = focusedElement.dataset.diffSide || '';
          openForm({ filePath: dfp, afterBlockIndex: null, startLine: lineNum, endLine: lineNum, editingId: null, side: side || undefined });
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
      case 'C': {
        e.preventDefault();
        toggleCommentsPanel();
        break;
      }
      case 't': {
        var tocBtn = document.getElementById('tocToggle');
        if (tocBtn.style.display === 'none') return;
        e.preventDefault();
        tocBtn.click();
        break;
      }
      case 'n': {
        if (changeGroups.length === 0) break;
        e.preventDefault();
        navigateToChange(1);
        break;
      }
      case 'N': {
        if (changeGroups.length === 0) break;
        e.preventDefault();
        navigateToChange(-1);
        break;
      }
      case '?': {
        e.preventDefault();
        toggleShortcutsOverlay();
        break;
      }
      case 'Escape': {
        e.preventDefault();
        if (activeForms.length > 0) cancelComment(activeForms[activeForms.length - 1]);
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
