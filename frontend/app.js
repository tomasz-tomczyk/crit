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
      return '';
    }
  });

  // ===== File Reference Inline Rule =====
  commentMd.inline.ruler.push('file_ref', function(state, silent) {
    const start = state.pos;
    const max = state.posMax;
    if (state.src.charCodeAt(start) !== 0x40 /* @ */) return false;
    if (start > 0 && !/\s/.test(state.src[start - 1])) return false;
    let end = start + 1;
    while (end < max && /[a-zA-Z0-9._\-\/]/.test(state.src[end])) end++;
    const path = state.src.substring(start + 1, end);
    if (path.length === 0 || (path.indexOf('.') === -1 && path.indexOf('/') === -1)) return false;
    if (!silent) {
      const token = state.push('file_ref', '', 0);
      token.content = path;
    }
    state.pos = end;
    return true;
  });
  commentMd.renderer.rules.file_ref = function(tokens, idx) {
    const path = tokens[idx].content;
    return '<span class="file-ref">' + escapeHtml(path) + '</span>';
  };

  // ===== Suggestion Diff Renderer =====
  function renderSuggestionDiff(suggestionContent, originalLines) {
    const sugLines = suggestionContent.replace(/\n$/, '').split('\n');
    let html = '<div class="suggestion-diff">';
    html += '<div class="suggestion-header">Suggested change</div>';

    const origLen = (originalLines && originalLines.length > 0) ? originalLines.length : 0;
    const isEmptySuggestion = sugLines.length === 1 && sugLines[0] === '' && origLen > 0;
    const sugLen = isEmptySuggestion ? 0 : sugLines.length;
    const pairedLen = Math.min(origLen, sugLen);

    // Compute word-level diffs for paired lines
    const delContents = [];
    const addContents = [];
    for (let i = 0; i < pairedLen; i++) {
      const wd = wordDiff(originalLines[i], sugLines[i]);
      if (wd) {
        delContents.push(applyWordDiffToHtml(escapeHtml(originalLines[i]), wd.oldRanges, 'diff-word-del'));
        addContents.push(applyWordDiffToHtml(escapeHtml(sugLines[i]), wd.newRanges, 'diff-word-add'));
      } else {
        delContents.push(escapeHtml(originalLines[i]));
        addContents.push(escapeHtml(sugLines[i]));
      }
    }

    // All deletion lines first (paired + unpaired)
    for (let j = 0; j < origLen; j++) {
      const dc = j < pairedLen ? delContents[j] : escapeHtml(originalLines[j]);
      html += '<div class="suggestion-line suggestion-line-del">'
        + '<span class="suggestion-line-sign">\u2212</span>'
        + '<span class="suggestion-line-content">' + dc + '</span></div>';
    }

    // All addition lines (paired + unpaired)
    for (let k = 0; k < sugLen; k++) {
      const ac = k < pairedLen ? addContents[k] : escapeHtml(sugLines[k]);
      html += '<div class="suggestion-line suggestion-line-add">'
        + '<span class="suggestion-line-sign">+</span>'
        + '<span class="suggestion-line-content">' + ac + '</span></div>';
    }

    html += '</div>';
    return html;
  }

  (function() {
    const defaultFence = commentMd.renderer.rules.fence;
    commentMd.renderer.rules.fence = function(tokens, idx, options, env, self) {
      const token = tokens[idx];
      const info = token.info ? token.info.trim() : '';
      if (info === 'suggestion') {
        return renderSuggestionDiff(token.content, env && env.originalLines);
      }
      if (defaultFence) {
        return defaultFence(tokens, idx, options, env, self);
      }
      return self.renderToken(tokens, idx, options);
    };
  })();

  // ===== Document Markdown Renderer =====
  const documentMd = window.markdownit({
    html: true,
    typographer: true,
    linkify: true,
    highlight: function(str, lang) {
      if (lang && hljs.getLanguage(lang)) {
        try { return hljs.highlight(str, { language: lang }).value; } catch (_) {}
      }
      return '';
    }
  });

  // ===== Cookie helpers (persist across random ports on 127.0.0.1) =====
  function setCookie(name, value) {
    document.cookie = name + '=' + encodeURIComponent(value) + '; path=/; max-age=31536000; SameSite=Strict';
  }
  function getCookie(name) {
    const match = document.cookie.match('(?:^|; )' + name + '=([^;]*)');
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
  let waitingHasComments = false;
  let pendingUpdates = [];
  let pendingUpdatesVersion = '';

  let reviewComments = []; // review-level (general) comments
  let reviewCommentFormActive = false; // is the review comment form open?
  let reviewCommentEditingId = null; // id of review comment being edited, or null

  let settingsPanelOpen = false;
  let settingsPanelTab = 'settings';
  let cachedConfig = null; // populated on first panel open

  let diffMode = getCookie('crit-diff-mode') || 'split'; // 'split' or 'unified'
  let diffScope = getCookie('crit-diff-scope') || 'all'; // 'all', 'branch', 'staged', or 'unstaged'
  let diffCommit = '';
  let commitList = [];
  let diffActive = false; // rendered diff view toggle for file mode

  let filePickerReady = false;  // set true once /api/files/list is confirmed working
  let userActedThisRound = false; // tracks if user made any comment/resolve/edit action this round

  // Per-file active form state
  let activeFilePath = null;
  let activeForms = [];  // Array of { formKey, filePath, afterBlockIndex, startLine, endLine, editingId, side }
  let prData = null;     // PR metadata from /api/config (set once on load)
  let agentEnabled = false;
  let agentName = 'agent';
  let pendingAgentRequests = new Set();

  // Track manually toggled collapse state (comment ID → boolean, true = collapsed)
  const commentCollapseOverrides = {};

  // ===== SVG Icon Constants =====
  const ICON_CHEVRON = '<svg viewBox="0 0 16 16" fill="currentColor" width="16" height="16"><path d="M12.78 5.22a.75.75 0 0 1 0 1.06l-4.25 4.25a.75.75 0 0 1-1.06 0L3.22 6.28a.75.75 0 0 1 1.06-1.06L8 8.94l3.72-3.72a.75.75 0 0 1 1.06 0Z"/></svg>';
  const ICON_EDIT = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17 3a2.85 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/><path d="m15 5 4 4"/></svg>';
  const ICON_DELETE = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/></svg>';
  const ICON_RESOLVE = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>';
  const ICON_UNRESOLVE = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 0 1 9-9 9 9 0 0 1 6.36 2.64M21 12a9 9 0 0 1-9 9 9 9 0 0 1-6.36-2.64"/><polyline points="21 3 21 8 16 8"/><polyline points="3 21 3 16 8 16"/></svg>';
  const ICON_CLIPBOARD = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';
  const ICON_CHECK_SMALL = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6L9 17l-5-5"/></svg>';
  const ICON_COMMENT = '<svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor"><path d="M1 2.75C1 1.784 1.784 1 2.75 1h10.5c.966 0 1.75.784 1.75 1.75v7.5A1.75 1.75 0 0 1 13.25 12H9.06l-2.573 2.573A1.458 1.458 0 0 1 4 13.543V12H2.75A1.75 1.75 0 0 1 1 10.25Zm1.75-.25a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h2a.75.75 0 0 1 .75.75v2.19l2.72-2.72a.749.749 0 0 1 .53-.22h4.5a.25.25 0 0 0 .25-.25v-7.5a.25.25 0 0 0-.25-.25Z"/></svg>';

  function formKey(form) {
    if (form.scope === 'review') return 'review:' + (form.editingId || 'new');
    if (form.editingId) return form.filePath + ':edit:' + form.editingId;
    if (form.scope === 'file') return form.filePath + ':file';
    return form.filePath + ':' + form.startLine + ':' + form.endLine + ':' + (form.side || '');
  }

  function addForm(form) {
    form.formKey = formKey(form);
    const idx = activeForms.findIndex(function(f) { return f.formKey === form.formKey; });
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
  let unifiedVisualStart = null; // visual index range for unified drag (cross-number-space)
  let unifiedVisualEnd = null;
  let focusedBlockIndex = null;
  let focusedFilePath = null;
  let focusedElement = null; // currently focused navigable element
  let navElements = []; // cached .kb-nav list, rebuilt on render
  let changeGroups = [];      // [{elements: [DOM], filePath: string}]
  let currentChangeIdx = -1;

  const enc = encodeURIComponent;

  // Author color-coding for multi-reviewer comments
  const AUTHOR_COLOR_COUNT = 6;

  function authorColorIndex(name) {
    let hash = 0;
    for (const ch of name) hash = ((hash << 5) - hash + ch.charCodeAt(0)) | 0;
    return Math.abs(hash) % AUTHOR_COLOR_COUNT;
  }

  // Sort comparator: directories before files at each depth, then alphabetical
  function fileSortComparator(a, b) {
    const pa = a.path.split('/'), pb = b.path.split('/');
    const min = Math.min(pa.length, pb.length);
    for (let i = 0; i < min - 1; i++) {
      if (pa[i] !== pb[i]) return pa[i].localeCompare(pb[i]);
    }
    if (pa.length !== pb.length) return pb.length - pa.length;
    return pa[pa.length - 1].localeCompare(pb[pa.length - 1]);
  }

  // Fetch and build file objects from the API for a list of file infos.
  // Files marked as lazy by the backend are returned with metadata only;
  // their content/diff/comments are fetched on demand when expanded.
  async function loadAllFileData(fileInfos, scope) {
    const hasLazy = fileInfos.some(function(fi) { return fi.lazy; });

    // If no lazy files, load everything eagerly (identical to previous behavior)
    if (!hasLazy) {
      return Promise.all(fileInfos.map(function(fi) { return loadSingleFile(fi, scope); }));
    }

    // Split into eager and lazy batches
    const eager = [];
    const lazy = [];
    for (let i = 0; i < fileInfos.length; i++) {
      if (fileInfos[i].lazy) {
        lazy.push(fileInfos[i]);
      } else {
        eager.push(fileInfos[i]);
      }
    }

    // Load eager files fully
    const eagerFiles = await Promise.all(eager.map(function(fi) { return loadSingleFile(fi, scope); }));

    // Create lightweight placeholders for lazy files
    const lazyFiles = lazy.map(function(fi) {
      return {
        path: fi.path,
        status: fi.status,
        fileType: fi.file_type,
        content: '',
        previousContent: '',
        comments: [],
        diffHunks: [],
        lineBlocks: null,
        previousLineBlocks: null,
        tocItems: [],
        collapsed: true,
        viewMode: (session.mode === 'git') ? 'diff' : 'document',
        additions: fi.additions || 0,
        deletions: fi.deletions || 0,
        lazy: true,
        diffTooLarge: false,
        diffLoaded: false,
      };
    });

    return eagerFiles.concat(lazyFiles);
  }

  // Load a single file's content, comments, and diff from the API.
  async function loadSingleFile(fi, scope) {
    let diffUrl = '/api/file/diff?path=' + enc(fi.path);
    if (scope && scope !== 'all') {
      diffUrl += '&scope=' + enc(scope);
    }
    if (diffCommit) {
      diffUrl += '&commit=' + enc(diffCommit);
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
      lazy: false,
    };

    // Mark large diffs for deferred rendering
    let diffLineCount = 0;
    for (let h = 0; h < f.diffHunks.length; h++) {
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
  }

  // ===== Viewed State =====
  function viewedStorageKey() {
    const paths = files.map(function(f) { return f.path; }).sort().join('\n');
    let hash = 0;
    for (let i = 0; i < paths.length; i++) {
      hash = ((hash << 5) - hash + paths.charCodeAt(i)) | 0;
    }
    return 'crit-viewed-' + (hash >>> 0).toString(36);
  }

  function saveViewedState() {
    const viewed = {};
    for (let i = 0; i < files.length; i++) {
      if (files[i].viewed) viewed[files[i].path] = true;
    }
    try { localStorage.setItem(viewedStorageKey(), JSON.stringify(viewed)); } catch (_) {}
  }

  function restoreViewedState() {
    try {
      const data = JSON.parse(localStorage.getItem(viewedStorageKey()) || '{}');
      for (let i = 0; i < files.length; i++) {
        files[i].viewed = !!data[files[i].path];
        if (files[i].viewed) files[i].collapsed = true;
      }
    } catch (_) {}
  }

  function toggleViewed(filePath) {
    const file = getFileByPath(filePath);
    if (!file) return;
    file.viewed = !file.viewed;
    saveViewedState();
    updateViewedCount();
    updateTreeViewedState();
    // Update the checkbox in the file header
    const section = document.getElementById('file-section-' + filePath);
    if (section) {
      const cb = section.querySelector('.file-header-viewed input');
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

  async function fetchWhenReady(url) {
    const start = Date.now();
    const maxWait = 5 * 60 * 1000; // 5 minutes
    while (true) {
      let r;
      try {
        r = await fetch(url);
      } catch (_) {
        // Network error — server may have shut down during init
        const el = document.getElementById('filesContainer');
        if (el) {
          el.innerHTML =
            '<div class="loading" style="padding: 40px; text-align: center; color: var(--fg-muted);">' +
            'Server disconnected</div>';
        }
        throw new Error('Server disconnected');
      }
      if (r.status === 503) {
        if (Date.now() - start > maxWait) {
          throw new Error('Server did not finish initializing within 5 minutes');
        }
        const elapsed = Math.round((Date.now() - start) / 1000);
        const loadingEl = document.getElementById('filesContainer');
        if (loadingEl) {
          loadingEl.innerHTML =
            '<div class="loading" style="padding: 40px; text-align: center; color: var(--fg-muted);">' +
            'Initializing\u2026 (' + elapsed + 's)</div>';
        }
        await new Promise(function(resolve) { setTimeout(resolve, 500); });
        continue;
      }
      if (r.status === 500) {
        let body = {};
        try { body = await r.json(); } catch (_) {}
        const msg = body.message || 'Server initialization failed';
        document.getElementById('filesContainer').innerHTML =
          '<div class="loading" style="padding: 40px; text-align: center; color: var(--fg-muted);">' +
          msg + '</div>';
        throw new Error(msg);
      }
      if (!r.ok) {
        throw new Error('Unexpected server response: ' + r.status);
      }
      return r;
    }
  }

  // ===== Init =====
  async function init() {
    initTheme();
    initWidth();

    // Measure actual header height and set CSS variable for sticky offsets
    function updateHeaderHeight() {
      const h = document.querySelector('.header');
      if (h) document.documentElement.style.setProperty('--header-height', h.getBoundingClientRect().height + 'px');
    }
    updateHeaderHeight();
    window.addEventListener('resize', updateHeaderHeight);

    document.getElementById('filesContainer').innerHTML =
      '<div class="loading" style="padding: 40px; text-align: center; color: var(--fg-muted);">Loading...</div>';

    const [sessionRes, configRes] = await Promise.all([
      fetchWhenReady('/api/session?scope=' + enc(diffScope)).then(r => r.json()),
      fetchWhenReady('/api/config').then(r => r.json()),
    ]);

    session = sessionRes;
    reviewComments = sessionRes.review_comments || [];

    // Fire-and-forget: verify file list endpoint is available for @-mention autocomplete
    fetch('/api/files/list')
      .then(r => { if (r.ok) filePickerReady = true; })
      .catch(() => {});

    // Config
    shareURL = configRes.share_url || '';
    hostedURL = configRes.hosted_url || '';
    deleteToken = configRes.delete_token || '';
    configAuthor = configRes.author || '';
    agentEnabled = configRes.agent_cmd_enabled || false;
    agentName = configRes.agent_name || 'agent';

    if (shareURL) {
      const shareBtn = document.getElementById('shareBtn');
      shareBtn.style.display = '';
      if (session.mode === 'git') {
        shareBtn.disabled = true;
        shareBtn.title = 'Sharing is not available in git mode';
      } else if (hostedURL) {
        setShareButtonState('shared');
      }
    }

    // Update notifications (brew upgrade + stale integrations)
    pendingUpdates = [];
    const hasBrew = configRes.latest_version && configRes.version && configRes.latest_version !== configRes.version;
    if (hasBrew) {
      pendingUpdates.push({
        label: 'Crit ' + configRes.latest_version + ' available',
        labelUrl: 'https://github.com/tomasz-tomczyk/crit/releases/tag/v' + configRes.latest_version,
        hint: 'brew update && brew upgrade crit'
      });
    }
    if (configRes.stale_integrations) {
      configRes.stale_integrations.forEach(function(si) {
        // Capitalize agent name for display
        const name = si.agent.replace(/\b\w/g, function(c) { return c.toUpperCase(); }).replace(/-/g, ' ');
        pendingUpdates.push({ label: name + ' plugin outdated', hint: si.hint });
      });
    }

    pendingUpdatesVersion = configRes.latest_version || configRes.version || '';
    const dismissed = getCookie('crit-updates-dismissed');
    if (pendingUpdates.length > 0 && dismissed !== pendingUpdatesVersion) {
      document.getElementById('updateBtn').style.display = '';
    }

    // Header context: branch name in git mode, filename in single-file file mode
    if (session.mode === 'git' && session.branch) {
      document.getElementById('branchContext').style.display = '';
      document.getElementById('branchName').textContent = session.branch;
      // Base branch picker: show in git mode when on a feature branch
      if (session.base_ref) {
        currentBaseBranch = session.base_branch_name || '';
        document.getElementById('baseBranchLabel').textContent = currentBaseBranch || 'base';
        document.getElementById('baseBranchArrow').style.display = '';
        fetchBranches();
      }
    } else if (session.mode !== 'git' && session.files && session.files.length === 1) {
      document.getElementById('branchContext').style.display = '';
      document.querySelector('.branch-icon').innerHTML = '<svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor"><path fill-rule="evenodd" d="M3.75 1.5a.25.25 0 0 0-.25.25v12.5c0 .138.112.25.25.25h8.5a.25.25 0 0 0 .25-.25V6H9.75A1.75 1.75 0 0 1 8 4.25V1.5H3.75zm5.75.56v2.19c0 .138.112.25.25.25h2.19L9.5 2.06zM2 1.75C2 .784 2.784 0 3.75 0h5.086c.464 0 .909.184 1.237.513l3.414 3.414c.329.328.513.773.513 1.237v8.086A1.75 1.75 0 0 1 12.25 15h-8.5A1.75 1.75 0 0 1 2 13.25V1.75z"/></svg>';
      document.getElementById('branchName').textContent = session.files[0].path.split('/').pop();
    }

    // PR overview panel toggle
    if (configRes.pr_url && configRes.pr_number) {
      prData = configRes;
      const prToggle = document.getElementById('prToggle');
      prToggle.style.display = '';
      document.getElementById('prToggleNumber').textContent = '#' + configRes.pr_number;
      if (configRes.pr_is_draft) prToggle.classList.add('pr-toggle-draft');
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
      const scopeToggle = document.getElementById('scopeToggle');
      scopeToggle.style.display = '';
      const scopes = session.available_scopes || ['all', 'staged', 'unstaged'];
      scopeToggle.querySelectorAll('.toggle-btn').forEach(function(b) {
        // Clear previous disabled state before re-evaluating
        b.disabled = false;
        b.classList.remove('disabled');
        if (b.dataset.scope !== 'all' && scopes.indexOf(b.dataset.scope) === -1) {
          b.disabled = true;
          b.classList.add('disabled');
        }
      });
      if (scopes.indexOf(diffScope) === -1) {
        diffScope = 'all';
        setCookie('crit-diff-scope', 'all');
        // Re-fetch session with corrected scope — the initial fetch used the
        // stale cookie value and may have returned an empty file list.
        const corrected = await fetchWhenReady('/api/session?scope=all').then(r => r.json());
        session = corrected;
        reviewComments = corrected.review_comments || [];
      }
      scopeToggle.querySelectorAll('.toggle-btn').forEach(function(b) {
        b.classList.toggle('active', b.dataset.scope === diffScope);
      });

      // Commit dropdown: visible only for all/branch scope in git mode
      if (diffScope === 'all' || diffScope === 'branch') {
        fetchCommits();
      } else {
        commitDropdownEl.style.display = 'none';
        diffCommit = '';
      }
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
    const hasDiffs = files.some(function(f) {
      return f.fileType === 'markdown' && f.previousLineBlocks && f.previousLineBlocks.length > 0;
    });
    const diffToggleBtn = document.getElementById('diffToggle');
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

  // ===== buildLineBlocks helpers =====

  // Find the matching close token for an open token at openIdx.
  function findCloseToken(tokens, openIdx) {
    const openType = tokens[openIdx].type;
    const closeType = openType.replace('_open', '_close');
    let depth = 1;
    for (let j = openIdx + 1; j < tokens.length; j++) {
      if (tokens[j].type === openType) depth++;
      if (tokens[j].type === closeType) { depth--; if (depth === 0) return j; }
    }
    return openIdx;
  }

  // Emit gap-line blocks for uncovered source lines up to (but not including) `upTo`.
  function addGapLineBlocks(blocks, sourceLines, coveredUpTo, upTo) {
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
    return coveredUpTo;
  }

  // Handle a fence (code block) token — split into per-line blocks.
  function handleFenceToken(token, blocks, sourceLines, coveredUpTo, blockStart, blockEnd) {
    const lang = token.info.trim().split(/\s+/)[0] || '';

    // Mermaid diagrams: render as a single block (not split per-line)
    if (lang === 'mermaid') {
      blocks.push({
        startLine: blockStart + 1, endLine: blockEnd,
        html: '<pre><code class="language-mermaid">' + escapeHtml(token.content) + '</code></pre>',
        isEmpty: false, cssClass: 'mermaid-block'
      });
      return addGapLineBlocks(blocks, sourceLines, blockEnd, blockEnd);
    }

    let highlighted = '';
    if (lang && hljs.getLanguage(lang)) {
      try { highlighted = hljs.highlight(token.content, { language: lang }).value; } catch (_) {}
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
      const isLast = (ci === codeLines.length - 1 && blockEnd <= ln);
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
  function handleListToken(tokens, i, token, md, blocks, sourceLines, coveredUpTo, blockEnd) {
    const listCloseIdx = findCloseToken(tokens, i);
    const listTag = token.type === 'bullet_list_open' ? 'ul' : 'ol';
    let j = i + 1;

    while (j < listCloseIdx) {
      if (tokens[j].type === 'list_item_open') {
        const itemMap = tokens[j].map;
        const itemCloseIdx = findCloseToken(tokens, j);

        if (itemMap) {
          coveredUpTo = addGapLineBlocks(blocks, sourceLines, coveredUpTo, itemMap[0]);
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

    coveredUpTo = addGapLineBlocks(blocks, sourceLines, coveredUpTo, blockEnd);
    return { nextIndex: listCloseIdx + 1, coveredUpTo: coveredUpTo };
  }

  // Handle a table token — split into per-row blocks.
  function handleTableToken(tokens, i, md, blocks, sourceLines, coveredUpTo, blockEnd) {
    const tableCloseIdx = findCloseToken(tokens, i);

    // Build colgroup from header cell alignments
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
        const trCloseIdx = findCloseToken(tokens, j);
        const trMap = tokens[j].map;

        if (trMap) {
          // Emit separator / gap lines between rows
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

    // Mark the last table row
    if (blocks.length > 0 && blocks[blocks.length - 1].cssClass &&
        blocks[blocks.length - 1].cssClass.includes('table-row')) {
      blocks[blocks.length - 1].cssClass += ' table-last';
    }

    coveredUpTo = addGapLineBlocks(blocks, sourceLines, coveredUpTo, blockEnd);
    return { nextIndex: tableCloseIdx + 1, coveredUpTo: coveredUpTo };
  }

  // Handle a blockquote token — split into child blocks.
  function handleBlockquoteToken(tokens, i, md, blocks, sourceLines, coveredUpTo, blockStart, blockEnd) {
    const bqCloseIdx = findCloseToken(tokens, i);
    let j = i + 1;
    let hasChildren = false;

    while (j < bqCloseIdx) {
      if (tokens[j].nesting === -1 || !tokens[j].map) { j++; continue; }
      hasChildren = true;
      const childMap = tokens[j].map;
      let childCloseIdx = j;
      if (tokens[j].nesting === 1) childCloseIdx = findCloseToken(tokens, j);
      coveredUpTo = addGapLineBlocks(blocks, sourceLines, coveredUpTo, childMap[0]);
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

    coveredUpTo = addGapLineBlocks(blocks, sourceLines, coveredUpTo, blockEnd);
    return { nextIndex: bqCloseIdx + 1, coveredUpTo: coveredUpTo };
  }

  // ===== buildLineBlocks =====
  // Parses markdown tokens into a flat array of commentable line blocks.
  // Delegates to per-token-type handlers for fence, list, table, and blockquote tokens.

  function buildLineBlocks(tokens, md, content) {
    const sourceLines = content.split('\n');
    const totalLines = sourceLines.length;
    const blocks = [];
    let coveredUpTo = 0;

    let i = 0;
    while (i < tokens.length) {
      const token = tokens[i];
      if (token.hidden || !token.map) { i++; continue; }

      const blockStart = token.map[0];
      const blockEnd = token.map[1];

      coveredUpTo = addGapLineBlocks(blocks, sourceLines, coveredUpTo, blockStart);

      // Code blocks (fence): split into per-line blocks
      if (token.type === 'fence') {
        coveredUpTo = handleFenceToken(token, blocks, sourceLines, coveredUpTo, blockStart, blockEnd);
        i++;
        continue;
      }

      // Lists: split into per-item blocks
      if (token.type === 'bullet_list_open' || token.type === 'ordered_list_open') {
        const listResult = handleListToken(tokens, i, token, md, blocks, sourceLines, coveredUpTo, blockEnd);
        i = listResult.nextIndex;
        coveredUpTo = listResult.coveredUpTo;
        continue;
      }

      // Tables: split into per-row blocks
      if (token.type === 'table_open') {
        const tableResult = handleTableToken(tokens, i, md, blocks, sourceLines, coveredUpTo, blockEnd);
        i = tableResult.nextIndex;
        coveredUpTo = tableResult.coveredUpTo;
        continue;
      }

      // Blockquotes: split into child blocks
      if (token.type === 'blockquote_open') {
        const bqResult = handleBlockquoteToken(tokens, i, md, blocks, sourceLines, coveredUpTo, blockStart, blockEnd);
        i = bqResult.nextIndex;
        coveredUpTo = bqResult.coveredUpTo;
        continue;
      }

      // Default: render as single block
      let closeIdx = i;
      if (token.nesting === 1) closeIdx = findCloseToken(tokens, i);

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

    coveredUpTo = addGapLineBlocks(blocks, sourceLines, coveredUpTo, totalLines);
    return blocks;
  }

  // ===== Utility Functions =====
  function processTaskLists(html) {
    return html.replace(
      /(<li[^>]*class="task-list-item"[^>]*>)\s*<p>\[([ x])\]\s*/gi,
      function(_, liTag, checked) {
        const checkbox = checked === 'x'
          ? '<input type="checkbox" checked disabled>'
          : '<input type="checkbox" disabled>';
        return liTag + '<p>' + checkbox;
      }
    ).replace(
      /(<li[^>]*class="task-list-item"[^>]*>)\[([ x])\]\s*/gi,
      function(_, liTag, checked) {
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

  function relativeTime(dateStr) {
    const now = Date.now();
    const then = new Date(dateStr).getTime();
    const diff = Math.floor((now - then) / 1000);
    if (diff < 60) return 'just now';
    if (diff < 3600) return Math.floor(diff / 60) + 'm ago';
    if (diff < 86400) return Math.floor(diff / 3600) + 'h ago';
    if (diff < 604800) return Math.floor(diff / 86400) + 'd ago';
    return Math.floor(diff / 604800) + 'w ago';
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
  let activeTreePath = null;
  let treeObserver = null;
  let ignoreTreeObserverUntil = 0;
  const treeFolderState = {}; // { 'src': true, 'src/components': false } — true = collapsed

  function buildFileTree(fileList) {
    // Build a nested tree from flat paths
    const root = { children: {}, files: [] };
    for (let i = 0; i < fileList.length; i++) {
      const f = fileList[i];
      const parts = f.path.split('/');
      let node = root;
      for (let j = 0; j < parts.length - 1; j++) {
        const dirName = parts[j];
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
    const dirs = Object.keys(tree.children);
    const result = { children: {}, files: tree.files };
    for (let i = 0; i < dirs.length; i++) {
      let name = dirs[i];
      let child = tree.children[name];
      // Recursively collapse child first
      child = collapseCommonPrefixes(child);
      // If child has exactly one subdirectory and no files, merge
      let childDirs = Object.keys(child.children);
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
    const panel = document.getElementById('fileTreePanel');
    if (files.length <= 1 && session.mode !== 'git') {
      panel.style.display = 'none';
      return;
    }
    panel.style.display = '';

    // Stats
    let totalAdd = 0, totalDel = 0;
    for (let i = 0; i < files.length; i++) { totalAdd += files[i].additions; totalDel += files[i].deletions; }
    const statsEl = document.getElementById('fileTreeStats');
    statsEl.innerHTML =
      '<span>' + files.length + '</span>' +
      (totalAdd ? ' <span class="tree-stat-add">+' + totalAdd + '</span>' : '') +
      (totalDel ? ' <span class="tree-stat-del">-' + totalDel + '</span>' : '');

    // Collapse/expand all button
    const existingBtn = document.querySelector('.file-tree-collapse-btn');
    if (existingBtn) existingBtn.remove();
    if (files.length > 1) {
      const collapseBtn = document.createElement('button');
      collapseBtn.className = 'file-tree-collapse-btn';
      collapseBtn.title = 'Collapse all files';
      // Stacked chevron SVG
      collapseBtn.innerHTML = '<svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor"><path d="M4.22 3.22a.75.75 0 0 1 1.06 0L8 5.94l2.72-2.72a.75.75 0 1 1 1.06 1.06l-3.25 3.25a.75.75 0 0 1-1.06 0L4.22 4.28a.75.75 0 0 1 0-1.06zm0 5a.75.75 0 0 1 1.06 0L8 10.94l2.72-2.72a.75.75 0 1 1 1.06 1.06l-3.25 3.25a.75.75 0 0 1-1.06 0L4.22 9.28a.75.75 0 0 1 0-1.06z"/></svg>';
      collapseBtn.addEventListener('click', function() {
        const anyExpanded = files.some(function(f) { return !f.collapsed; });
        for (let i = 0; i < files.length; i++) {
          files[i].collapsed = anyExpanded;
        }
        const sections = document.querySelectorAll('.file-section');
        for (let i = 0; i < sections.length; i++) {
          sections[i].open = !anyExpanded;
        }
        collapseBtn.title = anyExpanded ? 'Expand all files' : 'Collapse all files';
        collapseBtn.classList.toggle('all-collapsed', anyExpanded);
      });
      const headerEl = document.querySelector('.file-tree-header');
      headerEl.appendChild(collapseBtn);
    }

    // Build and render tree
    let tree = buildFileTree(files);
    tree = collapseCommonPrefixes(tree);
    const body = document.getElementById('fileTreeBody');
    body.innerHTML = '';
    renderTreeNode(body, tree, 0, '');

    // Set up intersection observer for active file tracking
    setupTreeObserver();
  }

  function fileStatusIcon(status) {
    // GitHub-style: document icon with colored +/- badge
    const doc = '<path fill-rule="evenodd" d="M3.75 1.5a.25.25 0 0 0-.25.25v12.5c0 .138.112.25.25.25h8.5a.25.25 0 0 0 .25-.25V6H9.75A1.75 1.75 0 0 1 8 4.25V1.5H3.75zm5.75.56v2.19c0 .138.112.25.25.25h2.19L9.5 2.06zM2 1.75C2 .784 2.784 0 3.75 0h5.086c.464 0 .909.184 1.237.513l3.414 3.414c.329.328.513.773.513 1.237v8.086A1.75 1.75 0 0 1 12.25 15h-8.5A1.75 1.75 0 0 1 2 13.25V1.75z"/>';
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
    const folderSVG = '<svg viewBox="0 0 16 16" fill="currentColor"><path d="M1.75 1A1.75 1.75 0 0 0 0 2.75v10.5C0 14.216.784 15 1.75 15h12.5A1.75 1.75 0 0 0 16 13.25v-8.5A1.75 1.75 0 0 0 14.25 3H7.5a.25.25 0 0 1-.2-.1l-.9-1.2C6.07 1.26 5.55 1 5 1H1.75Z"/></svg>';

    // Render subdirectories
    const dirs = Object.keys(node.children).sort();
    for (let d = 0; d < dirs.length; d++) {
      const dirName = dirs[d];
      const fullPath = pathPrefix ? pathPrefix + '/' + dirName : dirName;
      const child = node.children[dirName];
      const isCollapsed = treeFolderState[fullPath] === true;

      const folder = document.createElement('div');
      folder.className = 'tree-folder' + (isCollapsed ? ' collapsed' : '');
      folder.dataset.folderPath = fullPath;

      const row = document.createElement('div');
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

      const childContainer = document.createElement('div');
      childContainer.className = 'tree-folder-children';
      renderTreeNode(childContainer, child, depth + 1, fullPath);
      folder.appendChild(childContainer);

      container.appendChild(folder);
    }

    // Render files
    const sortedFiles = node.files.slice().sort(function(a, b) { return a.path.localeCompare(b.path); });
    for (let fi = 0; fi < sortedFiles.length; fi++) {
      const f = sortedFiles[fi];
      const fileName = f.path.split('/').pop();
      const fileEl = document.createElement('div');
      fileEl.className = 'tree-file' + (activeTreePath === f.path ? ' active' : '') + (f.viewed ? ' viewed' : '');
      fileEl.dataset.treePath = f.path;
      fileEl.style.paddingLeft = (24 + depth * 16) + 'px';

      // In file mode, show plain file icon (no git status badge)
      const iconHtml = session.mode === 'git' ? fileStatusIcon(f.status) : fileStatusIcon('');
      let innerHtml =
        '<span class="tree-file-icon">' + iconHtml + '</span>' +
        '<span class="tree-file-name">' + escapeHtml(fileName) + '</span>';

      if (f.viewed) {
        innerHtml += '<span class="tree-viewed-check" title="Viewed">&#10003;</span>';
      }
      const unresolvedCount = f.comments.filter(function(c) { return !c.resolved; }).length;
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
    const allFiles = document.querySelectorAll('.tree-file');
    for (let i = 0; i < allFiles.length; i++) {
      allFiles[i].classList.toggle('active', allFiles[i].dataset.treePath === filePath);
    }
    // Scroll active item into view within the tree panel (manual scroll
    // to avoid scrollIntoView affecting ancestor scroll containers)
    const activeEl = document.querySelector('.tree-file.active');
    if (activeEl) {
      const panel = document.getElementById('fileTreeBody');
      const rect = activeEl.getBoundingClientRect();
      const panelRect = panel.getBoundingClientRect();
      if (rect.top < panelRect.top) {
        panel.scrollTop += rect.top - panelRect.top;
      } else if (rect.bottom > panelRect.bottom) {
        panel.scrollTop += rect.bottom - panelRect.bottom;
      }
    }
  }

  function updateTreeCommentBadges() {
    const allFiles = document.querySelectorAll('.tree-file');
    for (let i = 0; i < allFiles.length; i++) {
      const el = allFiles[i];
      const path = el.dataset.treePath;
      const file = getFileByPath(path);
      if (!file) continue;
      let badge = el.querySelector('.tree-comment-badge');
      const count = file.comments.filter(function(c) { return !c.resolved; }).length;
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
    const allFiles = document.querySelectorAll('.tree-file');
    for (let i = 0; i < allFiles.length; i++) {
      const el = allFiles[i];
      const path = el.dataset.treePath;
      const file = getFileByPath(path);
      if (!file) continue;
      el.classList.toggle('viewed', !!file.viewed);
      let check = el.querySelector('.tree-viewed-check');
      if (file.viewed) {
        if (!check) {
          check = document.createElement('span');
          check.className = 'tree-viewed-check';
          check.title = 'Viewed';
          check.textContent = '\u2713';
          // Insert before comment badge if present, else append
          const badge = el.querySelector('.tree-comment-badge');
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
    const sections = document.querySelectorAll('.file-section[id]');
    if (sections.length === 0) return;

    treeObserver = new IntersectionObserver(function(entries) {
      // Skip observer updates briefly after a manual scrollToFile click
      if (Date.now() < ignoreTreeObserverUntil) return;
      // Find the topmost visible section
      let bestPath = null;
      let bestTop = Infinity;
      for (let i = 0; i < entries.length; i++) {
        if (entries[i].isIntersecting) {
          const top = entries[i].boundingClientRect.top;
          if (top < bestTop) {
            bestTop = top;
            bestPath = entries[i].target.id.replace('file-section-', '');
          }
        }
      }
      if (bestPath) updateTreeActive(bestPath);
    }, { rootMargin: '-60px 0px -70% 0px' });

    for (let i = 0; i < sections.length; i++) {
      treeObserver.observe(sections[i]);
    }
  }

  function scrollToFile(filePath) {
    const sectionEl = document.getElementById('file-section-' + filePath);
    if (!sectionEl) return;
    // Uncollapse if collapsed
    const file = getFileByPath(filePath);
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
    const docEls = document.querySelectorAll('.line-block-added, .line-block-modified, .deletion-marker');
    // Diff view: diff-added and diff-removed blocks in rendered diff (file mode)
    const diffEls = document.querySelectorAll('.diff-view .line-block.diff-added, .diff-view .line-block.diff-removed, .diff-view-unified .line-block.diff-added, .diff-view-unified .line-block.diff-removed');
    const all = docEls.length > 0 ? docEls : diffEls;
    if (all.length === 0) { currentChangeIdx = -1; updateChangeCounters(); return; }
    let group = null;
    for (let i = 0; i < all.length; i++) {
      const el = all[i];
      const fp = el.dataset.filePath;
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
    let node = a.nextElementSibling;
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

    const viewCenter = window.innerHeight / 2;
    const threshold = 50;
    let targetIdx = -1;

    // Check if the previously navigated change is still near viewport center
    // (i.e. user hasn't scrolled away manually)
    let currentIsCentered = false;
    if (currentChangeIdx >= 0 && currentChangeIdx < changeGroups.length) {
      const curRect = changeGroups[currentChangeIdx].elements[0].getBoundingClientRect();
      const curCenter = (curRect.top + curRect.bottom) / 2;
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
        for (let i = 0; i < changeGroups.length; i++) {
          let rect = changeGroups[i].elements[0].getBoundingClientRect();
          let elCenter = (rect.top + rect.bottom) / 2;
          if (elCenter > viewCenter + threshold) { targetIdx = i; break; }
        }
        if (targetIdx === -1) targetIdx = 0;
      } else {
        for (let i = changeGroups.length - 1; i >= 0; i--) {
          const rect = changeGroups[i].elements[0].getBoundingClientRect();
          const elCenter = (rect.top + rect.bottom) / 2;
          if (elCenter < viewCenter - threshold) { targetIdx = i; break; }
        }
        if (targetIdx === -1) targetIdx = changeGroups.length - 1;
      }
    }

    currentChangeIdx = targetIdx;
    const group = changeGroups[currentChangeIdx];
    group.elements[0].scrollIntoView({ block: 'center', behavior: 'instant' });
    group.elements.forEach(function(el) { el.classList.add('change-flash'); });
    focusedElement = group.elements[0];
    focusedFilePath = group.filePath;
    const bi = parseInt(group.elements[0].dataset.blockIndex);
    if (!isNaN(bi)) focusedBlockIndex = bi;
    updateChangeCounters();
  }

  function updateChangeCounters() {
    const labels = document.querySelectorAll('.change-nav-label');
    labels.forEach(function(label) {
      const fp = label.dataset.filePath;
      // Count groups for this file
      const fileGroups = changeGroups.filter(function(g) { return g.filePath === fp; });
      const total = fileGroups.length;
      // Find current index within this file's groups
      let current = 0;
      if (currentChangeIdx >= 0) {
        const globalGroup = changeGroups[currentChangeIdx];
        if (globalGroup.filePath === fp) {
          current = fileGroups.indexOf(globalGroup) + 1;
        }
      }
      label.textContent = (current || '-') + ' / ' + total + ' change' + (total !== 1 ? 's' : '');
    });
  }

  // Re-render only a single file section (preserves scroll position)
  function saveOpenFormContent(filePath) {
    let fileForms = getFormsForFile(filePath);
    for (let i = 0; i < fileForms.length; i++) {
      const ta = document.querySelector('.comment-form[data-form-key="' + fileForms[i].formKey + '"] textarea');
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
    });
    section.addEventListener('toggle', function() {
      file.collapsed = !section.open;
    });

    // Lazy file: load content on first expand
    if (file.lazy) {
      section.addEventListener('toggle', function onLazyExpand() {
        if (!section.open || !file.lazy) return;
        if (file._lazyLoading) return;
        file._lazyLoading = true;
        section.removeEventListener('toggle', onLazyExpand);
        section.classList.add('file-section-loading');

        loadSingleFile({
          path: file.path,
          status: file.status,
          file_type: file.fileType,
          additions: file.additions,
          deletions: file.deletions,
        }, diffScope).then(function(loaded) {
          // Copy loaded data into the existing file object
          file.content = loaded.content;
          file.previousContent = loaded.previousContent;
          file.comments = loaded.comments;
          file.diffHunks = loaded.diffHunks;
          file.lineBlocks = loaded.lineBlocks;
          file.previousLineBlocks = loaded.previousLineBlocks;
          file.tocItems = loaded.tocItems;
          file.diffTooLarge = loaded.diffTooLarge;
          file.diffLoaded = loaded.diffLoaded;
          file.lazy = false;
          file._lazyLoading = false;
          if (loaded.highlightCache) file.highlightCache = loaded.highlightCache;
          if (loaded.lang) file.lang = loaded.lang;

          // Re-render this file section in place
          section.classList.remove('file-section-loading');
          const newSection = renderFileSection(file);
          newSection.open = section.open;
          section.replaceWith(newSection);

          // Update UI state
          renderFileTree();
          updateCommentCount();
          rebuildNavList();
        }).catch(function() {
          file._lazyLoading = false;
          // Guard against stale DOM node: only re-attach if still in the document
          if (!section.isConnected) return;
          section.classList.remove('file-section-loading');
          section.addEventListener('toggle', onLazyExpand);
        });
      });
    }

    const dirParts = file.path.split('/');
    const fileName = dirParts.pop();
    const dirPath = dirParts.length > 0 ? dirParts.join('/') + '/' : '';

    // In file mode, hide the badge (status like "modified" is only meaningful in git mode)
    const showBadge = session.mode === 'git';
    let badgeLabel = file.status.charAt(0).toUpperCase() + file.status.slice(1);
    if (file.status === 'untracked') badgeLabel = 'New';
    if (file.status === 'added') badgeLabel = 'New File';

    // In single-file file mode, hide the file header (filename is shown in the header bar)
    const singleFileMode = session.mode !== 'git' && files.length === 1;
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
      '';

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
        let fileForms = getFormsForFile(file.path);
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
        const changeNav = document.createElement('div');
        changeNav.className = 'change-nav';
        changeNav.innerHTML =
          '<button class="change-nav-btn" data-dir="-1" title="Previous change (N)">&#9650;</button>' +
          '<span class="change-nav-label" data-file-path="' + escapeHtml(file.path) + '"></span>' +
          '<button class="change-nav-btn" data-dir="1" title="Next change (n)">&#9660;</button>';
        changeNav.addEventListener('click', function(e) {
          const btn = e.target.closest('.change-nav-btn');
          if (!btn) return;
          e.preventDefault();
          e.stopPropagation();
          navigateToChange(parseInt(btn.dataset.dir));
        });
        header.appendChild(changeNav);
      }
    }

    // File comment button
    const fileCommentBtn = document.createElement('button');
    fileCommentBtn.className = 'file-comment-btn';
    fileCommentBtn.title = 'Add file-level comment';
    fileCommentBtn.setAttribute('aria-label', 'Add file-level comment');
    fileCommentBtn.innerHTML = ICON_COMMENT;
    fileCommentBtn.addEventListener('click', function(e) {
      e.stopPropagation(); // Don't toggle the <details>
      e.preventDefault();
      openFileCommentForm(file.path);
    });
    header.appendChild(fileCommentBtn);

    // Viewed checkbox
    const viewedLabel = document.createElement('label');
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

    // File-level comments container (between header and file body)
    const fileComments = file.comments.filter(function(c) { return c.scope === 'file'; });
    const fileForm = getFormsForFile(file.path).find(function(f) { return f.scope === 'file'; });
    if (fileComments.length > 0 || fileForm) {
      const fileCommentsContainer = document.createElement('div');
      fileCommentsContainer.className = 'file-comments';
      for (let ci = 0; ci < fileComments.length; ci++) {
        const comment = fileComments[ci];
        if (comment.resolved) {
          fileCommentsContainer.appendChild(createResolvedElement(comment, file.path));
        } else {
          fileCommentsContainer.appendChild(createCommentElement(comment, file.path));
        }
      }
      if (fileForm) {
        fileCommentsContainer.appendChild(createFileCommentForm(fileForm));
      }
      section.appendChild(fileCommentsContainer);
    }

    // File body
    const body = document.createElement('div');
    body.className = 'file-body';

    const showDiff = file.viewMode === 'diff' || (file.fileType === 'code' && session.mode === 'git');

    if (file.status === 'deleted' && (!file.diffHunks || file.diffHunks.length === 0)) {
      const deleted = document.createElement('div');
      deleted.className = 'diff-deleted-placeholder';
      deleted.textContent = 'This file was deleted.';
      body.appendChild(deleted);
    } else if (showDiff && file.diffTooLarge && !file.diffLoaded) {
      let diffLineCount = 0;
      if (file.diffHunks) {
        for (let h = 0; h < file.diffHunks.length; h++) {
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
    highlightQuotesInSection(section, file);
    return section;
  }

  // ===== Rendered Diff View (Markdown, file mode) =====

  // Build sets of added/removed line numbers from diff hunks
  function buildDiffLineSetFromHunks(hunks) {
    const added = new Set();
    const removed = new Set();
    for (let h = 0; h < hunks.length; h++) {
      const lines = hunks[h].Lines || [];
      for (let l = 0; l < lines.length; l++) {
        if (lines[l].Type === 'add' && lines[l].NewNum) added.add(lines[l].NewNum);
        if (lines[l].Type === 'del' && lines[l].OldNum) removed.add(lines[l].OldNum);
      }
    }
    return { added: added, removed: removed };
  }

  // Classify a block as diff-added, diff-removed, or unchanged
  function classifyBlock(block, changedLines) {
    for (let ln = block.startLine; ln <= block.endLine; ln++) {
      if (changedLines.has(ln)) return true;
    }
    return false;
  }

  function applyBlockSelectionState(el, filePath, startLine, endLine, blockIndex) {
    let fileForms = getFormsForFile(filePath);
    const hasForm = fileForms.some(function(f) {
      return !f.editingId && startLine >= f.startLine && endLine <= f.endLine;
    });
    const inSelection = activeFilePath === filePath && selectionStart !== null && selectionEnd !== null &&
      startLine >= selectionStart && endLine <= selectionEnd;
    el.classList.toggle('selected', inSelection);
    el.classList.toggle('form-selected', hasForm && !inSelection);

    if (blockIndex !== undefined) {
      (function(fp, idx, elem) {
        elem.addEventListener('mouseenter', function() {
          focusedFilePath = fp;
          focusedBlockIndex = idx;
          focusedElement = elem;
        });
      })(filePath, blockIndex, el);

      if (focusedFilePath === filePath && focusedBlockIndex === blockIndex) {
        el.classList.add('focused');
      }
    }
  }

  // Annotate blocks with isDiff flag based on changed line numbers
  function annotateBlocks(blocks, changedLines) {
    return blocks.map(function(b) {
      return Object.assign({}, b, { isDiff: classifyBlock(b, changedLines) });
    });
  }

  function renderRenderedDiffSplit(file) {
    const container = document.createElement('div');
    container.className = 'diff-view';

    let lineSets = buildDiffLineSetFromHunks(file.diffHunks);
    const prevBlocks = annotateBlocks(file.previousLineBlocks, lineSets.removed);
    const currBlocks = annotateBlocks(file.lineBlocks, lineSets.added);

    // Compute word-level diffs for paired changed blocks.
    // Only apply when blocks are sufficiently similar (>30% token overlap) to avoid noise.
    const prevDiffBlocks = prevBlocks.filter(function(b) { return b.isDiff; });
    const currDiffBlocks = currBlocks.filter(function(b) { return b.isDiff; });
    const pairCount = Math.min(prevDiffBlocks.length, currDiffBlocks.length);
    for (let p = 0; p < pairCount; p++) {
      applyWordDiffPair(prevDiffBlocks[p], currDiffBlocks[p]);
    }

    // Labels row
    const leftLabel = document.createElement('div');
    leftLabel.className = 'diff-view-side-label';
    leftLabel.textContent = 'Previous round';
    container.appendChild(leftLabel);
    const rightLabel = document.createElement('div');
    rightLabel.className = 'diff-view-side-label';
    rightLabel.textContent = 'Current round';
    container.appendChild(rightLabel);

    // Two-pointer merge for horizontal alignment
    let commentsMap = buildCommentsMap(file.comments);
    let commentRangeSet = buildCommentedRangeSet(file.comments);
    let oldIdx = 0, newIdx = 0;

    while (oldIdx < prevBlocks.length || newIdx < currBlocks.length) {
      const leftCell = document.createElement('div');
      leftCell.className = 'diff-view-cell';
      const rightCell = document.createElement('div');
      rightCell.className = 'diff-view-cell';

      if (oldIdx >= prevBlocks.length) {
        // Old exhausted — remaining new blocks are additions
        rightCell.appendChild(renderUnifiedBlock(currBlocks[newIdx], 'diff-added', file, true, newIdx, commentsMap, commentRangeSet));
        newIdx++;
      } else if (newIdx >= currBlocks.length) {
        // New exhausted — remaining old blocks are deletions
        leftCell.appendChild(renderUnifiedBlock(prevBlocks[oldIdx], 'diff-removed', file, false, oldIdx, null, null));
        oldIdx++;
      } else if (prevBlocks[oldIdx].isDiff && currBlocks[newIdx].isDiff) {
        // Both changed — paired change
        leftCell.appendChild(renderUnifiedBlock(prevBlocks[oldIdx], 'diff-removed', file, false, oldIdx, null, null));
        rightCell.appendChild(renderUnifiedBlock(currBlocks[newIdx], 'diff-added', file, true, newIdx, commentsMap, commentRangeSet));
        oldIdx++;
        newIdx++;
      } else if (prevBlocks[oldIdx].isDiff) {
        // Old removed only — spacer on right
        leftCell.appendChild(renderUnifiedBlock(prevBlocks[oldIdx], 'diff-removed', file, false, oldIdx, null, null));
        oldIdx++;
      } else if (currBlocks[newIdx].isDiff) {
        // New added only — spacer on left
        rightCell.appendChild(renderUnifiedBlock(currBlocks[newIdx], 'diff-added', file, true, newIdx, commentsMap, commentRangeSet));
        newIdx++;
      } else {
        // Both unchanged — render both, advance both
        leftCell.appendChild(renderUnifiedBlock(prevBlocks[oldIdx], null, file, false, oldIdx, null, null));
        rightCell.appendChild(renderUnifiedBlock(currBlocks[newIdx], null, file, true, newIdx, commentsMap, commentRangeSet));
        oldIdx++;
        newIdx++;
      }

      container.appendChild(leftCell);
      container.appendChild(rightCell);
    }

    return container;
  }

  function buildContentClasses(block) {
    let classes = 'line-content';
    if (block.isEmpty) classes += ' empty-line';
    if (block.cssClass) classes += ' ' + block.cssClass;
    return classes;
  }

  // Render a single block for the unified diff view.
  // When commentable=true, includes gutter, keyboard nav, comments. Otherwise read-only.
  function renderUnifiedBlock(block, diffClass, file, commentable, blockIndex, commentsMap, commentRangeSet) {
    const frag = document.createDocumentFragment();

    const lineBlockEl = document.createElement('div');
    lineBlockEl.className = 'line-block';
    lineBlockEl.dataset.filePath = file.path;
    if (commentable) {
      lineBlockEl.classList.add('kb-nav');
      lineBlockEl.dataset.blockIndex = blockIndex;
      lineBlockEl.dataset.startLine = block.startLine;
      lineBlockEl.dataset.endLine = block.endLine;
    }
    if (diffClass) lineBlockEl.classList.add(diffClass);

    let blockComments = null;
    if (commentable) {
      blockComments = getCommentsForBlock(block, commentsMap);
      let blockInCommentRange = false;
      for (let ln = block.startLine; ln <= block.endLine; ln++) {
        if (commentRangeSet.has(ln + ':')) { blockInCommentRange = true; break; }
      }
      if (blockInCommentRange) lineBlockEl.classList.add('has-comment');

      applyBlockSelectionState(lineBlockEl, file.path, block.startLine, block.endLine, blockIndex);

      const commentGutter = document.createElement('div');
      commentGutter.className = 'line-comment-gutter';
      commentGutter.dataset.startLine = block.startLine;
      commentGutter.dataset.endLine = block.endLine;
      commentGutter.dataset.filePath = file.path;
      const lineAdd = document.createElement('span');
      lineAdd.className = 'line-add';
      lineAdd.textContent = '+';
      commentGutter.appendChild(lineAdd);
      commentGutter.addEventListener('mousedown', handleGutterMouseDown);
      lineBlockEl.appendChild(commentGutter);
    } else {
      // Non-commentable block: still add gutter but mark as read-only
      const roGutter = document.createElement('div');
      roGutter.className = 'line-comment-gutter diff-no-comment';
      lineBlockEl.appendChild(roGutter);
    }

    // Line number gutter
    const gutter = document.createElement('div');
    gutter.className = 'line-gutter';
    const lineNum = document.createElement('span');
    lineNum.className = 'line-num';
    lineNum.textContent = block.startLine;
    gutter.appendChild(lineNum);
    lineBlockEl.insertBefore(gutter, lineBlockEl.firstChild);

    const contentEl = document.createElement('div');
    contentEl.className = buildContentClasses(block);
    let html = block.wordDiffHtml || block.html;
    html = processTaskLists(html);
    html = rewriteImageSrcs(html);
    contentEl.innerHTML = html;
    lineBlockEl.appendChild(contentEl);

    frag.appendChild(lineBlockEl);

    // Comments after block (only on commentable/new side)
    if (commentable && blockComments) {
      for (let ci = 0; ci < blockComments.length; ci++) {
        if (blockComments[ci].resolved) {
          frag.appendChild(createResolvedElement(blockComments[ci], file.path));
        } else {
          frag.appendChild(createCommentElement(blockComments[ci], file.path));
        }
      }
      const fileForms = getFormsForFile(file.path);
      for (let fi = 0; fi < fileForms.length; fi++) {
        if (!fileForms[fi].editingId && fileForms[fi].afterBlockIndex === blockIndex) {
          frag.appendChild(createCommentForm(fileForms[fi]));
        }
      }
    }

    return frag;
  }

  function renderRenderedDiffUnified(file) {
    const container = document.createElement('div');
    container.className = 'diff-view-unified';

    const lineSets = buildDiffLineSetFromHunks(file.diffHunks);
    const oldBlocks = file.previousLineBlocks;
    const newBlocks = file.lineBlocks;

    let commentsMap = buildCommentsMap(file.comments);
    let commentRangeSet = buildCommentedRangeSet(file.comments);

    // Two-pointer merge: walk both block lists simultaneously
    let oldIdx = 0;
    let newIdx = 0;

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
        // Collect consecutive removed blocks
        const removedRun = [];
        while (oldIdx < oldBlocks.length && classifyBlock(oldBlocks[oldIdx], lineSets.removed)) {
          removedRun.push(oldIdx);
          oldIdx++;
        }
        // Collect consecutive added blocks
        const addedRun = [];
        while (newIdx < newBlocks.length && classifyBlock(newBlocks[newIdx], lineSets.added)) {
          addedRun.push(newIdx);
          newIdx++;
        }
        // Pair removed/added blocks by similarity for word diff
        const rmTexts = removedRun.map(function(idx) { return htmlToText(oldBlocks[idx].html); });
        const adTexts = addedRun.map(function(idx) { return htmlToText(newBlocks[idx].html); });
        const mdPairs = bestWordDiffPairing(rmTexts, adTexts);
        for (let rp = 0; rp < mdPairs.length; rp++) {
          applyWordDiffPair(oldBlocks[removedRun[mdPairs[rp][0]]], newBlocks[addedRun[mdPairs[rp][1]]]);
        }
        // Emit all removed then all added
        for (let ri = 0; ri < removedRun.length; ri++) {
          container.appendChild(renderUnifiedBlock(oldBlocks[removedRun[ri]], 'diff-removed', file, false, removedRun[ri], null, null));
        }
        for (let ai = 0; ai < addedRun.length; ai++) {
          container.appendChild(renderUnifiedBlock(newBlocks[addedRun[ai]], 'diff-added', file, true, addedRun[ai], commentsMap, commentRangeSet));
        }
      } else if (classifyBlock(newBlocks[newIdx], lineSets.added)) {
        // New block is added (no preceding removal) — emit with green highlight + comments
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
    const added = new Set();
    const modified = new Set();
    const deletionPoints = [];

    for (let h = 0; h < file.diffHunks.length; h++) {
      const lines = file.diffHunks[h].Lines || [];
      let lastContextNewNum = file.diffHunks[h].NewStart > 0 ? file.diffHunks[h].NewStart - 1 : 0;
      let i = 0;
      while (i < lines.length) {
        if (lines[i].Type === 'context') {
          lastContextNewNum = lines[i].NewNum;
          i++;
        } else {
          // Collect consecutive change group (dels then adds, or interleaved)
          const dels = [], adds = [];
          while (i < lines.length && lines[i].Type !== 'context') {
            if (lines[i].Type === 'del') dels.push(lines[i]);
            if (lines[i].Type === 'add') adds.push(lines[i]);
            i++;
          }
          if (dels.length > 0 && adds.length > 0) {
            // Modification: mark add lines as modified (amber)
            for (let a = 0; a < adds.length; a++) {
              if (adds[a].NewNum) modified.add(adds[a].NewNum);
            }
          } else if (adds.length > 0) {
            // Pure addition (green)
            for (let a = 0; a < adds.length; a++) {
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
    const deletionMarkerMap = {};
    if (changeInfo) {
      for (let dp = 0; dp < changeInfo.deletionPoints.length; dp++) {
        const pt = changeInfo.deletionPoints[dp];
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
      let blockInCommentRange = false;
      for (let ln = block.startLine; ln <= block.endLine; ln++) {
        if (commentRangeSet.has(ln + ':')) { blockInCommentRange = true; break; }
      }
      if (blockInCommentRange) lineBlockEl.classList.add('has-comment');

      // Mark blocks that overlap inter-round changes (color-coded)
      if (changeInfo) {
        let blockChangeType = null;
        for (let ln = block.startLine; ln <= block.endLine; ln++) {
          if (changeInfo.modified.has(ln)) { blockChangeType = 'modified'; break; }
          if (changeInfo.added.has(ln)) { blockChangeType = 'added'; }
        }
        if (blockChangeType === 'modified') lineBlockEl.classList.add('line-block-modified');
        else if (blockChangeType === 'added') lineBlockEl.classList.add('line-block-added');
      }

      applyBlockSelectionState(lineBlockEl, file.path, block.startLine, block.endLine, bi);

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
        const isAnchorBlock = block.startLine <= dragState.anchorEndLine && block.endLine >= dragState.anchorStartLine;
        const isCurrentBlock = block.startLine <= dragState.currentEndLine && block.endLine >= dragState.currentStartLine;
        const inRange = block.startLine >= selectionStart && block.endLine <= selectionEnd;
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
      content.className = buildContentClasses(block);
      let html = block.html;
      html = processTaskLists(html);
      html = rewriteImageSrcs(html);
      content.innerHTML = html;

      gutter.appendChild(commentGutter);
      lineBlockEl.appendChild(gutter);
      lineBlockEl.appendChild(content);

      // Insert deletion marker before this block if deletions occurred before it
      if (changeInfo && bi === 0 && deletionMarkerMap[0]) {
        const marker0 = document.createElement('div');
        marker0.className = 'deletion-marker';
        marker0.dataset.filePath = file.path;
        marker0.textContent = '\u2212' + deletionMarkerMap[0].count + ' line' + (deletionMarkerMap[0].count !== 1 ? 's' : '');
        container.appendChild(marker0);
      }

      container.appendChild(lineBlockEl);

      // Insert deletion marker after this block if deletions occurred after it
      if (changeInfo && deletionMarkerMap[block.endLine]) {
        const marker = document.createElement('div');
        marker.className = 'deletion-marker';
        marker.dataset.filePath = file.path;
        marker.textContent = '\u2212' + deletionMarkerMap[block.endLine].count + ' line' + (deletionMarkerMap[block.endLine].count !== 1 ? 's' : '');
        container.appendChild(marker);
      }

      // Comments after block
      for (const comment of blockComments) {
        if (comment.resolved) {
          container.appendChild(createResolvedElement(comment, file.path));
        } else {
          container.appendChild(createCommentElement(comment, file.path));
        }
      }

      // Comment form
      const fileForms = getFormsForFile(file.path);
      for (let fi = 0; fi < fileForms.length; fi++) {
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

  // ===== Word-Level Diff =====

  // Split a line into tokens for similarity comparison.
  function tokenize(line) {
    const tokens = [];
    const re = /[\w]+|[^\w]/g;
    let match;
    while ((match = re.exec(line)) !== null) {
      tokens.push(match[0]);
    }
    return tokens;
  }

  // Compute similarity between two strings using token multiset Dice coefficient.
  // Returns 0–1 (1 = identical tokens, 0 = nothing in common).
  // Only counts word tokens (identifiers, numbers) — single punctuation characters
  // like ", :, {, }, etc. are structural noise that inflates similarity between
  // unrelated JSON/code lines.
  function lineSimilarity(a, b) {
    if (a === b) return 1;
    if (!a || !b) return 0;
    const wordRe = /^\w+$/;
    const tokA = tokenize(a).filter(function(t) { return wordRe.test(t); });
    const tokB = tokenize(b).filter(function(t) { return wordRe.test(t); });
    if (tokA.length === 0 && tokB.length === 0) return 1;
    if (tokA.length === 0 || tokB.length === 0) return 0;
    const counts = {};
    for (let i = 0; i < tokA.length; i++) {
      counts[tokA[i]] = (counts[tokA[i]] || 0) + 1;
    }
    let common = 0;
    for (let i = 0; i < tokB.length; i++) {
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
    const delCount = delTexts.length;
    const addCount = addTexts.length;
    const pairCount = Math.min(delCount, addCount);
    if (pairCount === 0) return [];
    // Large blocks are code rewrites, not line edits — skip word-diff entirely.
    // This matches GitHub's behavior of not highlighting large del/add blocks.
    if (delCount + addCount > 8) return [];
    // 1:1 — pair directly if similar enough (most common case)
    if (delCount === 1 && addCount === 1) {
      return lineSimilarity(delTexts[0], addTexts[0]) >= 0.4 ? [[0, 0]] : [];
    }
    // Compute all similarity scores
    const candidates = [];
    for (let d = 0; d < delCount; d++) {
      for (let a = 0; a < addCount; a++) {
        candidates.push({ d: d, a: a, score: lineSimilarity(delTexts[d], addTexts[a]) });
      }
    }
    candidates.sort(function(x, y) { return y.score - x.score; });
    // Greedy assignment: pick highest similarity first
    const usedDels = {};
    const usedAdds = {};
    const pairs = [];
    for (let i = 0; i < candidates.length; i++) {
      const c = candidates[i];
      if (usedDels[c.d] || usedAdds[c.a]) continue;
      if (c.score < 0.4) break;
      pairs.push([c.d, c.a]);
      usedDels[c.d] = true;
      usedAdds[c.a] = true;
      if (pairs.length === pairCount) break;
    }
    return pairs;
  }

  // Compute LCS membership for two token arrays.
  // Returns { oldKeep: boolean[], newKeep: boolean[] } where true = token is in LCS (unchanged).
  function computeTokenLCS(oldTokens, newTokens) {
    const m = oldTokens.length;
    const n = newTokens.length;
    const dp = [];
    for (let i = 0; i <= m; i++) {
      dp[i] = new Array(n + 1).fill(0);
    }
    for (let i = 1; i <= m; i++) {
      for (let j = 1; j <= n; j++) {
        if (oldTokens[i - 1] === newTokens[j - 1]) {
          dp[i][j] = dp[i - 1][j - 1] + 1;
        } else {
          dp[i][j] = Math.max(dp[i - 1][j], dp[i][j - 1]);
        }
      }
    }
    const oldKeep = new Array(m).fill(false);
    const newKeep = new Array(n).fill(false);
    let i = m, j = n;
    while (i > 0 && j > 0) {
      if (oldTokens[i - 1] === newTokens[j - 1]) {
        oldKeep[i - 1] = true;
        newKeep[j - 1] = true;
        i--; j--;
      } else if (dp[i - 1][j] >= dp[i][j - 1]) {
        i--;
      } else {
        j--;
      }
    }
    return { oldKeep: oldKeep, newKeep: newKeep };
  }

  // Compute word-level diff between two lines using token LCS.
  // Tokenizes into words/punctuation, finds the longest common subsequence,
  // then builds character ranges for changed tokens.
  // This produces whole-word highlights (like GitHub) instead of character-level fragments.
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

    const oldTokens = tokenize(oldLine);
    const newTokens = tokenize(newLine);
    if (oldTokens.length === 0 && newTokens.length === 0) return null;
    // Skip if token counts are huge (LCS is O(m*n))
    if (oldTokens.length > 200 || newTokens.length > 200) return null;

    const result = computeTokenLCS(oldTokens, newTokens);
    const oldKeep = result.oldKeep;
    const newKeep = result.newKeep;

    // If everything changed (no LCS), skip — lines probably don't correspond
    const oldUnchanged = oldKeep.filter(Boolean).length;
    const newUnchanged = newKeep.filter(Boolean).length;
    if (oldUnchanged === 0 && newUnchanged === 0) return null;
    // If nothing changed, skip
    if (oldUnchanged === oldTokens.length && newUnchanged === newTokens.length) return null;

    // Build character ranges for changed tokens.
    // Adjacent changed tokens merge into one range automatically.
    function buildRanges(tokens, keep) {
      const ranges = [];
      let charIdx = 0;
      let rangeStart = -1;
      for (let i = 0; i < tokens.length; i++) {
        if (!keep[i]) {
          if (rangeStart === -1) rangeStart = charIdx;
        } else {
          if (rangeStart !== -1) {
            ranges.push([rangeStart, charIdx]);
            rangeStart = -1;
          }
        }
        charIdx += tokens[i].length;
      }
      if (rangeStart !== -1) ranges.push([rangeStart, charIdx]);
      return ranges;
    }

    const oldRanges = buildRanges(oldTokens, oldKeep);
    const newRanges = buildRanges(newTokens, newKeep);

    if (oldRanges.length === 0 && newRanges.length === 0) return null;

    // If most of the line changed, the lines probably don't correspond —
    // skip word-diff to avoid noisy highlights on unrelated lines.
    const oldChanged = oldRanges.reduce(function(s, r) { return s + r[1] - r[0]; }, 0);
    const newChanged = newRanges.reduce(function(s, r) { return s + r[1] - r[0]; }, 0);
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

    let result = '';
    let charIdx = 0;       // visible character index
    let rangeIdx = 0;      // which range we're processing
    let inRange = false;   // currently inside a word-diff span
    let i = 0;             // position in html string

    while (i < html.length) {
      // Skip HTML tags (don't count them as visible characters)
      if (html[i] === '<') {
        // If we're in a word-diff range, close it before the tag, reopen after
        if (inRange) result += '</span>';
        const tagEnd = html.indexOf('>', i);
        if (tagEnd === -1) { result += html.slice(i); break; }
        result += html.slice(i, tagEnd + 1);
        i = tagEnd + 1;
        if (inRange) result += '<span class="' + cssClass + '">';
        continue;
      }

      // Handle HTML entities (e.g., &amp; &lt; &gt; &quot;) as single visible characters
      let visibleChar;
      if (html[i] === '&') {
        const semiIdx = html.indexOf(';', i);
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
    const oldText = htmlToText(oldBlock.html).replace(/\n/g, ' ');
    const newText = htmlToText(newBlock.html).replace(/\n/g, ' ');
    const wd = wordDiff(oldText, newText);
    if (!wd) return;
    const oldChangedChars = wd.oldRanges.reduce(function(s, r) { return s + r[1] - r[0]; }, 0);
    const newChangedChars = wd.newRanges.reduce(function(s, r) { return s + r[1] - r[0]; }, 0);
    if (oldText.length > 0 && oldChangedChars / oldText.length > 0.7) return;
    if (newText.length > 0 && newChangedChars / newText.length > 0.7) return;
    oldBlock.wordDiffHtml = applyWordDiffToHtml(oldBlock.html, wd.oldRanges, 'diff-word-del');
    newBlock.wordDiffHtml = applyWordDiffToHtml(newBlock.html, wd.newRanges, 'diff-word-add');
  }

  // Pre-compute word diffs for all paired del/add runs in a hunk.
  // Returns a Map<lineIndex, { ranges, cssClass }> mapping hunk line indices to word-diff info.
  function buildHunkWordDiffs(hunk) {
    const wordDiffMap = new Map();
    const lines = hunk.Lines;
    let i = 0;
    while (i < lines.length) {
      if (lines[i].Type === 'del') {
        // Collect consecutive dels
        const delStart = i;
        while (i < lines.length && lines[i].Type === 'del') i++;
        // Collect consecutive adds
        const addStart = i;
        while (i < lines.length && lines[i].Type === 'add') i++;
        // Pair by similarity so word diffs highlight the right counterpart
        const delCount = addStart - delStart;
        const addCount = i - addStart;
        const delTexts = [];
        for (let d = 0; d < delCount; d++) delTexts.push(lines[delStart + d].Content);
        const addTexts = [];
        for (let a = 0; a < addCount; a++) addTexts.push(lines[addStart + a].Content);
        const pairs = bestWordDiffPairing(delTexts, addTexts);
        for (let p = 0; p < pairs.length; p++) {
          const dIdx = delStart + pairs[p][0];
          const aIdx = addStart + pairs[p][1];
          const wd = wordDiff(lines[dIdx].Content, lines[aIdx].Content);
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

  // ===== Diff Gutter Drag (multi-line comment selection) =====
  let diffDragState = null; // { filePath, side, anchorLine, currentLine }

  // Tag a diff line element with data attributes for drag detection + keyboard nav
  // For split mode, navEl (the row) gets kb-nav; el (the side) gets data attrs for drag.
  function tagDiffLine(el, filePath, lineNum, side, navEl) {
    el.dataset.diffFilePath = filePath;
    el.dataset.diffLineNum = lineNum;
    el.dataset.diffSide = side || '';
    // In split mode, kb-nav goes on the row; in unified, on the line itself
    const nav = navEl || el;
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
    const sideMatch = diffMode === 'split' ? diffDragState && diffDragState.side === (side || '') : true;
    if (diffDragState && diffDragState.filePath === filePath && sideMatch && selectionStart !== null && selectionEnd !== null) {
      let isAnchor, isCurrent, inRange, isRangeStart, isRangeEnd;
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
      const hoverVisualIdx = parseInt(diffLine.dataset.diffVisualIdx);
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

    const fp = diffDragState.filePath;
    const side = diffDragState.side;
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
      const contentLines = file.content.split('\n');

      // Build context lines to bridge the gap
      const contextLines = [];
      for (let i = 0; i < gap; i++) {
        const newLineNum = prevNewEnd + i;
        const oldLineNum = prevOldEnd + i;
        const text = newLineNum <= contentLines.length ? contentLines[newLineNum - 1] : '';
        contextLines.push({ Type: 'context', Content: text, OldNum: oldLineNum, NewNum: newLineNum });
      }

      // Merge: prev hunk + context lines + next hunk → single hunk
      let hunks = file.diffHunks;
      const merged = {
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
      let el = comment.resolved
        ? createResolvedElement(comment, filePath)
        : createCommentElement(comment, filePath);
      if (side === 'old') el.classList.add('diff-comment-left');
      else el.classList.add('diff-comment-right');
      container.appendChild(el);
    }
  }

  // Helper: append comment form if it targets this line and side
  function appendDiffForm(container, filePath, lineNum, side) {
    const fileForms = getFormsForFile(filePath);
    for (let fi = 0; fi < fileForms.length; fi++) {
      const form = fileForms[fi];
      let formSide = form.side || '';
      if (!form.editingId && form.endLine === lineNum && formSide === (side || '')) {
        let el = createCommentForm(form);
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
      container.innerHTML = '<div class="diff-no-changes">No changes</div>';
      return container;
    }

    const commentsMap = buildDiffCommentsMap(file.comments);
    const commentVisualSet = buildUnifiedCommentVisualSet(hunks, file.comments);
    let visualIdx = 0; // sequential index for unified drag (old/new nums are different spaces)

    for (let hi = 0; hi < hunks.length; hi++) {
      const hunk = hunks[hi];

      if (hi > 0) {
        const spacer = renderDiffSpacer(hunks[hi - 1], hunk, file, hi - 1, hi);
        if (spacer) container.appendChild(spacer);
      }

      container.appendChild(renderDiffHunkHeader(hunk));

      const wordDiffMap = buildHunkWordDiffs(hunk);

      for (let li = 0; li < hunk.Lines.length; li++) {
        let line = hunk.Lines[li];
        const lineEl = document.createElement('div');
        lineEl.className = 'diff-line';
        if (line.Type === 'add') lineEl.classList.add('addition');
        if (line.Type === 'del') lineEl.classList.add('deletion');
        lineEl.dataset.diffVisualIdx = visualIdx;

        const commentLineNum = line.Type === 'del' ? line.OldNum : line.NewNum;
        const lineSide = line.Type === 'del' ? 'old' : '';
        if (commentVisualSet.has(visualIdx)) lineEl.classList.add('has-comment');

        // Tag for drag detection and selection highlighting
        if (commentLineNum) {
          tagDiffLine(lineEl, file.path, commentLineNum, lineSide);
          if (activeFilePath === file.path) {
            const inCurrentDrag = diffDragState && unifiedVisualStart !== null && unifiedVisualEnd !== null &&
                visualIdx >= unifiedVisualStart && visualIdx <= unifiedVisualEnd;
            const formSide = activeForms.length > 0 ? (activeForms[activeForms.length - 1].side || '') : '';
            const inCurrentForm = !diffDragState && selectionStart !== null && selectionEnd !== null &&
                lineSide === formSide && commentLineNum >= selectionStart && commentLineNum <= selectionEnd;
            const inCurrentSelUnified = inCurrentDrag || inCurrentForm;
            const hasFormUnified = getFormsForFile(file.path).some(function(f) {
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
        const wdInfo = wordDiffMap.get(li);
        contentEl.innerHTML = wdInfo ? applyWordDiffToHtml(hlLine, wdInfo.ranges, wdInfo.cssClass) : hlLine;

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
      container.innerHTML = '<div class="diff-no-changes">No changes</div>';
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
          const ctxComments = [
            ...(commentsMap[line.OldNum + ':old'] || []),
            ...(commentsMap[line.NewNum + ':'] || [])
          ];
          for (let ci = 0; ci < ctxComments.length; ci++) {
            const el = ctxComments[ci].resolved
              ? createResolvedElement(ctxComments[ci], file.path)
              : createCommentElement(ctxComments[ci], file.path);
            el.classList.add('diff-comment-right');
            container.appendChild(el);
          }
          appendDiffForm(container, file.path, line.OldNum, 'old');
          appendDiffForm(container, file.path, line.NewNum, '');
        } else {
          // Pair del/add lines by similarity for word diff (not positionally)
          const delTexts = [];
          for (let dt = 0; dt < seg.dels.length; dt++) delTexts.push(seg.dels[dt].Content);
          const addTexts = [];
          for (let at = 0; at < seg.adds.length; at++) addTexts.push(seg.adds[at].Content);
          const pairs = bestWordDiffPairing(delTexts, addTexts);

          // Build reverse mapping: addIdx → delIdx
          const addToDel = {};
          const pairedDels = {};
          for (let p = 0; p < pairs.length; p++) {
            addToDel[pairs[p][1]] = pairs[p][0];
            pairedDels[pairs[p][0]] = true;
          }

          // Build rows: unpaired dels first, then adds in order (paired adds bring their del)
          const splitRows = [];
          for (let d = 0; d < seg.dels.length; d++) {
            if (!pairedDels[d]) splitRows.push({ del: seg.dels[d], add: null, wd: null });
          }
          for (let a = 0; a < seg.adds.length; a++) {
            if (addToDel[a] !== undefined) {
              const pd = seg.dels[addToDel[a]];
              splitRows.push({ del: pd, add: seg.adds[a], wd: wordDiff(pd.Content, seg.adds[a].Content) });
            } else {
              splitRows.push({ del: null, add: seg.adds[a], wd: null });
            }
          }

          for (let j = 0; j < splitRows.length; j++) {
            const sr = splitRows[j];
            const del = sr.del;
            const add = sr.add;
            const wd = sr.wd;
            const row = makeSplitRow(
              del ? { num: del.OldNum, content: del.Content, type: 'del', wordRanges: wd ? wd.oldRanges : null } : null,
              add ? { num: add.NewNum, content: add.Content, type: 'add', wordRanges: wd ? wd.newRanges : null } : null,
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

    let leftCommentGutter;
    if (left && left.num) {
      leftCommentGutter = makeDiffCommentGutter(file.path, left.num, 'old');
      tagDiffLine(leftEl, file.path, left.num, 'old', row);
      if (commentRangeSet.has(left.num + ':old')) leftEl.classList.add('has-comment');
      const selSide = diffDragState ? diffDragState.side : (activeForms.length > 0 ? activeForms[activeForms.length - 1].side : null);
      const inCurrentSelLeft = activeFilePath === file.path && selectionStart !== null && selectionEnd !== null &&
          left.num >= selectionStart && left.num <= selectionEnd && selSide === 'old';
      const hasFormLeft = getFormsForFile(file.path).some(function(f) {
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
      let hlHtml = highlightDiffLine(left.content, left.num, 'old', file.highlightCache, file.lang);
      leftContent.innerHTML = left.wordRanges ? applyWordDiffToHtml(hlHtml, left.wordRanges, 'diff-word-del') : hlHtml;
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

    let rightCommentGutter;
    if (right && right.num) {
      if (right.type === 'add' || right.type === 'context') {
        rightCommentGutter = makeDiffCommentGutter(file.path, right.num, '');
      } else {
        rightCommentGutter = makeDiffCommentGutter(file.path, 0, '');
      }
      tagDiffLine(rightEl, file.path, right.num, '', row);
      if (commentRangeSet.has(right.num + ':')) rightEl.classList.add('has-comment');
      const selSideR = diffDragState ? diffDragState.side : (activeForms.length > 0 ? activeForms[activeForms.length - 1].side : null);
      const inCurrentSelRight = activeFilePath === file.path && selectionStart !== null && selectionEnd !== null &&
          right.num >= selectionStart && right.num <= selectionEnd && (selSideR || '') === '';
      const hasFormRight = getFormsForFile(file.path).some(function(f) {
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
      const hlHtml = highlightDiffLine(right.content, right.num, right.type === 'del' ? 'old' : '', file.highlightCache, file.lang);
      rightContent.innerHTML = right.wordRanges ? applyWordDiffToHtml(hlHtml, right.wordRanges, 'diff-word-add') : hlHtml;
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

  // Single-pass builder that produces all three comment index structures:
  //   commentsMap: { end_line → [comment] }          (document view)
  //   diffCommentsMap: { "end_line:side" → [comment] } (diff view)
  //   commentedRangeSet: Set<"line:side">              (highlight ranges)
  function buildCommentIndices(comments) {
    const commentsMap = {};
    const diffCommentsMap = {};
    const rangeSet = new Set();
    for (const c of comments) {
      // commentsMap — keyed by end_line only
      const lineKey = c.end_line;
      if (!commentsMap[lineKey]) commentsMap[lineKey] = [];
      commentsMap[lineKey].push(c);
      // diffCommentsMap — keyed by "end_line:side"
      const sideKey = c.end_line + ':' + (c.side || '');
      if (!diffCommentsMap[sideKey]) diffCommentsMap[sideKey] = [];
      diffCommentsMap[sideKey].push(c);
      // commentedRangeSet — only unresolved, non-file-scope comments
      if (!c.resolved && c.scope !== 'file') {
        const side = c.side || '';
        for (let ln = c.start_line; ln <= c.end_line; ln++) rangeSet.add(ln + ':' + side);
      }
    }
    return { commentsMap: commentsMap, diffCommentsMap: diffCommentsMap, rangeSet: rangeSet };
  }

  // Convenience wrappers that maintain the existing call-site API
  function buildCommentsMap(comments) {
    return buildCommentIndices(comments).commentsMap;
  }

  function buildDiffCommentsMap(comments) {
    return buildCommentIndices(comments).diffCommentsMap;
  }

  function buildCommentedRangeSet(comments) {
    return buildCommentIndices(comments).rangeSet;
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
      if (c.scope === 'file') continue;
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
    const section = document.getElementById('file-section-' + filePath);
    if (!section) return;

    // Markdown line blocks: toggle .selected on line-block, update comment gutter drag classes
    const lineBlocks = section.querySelectorAll('.line-block[data-file-path="' + filePath + '"]');
    for (let i = 0; i < lineBlocks.length; i++) {
      const lb = lineBlocks[i];
      const startLine = parseInt(lb.dataset.startLine);
      const endLine = parseInt(lb.dataset.endLine);
      applyBlockSelectionState(lb, filePath, startLine, endLine);

      // Update the comment gutter within this line block
      const gutter = lb.querySelector('.line-comment-gutter');
      if (gutter && dragState && dragState.filePath === filePath && selectionStart !== null) {
        const isAnchorBlock = startLine <= dragState.anchorEndLine && endLine >= dragState.anchorStartLine;
        const isCurrentBlock = startLine <= dragState.currentEndLine && endLine >= dragState.currentStartLine;
        const gutterInRange = startLine >= selectionStart && endLine <= selectionEnd;
        gutter.classList.toggle('drag-endpoint', isAnchorBlock || isCurrentBlock);
        gutter.classList.toggle('drag-range', gutterInRange);
        gutter.classList.toggle('drag-range-start', gutterInRange && startLine === selectionStart);
        gutter.classList.toggle('drag-range-end', gutterInRange && endLine === selectionEnd);
      }
    }

    // Diff line elements: toggle .selected on diff lines and drag-range on gutters
    if (diffDragState && diffDragState.filePath === filePath) {
      // Unified mode: toggle .selected on .diff-line elements
      const unifiedLines = section.querySelectorAll('.diff-container.unified .diff-line[data-diff-visual-idx]');
      for (let ui = 0; ui < unifiedLines.length; ui++) {
        const uLine = unifiedLines[ui];
        const uVisualIdx = parseInt(uLine.dataset.diffVisualIdx);
        const uSelected = unifiedVisualStart !== null && unifiedVisualEnd !== null &&
                        uVisualIdx >= unifiedVisualStart && uVisualIdx <= unifiedVisualEnd;
        const uLineNum = parseInt(uLine.dataset.diffLineNum);
        const uSide = uLine.dataset.diffSide || '';
        const uHasForm = getFormsForFile(filePath).some(function(f) {
          return !f.editingId && uLineNum >= f.startLine && uLineNum <= f.endLine && (f.side || '') === uSide;
        });
        uLine.classList.toggle('selected', uSelected);
        uLine.classList.toggle('form-selected', uHasForm && !uSelected);
      }

      // Split mode: toggle .selected on .diff-split-side elements
      const splitSides = section.querySelectorAll('.diff-container.split .diff-split-side[data-diff-line-num]');
      for (let si = 0; si < splitSides.length; si++) {
        const sSide = splitSides[si];
        const sLineNum = parseInt(sSide.dataset.diffLineNum);
        const sSideVal = sSide.dataset.diffSide || '';
        const sSideMatch = diffDragState.side === sSideVal;
        const sSelected = sSideMatch && selectionStart !== null && selectionEnd !== null &&
                        sLineNum >= selectionStart && sLineNum <= selectionEnd;
        const sHasForm = getFormsForFile(filePath).some(function(f) {
          return !f.editingId && sLineNum >= f.startLine && sLineNum <= f.endLine && (f.side || '') === sSideVal;
        });
        sSide.classList.toggle('selected', sSelected);
        sSide.classList.toggle('form-selected', sHasForm && !sSelected);
      }
    }

    // Diff gutter elements: toggle drag-range classes
    const diffGutters = section.querySelectorAll('.diff-comment-gutter');
    for (let j = 0; j < diffGutters.length; j++) {
      const col = diffGutters[j];
      const btn = col.querySelector('.diff-comment-btn');
      if (!btn) continue;
      const lineNum = parseInt(btn.dataset.lineNum);
      let side = btn.dataset.side || '';
      const visualIdx = btn.dataset.visualIdx !== undefined ? parseInt(btn.dataset.visualIdx) : undefined;
      if (!lineNum) continue;

      const sideMatch = diffMode === 'split' ? (diffDragState && diffDragState.side === side) : true;
      const isActive = diffDragState && diffDragState.filePath === filePath && sideMatch && selectionStart !== null && selectionEnd !== null;

      if (isActive) {
        let isAnchor, isCurrent, dgInRange, isRangeStart, isRangeEnd;
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

    const fp = dragState.filePath;
    dragState = null;
    openForm({
      filePath: fp,
      afterBlockIndex: lastBlockIndex,
      startLine: rangeStart,
      endLine: rangeEnd,
      editingId: null,
    });
  }

  // ===== Text Selection → Line Range Mapping =====

  function getLineRangeFromSelection(selection) {
    if (!selection || selection.isCollapsed || !selection.toString().trim()) return null;

    const anchorNode = selection.anchorNode;
    const focusNode = selection.focusNode;
    if (!anchorNode || !focusNode) return null;

    // Walk up from a node to find the nearest commentable element.
    // Returns { filePath, startLine, endLine, blockIndex, side } or null.
    function findLineInfo(node) {
      const el = node.nodeType === Node.TEXT_NODE ? node.parentElement : node;
      if (!el) return null;

      // Check if inside a comment — don't trigger on existing comment text
      if (el.closest('.comment-form-wrapper') || el.closest('.comment-card')) return null;

      // Check if inside non-commentable UI (header, file tree, buttons)
      if (el.closest('.header') || el.closest('.file-tree') || el.closest('.toc-panel')) return null;

      // Try markdown line-block first
      const lineBlock = el.closest('.line-block[data-file-path]');
      if (lineBlock) {
        return {
          filePath: lineBlock.dataset.filePath,
          startLine: parseInt(lineBlock.dataset.startLine),
          endLine: parseInt(lineBlock.dataset.endLine),
          blockIndex: lineBlock.dataset.blockIndex != null ? parseInt(lineBlock.dataset.blockIndex) : null,
          side: undefined
        };
      }

      // Try diff line
      const diffLine = el.closest('[data-diff-line-num]');
      if (diffLine && parseInt(diffLine.dataset.diffLineNum) > 0) {
        return {
          filePath: diffLine.dataset.diffFilePath,
          startLine: parseInt(diffLine.dataset.diffLineNum),
          endLine: parseInt(diffLine.dataset.diffLineNum),
          blockIndex: null,
          side: diffLine.dataset.diffSide || undefined
        };
      }

      return null;
    }

    const anchorInfo = findLineInfo(anchorNode);
    const focusInfo = findLineInfo(focusNode);

    if (!anchorInfo || !focusInfo) return null;

    // Both ends must be in the same file
    if (anchorInfo.filePath !== focusInfo.filePath) return null;

    // For diff selections, both ends must be on the same side
    if (anchorInfo.side !== focusInfo.side) return null;

    // Compute union range
    const startLine = Math.min(anchorInfo.startLine, focusInfo.startLine);
    const endLine = Math.max(anchorInfo.endLine, focusInfo.endLine);
    const filePath = anchorInfo.filePath;
    const side = anchorInfo.side;

    // Determine afterBlockIndex: use the larger blockIndex (form appears after last block in range)
    let afterBlockIndex = null;
    if (anchorInfo.blockIndex != null && focusInfo.blockIndex != null) {
      afterBlockIndex = Math.max(anchorInfo.blockIndex, focusInfo.blockIndex);
    }

    return { filePath, startLine, endLine, afterBlockIndex, side };
  }

  function openForm(newForm) {
    const fk = formKey(newForm);
    const existing = activeForms.find(function(f) { return f.formKey === fk; });
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

  function openFileCommentForm(filePath) {
    const newForm = {
      filePath: filePath,
      scope: 'file',
      startLine: 0,
      endLine: 0,
      afterBlockIndex: null
    };
    const fk = formKey(newForm);
    const existing = activeForms.find(function(f) { return f.formKey === fk; });
    if (existing) {
      renderFileByPath(filePath);
      focusCommentTextarea(existing.formKey);
      return;
    }
    addForm(newForm);
    renderFileByPath(filePath);
    focusCommentTextarea(newForm.formKey);
  }

  function createFileCommentForm(formObj) {
    let initialBody = '';
    if (formObj.editingId) {
      const file = getFileByPath(formObj.filePath);
      if (file) {
        const existing = file.comments.find(function(c) { return c.id === formObj.editingId; });
        if (existing) initialBody = existing.body;
      }
    } else if (formObj.draftBody) {
      initialBody = formObj.draftBody;
    }
    return createCommentFormUI({
      formObj: formObj,
      headerText: formObj.editingId ? 'Editing comment' : 'Comment',
      submitText: formObj.editingId ? 'Update' : 'Submit',
      initialBody: initialBody,
      autoFocus: false
    });
  }

  function focusCommentTextarea(targetFormKey) {
    requestAnimationFrame(() => {
      if (targetFormKey) {
        const ta = document.querySelector('.comment-form[data-form-key="' + targetFormKey + '"] textarea');
        if (ta) { ta.focus(); return; }
      }
      const forms = document.querySelectorAll('.comment-form textarea');
      if (forms.length > 0) forms[forms.length - 1].focus();
    });
  }

  // ===== Comment Templates =====
  function getTemplates() {
    try {
      const raw = getCookie('crit-templates');
      if (raw) {
        const parsed = JSON.parse(raw);
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
    const templates = getTemplates();
    if (templates.length === 0) {
      bar.style.display = 'none';
      return;
    }
    bar.style.display = '';
    templates.forEach(function(tmpl, i) {
      const chip = document.createElement('button');
      chip.className = 'template-chip';
      chip.title = tmpl;
      const label = document.createElement('span');
      label.className = 'template-chip-label';
      label.textContent = tmpl;
      chip.appendChild(label);
      const del = document.createElement('span');
      del.className = 'template-chip-delete';
      del.textContent = '\u00d7';
      del.title = 'Remove template';
      del.addEventListener('click', function(e) {
        e.preventDefault();
        e.stopPropagation();
        let t = getTemplates();
        t.splice(i, 1);
        saveTemplates(t);
        populateTemplateBar(bar, textarea);
      });
      chip.appendChild(del);
      chip.addEventListener('click', function(e) {
        e.preventDefault();
        const start = textarea.selectionStart;
        const end = textarea.selectionEnd;
        textarea.value = textarea.value.substring(0, start) + tmpl + textarea.value.substring(end);
        textarea.selectionStart = textarea.selectionEnd = start + tmpl.length;
        textarea.focus();
        textarea.dispatchEvent(new Event('input'));
      });
      bar.appendChild(chip);
    });
  }

  function createTemplateBar(textarea) {
    const bar = document.createElement('div');
    bar.className = 'comment-template-bar';
    populateTemplateBar(bar, textarea);
    return bar;
  }

  function attachTemplateUI(form, textarea, actions) {
    const templateBar = createTemplateBar(textarea);

    const saveTemplateBtn = document.createElement('button');
    saveTemplateBtn.className = 'btn btn-sm';
    saveTemplateBtn.textContent = '+ Template';
    saveTemplateBtn.addEventListener('click', function(e) {
      e.preventDefault();
      showSaveTemplateDialog(textarea, templateBar);
    });

    const suggestBtn = document.createElement('button');
    suggestBtn.className = 'btn btn-sm';
    suggestBtn.textContent = '\u00B1 Suggest';
    suggestBtn.title = 'Insert the selected lines as a suggestion';
    suggestBtn.addEventListener('click', function() { insertSuggestion(textarea); });

    const leftGroup = document.createElement('div');
    leftGroup.className = 'comment-form-actions-left';
    leftGroup.appendChild(suggestBtn);
    leftGroup.appendChild(saveTemplateBtn);

    actions.insertBefore(leftGroup, actions.firstChild);
    form.insertBefore(templateBar, form.querySelector('textarea'));
  }

  function showSaveTemplateDialog(textarea, templateBar) {
    const text = textarea.value.trim();
    if (!text) {
      textarea.focus();
      return;
    }
    const overlay = document.createElement('div');
    overlay.className = 'save-template-overlay active';

    const dialog = document.createElement('div');
    dialog.className = 'save-template-dialog';

    const title = document.createElement('h3');
    title.textContent = 'Save as template';
    dialog.appendChild(title);

    const desc = document.createElement('p');
    desc.textContent = 'Edit the template text, then save.';
    dialog.appendChild(desc);

    const input = document.createElement('textarea');
    input.className = 'save-template-input';
    input.value = text;
    input.rows = 3;
    dialog.appendChild(input);

    const btns = document.createElement('div');
    btns.className = 'save-template-actions';

    const cancelBtn = document.createElement('button');
    cancelBtn.className = 'btn btn-sm';
    cancelBtn.textContent = 'Cancel';
    cancelBtn.addEventListener('click', function() { overlay.remove(); textarea.focus(); });

    const saveBtn = document.createElement('button');
    saveBtn.className = 'btn btn-sm btn-primary';
    saveBtn.textContent = 'Save';
    saveBtn.addEventListener('click', function() {
      const val = input.value.trim();
      if (!val) return;
      const t = getTemplates();
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

  // ===== File Picker Autocomplete =====

  function attachFilePicker(textarea) {
    let dropdown = null;
    let activeIndex = -1;
    let triggerStart = -1;
    let navigated = false;
    let suppressInput = false;

    textarea.addEventListener('input', function() {
      if (suppressInput) { suppressInput = false; return; }
      const val = textarea.value;
      const cursor = textarea.selectionStart;

      // Find the '@' trigger: scan backwards from cursor
      let atPos = -1;
      for (let i = cursor - 1; i >= 0; i--) {
        const ch = val[i];
        if (ch === '@') {
          // '@' must be at start of line or preceded by whitespace
          if (i === 0 || /\s/.test(val[i - 1])) {
            atPos = i;
          }
          break;
        }
        if (/\s/.test(ch)) break;
      }

      if (atPos === -1 || !filePickerReady) {
        hideDropdown();
        return;
      }

      triggerStart = atPos;
      const query = val.substring(atPos + 1, cursor);

      fetch('/api/files/list?q=' + encodeURIComponent(query))
        .then(function(r) { return r.ok ? r.json() : []; })
        .then(function(matches) {
          if (matches.length === 0) {
            hideDropdown();
            return;
          }
          showDropdown(matches);
        })
        .catch(function() { hideDropdown(); });
    });

    textarea.addEventListener('keydown', function(e) {
      if (!dropdown) return;

      if (e.key === 'ArrowDown') {
        e.preventDefault();
        e.stopImmediatePropagation();
        activeIndex = Math.min(activeIndex + 1, dropdown.children.length - 1);
        navigated = true;
        highlightItem();
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        e.stopImmediatePropagation();
        activeIndex = Math.max(activeIndex - 1, 0);
        navigated = true;
        highlightItem();
      } else if ((e.key === 'Tab' || e.key === 'Enter') && navigated) {
        if (activeIndex >= 0 && activeIndex < dropdown.children.length) {
          e.preventDefault();
          e.stopImmediatePropagation();
          selectItem(dropdown.children[activeIndex].dataset.path);
        }
      } else if (e.key === 'Escape') {
        e.preventDefault();
        e.stopImmediatePropagation();
        hideDropdown();
      }
    });

    textarea.addEventListener('blur', function() {
      setTimeout(hideDropdown, 200);
    });

    function showDropdown(matches) {
      if (!dropdown) {
        dropdown = document.createElement('div');
        dropdown.className = 'file-picker-dropdown';
        document.body.appendChild(dropdown);
      }

      // Position below the @ cursor line
      const textareaRect = textarea.getBoundingClientRect();
      const textBeforeCursor = textarea.value.substring(0, textarea.selectionStart);
      const lineNumber = textBeforeCursor.split('\n').length;
      const computedStyle = window.getComputedStyle(textarea);
      const lineHeight = parseFloat(computedStyle.lineHeight) || 22.4;
      const paddingTop = parseFloat(computedStyle.paddingTop) || 10;
      const cursorY = textareaRect.top + paddingTop + (lineNumber * lineHeight) - textarea.scrollTop;
      dropdown.style.left = textareaRect.left + 'px';
      dropdown.style.width = textareaRect.width + 'px';
      dropdown.style.top = cursorY + 'px';

      dropdown.innerHTML = '';
      activeIndex = 0;
      navigated = false;

      matches.forEach(function(filePath, idx) {
        const item = document.createElement('div');
        item.className = 'file-picker-item';
        item.dataset.path = filePath;

        const lastSlash = filePath.lastIndexOf('/');
        if (lastSlash >= 0) {
          const dirSpan = document.createElement('span');
          dirSpan.className = 'file-picker-dir';
          dirSpan.textContent = filePath.substring(0, lastSlash + 1);
          item.appendChild(dirSpan);
          item.appendChild(document.createTextNode(filePath.substring(lastSlash + 1)));
        } else {
          item.textContent = filePath;
        }

        item.addEventListener('mousedown', function(e) {
          e.preventDefault();
          selectItem(filePath);
        });
        item.addEventListener('mouseenter', function() {
          activeIndex = idx;
          highlightItem();
        });
        dropdown.appendChild(item);
      });

      highlightItem();
    }

    function highlightItem() {
      if (!dropdown) return;
      const items = dropdown.children;
      for (let i = 0; i < items.length; i++) {
        items[i].classList.toggle('active', i === activeIndex);
      }
      if (activeIndex >= 0 && items[activeIndex]) {
        items[activeIndex].scrollIntoView({ block: 'nearest' });
      }
    }

    function selectItem(filePath) {
      const val = textarea.value;
      const cursor = textarea.selectionStart;
      const before = val.substring(0, triggerStart);
      const after = val.substring(cursor);
      const insertion = '@' + filePath + ' ';
      textarea.value = before + insertion + after;
      const newCursor = before.length + insertion.length;
      textarea.selectionStart = textarea.selectionEnd = newCursor;
      textarea.focus();
      hideDropdown();
      suppressInput = true;
      textarea.dispatchEvent(new Event('input', { bubbles: true }));
    }

    function hideDropdown() {
      if (dropdown) {
        dropdown.remove();
        dropdown = null;
        activeIndex = -1;
        triggerStart = -1;
      }
    }
  }

  // ===== Comment Form =====
  function createCommentFormUI(opts) {
    let formObj = opts.formObj;

    const wrapper = document.createElement('div');
    wrapper.className = 'comment-form-wrapper';

    const form = document.createElement('div');
    form.className = 'comment-form';
    form.dataset.formKey = formObj.formKey;

    const header = document.createElement('div');
    header.className = 'comment-form-header';
    header.textContent = opts.headerText;

    const textarea = document.createElement('textarea');
    textarea.placeholder = 'Leave a review comment... (Ctrl+Enter to submit, Escape to cancel)';
    textarea.dataset.formKey = formObj.formKey;
    if (opts.initialBody) textarea.value = opts.initialBody;

    attachFilePicker(textarea);

    const doSubmit = opts.onSubmit
      ? function() { opts.onSubmit(textarea.value); }
      : function() { submitComment(textarea.value, formObj); };
    const doCancel = opts.onCancel
      ? function() { opts.onCancel(); }
      : function() { cancelComment(formObj); };

    textarea.addEventListener('keydown', function(e) {
      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        e.stopPropagation();
        doSubmit();
      } else if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        doCancel();
      }
    });

    if (!opts.onSubmit) {
      textarea.addEventListener('input', function() { debouncedSaveDraft(textarea.value, formObj); });
    }

    const actions = document.createElement('div');
    actions.className = 'comment-form-actions';

    const cancelBtn = document.createElement('button');
    cancelBtn.className = 'btn btn-sm';
    cancelBtn.textContent = 'Cancel';
    cancelBtn.addEventListener('click', doCancel);

    const submitBtn = document.createElement('button');
    submitBtn.className = 'btn btn-sm btn-primary';
    submitBtn.textContent = opts.submitText;
    submitBtn.addEventListener('click', doSubmit);

    actions.appendChild(cancelBtn);
    actions.appendChild(submitBtn);

    if (agentEnabled && !opts.editingId) {
      const sendBtn = document.createElement('button');
      sendBtn.className = 'btn btn-sm btn-agent';
      sendBtn.innerHTML = '<svg viewBox="0 0 24 24" width="12" height="12" fill="currentColor" style="vertical-align: -1px"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10"/></svg> Send now';
      sendBtn.title = 'Submit comment and send to agent';
      sendBtn.addEventListener('click', async function() {
        sendBtn.disabled = true;
        submitBtn.disabled = true;
        const fp = formObj.filePath;
        const comment = await submitComment(textarea.value, formObj);
        if (comment) {
          pendingAgentRequests.add(comment.id);
          renderFileByPath(fp);
          try {
            const res = await fetch('/api/agent/request', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ comment_id: comment.id, file_path: fp }),
            });
            if (!res.ok) throw new Error('Server returned ' + res.status);
            showMiniToast('Sent to agent');
          } catch (err) {
            console.error('Error sending to agent:', err);
            showMiniToast('Failed to send to agent');
            pendingAgentRequests.delete(comment.id);
            renderFileByPath(fp);
          }
        }
      });
      actions.appendChild(sendBtn);
    }

    form.appendChild(header);
    form.appendChild(textarea);
    form.appendChild(actions);
    attachTemplateUI(form, textarea, actions);
    wrapper.appendChild(form);

    if (opts.autoFocus) {
      requestAnimationFrame(function() { textarea.focus(); });
    }

    return wrapper;
  }

  function createCommentForm(formObj) {
    const lineRef = formObj.startLine === formObj.endLine
      ? 'Line ' + formObj.startLine
      : 'Lines ' + formObj.startLine + '-' + formObj.endLine;
    let initialBody = '';
    if (formObj.editingId) {
      const file = getFileByPath(formObj.filePath);
      if (file) {
        const existing = file.comments.find(function(c) { return c.id === formObj.editingId; });
        if (existing) initialBody = existing.body;
      }
    } else if (formObj.draftBody) {
      initialBody = formObj.draftBody;
    }
    return createCommentFormUI({
      formObj: formObj,
      headerText: (formObj.editingId ? 'Editing comment on ' : 'Comment on ') + lineRef,
      submitText: formObj.editingId ? 'Update' : 'Submit',
      initialBody: initialBody,
      autoFocus: false
    });
  }

  function getOldSideLinesFromHunks(file, startLine, endLine) {
    let lines = [];
    if (!file.diffHunks) return lines;
    for (let h = 0; h < file.diffHunks.length; h++) {
      const hunkLines = file.diffHunks[h].Lines || [];
      for (let i = 0; i < hunkLines.length; i++) {
        const dl = hunkLines[i];
        if ((dl.Type === 'context' || dl.Type === 'del') && dl.OldNum >= startLine && dl.OldNum <= endLine) {
          lines.push({ num: dl.OldNum, content: dl.Content });
        }
      }
    }
    lines.sort(function(a, b) { return a.num - b.num; });
    return lines.map(function(l) { return l.content; });
  }

  function insertSuggestion(textarea) {
    let key = textarea.dataset.formKey;
    let formObj = activeForms.find(function(f) { return f.formKey === key; });
    if (!formObj) return;
    const file = getFileByPath(formObj.filePath);
    if (!file) return;
    let lines;
    if (formObj.quote) {
      lines = formObj.quote.split('\n');
    } else if (formObj.side === 'old') {
      lines = getOldSideLinesFromHunks(file, formObj.startLine, formObj.endLine);
    } else {
      lines = file.content.split('\n').slice(formObj.startLine - 1, formObj.endLine);
    }
    if (lines.length === 0) return;
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
    if (!body.trim() || !formObj) return null;
    clearDraft(formObj);
    let created;
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
        userActedThisRound = true;
      } else {
        const payload = {
          body: body.trim()
        };
        if (formObj.scope === 'file') {
          payload.scope = 'file';
        } else {
          payload.start_line = formObj.startLine;
          payload.end_line = formObj.endLine;
        }
        if (formObj.quote) payload.quote = formObj.quote;
        if (formObj.quoteOffset != null) payload.quote_offset = formObj.quoteOffset;
        if (formObj.side) payload.side = formObj.side;
        if (configAuthor) payload.author = configAuthor;
        const res = await fetch('/api/file/comments?path=' + enc(filePath), {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
        const newComment = await res.json();
        file.comments.push(newComment);
        created = newComment;
        userActedThisRound = true;
      }
    } catch (err) {
      console.error('Error saving comment:', err);
      return null;
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
    updateTreeCommentBadges();
    updateCommentCount();
    return created || null;
  }

  function cancelComment(formObj) {
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
    let key = getDraftKey(formObj);
    if (!key) return;
    try {
      localStorage.setItem(key, JSON.stringify({
        filePath: formObj.filePath,
        startLine: formObj.startLine,
        endLine: formObj.endLine,
        afterBlockIndex: formObj.afterBlockIndex,
        editingId: formObj.editingId,
        side: formObj.side || '',
        scope: formObj.scope || '',
        body: body,
        savedAt: Date.now()
      }));
    } catch (_) {}
  }

  function debouncedSaveDraft(body, formObj) {
    if (!formObj) return;
    let key = formObj.formKey;
    clearTimeout(draftTimers[key]);
    draftTimers[key] = setTimeout(function() { saveDraft(body, formObj); }, 500);
  }

  function clearDraft(formObj) {
    if (!formObj) return;
    let key = formObj.formKey;
    if (draftTimers[key]) {
      clearTimeout(draftTimers[key]);
      delete draftTimers[key];
    }
    const draftKey = getDraftKey(formObj);
    if (draftKey) {
      try { localStorage.removeItem(draftKey); } catch (_) {}
    }
  }

  window.addEventListener('beforeunload', function() {
    activeForms.forEach(function(formObj) {
      const el = document.querySelector('.comment-form[data-form-key="' + formObj.formKey + '"] textarea');
      if (el) saveDraft(el.value, formObj);
    });
  });

  function restoreDrafts() {
    let restored = false;
    const keysToProcess = [];
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (k && k.startsWith('crit-draft-')) keysToProcess.push(k);
    }
    for (let ki = 0; ki < keysToProcess.length; ki++) {
      const key = keysToProcess[ki];
      try {
        const raw = localStorage.getItem(key);
        if (!raw) continue;
        const draft = JSON.parse(raw);

        if (Date.now() - draft.savedAt > 24 * 60 * 60 * 1000) {
          localStorage.removeItem(key);
          continue;
        }

        const file = getFileByPath(draft.filePath);
        if (!file) { localStorage.removeItem(key); continue; }

        if (draft.scope !== 'file' && file.fileType === 'markdown' && file.content) {
          const totalLines = file.content.split('\n').length;
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

        const formObj = {
          filePath: file.path,
          afterBlockIndex: draft.afterBlockIndex,
          startLine: draft.startLine,
          endLine: draft.endLine,
          editingId: draft.editingId,
          side: draft.side || '',
          scope: draft.scope || '',
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
      const renderedFiles = {};
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
    const t = document.createElement('div');
    t.className = 'mini-toast';
    t.textContent = message;
    document.body.appendChild(t);
    requestAnimationFrame(function() { t.classList.add('mini-toast-visible'); });
    setTimeout(function() {
      t.classList.remove('mini-toast-visible');
      setTimeout(function() { t.remove(); }, 300);
    }, 3000);
  }

  // ===== Agent Button =====
  function isLiveThread(comment) {
    if (!agentEnabled || !comment.replies) return false;
    return comment.replies.some(function(r) { return r.author === agentName; });
  }

  function checkAgentReplies(comments) {
    for (const c of comments) {
      if (pendingAgentRequests.has(c.id) && c.replies && c.replies.length > 0) {
        const lastReply = c.replies[c.replies.length - 1];
        if (lastReply.author === agentName) {
          pendingAgentRequests.delete(c.id);
        }
      }
    }
  }

  // ===== Comment Display =====
  function buildCommentEnv(comment, filePath) {
    const env = {};
    const file = getFileByPath(filePath);
    if (file && file.content && comment.start_line && comment.end_line && !comment.side) {
      env.originalLines = comment.quote
        ? comment.quote.split('\n')
        : file.content.split('\n').slice(comment.start_line - 1, comment.end_line);
    }
    return env;
  }

  // Shared helper for building comment card skeleton (header, body, replies)
  function buildCommentCard(comment, filePath, opts) {
    // opts: { wrapperClass, cardClassExtra, collapseDefault, showLineRef, showCarriedForward, repliesExtraClass, showReplyInput }
    const wrapper = document.createElement('div');
    wrapper.className = opts.wrapperClass || 'comment-block';

    const card = document.createElement('div');
    let cardClass = 'comment-card';
    if (opts.cardClassExtra) cardClass += ' ' + opts.cardClassExtra;
    card.className = cardClass;
    card.dataset.commentId = comment.id;

    // Collapse state — live threads never auto-collapse
    const liveOrPending = isLiveThread(comment) || pendingAgentRequests.has(comment.id);
    const isCollapsed = liveOrPending ? false
      : opts.collapseDefault
        ? (commentCollapseOverrides[comment.id] !== undefined ? commentCollapseOverrides[comment.id] : true)
        : (commentCollapseOverrides[comment.id] === true);
    if (isCollapsed) card.classList.add('collapsed');

    const header = document.createElement('div');
    header.className = 'comment-header';

    const collapseBtn = document.createElement('button');
    collapseBtn.className = 'comment-collapse-btn';
    collapseBtn.title = isCollapsed ? 'Expand comment' : 'Collapse comment';
    collapseBtn.innerHTML = ICON_CHEVRON;
    collapseBtn.addEventListener('click', function(e) {
      e.stopPropagation();
      card.classList.toggle('collapsed');
      commentCollapseOverrides[comment.id] = card.classList.contains('collapsed');
      collapseBtn.title = card.classList.contains('collapsed') ? 'Expand comment' : 'Collapse comment';
    });

    const headerLeft = document.createElement('div');
    headerLeft.className = 'comment-header-left';
    headerLeft.prepend(collapseBtn);
    if (comment.author) {
      const authorBadge = document.createElement('span');
      authorBadge.className = 'comment-author-badge author-color-' + authorColorIndex(comment.author);
      authorBadge.textContent = '@' + comment.author;
      headerLeft.appendChild(authorBadge);
    }
    if (comment.review_round >= 1) {
      const roundBadge = document.createElement('span');
      const rc = comment.review_round === session.review_round ? ' round-current' : comment.review_round === session.review_round - 1 ? ' round-latest' : '';
      roundBadge.className = 'comment-round-badge' + rc;
      roundBadge.textContent = 'R' + comment.review_round;
      headerLeft.appendChild(roundBadge);
    }
    if (opts.showLineRef && comment.scope !== 'file') {
      const lineRef = document.createElement('span');
      lineRef.className = 'comment-line-ref';
      lineRef.textContent = comment.start_line === comment.end_line
        ? 'Line ' + comment.start_line
        : 'Lines ' + comment.start_line + '-' + comment.end_line;
      headerLeft.appendChild(lineRef);
    }
    const time = document.createElement('span');
    time.className = 'comment-time';
    time.textContent = formatTime(comment.created_at);
    headerLeft.appendChild(time);

    if (liveOrPending) {
      const badge = document.createElement('span');
      badge.className = 'live-thread-badge' + (pendingAgentRequests.has(comment.id) ? ' pulsing' : '');
      badge.innerHTML = '<svg viewBox="0 0 24 24" width="10" height="10" fill="currentColor" style="vertical-align: -1px"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10"/></svg> live';
      headerLeft.appendChild(badge);
    }

    const actions = document.createElement('div');
    actions.className = 'comment-actions';

    header.appendChild(headerLeft);
    header.appendChild(actions);

    const bodyEl = document.createElement('div');
    bodyEl.className = 'comment-body';
    bodyEl.innerHTML = commentMd.render(comment.body, filePath ? buildCommentEnv(comment, filePath) : undefined);

    card.appendChild(header);
    card.appendChild(bodyEl);

    // Render replies
    if (comment.replies && comment.replies.length > 0) {
      card.appendChild(renderReplyList(comment, filePath || '', opts.repliesExtraClass));
    }

    // Pending agent indicator
    if (pendingAgentRequests.has(comment.id)) {
      const pending = document.createElement('div');
      pending.className = 'agent-pending-reply';
      pending.dataset.commentId = comment.id;
      pending.innerHTML =
        '<span class="agent-pending-author">@' + agentName + '</span>' +
        '<span class="agent-pending-cursor">_</span>';
      card.appendChild(pending);
    }

    // Reply input
    if (opts.showReplyInput && filePath) {
      card.appendChild(createReplyInput(comment.id, filePath));
    }

    if (pendingAgentRequests.has(comment.id) || isLiveThread(comment)) {
      wrapper.classList.add('live-thread');
    }
    if (pendingAgentRequests.has(comment.id)) {
      wrapper.classList.add('agent-pending');
    }

    wrapper.appendChild(card);
    return { wrapper: wrapper, card: card, actions: actions };
  }

  function createCommentElement(comment, filePath) {
    if (findFormForEdit(comment.id)) {
      return createInlineEditor(comment, filePath);
    }

    const parts = buildCommentCard(comment, filePath, {
      wrapperClass: 'comment-block',
      cardClassExtra: comment.carried_forward ? 'carried-forward' : '',
      collapseDefault: false,
      showLineRef: true,
      showCarriedForward: true,
      showReplyInput: true,
    });

    const editBtn = document.createElement('button');
    editBtn.title = 'Edit';
    editBtn.innerHTML = ICON_EDIT;
    editBtn.addEventListener('click', () => editComment(comment, filePath));

    const deleteBtn = document.createElement('button');
    deleteBtn.className = 'delete-btn';
    deleteBtn.title = 'Delete';
    deleteBtn.innerHTML = ICON_DELETE;
    deleteBtn.addEventListener('click', () => deleteComment(comment.id, filePath));

    const resolveBtn = document.createElement('button');
    resolveBtn.className = 'resolve-btn';
    resolveBtn.title = 'Resolve';
    resolveBtn.setAttribute('aria-label', 'Resolve thread');
    resolveBtn.innerHTML = ICON_RESOLVE + '<span>Resolve</span>';
    resolveBtn.addEventListener('click', function() {
      toggleResolveStatus(comment.id, 'file', 'resolve', filePath);
    });

    parts.actions.appendChild(resolveBtn);
    parts.actions.appendChild(editBtn);
    parts.actions.appendChild(deleteBtn);

    return parts.wrapper;
  }

  // Build a reply list container for a comment's replies
  function renderReplyList(comment, filePath, extraClass) {
    const repliesContainer = document.createElement('div');
    repliesContainer.className = 'comment-replies' + (extraClass ? ' ' + extraClass : '');
    comment.replies.forEach(function(reply) {
      const replyEl = document.createElement('div');
      replyEl.className = 'comment-reply';
      replyEl.dataset.replyId = reply.id;

      const replyHeader = document.createElement('div');
      replyHeader.className = 'reply-header';

      const replyMeta = document.createElement('div');
      replyMeta.className = 'reply-meta';
      if (reply.author) {
        const replyAuthorBadge = document.createElement('span');
        replyAuthorBadge.className = 'comment-author-badge author-color-' + authorColorIndex(reply.author);
        replyAuthorBadge.textContent = '@' + reply.author;
        replyMeta.appendChild(replyAuthorBadge);
      }
      const replyTime = document.createElement('span');
      replyTime.className = 'reply-time';
      replyTime.textContent = formatTime(reply.created_at);
      replyMeta.appendChild(replyTime);
      replyHeader.appendChild(replyMeta);

      const replyActions = document.createElement('div');
      replyActions.className = 'reply-actions';
      const replyEditBtn = document.createElement('button');
      replyEditBtn.title = 'Edit';
      replyEditBtn.innerHTML = ICON_EDIT;
      replyEditBtn.addEventListener('click', function(e) { e.stopPropagation(); editReply(comment.id, reply.id, filePath); });
      const replyDeleteBtn = document.createElement('button');
      replyDeleteBtn.className = 'delete-btn';
      replyDeleteBtn.title = 'Delete';
      replyDeleteBtn.innerHTML = ICON_DELETE;
      replyDeleteBtn.addEventListener('click', function(e) { e.stopPropagation(); deleteReply(comment.id, reply.id, filePath); });
      replyActions.appendChild(replyEditBtn);
      replyActions.appendChild(replyDeleteBtn);
      replyHeader.appendChild(replyActions);

      replyEl.appendChild(replyHeader);

      const replyBody = document.createElement('div');
      replyBody.className = 'reply-body';
      replyBody.dataset.rawBody = reply.body;
      replyBody.innerHTML = commentMd.render(reply.body);
      replyEl.appendChild(replyBody);

      repliesContainer.appendChild(replyEl);
    });
    return repliesContainer;
  }

  // ===== Quote Highlighting in Document/Diff Body =====

  function highlightQuotesInSection(sectionEl, file) {
    const quotedComments = file.comments.filter(function(c) { return c.quote && !c.resolved; });

    // Also highlight quotes from open (unsaved) comment forms
    const formQuotes = getFormsForFile(file.path)
      .filter(function(f) { return f.quote && !f.editingId; })
      .map(function(f) {
        return { start_line: f.startLine, end_line: f.endLine, quote: f.quote, quote_offset: f.quoteOffset, id: 'draft-' + f.formKey, side: f.side };
      });
    const allQuoted = quotedComments.concat(formQuotes);
    if (allQuoted.length === 0) return;

    allQuoted.forEach(function(comment) {
      // Find the content elements in this comment's line range
      const contentEls = [];
      for (let ln = comment.start_line; ln <= comment.end_line; ln++) {
        // Document view: line-blocks with data-file-path
        sectionEl.querySelectorAll('.line-block[data-file-path="' + CSS.escape(file.path) + '"]').forEach(function(el) {
          const s = parseInt(el.dataset.startLine);
          const e = parseInt(el.dataset.endLine);
          if (s <= ln && e >= ln) {
            // Get the content div (skip gutter)
            let content = el.querySelector('.line-content');
            if (content && contentEls.indexOf(content) === -1) contentEls.push(content);
          }
        });
        // Diff view: diff lines with data-diff-line-num
        // Filter by side to avoid matching the wrong line in unified diff
        // (deleted and added lines can share the same line number)
        const commentSide = comment.side || '';
        sectionEl.querySelectorAll('[data-diff-file-path="' + CSS.escape(file.path) + '"][data-diff-line-num="' + ln + '"]').forEach(function(el) {
          if (el.dataset.diffSide !== commentSide) return;
          const content = el.querySelector('.diff-content');
          if (content && contentEls.indexOf(content) === -1) contentEls.push(content);
        });
      }

      if (contentEls.length === 0) return;

      // Collect all text nodes across the content elements
      const textNodes = [];
      contentEls.forEach(function(el) {
        const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT, null);
        let node;
        while ((node = walker.nextNode())) {
          if (node.textContent.length > 0) textNodes.push(node);
        }
      });

      if (textNodes.length === 0) return;

      // Build concatenated text and find the quote within it.
      // Normalize the quote: collapse whitespace/newlines so cross-line selections match.
      const fullText = textNodes.map(function(n) { return n.textContent; }).join('');
      const normalizedQuote = comment.quote.replace(/\s+/g, ' ');
      const normalizedFull = fullText.replace(/\s+/g, ' ');
      let quoteIdx = -1;
      // Use quote_offset when available to disambiguate duplicate substrings
      if (comment.quote_offset != null) {
        const candidateIdx = comment.quote_offset;
        if (normalizedFull.slice(candidateIdx, candidateIdx + normalizedQuote.length) === normalizedQuote) {
          quoteIdx = candidateIdx;
        }
      }
      if (quoteIdx === -1) {
        quoteIdx = normalizedFull.indexOf(normalizedQuote);
      }
      if (quoteIdx === -1) {
        quoteIdx = normalizedFull.toLowerCase().indexOf(normalizedQuote.toLowerCase());
      }
      if (quoteIdx === -1) return;

      // Map the normalized index back to the original fullText position.
      // Walk the original text, skipping collapsed whitespace to find the real start.
      let origIdx = 0, normIdx = 0;
      while (normIdx < quoteIdx && origIdx < fullText.length) {
        if (/\s/.test(fullText[origIdx])) {
          // In normalized form, consecutive whitespace collapses to one space
          while (origIdx < fullText.length && /\s/.test(fullText[origIdx])) origIdx++;
          normIdx++;
        } else {
          origIdx++;
          normIdx++;
        }
      }
      quoteIdx = origIdx;
      // Find the end position similarly
      let matchLen = 0, ni = 0;
      while (ni < normalizedQuote.length && (origIdx + matchLen) < fullText.length) {
        if (/\s/.test(fullText[origIdx + matchLen])) {
          while ((origIdx + matchLen) < fullText.length && /\s/.test(fullText[origIdx + matchLen])) matchLen++;
          ni++;
        } else {
          matchLen++;
          ni++;
        }
      }

      // Walk text nodes to find which ones overlap with the quote range
      const quoteEnd = quoteIdx + matchLen;
      let pos = 0;
      for (let i = 0; i < textNodes.length; i++) {
        const node = textNodes[i];
        const nodeEnd = pos + node.textContent.length;
        if (nodeEnd <= quoteIdx) { pos = nodeEnd; continue; }
        if (pos >= quoteEnd) break;

        // This node overlaps with the quote range
        const startInNode = Math.max(0, quoteIdx - pos);
        const endInNode = Math.min(node.textContent.length, quoteEnd - pos);

        // Skip wrapping whitespace-only matches (e.g. newlines between blocks)
        const matchText = node.textContent.slice(startInNode, endInNode);
        if (!matchText.trim()) { pos = nodeEnd; continue; }

        if (startInNode === 0 && endInNode === node.textContent.length) {
          // Wrap entire text node
          const mark = document.createElement('mark');
          mark.className = 'quote-highlight';
          mark.dataset.commentId = comment.id;
          node.parentNode.replaceChild(mark, node);
          mark.appendChild(node);
        } else {
          // Split and wrap partial text
          const before = node.textContent.slice(0, startInNode);
          const middle = node.textContent.slice(startInNode, endInNode);
          const after = node.textContent.slice(endInNode);
          const frag = document.createDocumentFragment();
          if (before) frag.appendChild(document.createTextNode(before));
          const mark = document.createElement('mark');
          mark.className = 'quote-highlight';
          mark.dataset.commentId = comment.id;
          mark.textContent = middle;
          frag.appendChild(mark);
          if (after) frag.appendChild(document.createTextNode(after));
          node.parentNode.replaceChild(frag, node);
        }
        pos = nodeEnd;
      }
    });
  }

  function createInlineEditor(comment, filePath) {
    const formObj = findFormForEdit(comment.id);
    if (!formObj) return null;

    let headerText;
    if (comment.scope === 'file') {
      headerText = 'Editing file comment';
    } else {
      let lineRef = comment.start_line === comment.end_line
        ? 'Line ' + comment.start_line
        : 'Lines ' + comment.start_line + '-' + comment.end_line;
      headerText = 'Editing comment on ' + lineRef;
    }
    const formEl = createCommentFormUI({
      formObj: formObj,
      headerText: headerText,
      submitText: 'Update Comment',
      initialBody: comment.body,
      autoFocus: true
    });

    // Keep replies visible below the edit form, inside the form's card
    if (comment.replies && comment.replies.length > 0) {
      const formCard = formEl.querySelector('.comment-form');
      if (formCard) {
        formCard.appendChild(renderReplyList(comment, filePath));
      }
    }
    return formEl;
  }

  function editComment(comment, filePath) {
    const form = {
      filePath: filePath,
      afterBlockIndex: null,
      startLine: comment.start_line,
      endLine: comment.end_line,
      editingId: comment.id,
    };
    if (comment.scope === 'file') form.scope = 'file';
    openForm(form);
  }

  async function deleteComment(id, filePath) {
    const file = getFileByPath(filePath);
    if (!file) return;
    try {
      await fetch('/api/comment/' + id + '?path=' + enc(filePath), { method: 'DELETE' });
      file.comments = file.comments.filter(c => c.id !== id);
      pendingAgentRequests.delete(id);
      userActedThisRound = true;
    } catch (err) {
      console.error('Error deleting comment:', err);
    }
    if (navCommentId === id) navCommentId = null;
    renderFileByPath(filePath);
    updateTreeCommentBadges();
    updateCommentCount();
  }

  // Shared resolve/unresolve handler for both file-level and review-level comments.
  // `type` is 'file' or 'review'; `action` is 'resolve' or 'unresolve'.
  async function toggleResolveStatus(commentId, type, action, filePath) {
    const resolved = action === 'resolve';
    const url = type === 'file'
      ? '/api/comment/' + commentId + '/resolve?path=' + enc(filePath)
      : '/api/review-comment/' + commentId + '/resolve';
    try {
      const res = await fetch(url, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ resolved: resolved }),
      });
      if (!res.ok) throw new Error('Server returned ' + res.status);
    } catch (err) {
      console.error('Error ' + action + ':', err);
      showMiniToast('Failed to ' + action + ' comment');
      return;
    }
    userActedThisRound = true;
    if (type === 'file') {
      refreshFileComments(filePath);
    } else {
      await refreshReviewComments();
      renderCommentsPanel();
    }
  }

  // Re-fetch comments for a file from the API and re-render
  async function refreshFileComments(filePath) {
    const file = getFileByPath(filePath);
    if (!file) return;
    try {
      const res = await fetch('/api/file/comments?path=' + enc(filePath));
      if (res.ok) {
        file.comments = await res.json();
      }
    } catch (err) {
      console.error('Error refreshing comments:', err);
    }
    checkAgentReplies(file.comments);
    renderFileByPath(filePath);
    updateCommentCount();
    updateTreeCommentBadges();
  }

  // ===== Review-Level (General) Comments =====
  let reviewCommentSubmitting = false;
  async function addReviewComment(body) {
    if (!body.trim() || reviewCommentSubmitting) return;
    reviewCommentSubmitting = true;
    try {
      const res = await fetch('/api/comments', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ body: body.trim(), author: configAuthor })
      });
      if (!res.ok) throw new Error('Server returned ' + res.status);
      const newComment = await res.json();
      reviewComments.push(newComment);
      userActedThisRound = true;
    } catch (err) {
      console.error('Error adding review comment:', err);
      showMiniToast('Failed to add comment');
      reviewCommentSubmitting = false;
      return;
    }
    reviewCommentSubmitting = false;
    reviewCommentFormActive = false;
    reviewCommentEditingId = null;
    updateCommentCount();
  }

  async function updateReviewComment(id, body) {
    if (!body.trim()) return;
    try {
      const res = await fetch('/api/review-comment/' + id, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ body: body.trim() })
      });
      if (!res.ok) throw new Error('Server returned ' + res.status);
      const updated = await res.json();
      const idx = reviewComments.findIndex(function(c) { return c.id === id; });
      if (idx >= 0) reviewComments[idx] = updated;
      userActedThisRound = true;
    } catch (err) {
      console.error('Error updating review comment:', err);
      showMiniToast('Failed to update comment');
      return;
    }
    reviewCommentFormActive = false;
    reviewCommentEditingId = null;
    updateCommentCount();
  }

  async function deleteReviewComment(id) {
    try {
      const res = await fetch('/api/review-comment/' + id, { method: 'DELETE' });
      if (!res.ok) throw new Error('Server returned ' + res.status);
      reviewComments = reviewComments.filter(function(c) { return c.id !== id; });
      userActedThisRound = true;
    } catch (err) {
      console.error('Error deleting review comment:', err);
      showMiniToast('Failed to delete comment');
      return;
    }
    if (navCommentId === id) navCommentId = null;
    updateCommentCount();
  }

  function openReviewCommentForm() {
    // No-op if form is already open
    if (reviewCommentFormActive && !reviewCommentEditingId) return;
    // Open panel if closed
    const panel = document.getElementById('commentsPanel');
    if (panel.classList.contains('comments-panel-hidden')) {
      panel.classList.remove('comments-panel-hidden');
      updateTocPosition();
    }
    reviewCommentFormActive = true;
    reviewCommentEditingId = null;
    renderCommentsPanel();
    // Focus the textarea
    requestAnimationFrame(function() {
      const ta = document.querySelector('#commentsPanelBody textarea');
      if (ta) ta.focus();
    });
  }

  function openReviewCommentEditForm(comment) {
    // Open panel if closed
    const panel = document.getElementById('commentsPanel');
    if (panel.classList.contains('comments-panel-hidden')) {
      panel.classList.remove('comments-panel-hidden');
      updateTocPosition();
    }
    reviewCommentFormActive = true;
    reviewCommentEditingId = comment.id;
    renderCommentsPanel();
    requestAnimationFrame(function() {
      const ta = document.querySelector('#commentsPanelBody textarea');
      if (ta) ta.focus();
    });
  }

  function cancelReviewCommentForm() {
    reviewCommentFormActive = false;
    reviewCommentEditingId = null;
    renderCommentsPanel();
  }

  function createReviewCommentFormUI() {
    const formObj = { scope: 'review', filePath: '', startLine: 0, endLine: 0, formKey: 'review:new' };
    return createCommentFormUI({
      formObj: formObj,
      headerText: 'Comment',
      submitText: 'Submit',
      initialBody: '',
      autoFocus: false,
      onSubmit: function(body) { addReviewComment(body); },
      onCancel: function() { cancelReviewCommentForm(); },
    });
  }

  function createReviewCommentEditor(comment) {
    const formObj = { scope: 'review', filePath: '', startLine: 0, endLine: 0, editingId: comment.id, formKey: 'review:' + comment.id };
    const el = createCommentFormUI({
      formObj: formObj,
      headerText: 'Editing comment',
      submitText: 'Save',
      initialBody: comment.body,
      autoFocus: true,
      onSubmit: function(body) { updateReviewComment(comment.id, body); },
      onCancel: function() { cancelReviewCommentForm(); },
    });
    el.classList.add('panel-comment-block');
    return el;
  }

  async function refreshReviewComments() {
    try {
      const res = await fetch('/api/comments');
      if (res.ok) {
        reviewComments = await res.json();
      }
    } catch (err) {
      console.error('Error refreshing review comments:', err);
    }
    updateCommentCount();
  }

  async function editReply(commentId, replyId, filePath) {
    const replyEl = document.querySelector('[data-reply-id="' + replyId + '"]');
    if (!replyEl) return;
    const bodyEl = replyEl.querySelector('.reply-body');
    if (!bodyEl) return;
    // Use raw markdown if available, fall back to textContent
    const currentText = bodyEl.dataset.rawBody || bodyEl.textContent;

    const textarea = document.createElement('textarea');
    textarea.className = 'comment-textarea';
    textarea.value = currentText;
    textarea.rows = 3;
    bodyEl.replaceWith(textarea);
    textarea.focus();

    const saveBtn = document.createElement('button');
    saveBtn.className = 'btn btn-sm btn-primary';
    saveBtn.textContent = 'Save';
    const cancelBtn = document.createElement('button');
    cancelBtn.className = 'btn btn-sm';
    cancelBtn.textContent = 'Cancel';

    const btnRow = document.createElement('div');
    btnRow.className = 'reply-edit-actions';
    btnRow.appendChild(saveBtn);
    btnRow.appendChild(cancelBtn);
    replyEl.appendChild(btnRow);

    cancelBtn.addEventListener('click', () => refreshFileComments(filePath));
    saveBtn.addEventListener('click', async () => {
      const newBody = textarea.value.trim();
      if (!newBody) return;
      try {
        await fetch('/api/comment/' + commentId + '/replies/' + replyId + '?path=' + enc(filePath), {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ body: newBody })
        });
      } catch (err) {
        console.error('Error editing reply:', err);
        showMiniToast('Failed to edit reply');
        return;
      }
      userActedThisRound = true;
      refreshFileComments(filePath);
    });
  }

  async function deleteReply(commentId, replyId, filePath) {
    try {
      await fetch('/api/comment/' + commentId + '/replies/' + replyId + '?path=' + enc(filePath), {
        method: 'DELETE'
      });
      userActedThisRound = true;
    } catch (err) {
      console.error('Error deleting reply:', err);
    }
    refreshFileComments(filePath);
  }

  function createReplyInput(commentId, filePath) {
    const form = document.createElement('div');
    form.className = 'reply-form';

    // Check if this comment is pending agent response
    const isPending = pendingAgentRequests.has(commentId);

    const input = document.createElement('input');
    input.type = 'text';
    input.className = 'reply-input';
    input.placeholder = isPending ? 'Waiting for @' + agentName + '\u2026' : 'Write a reply\u2026';
    if (isPending) input.disabled = true;
    form.appendChild(input);

    // Expanded state elements (hidden initially)
    const textarea = document.createElement('textarea');
    textarea.className = 'reply-textarea';
    textarea.placeholder = isPending ? 'Waiting for @' + agentName + '\u2026' : 'Write a reply\u2026';
    textarea.rows = 3;
    if (isPending) textarea.disabled = true;

    const buttons = document.createElement('div');
    buttons.className = 'reply-form-buttons';

    const cancelBtn = document.createElement('button');
    cancelBtn.className = 'btn btn-sm';
    cancelBtn.textContent = 'Cancel';

    const submitBtn = document.createElement('button');
    submitBtn.className = 'btn btn-sm btn-primary';
    submitBtn.textContent = 'Reply';

    buttons.appendChild(cancelBtn);
    buttons.appendChild(submitBtn);

    attachFilePicker(textarea);

    function expand() {
      if (form.classList.contains('expanded')) return;
      form.classList.add('expanded');
      textarea.value = input.value;
      input.replaceWith(textarea);
      form.appendChild(buttons);
      textarea.focus();
    }

    function collapse() {
      if (!form.classList.contains('expanded')) return;
      form.classList.remove('expanded');
      textarea.replaceWith(input);
      input.value = '';
      if (buttons.parentNode) buttons.remove();
    }

    input.addEventListener('focus', expand);

    cancelBtn.addEventListener('click', collapse);

    // Collapse on blur if empty (with delay to allow button clicks)
    textarea.addEventListener('blur', function() {
      setTimeout(function() {
        if (form.classList.contains('expanded') && !textarea.value.trim() && !form.contains(document.activeElement)) {
          collapse();
        }
      }, 150);
    });

    submitBtn.addEventListener('click', async function() {
      const body = textarea.value.trim();
      if (!body) return;
      submitBtn.disabled = true;
      try {
        const payload = { body: body };
        if (configAuthor) payload.author = configAuthor;
        const res = await fetch('/api/comment/' + commentId + '/replies?path=' + enc(filePath), {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        if (!res.ok) throw new Error('Server returned ' + res.status);
        userActedThisRound = true;

        // In live threads, also send the reply to the agent
        const file = getFileByPath(filePath);
        const comment = file && file.comments ? file.comments.find(function(c) { return c.id === commentId; }) : null;
        if (comment && (isLiveThread(comment) || pendingAgentRequests.has(commentId))) {
          pendingAgentRequests.add(commentId);
          fetch('/api/agent/request', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ comment_id: commentId, file_path: filePath }),
          }).catch(function(err) {
            console.error('Error sending reply to agent:', err);
            pendingAgentRequests.delete(commentId);
            showMiniToast('Failed to send to agent');
          });
        }

        refreshFileComments(filePath);
      } catch (err) {
        console.error('Failed to add reply:', err);
        showMiniToast('Failed to save reply');
        submitBtn.disabled = false;
      }
    });

    textarea.addEventListener('keydown', function(e) {
      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        e.stopPropagation();
        submitBtn.click();
      }
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        if (!textarea.value.trim()) {
          collapse();
        }
      }
    });

    return form;
  }

  function createResolvedElement(comment, filePath) {
    const parts = buildCommentCard(comment, filePath, {
      wrapperClass: 'comment-block',
      cardClassExtra: 'resolved-card',
      collapseDefault: true,
      showLineRef: true,
      showCarriedForward: false,
      showReplyInput: true,
    });

    const unresolveBtn = document.createElement('button');
    unresolveBtn.className = 'resolve-btn resolve-btn--active';
    unresolveBtn.title = 'Unresolve';
    unresolveBtn.setAttribute('aria-label', 'Unresolve thread');
    unresolveBtn.innerHTML = ICON_UNRESOLVE + '<span>Unresolve</span>';
    unresolveBtn.addEventListener('click', function() {
      toggleResolveStatus(comment.id, 'file', 'unresolve', filePath);
    });

    const deleteBtn = document.createElement('button');
    deleteBtn.className = 'delete-btn';
    deleteBtn.title = 'Delete';
    deleteBtn.innerHTML = ICON_DELETE;
    deleteBtn.addEventListener('click', function() { deleteComment(comment.id, filePath); });

    parts.actions.appendChild(unresolveBtn);
    parts.actions.appendChild(deleteBtn);

    return parts.wrapper;
  }

  // ===== Comment Count =====
  function updateCommentCount() {
    let unresolved = 0, resolved = 0;
    for (const f of files) {
      for (const c of f.comments) {
        if (c.resolved) resolved++; else unresolved++;
      }
    }
    for (const c of reviewComments) {
      if (c.resolved) resolved++; else unresolved++;
    }
    const total = unresolved + resolved;
    const navGroup = document.getElementById('commentNavGroup');
    const el = document.getElementById('commentCount');
    const numEl = document.getElementById('commentCountNumber');
    if (total === 0) {
      if (navGroup) navGroup.style.display = '';
      if (navGroup) navGroup.classList.remove('has-comments');
      el.classList.add('comment-count-resolved');
      el.title = 'Toggle comments panel';
      numEl.textContent = '';
    } else if (unresolved > 0) {
      if (navGroup) { navGroup.style.display = ''; navGroup.classList.add('has-comments'); }
      el.classList.remove('comment-count-resolved');
      el.title = unresolved + ' unresolved comment' + (unresolved === 1 ? '' : 's') + ' — toggle panel';
      numEl.textContent = unresolved;
    } else {
      if (navGroup) { navGroup.style.display = ''; navGroup.classList.add('has-comments'); }
      el.classList.add('comment-count-resolved');
      el.title = total + ' resolved comment' + (total === 1 ? '' : 's') + ' — toggle panel';
      numEl.textContent = total;
    }
    renderCommentsPanel();
    if (uiState === 'reviewing') {
      document.getElementById('finishBtn').textContent = unresolved === 0 ? 'Approve' : 'Finish Review';
    }
  }

  function updateTocPosition() {
    const toc = document.getElementById('toc');
    const commentsPanel = document.getElementById('commentsPanel');
    const prPanel = document.getElementById('prPanel');
    if (!toc) return;
    const commentsOpen = commentsPanel && !commentsPanel.classList.contains('comments-panel-hidden');
    const prOpen = prPanel && !prPanel.classList.contains('pr-panel-hidden');
    const tocBaseRight = 16;
    let panelWidth = 0;
    if (commentsOpen && commentsPanel) panelWidth = commentsPanel.offsetWidth;
    if (prOpen && prPanel) panelWidth = prPanel.offsetWidth;
    toc.style.right = panelWidth > 0 ? (panelWidth + tocBaseRight) + 'px' : '';
  }

  function toggleCommentsPanel() {
    const panel = document.getElementById('commentsPanel');
    const isHidden = panel.classList.contains('comments-panel-hidden');
    panel.classList.toggle('comments-panel-hidden');
    if (isHidden) {
      // Close PR panel when opening comments
      document.getElementById('prPanel').classList.add('pr-panel-hidden');
      renderCommentsPanel();
    }
    updateTocPosition();
  }

  function createPanelCommentCard(comment, filePath) {
    // Build a real comment card for the panel, but without reply input/buttons
    const isGeneral = !filePath;
    const isResolved = comment.resolved;

    const cardClassExtra = [
      isResolved ? 'resolved-card' : '',
      comment.carried_forward ? 'carried-forward' : '',
    ].filter(Boolean).join(' ');

    const parts = buildCommentCard(comment, filePath || '', {
      wrapperClass: 'comment-block panel-comment-block',
      cardClassExtra: cardClassExtra,
      collapseDefault: isResolved,
      showLineRef: !isGeneral,
      showCarriedForward: true,
      repliesExtraClass: 'panel-replies',
      showReplyInput: false,
    });

    if (isGeneral) {
      // General comments: resolve/unresolve, edit, and delete
      if (isResolved) {
        const unresolveBtn = document.createElement('button');
        unresolveBtn.className = 'resolve-btn resolve-btn--active';
        unresolveBtn.title = 'Unresolve';
        unresolveBtn.setAttribute('aria-label', 'Unresolve thread');
        unresolveBtn.innerHTML = ICON_UNRESOLVE + '<span>Unresolve</span>';
        unresolveBtn.addEventListener('click', function(e) {
          e.stopPropagation();
          toggleResolveStatus(comment.id, 'review', 'unresolve', null);
        });
        parts.actions.appendChild(unresolveBtn);
      } else {
        const resolveBtn = document.createElement('button');
        resolveBtn.className = 'resolve-btn';
        resolveBtn.title = 'Resolve';
        resolveBtn.setAttribute('aria-label', 'Resolve thread');
        resolveBtn.innerHTML = ICON_RESOLVE + '<span>Resolve</span>';
        resolveBtn.addEventListener('click', function(e) {
          e.stopPropagation();
          toggleResolveStatus(comment.id, 'review', 'resolve', null);
        });
        parts.actions.appendChild(resolveBtn);
      }
      const editBtn = document.createElement('button');
      editBtn.title = 'Edit';
      editBtn.innerHTML = ICON_EDIT;
      editBtn.addEventListener('click', function(e) {
        e.stopPropagation();
        openReviewCommentEditForm(comment);
      });
      const deleteBtn = document.createElement('button');
      deleteBtn.className = 'delete-btn';
      deleteBtn.title = 'Delete';
      deleteBtn.innerHTML = ICON_DELETE;
      deleteBtn.addEventListener('click', function(e) {
        e.stopPropagation();
        deleteReviewComment(comment.id);
      });
      parts.actions.appendChild(editBtn);
      parts.actions.appendChild(deleteBtn);
    }
    // File comments are clickable to scroll to inline location
    if (!isGeneral) {
      parts.wrapper.style.cursor = 'pointer';
      parts.wrapper.addEventListener('click', function(e) {
        // Don't scroll if clicking action buttons
        if (e.target.closest('.comment-actions')) return;
        scrollToComment(comment.id, filePath);
      });
    }

    return parts.wrapper;
  }

  function renderCommentsPanel() {
    const panel = document.getElementById('commentsPanel');
    if (panel.classList.contains('comments-panel-hidden')) return;

    const showResolved = document.getElementById('showResolvedToggle').checked;
    const body = document.getElementById('commentsPanelBody');
    const savedScroll = body.scrollTop;
    body.innerHTML = '';

    // Show/hide the filter bar only when resolved comments exist
    const hasResolved = files.some(function(f) { return f.comments.some(function(c) { return c.resolved; }); })
      || reviewComments.some(function(c) { return c.resolved; });
    document.getElementById('commentsPanelFilter').style.display = hasResolved ? '' : 'none';

    let hasComments = false;

    // Render general comment compose form at the top when active
    if (reviewCommentFormActive && !reviewCommentEditingId) {
      body.appendChild(createReviewCommentFormUI());
    }

    // Render review-level (general) comments first
    const visibleReviewComments = reviewComments.filter(function(c) {
      return showResolved ? true : !c.resolved;
    });
    if (visibleReviewComments.length > 0) {
      hasComments = true;
      const group = document.createElement('div');
      group.className = 'comments-panel-file-group';

      const groupName = document.createElement('div');
      groupName.className = 'comments-panel-file-name';
      groupName.textContent = 'Review';
      group.appendChild(groupName);

      for (let j = 0; j < visibleReviewComments.length; j++) {
        const comment = visibleReviewComments[j];
        // If editing this comment, show editor instead
        if (reviewCommentEditingId === comment.id) {
          group.appendChild(createReviewCommentEditor(comment));
          continue;
        }
        group.appendChild(createPanelCommentCard(comment, null));
      }
      body.appendChild(group);
    }

    for (let i = 0; i < files.length; i++) {
      const file = files[i];
      const visibleComments = file.comments.filter(function(c) {
        return showResolved ? true : !c.resolved;
      });
      if (visibleComments.length === 0) continue;
      hasComments = true;

      // Sort by start_line
      visibleComments.sort(function(a, b) { return a.start_line - b.start_line; });

      const group = document.createElement('div');
      group.className = 'comments-panel-file-group';

      // File name header (only in multi-file mode)
      if (files.length > 1) {
        const fileName = document.createElement('div');
        fileName.className = 'comments-panel-file-name';
        fileName.textContent = file.path;
        fileName.title = file.path;
        group.appendChild(fileName);
      }

      for (let j = 0; j < visibleComments.length; j++) {
        const comment = visibleComments[j];
        group.appendChild(createPanelCommentCard(comment, file.path));
      }

      body.appendChild(group);
    }

    if (!hasComments && !reviewCommentFormActive) {
      const empty = document.createElement('div');
      empty.className = 'comments-panel-empty';
      empty.textContent = showResolved ? 'No comments yet' : 'No unresolved comments';
      body.appendChild(empty);
    }
    body.scrollTop = savedScroll;
  }

  function scrollToComment(commentId, filePath) {
    // 1. Find the file section and expand if collapsed
    const section = document.getElementById('file-section-' + filePath);
    if (!section) return;
    if (!section.open) section.open = true;

    // 2. Find the inline comment card by comment ID
    const commentCard = section.querySelector('.comment-card[data-comment-id="' + CSS.escape(commentId) + '"]');
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

  // ===== PR Overview Panel =====
  function togglePRPanel() {
    const panel = document.getElementById('prPanel');
    const isHidden = panel.classList.contains('pr-panel-hidden');
    panel.classList.toggle('pr-panel-hidden');
    // Close comments panel if opening PR panel
    if (isHidden) {
      document.getElementById('commentsPanel').classList.add('comments-panel-hidden');
      renderPRPanel();
    }
    updateTocPosition();
  }

  function renderPRPanel() {
    const panel = document.getElementById('prPanel');
    if (panel.classList.contains('pr-panel-hidden')) return;
    const pr = prData;
    if (!pr) return;

    const body = document.getElementById('prPanelBody');
    body.innerHTML = '';

    // PR title row with close button
    const linkSection = document.createElement('div');
    linkSection.className = 'pr-panel-link-section';

    const prLink = document.createElement('a');
    prLink.className = 'pr-panel-pr-link';
    prLink.href = pr.pr_url;
    prLink.target = '_blank';
    prLink.rel = 'noopener noreferrer';
    prLink.innerHTML = '<svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor"><path d="M1.5 3.25a2.25 2.25 0 1 1 3 2.122v5.256a2.251 2.251 0 1 1-1.5 0V5.372A2.25 2.25 0 0 1 1.5 3.25Zm5.677-.177L9.573.677A.25.25 0 0 1 10 .854V2.5h1A2.5 2.5 0 0 1 13.5 5v5.628a2.251 2.251 0 1 1-1.5 0V5a1 1 0 0 0-1-1h-1v1.646a.25.25 0 0 1-.427.177L7.177 3.427a.25.25 0 0 1 0-.354ZM3.75 2.5a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Zm0 9.5a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Zm8.25.75a.75.75 0 1 0 1.5 0 .75.75 0 0 0-1.5 0Z"/></svg>' +
      '<span class="pr-panel-pr-title-text">' + escapeHtml(pr.pr_title || 'Pull Request') + ' <span class="pr-panel-pr-number">#' + pr.pr_number + '</span></span>';
    linkSection.appendChild(prLink);

    const closeBtn = document.createElement('button');
    closeBtn.className = 'pr-panel-close';
    closeBtn.title = 'Close';
    closeBtn.setAttribute('aria-label', 'Close PR panel');
    closeBtn.innerHTML = '&#x2715;';
    closeBtn.addEventListener('click', function() {
      document.getElementById('prPanel').classList.add('pr-panel-hidden');
      updateTocPosition();
    });
    linkSection.appendChild(closeBtn);

    body.appendChild(linkSection);

    // State badge + meta
    const metaSection = document.createElement('div');
    metaSection.className = 'pr-panel-meta';

    const stateLabel = (pr.pr_state || 'OPEN').toUpperCase();
    let stateClass = 'pr-panel-state';
    if (stateLabel === 'MERGED') stateClass += ' pr-panel-state-merged';
    else if (stateLabel === 'CLOSED') stateClass += ' pr-panel-state-closed';
    else stateClass += ' pr-panel-state-open';
    if (pr.pr_is_draft) stateClass += ' pr-panel-state-draft';

    const stateBadge = document.createElement('span');
    stateBadge.className = stateClass;
    stateBadge.textContent = pr.pr_is_draft ? 'Draft' : stateLabel.charAt(0) + stateLabel.slice(1).toLowerCase();
    metaSection.appendChild(stateBadge);

    if (pr.pr_author) {
      const authorEl = document.createElement('span');
      authorEl.className = 'pr-panel-author';
      authorEl.textContent = pr.pr_author;
      metaSection.appendChild(authorEl);
    }

    body.appendChild(metaSection);

    // Branch info
    if (pr.pr_head_ref && pr.pr_base_ref) {
      const branchInfo = document.createElement('div');
      branchInfo.className = 'pr-panel-branches';
      branchInfo.innerHTML =
        '<span class="pr-panel-branch">' + escapeHtml(pr.pr_head_ref) + '</span>' +
        '<svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor" class="pr-panel-arrow"><path d="M6.22 3.22a.75.75 0 0 1 1.06 0l4.25 4.25a.75.75 0 0 1 0 1.06l-4.25 4.25a.75.75 0 0 1-1.06-1.06L9.94 8 6.22 4.28a.75.75 0 0 1 0-1.06Z"/></svg>' +
        '<span class="pr-panel-branch">' + escapeHtml(pr.pr_base_ref) + '</span>';
      body.appendChild(branchInfo);
    }

    // Stats
    const statsSection = document.createElement('div');
    statsSection.className = 'pr-panel-stats';

    if (pr.pr_changed_files !== undefined) {
      const filesStat = document.createElement('span');
      filesStat.className = 'pr-panel-stat';
      filesStat.innerHTML = '<svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor"><path fill-rule="evenodd" d="M3.75 1.5a.25.25 0 0 0-.25.25v12.5c0 .138.112.25.25.25h8.5a.25.25 0 0 0 .25-.25V6H9.75A1.75 1.75 0 0 1 8 4.25V1.5H3.75zm5.75.56v2.19c0 .138.112.25.25.25h2.19L9.5 2.06zM2 1.75C2 .784 2.784 0 3.75 0h5.086c.464 0 .909.184 1.237.513l3.414 3.414c.329.328.513.773.513 1.237v8.086A1.75 1.75 0 0 1 12.25 15h-8.5A1.75 1.75 0 0 1 2 13.25V1.75z"/></svg>' +
        pr.pr_changed_files + ' file' + (pr.pr_changed_files !== 1 ? 's' : '');
      statsSection.appendChild(filesStat);
    }

    if (pr.pr_additions !== undefined || pr.pr_deletions !== undefined) {
      const diffStat = document.createElement('span');
      diffStat.className = 'pr-panel-stat';
      diffStat.innerHTML =
        '<span class="pr-panel-additions">+' + (pr.pr_additions || 0) + '</span>' +
        '<span class="pr-panel-deletions">-' + (pr.pr_deletions || 0) + '</span>';
      statsSection.appendChild(diffStat);
    }

    body.appendChild(statsSection);

    // Description (PR body)
    if (pr.pr_body && pr.pr_body.trim()) {
      const descSection = document.createElement('div');
      descSection.className = 'pr-panel-description';

      const descTitle = document.createElement('div');
      descTitle.className = 'pr-panel-section-title';
      descTitle.textContent = 'Description';
      descSection.appendChild(descTitle);

      const descBody = document.createElement('div');
      descBody.className = 'pr-panel-description-body';
      descBody.innerHTML = commentMd.render(pr.pr_body);
      descSection.appendChild(descBody);

      body.appendChild(descSection);
    }
  }

  function updateViewedCount() {
    let viewed = 0;
    for (let i = 0; i < files.length; i++) {
      if (files[i].viewed) viewed++;
    }
    const el = document.getElementById('viewedCount');
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
    if (state === 'reviewing') waitingHasComments = false;
    const finishBtn = document.getElementById('finishBtn');
    const waitingOverlay = document.getElementById('waitingOverlay');

    switch (state) {
      case 'reviewing':
        let unresolvedComments = 0;
        for (let fi = 0; fi < files.length; fi++) {
          if (files[fi].comments) unresolvedComments += files[fi].comments.filter(function(c) { return !c.resolved; }).length;
        }
        unresolvedComments += reviewComments.filter(function(c) { return !c.resolved; }).length;
        finishBtn.textContent = unresolvedComments === 0 ? 'Approve' : 'Finish Review';
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

  // ===== General Comment Button (in panel header) =====
  document.getElementById('panelAddCommentBtn').addEventListener('click', openReviewCommentForm);

  // ===== Finish Review =====
  async function doFinishReview() {
    try {
      const resp = await fetch('/api/finish', { method: 'POST' });
      const data = await resp.json();
      const hasComments = !!data.prompt;
      waitingHasComments = hasComments;
      const prompt = data.prompt || 'I reviewed the changes, no feedback, good to go!';

      document.getElementById('waitingPrompt').textContent = prompt;

      if (hasComments) {
        document.getElementById('waitingMessage').innerHTML =
          'Your agent has been notified. Waiting for updates\u2026' +
          '<span class="waiting-fallback">If your agent wasn\u2019t listening, paste the prompt below.</span>';
        const clipEl = document.getElementById('waitingClipboard');
        clipEl.textContent = 'Copy prompt';
        clipEl.classList.remove('clipboard-confirm');
      } else {
        document.getElementById('waitingMessage').textContent =
          'You can close this browser tab, or leave it open for another round.';
        const clipEl = document.getElementById('waitingClipboard');
        clipEl.textContent = 'Copy prompt';
        clipEl.classList.remove('clipboard-confirm');
      }

      try { await navigator.clipboard.writeText(prompt); } catch (_) {}
    } catch (_) {}

    setUIState('waiting');
  }

  async function resolveAllAndFinish() {
    // Resolve all unresolved file comments
    for (let fi = 0; fi < files.length; fi++) {
      const fileComments = files[fi].comments || [];
      for (let ci = 0; ci < fileComments.length; ci++) {
        if (!fileComments[ci].resolved) {
          try {
            await fetch('/api/comment/' + fileComments[ci].id + '/resolve?path=' + enc(files[fi].path), {
              method: 'PUT',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ resolved: true }),
            });
          } catch (_) {}
        }
      }
    }
    // Resolve all unresolved review comments
    for (let ri = 0; ri < reviewComments.length; ri++) {
      if (!reviewComments[ri].resolved) {
        try {
          await fetch('/api/review-comment/' + reviewComments[ri].id + '/resolve', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ resolved: true }),
          });
        } catch (_) {}
      }
    }
    await doFinishReview();
  }

  function showNoChangesConfirm() {
    document.getElementById('noChangesOverlay').classList.add('active');
  }

  function hideNoChangesConfirm() {
    document.getElementById('noChangesOverlay').classList.remove('active');
  }

  document.getElementById('noChangesResolveAll').addEventListener('click', async function() {
    hideNoChangesConfirm();
    await resolveAllAndFinish();
  });

  document.getElementById('noChangesSendAnyway').addEventListener('click', async function() {
    hideNoChangesConfirm();
    await doFinishReview();
  });

  document.getElementById('noChangesGoBack').addEventListener('click', function() {
    hideNoChangesConfirm();
  });

  document.getElementById('finishBtn').addEventListener('click', async function() {
    if (uiState !== 'reviewing') return;

    // Check if user took no action but there are unresolved comments.
    // Only warn when ALL unresolved comments are carried-forward (from a previous round)
    // and the user hasn't added, edited, resolved, or replied to anything this round.
    let unresolvedCount = 0;
    let hasNewComments = false;
    for (let fi = 0; fi < files.length; fi++) {
      if (!files[fi].comments) continue;
      for (let ci = 0; ci < files[fi].comments.length; ci++) {
        const c = files[fi].comments[ci];
        if (!c.resolved) unresolvedCount++;
        if (!c.carried_forward) hasNewComments = true;
      }
    }
    for (let ri = 0; ri < reviewComments.length; ri++) {
      if (!reviewComments[ri].resolved) unresolvedCount++;
      if (!reviewComments[ri].carried_forward) hasNewComments = true;
    }

    if (!userActedThisRound && !hasNewComments && unresolvedCount > 0) {
      showNoChangesConfirm();
      return;
    }

    await doFinishReview();
  });

  document.getElementById('backToEditing').addEventListener('click', function() {
    setUIState('reviewing');
  });

  document.getElementById('waitingClipboard').addEventListener('click', async function() {
    const prompt = document.getElementById('waitingPrompt').textContent;
    try {
      await navigator.clipboard.writeText(prompt);
      const el = document.getElementById('waitingClipboard');
      el.textContent = '\u2713 Copied';
      el.setAttribute('aria-label', 'Copied');
      announceCopy();
      el.classList.remove('clipboard-confirm');
      void el.offsetWidth;
      el.classList.add('clipboard-confirm');
      setTimeout(function() {
        el.textContent = 'Copy prompt';
        el.setAttribute('aria-label', 'Copy prompt');
      }, 2000);
    } catch (_) {}
  });

  // ===== SSE Client =====

  function connectSSE() {
    const source = new EventSource('/api/events');

    source.addEventListener('file-changed', async function() {
      try {
        // Reset action tracking for new round
        userActedThisRound = false;

        // Capture per-file user state before rebuilding
        const prevState = {};
        for (let pi = 0; pi < files.length; pi++) {
          prevState[files[pi].path] = {
            viewMode: files[pi].viewMode,
            collapsed: files[pi].collapsed,
            diffLoaded: files[pi].diffLoaded,
            viewed: files[pi].viewed,
          };
        }

        // Clear commit filter on round-complete
        diffCommit = '';

        // Re-fetch everything on file-changed (round complete)
        const sessionRes = await fetch('/api/session?scope=' + enc(diffScope)).then(r => r.json());
        session = sessionRes;
        reviewComments = sessionRes.review_comments || [];

        // Reload all files
        files = await loadAllFileData(session.files || [], diffScope);

        // Restore per-file user state from previous round
        for (let fi = 0; fi < files.length; fi++) {
          const prev = prevState[files[fi].path];
          if (prev) {
            files[fi].viewMode = prev.viewMode;
            // Lazy files must stay collapsed — they have no content to render
            if (!files[fi].lazy) files[fi].collapsed = prev.collapsed;
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
        reviewCommentFormActive = false;
        reviewCommentEditingId = null;
        navCommentId = null;

        saveViewedState();
        updateHeaderRound();
        updateDiffModeToggle();
        renderFileTree();
        renderAllFiles();
        buildToc();
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
          const promptEl = document.getElementById('waitingPrompt');
          const clipEl = document.getElementById('waitingClipboard');
          if (promptEl) promptEl.style.display = 'none';
          if (clipEl) clipEl.style.display = 'none';
          if (waitingHasComments) {
            document.getElementById('waitingMessage').textContent = 'Waiting for your agent to finish...';
          }
        }
      } catch (_) {}
    });

    source.addEventListener('comments-changed', async function() {
      try {
        // Only re-fetch comments data, not file content or diffs (those only
        // change on file-changed events). This reduces O(3N) to O(N) requests.
        await Promise.all(files.map(async function(f) {
          return fetch('/api/file/comments?path=' + enc(f.path))
            .then(function(r) { return r.ok ? r.json() : []; })
            .then(function(comments) { f.comments = Array.isArray(comments) ? comments : []; })
            .catch(function() {});
        }));
        // Also refresh review-level comments
        try {
          const rcRes = await fetch('/api/comments');
          if (rcRes.ok) reviewComments = await rcRes.json();
        } catch (_) {}
        // Save form drafts and focused element before re-render
        let focusedFormKey = null;
        let focusedSelStart = 0;
        let focusedSelEnd = 0;
        const activeEl = document.activeElement;
        if (activeEl && activeEl.tagName === 'TEXTAREA') {
          const formEl = activeEl.closest('.comment-form');
          if (formEl) {
            focusedFormKey = formEl.dataset.formKey;
            focusedSelStart = activeEl.selectionStart;
            focusedSelEnd = activeEl.selectionEnd;
          }
        }
        for (let i = 0; i < files.length; i++) {
          checkAgentReplies(files[i].comments);
          saveOpenFormContent(files[i].path);
        }
        renderAllFiles();
        updateCommentCount();
        updateTreeCommentBadges();
        // Restore focus
        if (focusedFormKey) {
          const ta = document.querySelector('.comment-form[data-form-key="' + focusedFormKey + '"] textarea');
          if (ta) {
            ta.focus();
            ta.selectionStart = focusedSelStart;
            ta.selectionEnd = focusedSelEnd;
          }
        }
      } catch (err) {
        console.error('Error handling comments-changed:', err);
      }
    });

    source.addEventListener('base-changed', function() {
      reloadForScope();
      fetchCommits();
    });

    source.addEventListener('server-shutdown', function() {
      source.close();
      showDisconnected();
    });

    let sseErrorCount = 0;
    source.addEventListener('message', function() { sseErrorCount = 0; });
    source.addEventListener('file-changed', function() { sseErrorCount = 0; });
    source.addEventListener('comments-changed', function() { sseErrorCount = 0; });
    source.addEventListener('base-changed', function() { sseErrorCount = 0; });

    source.onerror = function() {
      sseErrorCount++;
      if (sseErrorCount >= 3) {
        showMiniToast('Connection lost \u2014 retrying\u2026');
      }
    };
  }

  function showDisconnected() {
    const overlay = document.createElement('div');
    overlay.className = 'disconnected-overlay';
    const box = document.createElement('div');
    box.className = 'disconnected-dialog';
    box.innerHTML = '<div class="disconnected-title">Server stopped</div><div class="disconnected-message">You can close this tab.</div>';
    overlay.appendChild(box);
    document.body.appendChild(overlay);
  }

  // ===== Share =====
  let shareModalEl = null;
  function setShareButtonState(state) {
    const btn = document.getElementById('shareBtn');
    if (state === 'shared') {
      btn.textContent = 'Shared';
      btn.classList.add('btn-success');
      btn.disabled = false;
    } else if (state === 'sharing') {
      btn.textContent = 'Sharing\u2026';
      btn.classList.remove('btn-success');
      btn.disabled = true;
    } else {
      btn.textContent = 'Share';
      btn.classList.remove('btn-success');
      btn.disabled = false;
    }
  }

  function closeShareModal() {
    if (shareModalEl) {
      shareModalEl.remove();
      shareModalEl = null;
    }
  }

  function showShareModal() {
    closeShareModal();

    const overlay = document.createElement('div');
    overlay.className = 'share-overlay';
    overlay.setAttribute('role', 'dialog');
    overlay.setAttribute('aria-modal', 'true');
    overlay.setAttribute('aria-label', 'Share review');
    overlay.innerHTML =
      '<div class="share-dialog">' +
        '<h3><svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M13.25 5.5l-5.5 5.5-3.5-3.5"/></svg>Review shared</h3>' +
        '<div class="share-dialog-qr" id="modalQR"></div>' +
        '<div class="share-dialog-url">' +
          '<span>' + escapeHtml(hostedURL) + '</span>' +
          '<button class="copy-icon-btn" id="modalCopyBtn" title="Copy link" aria-label="Copy link">' +
            ICON_CLIPBOARD +
          '</button>' +
        '</div>' +
        '<div class="share-dialog-actions">' +
          (deleteToken ? '<button class="btn btn-sm btn-danger" id="modalUnpublishBtn">Unpublish</button>' : '') +
          '<button class="btn btn-sm" id="modalCloseBtn">Close</button>' +
        '</div>' +
      '</div>';

    document.body.appendChild(overlay);
    shareModalEl = overlay;

    // Fetch QR code
    fetch('/api/qr?url=' + encodeURIComponent(hostedURL))
      .then(function(r) { return r.text(); })
      .then(function(svg) {
        const qrEl = document.getElementById('modalQR');
        if (qrEl) qrEl.innerHTML = svg;
      })
      .catch(function() {});

    // Close on overlay background click
    overlay.addEventListener('click', function(e) {
      if (e.target === overlay) closeShareModal();
    });

    // Close on Escape
    overlay.addEventListener('keydown', function(e) {
      if (e.key === 'Escape') closeShareModal();
    });

    overlay.querySelector('#modalCloseBtn').addEventListener('click', closeShareModal);

    overlay.querySelector('#modalCopyBtn').addEventListener('click', function() {
      navigator.clipboard.writeText(hostedURL).catch(function() {});
      this.innerHTML = ICON_CHECK_SMALL;
      this.setAttribute('aria-label', 'Copied');
      announceCopy();
      const copyBtn = this;
      setTimeout(function() {
        copyBtn.innerHTML = ICON_CLIPBOARD;
        copyBtn.setAttribute('aria-label', 'Copy link');
      }, 2000);
    });

    if (deleteToken) {
      overlay.querySelector('#modalUnpublishBtn').addEventListener('click', showUnpublishConfirm);
    }
  }

  function showUnpublishConfirm() {
    if (!shareModalEl) return;
    const dialog = shareModalEl.querySelector('.share-dialog');
    dialog.innerHTML =
      '<h3>Unpublish</h3>' +
      '<div class="share-dialog-confirm">' +
        '<p>Unpublish this review?</p>' +
        '<p class="confirm-detail">The shared link will stop working. Comments added by viewers will be lost.</p>' +
        '<div class="confirm-actions">' +
          '<button class="btn btn-sm btn-danger" id="confirmUnpublishBtn">Unpublish</button>' +
          '<button class="btn btn-sm" id="cancelUnpublishBtn">Cancel</button>' +
        '</div>' +
      '</div>';
    dialog.querySelector('#confirmUnpublishBtn').addEventListener('click', handleUnpublish);
    dialog.querySelector('#cancelUnpublishBtn').addEventListener('click', showShareModal);
  }

  async function handleUnpublish() {
    const btn = document.getElementById('confirmUnpublishBtn');
    if (btn) { btn.textContent = 'Unpublishing\u2026'; btn.disabled = true; }
    try {
      let resp = await fetch(shareURL + '/api/reviews', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ delete_token: deleteToken }),
      });
      const alreadyDeleted = resp.status === 404;
      if (!alreadyDeleted && !resp.ok) throw new Error('Server error ' + resp.status);
      hostedURL = '';
      deleteToken = '';
      fetch('/api/share-url', { method: 'DELETE' }).catch(function() {});
      closeShareModal();
      setShareButtonState('default');
    } catch (err) {
      closeShareModal();
      let el = showToast('share', 'error',
        '<span>Unpublish failed: ' + escapeHtml(err.message) + '</span>' +
        '<div class="toast-actions">' +
          '<button class="toast-btn toast-btn-filled" id="shareUnpublishRetryBtn">Retry</button>' +
          '<button class="toast-btn toast-btn-ghost" data-dismiss-toast="share">Dismiss</button>' +
        '</div>');
      el.querySelector('#shareUnpublishRetryBtn').addEventListener('click', function() {
        dismissToast('share');
        handleUnpublish();
      });
    }
  }

  document.getElementById('shareBtn').addEventListener('click', async function() {
    // If already shared, toggle modal
    if (hostedURL) {
      if (shareModalEl) {
        closeShareModal();
      } else {
        showShareModal();
      }
      return;
    }

    setShareButtonState('sharing');
    dismissToast('share');

    try {
      const resp = await fetch('/api/share', { method: 'POST' });
      if (!resp.ok) {
        const errBody = await resp.json().catch(function() { return {}; });
        throw new Error(errBody.error || 'Server error ' + resp.status);
      }
      const result = await resp.json();
      hostedURL = result.url;
      deleteToken = result.delete_token || '';
      setShareButtonState('shared');
      showShareModal();
    } catch (err) {
      setShareButtonState('default');
      let el = showToast('share', 'error',
        '<span>Share failed: ' + escapeHtml(err.message) + '</span>' +
        '<div class="toast-actions">' +
          '<button class="toast-btn toast-btn-filled" id="shareRetryBtn">Retry</button>' +
          '<button class="toast-btn toast-btn-ghost" data-dismiss-toast="share">Dismiss</button>' +
        '</div>');
      el.querySelector('#shareRetryBtn').addEventListener('click', function() {
        dismissToast('share');
        document.getElementById('shareBtn').click();
      });
    }
  });

  // Announce copy action to screen readers via live region
  function announceCopy() {
    const el = document.getElementById('copyStatus');
    if (el) { el.textContent = ''; el.textContent = 'Copied to clipboard'; }
  }

  // ===== Toast System =====
  function showToast(id, type, content, opts) {
    dismissToast(id);
    const container = document.getElementById('toastContainer');
    const el = document.createElement('div');
    el.className = 'toast toast-' + type;
    el.id = 'toast-' + id;
    el.innerHTML = content;
    container.appendChild(el);
    if (opts && opts.autoDismiss) {
      setTimeout(function() { dismissToast(id); }, 4000);
    }
    return el;
  }

  function dismissToast(id) {
    const el = document.getElementById('toast-' + id);
    if (!el) return;
    el.classList.add('toast-out');
    el.addEventListener('animationend', function() { el.remove(); }, { once: true });
  }

  // Event delegation for toast dismiss buttons (replaces inline onclick)
  document.getElementById('toastContainer').addEventListener('click', function(e) {
    const btn = e.target.closest('[data-dismiss-toast]');
    if (btn) dismissToast(btn.dataset.dismissToast);
  });

  // ===== Table of Contents =====
  function buildToc() {
    const tocEl = document.getElementById('toc');
    const listEl = tocEl.querySelector('.toc-list');
    const toggleBtn = document.getElementById('tocToggle');
    listEl.innerHTML = '';

    function hideToc() {
      toggleBtn.style.display = 'none';
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
        const sectionEl = document.getElementById('file-section-' + item.filePath);
        if (sectionEl) {
          const file = getFileByPath(item.filePath);
          if (file) file.collapsed = false;
          sectionEl.open = true;
        }
        // Find the line block matching this heading's start line
        const target = sectionEl && sectionEl.querySelector('.line-block[data-start-line="' + item.startLine + '"]');
        if (target) {
          const mainHeader = document.querySelector('.header');
          const offset = (mainHeader ? mainHeader.offsetHeight : 49) + 8;
          const y = target.getBoundingClientRect().top + window.scrollY - offset;
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
        const sectionEl = document.getElementById('file-section-' + item.filePath);
        const block = sectionEl && sectionEl.querySelector('.line-block[data-start-line="' + item.startLine + '"]');
        if (!block) continue;
        const rect = block.getBoundingClientRect();
        if (rect.top <= headerHeight) {
          activeItem = item;
        }
      }

      const tocLinks = document.querySelectorAll('.toc-list a');
      for (const link of tocLinks) {
        const isActive = activeItem &&
          link.dataset.startLine === String(activeItem.startLine) &&
          link.dataset.filePath === activeItem.filePath;
        link.classList.toggle('toc-active', !!isActive);
      }
    };

    window.addEventListener('scroll', tocScrollHandler, { passive: true });
    tocScrollHandler();
  }

  // ===== Mermaid =====
  function getMermaidTheme() {
    const dataTheme = document.documentElement.getAttribute('data-theme');
    if (dataTheme === 'light') return 'default';
    if (dataTheme === 'dark') return 'dark';
    // System theme: check prefers-color-scheme
    return window.matchMedia('(prefers-color-scheme: light)').matches ? 'default' : 'dark';
  }

  function renderMermaidBlocks() {
    if (typeof mermaid === 'undefined') return;
    mermaid.initialize({ startOnLoad: false, theme: getMermaidTheme() });
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

    // Re-initialize mermaid diagrams with updated theme
    if (typeof mermaid !== 'undefined') {
      mermaid.initialize({ startOnLoad: false, theme: getMermaidTheme() });
      try { mermaid.run(); } catch (_) {}
    }
  };

  // ===== Width =====
  function initWidth() {
    const saved = getCookie('crit-width') || 'default';
    applyWidth(saved);
  }

  function applyWidth(choice) {
    setCookie('crit-width', choice);
    if (choice === 'compact') document.documentElement.setAttribute('data-width', 'compact');
    else if (choice === 'wide') document.documentElement.setAttribute('data-width', 'wide');
    else document.documentElement.setAttribute('data-width', 'default');
  }

  // ===== Update Button =====
  document.getElementById('updateBtn').addEventListener('click', function() {
    openSettingsPanel('settings');
  });

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

  // ===== Commit Picker (sidebar dropdown) =====
  const commitDropdownEl = document.getElementById('commitDropdown');

  async function fetchCommits() {
    try {
      const res = await fetch('/api/commits');
      if (!res.ok) { commitDropdownEl.style.display = 'none'; return; }
      commitList = await res.json();
      if (!commitList || commitList.length < 2) {
        commitDropdownEl.style.display = 'none';
        diffCommit = '';
        return;
      }
      if (diffCommit && !commitList.some(function(c) { return c.sha === diffCommit; })) {
        diffCommit = '';
      }
      commitDropdownEl.style.display = '';
      renderCommitPicker();
    } catch (e) {
      commitDropdownEl.style.display = 'none';
    }
  }

  function renderCommitPicker() {
    const list = document.getElementById('commitDropdownList');
    const allItem = document.querySelector('.commit-picker-item[data-commit=""]');
    const label = document.getElementById('commitDropdownLabel');

    if (diffCommit) {
      if (allItem) allItem.classList.remove('active');
      const sel = commitList.find(function(c) { return c.sha === diffCommit; });
      if (sel && label) label.textContent = sel.short_sha + ' ' + (sel.message.length > 30 ? sel.message.slice(0, 30) + '\u2026' : sel.message);
    } else {
      if (allItem) allItem.classList.add('active');
      if (label) label.textContent = 'All commits';
    }

    list.innerHTML = commitList.map(function(c) {
      const active = c.sha === diffCommit ? ' active' : '';
      const time = c.date ? '<span class="commit-picker-item-time">' + relativeTime(c.date) + '</span>' : '';
      return '<div class="commit-picker-item' + active + '" data-commit="' + c.sha + '">'
        + '<span class="commit-picker-item-sha">' + escapeHtml(c.short_sha) + '</span>'
        + '<span class="commit-picker-item-msg">' + escapeHtml(c.message.length > 40 ? c.message.slice(0, 40) + '\u2026' : c.message) + '</span>'
        + time
        + '</div>';
    }).join('');
  }

  // Toggle dropdown open/close
  document.getElementById('commitDropdownBtn').addEventListener('click', function() {
    commitDropdownEl.classList.toggle('open');
  });

  // Close on outside click
  document.addEventListener('click', function(e) {
    if (!commitDropdownEl.contains(e.target)) {
      commitDropdownEl.classList.remove('open');
    }
  });

  // Close on Escape (only when open)
  document.addEventListener('keydown', function(e) {
    if (e.key === 'Escape' && commitDropdownEl.classList.contains('open')) {
      commitDropdownEl.classList.remove('open');
      e.stopImmediatePropagation();
    }
  });

  // Item selection (delegate from dropdown menu)
  document.getElementById('commitDropdownMenu').addEventListener('click', function(e) {
    const item = e.target.closest('.commit-picker-item');
    if (!item) return;
    const sha = item.dataset.commit;
    if (sha === diffCommit) {
      commitDropdownEl.classList.remove('open');
      return;
    }
    diffCommit = sha;
    renderCommitPicker();
    commitDropdownEl.classList.remove('open');
    reloadForScope();
  });

  // ===== Scope Toggle (All / Branch / Staged / Unstaged) =====
  document.getElementById('scopeToggle').addEventListener('click', async function(e) {
    const btn = e.target.closest('.toggle-btn');
    if (!btn || btn.disabled || btn.classList.contains('active')) return;
    let scope = btn.dataset.scope;
    diffScope = scope;
    navCommentId = null;
    setCookie('crit-diff-scope', scope);
    if (scope !== 'all' && scope !== 'branch') {
      diffCommit = '';
      commitDropdownEl.style.display = 'none';
    } else {
      fetchCommits();
    }
    document.querySelectorAll('#scopeToggle .toggle-btn').forEach(function(b) {
      b.classList.toggle('active', b.dataset.scope === scope);
    });
    await reloadForScope();
  });

  let reloadInFlight = null;
  async function reloadForScope() {
    if (reloadInFlight) return reloadInFlight;
    reloadInFlight = (async function() {
      try {
        document.getElementById('filesContainer').innerHTML =
          '<div class="loading" style="padding: 40px; text-align: center; color: var(--fg-muted);">Loading...</div>';

        let sessionUrl = '/api/session?scope=' + enc(diffScope);
        if (diffCommit) sessionUrl += '&commit=' + enc(diffCommit);
        const sessionRes = await fetch(sessionUrl).then(function(r) { return r.json(); });
        session = sessionRes;
        reviewComments = sessionRes.review_comments || [];

        // Update base branch label if it changed
        if (session.base_branch_name) {
          currentBaseBranch = session.base_branch_name;
          document.getElementById('baseBranchLabel').textContent = currentBaseBranch;
        }

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
      } finally {
        reloadInFlight = null;
      }
    })();
    return reloadInFlight;
  }

  // ===== Base Branch Picker =====
  const baseBranchPickerEl = document.getElementById('baseBranchPicker');
  const baseBranchBtnEl = document.getElementById('baseBranchBtn');
  let baseBranches = [];
  let currentBaseBranch = ''; // display name of the current base branch
  const branchPicker = { highlightedIdx: -1 }; // keyboard-highlighted item index

  async function fetchBranches() {
    try {
      const res = await fetch('/api/branches');
      if (!res.ok) return;
      baseBranches = await res.json();
      if (!baseBranches || baseBranches.length < 2) {
        baseBranchPickerEl.classList.remove('open');
        baseBranchPickerEl.style.display = 'none';
        document.getElementById('baseBranchArrow').style.display = 'none';
        return;
      }
      baseBranchPickerEl.style.display = '';
      renderBaseBranchList();
    } catch (e) {
      baseBranchPickerEl.classList.remove('open');
      baseBranchPickerEl.style.display = 'none';
      document.getElementById('baseBranchArrow').style.display = 'none';
    }
  }

  function getVisibleItems() {
    return Array.from(document.getElementById('baseBranchList').querySelectorAll('.base-branch-item'));
  }

  function updateHighlight() {
    const items = getVisibleItems();
    items.forEach(function(el, i) {
      el.classList.toggle('highlighted', i === branchPicker.highlightedIdx);
    });
    if (branchPicker.highlightedIdx >= 0 && branchPicker.highlightedIdx < items.length) {
      items[branchPicker.highlightedIdx].scrollIntoView({ block: 'nearest' });
    }
  }

  function renderBaseBranchList(filter) {
    const list = document.getElementById('baseBranchList');
    let filtered = baseBranches;
    if (filter) {
      const lower = filter.toLowerCase();
      filtered = baseBranches.filter(function(b) { return b.toLowerCase().indexOf(lower) !== -1; });
    }
    list.innerHTML = filtered.map(function(b) {
      const active = b === currentBaseBranch ? ' active' : '';
      return '<div class="base-branch-item' + active + '" data-branch="' + escapeHtml(b) + '">' + escapeHtml(b) + '</div>';
    }).join('');
    if (filtered.length === 0) {
      list.innerHTML = '<div style="padding: 8px 10px; font-size: 12px; color: var(--fg-muted);">No matching branches</div>';
    }
    branchPicker.highlightedIdx = -1;
  }

  async function selectBaseBranch(branch) {
    if (branch === currentBaseBranch) {
      baseBranchPickerEl.classList.remove('open');
      baseBranchBtnEl.setAttribute('aria-expanded', 'false');
      return;
    }
    baseBranchPickerEl.classList.remove('open');
    baseBranchBtnEl.setAttribute('aria-expanded', 'false');
    const previousBranch = currentBaseBranch;
    const previousLabel = document.getElementById('baseBranchLabel').textContent;
    document.getElementById('baseBranchLabel').textContent = branch;
    currentBaseBranch = branch;
    try {
      const res = await fetch('/api/base-branch', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ branch: branch }),
      });
      if (!res.ok) {
        const errText = await res.text();
        console.error('Failed to change base branch:', errText);
        currentBaseBranch = previousBranch;
        document.getElementById('baseBranchLabel').textContent = previousLabel;
        return;
      }
      // Reload immediately for responsiveness; SSE 'base-changed' will also
      // call reloadForScope() but the dedup guard collapses double-calls.
      await reloadForScope();
      fetchCommits();
    } catch (err) {
      console.error('Error changing base branch:', err);
      currentBaseBranch = previousBranch;
      document.getElementById('baseBranchLabel').textContent = previousLabel;
    }
  }

  // Toggle dropdown
  document.getElementById('baseBranchBtn').addEventListener('click', function() {
    baseBranchPickerEl.classList.toggle('open');
    const isOpen = baseBranchPickerEl.classList.contains('open');
    baseBranchBtnEl.setAttribute('aria-expanded', String(isOpen));
    if (isOpen) {
      const search = document.getElementById('baseBranchSearch');
      search.value = '';
      branchPicker.highlightedIdx = -1;
      renderBaseBranchList();
      search.focus();
    }
  });

  // Filter on typing
  document.getElementById('baseBranchSearch').addEventListener('input', function(e) {
    renderBaseBranchList(e.target.value);
  });

  // Keyboard navigation in search input
  document.getElementById('baseBranchSearch').addEventListener('keydown', function(e) {
    e.stopPropagation();
    const items = getVisibleItems();
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      branchPicker.highlightedIdx = Math.min(branchPicker.highlightedIdx + 1, items.length - 1);
      updateHighlight();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      if (branchPicker.highlightedIdx > 0) {
        branchPicker.highlightedIdx--;
        updateHighlight();
      }
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (branchPicker.highlightedIdx >= 0 && branchPicker.highlightedIdx < items.length) {
        const branch = items[branchPicker.highlightedIdx].dataset.branch;
        if (branch) selectBaseBranch(branch);
      }
    } else if (e.key === 'Escape') {
      baseBranchPickerEl.classList.remove('open');
      baseBranchBtnEl.setAttribute('aria-expanded', 'false');
    }
  });

  // Close on outside click
  document.addEventListener('click', function(e) {
    if (!baseBranchPickerEl.contains(e.target)) {
      baseBranchPickerEl.classList.remove('open');
      baseBranchBtnEl.setAttribute('aria-expanded', 'false');
    }
  });

  // Close on Escape (only when open)
  document.addEventListener('keydown', function(e) {
    if (e.key === 'Escape' && baseBranchPickerEl.classList.contains('open')) {
      baseBranchPickerEl.classList.remove('open');
      baseBranchBtnEl.setAttribute('aria-expanded', 'false');
      e.stopImmediatePropagation();
    }
  });

  // Item selection via click
  document.getElementById('baseBranchList').addEventListener('click', function(e) {
    const item = e.target.closest('.base-branch-item');
    if (!item) return;
    const branch = item.dataset.branch;
    if (branch) selectBaseBranch(branch);
  });

  // ===== TOC Toggle =====
  document.getElementById('tocToggle').addEventListener('click', function() {
    const tocEl = document.getElementById('toc');
    tocEl.classList.toggle('toc-hidden');
    setCookie('crit-toc', tocEl.classList.contains('toc-hidden') ? 'closed' : 'open');
    buildToc();
  });

  document.querySelector('.toc-close').addEventListener('click', function() {
    document.getElementById('toc').classList.add('toc-hidden');
    setCookie('crit-toc', 'closed');
  });

  // ===== Comment Navigation =====
  let navCommentId = null;
  let navHighlightTimer;

  function navigateToComment(direction) {
    const panel = document.getElementById('commentsPanel');
    const container = document.getElementById('filesContainer');
    const cards = Array.from(container.querySelectorAll('.comment-card')).filter(function(card) {
      return !panel || !panel.contains(card);
    });
    if (cards.length === 0) return;

    const header = document.querySelector('.header');
    const headerHeight = header ? header.offsetHeight : 52;

    // Find current position by stored comment ID (immune to smooth-scroll race conditions)
    let idx = -1;
    if (navCommentId) {
      for (let i = 0; i < cards.length; i++) {
        if (cards[i].dataset.commentId === navCommentId) { idx = i; break; }
      }
    }

    let targetIdx;
    if (direction === 1) {
      if (idx < 0) {
        // First use: pick first card below the header area by viewport position
        targetIdx = -1;
        for (let j = 0; j < cards.length; j++) {
          if (cards[j].getBoundingClientRect().top > headerHeight + 8) { targetIdx = j; break; }
        }
        if (targetIdx < 0) targetIdx = 0;
      } else {
        targetIdx = idx >= cards.length - 1 ? 0 : idx + 1;
      }
    } else {
      targetIdx = idx <= 0 ? cards.length - 1 : idx - 1;
    }

    const target = cards[targetIdx];
    navCommentId = target.dataset.commentId;

    if (navHighlightTimer) {
      clearTimeout(navHighlightTimer);
      document.querySelectorAll('.comment-nav-highlight').forEach(function(el) {
        el.classList.remove('comment-nav-highlight');
      });
    }

    const rect = target.getBoundingClientRect();
    const fileSection = target.closest('.file-section');
    const fileHeader = fileSection ? fileSection.querySelector('.file-header') : null;
    const fileHeaderHeight = fileHeader ? fileHeader.offsetHeight : 0;
    window.scrollTo({ top: rect.top + window.scrollY - headerHeight - fileHeaderHeight - 16, behavior: 'smooth' });
    target.classList.add('comment-nav-highlight');
    navHighlightTimer = setTimeout(function() { target.classList.remove('comment-nav-highlight'); navHighlightTimer = null; }, 1000);
  }

  document.getElementById('commentNavPrev').addEventListener('click', function() { navigateToComment(-1); });
  document.getElementById('commentNavNext').addEventListener('click', function() { navigateToComment(1); });

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

  document.getElementById('prToggle').addEventListener('click', function() {
    togglePRPanel();
  });

  document.getElementById('showResolvedToggle').addEventListener('change', function() {
    renderCommentsPanel();
  });

  // ===== Settings Panel =====
  function openSettingsPanel(tab) {
    settingsPanelTab = tab || 'settings';
    settingsPanelOpen = true;
    const overlay = document.getElementById('settingsOverlay');
    overlay.classList.add('active');
    // Ensure the sliding underline element exists
    if (!overlay.querySelector('.settings-tab-underline')) {
      const underline = document.createElement('div');
      underline.className = 'settings-tab-underline';
      overlay.querySelector('.settings-tabs').appendChild(underline);
    }
    switchSettingsTab(settingsPanelTab);
    // Fetch config if not cached
    if (!cachedConfig) {
      fetch('/api/config').then(function(r) { return r.json(); }).then(function(cfg) {
        cachedConfig = cfg;
        renderSettingsPane(cfg);
        renderAboutPane(cfg);
      });
    }
    renderShortcutsPane();
    // Trap focus inside the settings dialog
    trapFocusIn(overlay);
  }

  let focusTrapCleanup = null;

  function trapFocusIn(container) {
    releaseFocusTrap();
    function handler(e) {
      if (e.key !== 'Tab') return;
      const focusable = container.querySelectorAll('button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])');
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (e.shiftKey) {
        if (document.activeElement === first) { e.preventDefault(); last.focus(); }
      } else {
        if (document.activeElement === last) { e.preventDefault(); first.focus(); }
      }
    }
    container.addEventListener('keydown', handler);
    focusTrapCleanup = function() { container.removeEventListener('keydown', handler); };
    // Focus the first focusable element
    const firstFocusable = container.querySelector('button:not([disabled]), [href], input:not([disabled])');
    if (firstFocusable) requestAnimationFrame(function() { firstFocusable.focus(); });
  }

  function releaseFocusTrap() {
    if (focusTrapCleanup) { focusTrapCleanup(); focusTrapCleanup = null; }
  }

  function closeSettingsPanel() {
    settingsPanelOpen = false;
    releaseFocusTrap();
    document.getElementById('settingsOverlay').classList.remove('active');
  }

  function switchSettingsTab(tab) {
    settingsPanelTab = tab;
    let activeBtn = null;
    document.querySelectorAll('.settings-tab[role="tab"]').forEach(function(t) {
      const isActive = t.dataset.tab === tab;
      t.classList.toggle('active', isActive);
      t.setAttribute('aria-selected', String(isActive));
      if (isActive) activeBtn = t;
    });
    document.querySelectorAll('.settings-pane').forEach(function(p) {
      p.classList.toggle('active', p.dataset.pane === tab);
    });
    // Position the sliding underline
    const underline = document.querySelector('.settings-tab-underline');
    if (underline && activeBtn) {
      const tabsRect = activeBtn.parentElement.getBoundingClientRect();
      const btnRect = activeBtn.getBoundingClientRect();
      underline.style.left = (btnRect.left - tabsRect.left) + 'px';
      underline.style.width = btnRect.width + 'px';
    }
  }

  function updatePillIndicator(indicatorId, values, current) {
    const indicator = document.getElementById(indicatorId);
    if (!indicator) return;
    const idx = values.indexOf(current);
    if (idx >= 0) {
      indicator.style.left = (idx * (100 / values.length)) + '%';
      indicator.style.width = (100 / values.length) + '%';
    }
  }

  function renderSettingsPane(cfg) {
    const pane = document.getElementById('settingsPane');
    const currentTheme = getCookie('crit-theme') || 'system';
    const currentWidth = getCookie('crit-width') || 'default';

    let html = '';

    // Display section
    html += '<div class="settings-section-label">Display</div>';
    html += '<div class="settings-display-group">';

    // Theme row
    html += '<div class="settings-display-row">';
    html += '<span class="settings-display-label">Theme</span>';
    html += '<div class="settings-pill settings-pill--theme" id="settingsThemePill" role="group" aria-label="Theme">';
    html += '<div class="settings-pill-indicator" id="settingsThemeIndicator"></div>';
    const themeIcons = {
      system: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor"><path fill-rule="evenodd" d="M2 4.25A2.25 2.25 0 0 1 4.25 2h7.5A2.25 2.25 0 0 1 14 4.25v5.5A2.25 2.25 0 0 1 11.75 12h-1.312c.1.128.21.248.328.36a.75.75 0 0 1 .234.545v.345a.75.75 0 0 1-.75.75h-4.5a.75.75 0 0 1-.75-.75v-.345a.75.75 0 0 1 .234-.545c.118-.111.228-.232.328-.36H4.25A2.25 2.25 0 0 1 2 9.75v-5.5Zm2.25-.75a.75.75 0 0 0-.75.75v4.5c0 .414.336.75.75.75h7.5a.75.75 0 0 0 .75-.75v-4.5a.75.75 0 0 0-.75-.75h-7.5Z" clip-rule="evenodd"/></svg>',
      light: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor"><path d="M8 1a.75.75 0 0 1 .75.75v1.5a.75.75 0 0 1-1.5 0v-1.5A.75.75 0 0 1 8 1ZM10.5 8a2.5 2.5 0 1 1-5 0 2.5 2.5 0 0 1 5 0ZM12.95 4.11a.75.75 0 1 0-1.06-1.06l-1.062 1.06a.75.75 0 0 0 1.061 1.062l1.06-1.061ZM15 8a.75.75 0 0 1-.75.75h-1.5a.75.75 0 0 1 0-1.5h1.5A.75.75 0 0 1 15 8ZM11.89 12.95a.75.75 0 0 0 1.06-1.06l-1.06-1.062a.75.75 0 0 0-1.062 1.061l1.061 1.06ZM8 12a.75.75 0 0 1 .75.75v1.5a.75.75 0 0 1-1.5 0v-1.5A.75.75 0 0 1 8 12ZM5.172 11.89a.75.75 0 0 0-1.061-1.062L3.05 11.89a.75.75 0 1 0 1.06 1.06l1.06-1.06ZM4 8a.75.75 0 0 1-.75.75h-1.5a.75.75 0 0 1 0-1.5h1.5A.75.75 0 0 1 4 8ZM4.11 5.172A.75.75 0 0 0 5.173 4.11L4.11 3.05a.75.75 0 1 0-1.06 1.06l1.06 1.06Z"/></svg>',
      dark: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor"><path d="M14.438 10.148c.19-.425-.321-.787-.748-.601A5.5 5.5 0 0 1 6.453 2.31c.186-.427-.176-.938-.6-.748a6.501 6.501 0 1 0 8.585 8.586Z"/></svg>'
    };
    ['system', 'light', 'dark'].forEach(function(theme) {
      const active = theme === currentTheme ? ' active' : '';
      html += '<button class="settings-pill-btn' + active + '" data-settings-theme="' + theme + '" title="' + theme.charAt(0).toUpperCase() + theme.slice(1) + ' theme">' + themeIcons[theme] + '</button>';
    });
    html += '</div></div>';

    // Width row
    html += '<div class="settings-display-row">';
    html += '<span class="settings-display-label">Content Width <span style="font-weight:400;color:var(--fg-muted)">(file mode)</span></span>';
    html += '<div class="settings-pill settings-pill--width" id="settingsWidthPill" role="group" aria-label="Content width">';
    html += '<div class="settings-pill-indicator" id="settingsWidthIndicator"></div>';
    ['compact', 'default', 'wide'].forEach(function(w) {
      const active = w === currentWidth ? ' active' : '';
      html += '<button class="settings-pill-btn' + active + '" data-settings-width="' + w + '">' + w.charAt(0).toUpperCase() + w.slice(1) + '</button>';
    });
    html += '</div></div>';
    html += '</div>'; // close settings-display-group

    // Configuration section
    html += '<div class="settings-section-label">Configuration</div>';
    html += '<div class="config-cards">';

    // Update card (shown only when an update is available)
    if (cfg.latest_version && cfg.version && cfg.latest_version !== cfg.version && !cfg.no_update_check) {
      const upgradeCmd = 'brew update && brew upgrade crit';
      const releaseUrl = 'https://github.com/tomasz-tomczyk/crit/releases/tag/v' + escapeHtml(cfg.latest_version);
      html += '<div class="config-card config-card--orange"><div class="config-card-header">';
      html += '<span class="config-card-icon" style="color:var(--yellow)">&#11014;</span>';
      html += '<span class="config-card-title">Update available</span>';
      html += '<span class="config-card-value">v' + escapeHtml(cfg.latest_version) + '</span>';
      html += '</div>';
      html += '<div class="config-card-cmd"><span>$ ' + escapeHtml(upgradeCmd) + '</span><button class="config-card-copy" data-copy="' + escapeHtml(upgradeCmd) + '">Copy</button></div>';
      html += '<div class="config-card-body"><a class="about-link" href="' + releaseUrl + '" target="_blank" rel="noopener">Release notes</a></div>';
      html += '</div>';
    }

    // Account card (only show if sharing is enabled)
    if (cfg.share_url) {
      if (cfg.auth_logged_in) {
        const display = cfg.auth_user_email || cfg.auth_user_name || 'Logged in';
        html += '<div class="config-card config-card--green"><div class="config-card-header">';
        html += '<span class="config-card-icon" style="color:var(--green)">&#10003;</span>';
        html += '<span class="config-card-title">Account</span>';
        html += '<span class="config-card-value">' + escapeHtml(display) + '</span>';
        html += '</div></div>';
      } else {
        html += '<div class="config-card config-card--red config-card--unconfigured"><div class="config-card-header">';
        html += '<span class="config-card-icon" style="color:var(--red)">&#9675;</span>';
        html += '<span class="config-card-title">Account</span>';
        html += '</div>';
        html += '<div class="config-card-body">Not logged in. Sign in to link reviews to your account and track review history.</div>';
        html += '<div class="config-card-cmd"><span>$ crit auth login</span><button class="config-card-copy" data-copy="crit auth login">Copy</button></div>';
        html += '</div>';
      }
    }

    // Agent Command card
    if (cfg.agent_cmd_enabled) {
      html += '<div class="config-card config-card--green"><div class="config-card-header">';
      html += '<span class="config-card-icon" style="color:var(--green)">&#10003;</span>';
      html += '<span class="config-card-title">Agent Command</span>';
      html += '</div>';
      html += '<div class="config-card-cmd-value"><code>' + escapeHtml(cfg.agent_cmd || cfg.agent_name || '') + '</code></div>';
      html += '</div>';
    } else {
      html += '<div class="config-card config-card--orange config-card--unconfigured"><div class="config-card-header">';
      html += '<span class="config-card-icon" style="color:var(--yellow)">&#9675;</span>';
      html += '<span class="config-card-title">Agent Command</span>';
      html += '</div>';
      html += '<div class="config-card-body">Edit <code>~/.crit.config.json</code> and set <code>agent_cmd</code> to send comments directly to your AI agent. <a href="https://github.com/tomasz-tomczyk/crit#send-to-agent-experimental" target="_blank" rel="noopener" style="color:var(--accent)">Learn more</a></div>';
      html += '<div class="config-card-snippet">{"agent_cmd": "claude -p"}\n// Also: "opencode ask", "aider --message"</div>';
      html += '</div>';
    }

    // Integration card (hidden if no_integration_check)
    if (!cfg.no_integration_check) {
      const integrations = cfg.integrations || [];
      const anyInstalled = cfg.any_integration_installed;
      if (anyInstalled) {
        const current = integrations.filter(function(i) { return i.status === 'current'; });
        const stale = integrations.filter(function(i) { return i.status === 'stale'; });
        if (stale.length > 0) {
          const si = stale[0];
          const name = si.agent.replace(/\b\w/g, function(c) { return c.toUpperCase(); }).replace(/-/g, ' ');
          html += '<div class="config-card config-card--yellow"><div class="config-card-header">';
          html += '<span class="config-card-icon" style="color:var(--yellow)">&#9888;</span>';
          html += '<span class="config-card-title">AI Integration</span>';
          html += '<span class="config-card-value">' + escapeHtml(name) + ' (update available)</span>';
          html += '</div>';
          const hintLines = si.hint.split('\n').map(function(l) { return l.trim(); }).filter(Boolean);
          hintLines.forEach(function(line) {
            const parts = line.split('|');
            let label = '';
            let cmd = line.replace(/^Run:\s*/i, '');
            if (parts.length === 2) {
              label = parts[0];
              cmd = parts[1];
            }
            html += '<div class="config-card-cmd">';
            if (label) html += '<span class="config-card-cmd-label">' + escapeHtml(label) + '</span>';
            html += '<span>$ ' + escapeHtml(cmd) + '</span><button class="config-card-copy" data-copy="' + escapeHtml(cmd) + '">Copy</button></div>';
          });
          html += '</div>';
        } else if (current.length > 0) {
          const name = current[0].agent.replace(/\b\w/g, function(c) { return c.toUpperCase(); }).replace(/-/g, ' ');
          html += '<div class="config-card config-card--green"><div class="config-card-header">';
          html += '<span class="config-card-icon" style="color:var(--green)">&#10003;</span>';
          html += '<span class="config-card-title">AI Integration</span>';
          html += '<span class="config-card-value">' + escapeHtml(name) + ' (up to date)</span>';
          html += '</div></div>';
        }
      } else {
        const available = (cfg.integrations_available || []).join(' \u00b7 ');
        html += '<div class="config-card config-card--blue config-card--unconfigured"><div class="config-card-header">';
        html += '<span class="config-card-icon" style="color:var(--accent)">&#128161;</span>';
        html += '<span class="config-card-title">AI Integration</span>';
        html += '<span class="config-card-badge">Recommended</span>';
        html += '</div>';
        html += '<div class="config-card-body">Install a plugin so your AI agent can launch crit, read comments, and iterate.</div>';
        html += '<div class="config-card-cmd"><span>$ crit install claude-code</span><button class="config-card-copy" data-copy="crit install claude-code">Copy</button></div>';
        if (available) html += '<div class="config-card-agents">Also: ' + escapeHtml(available) + '</div>';
        html += '</div>';
      }
    }

    // Share card
    if (cfg.share_url) {
      let hostname;
      try { hostname = new URL(cfg.share_url).hostname; } catch (_) { hostname = cfg.share_url; }
      html += '<div class="config-card config-card--green"><div class="config-card-header">';
      html += '<span class="config-card-icon" style="color:var(--green)">&#10003;</span>';
      html += '<span class="config-card-title">Sharing enabled</span>';
      html += '<span class="config-card-value">' + escapeHtml(hostname) + '</span>';
      html += '</div></div>';
    } else {
      html += '<div class="config-card config-card--gray config-card--unconfigured"><div class="config-card-header">';
      html += '<span class="config-card-icon" style="color:var(--fg-muted)">&mdash;</span>';
      html += '<span class="config-card-title">Share</span>';
      html += '<span class="config-card-value">Disabled</span>';
      html += '</div></div>';
    }
    html += '</div>'; // close config-cards

    pane.innerHTML = html;

    // Wire up theme pill clicks
    pane.querySelectorAll('[data-settings-theme]').forEach(function(btn) {
      btn.addEventListener('click', function() {
        const theme = btn.dataset.settingsTheme;
        applyTheme(theme);
        pane.querySelectorAll('[data-settings-theme]').forEach(function(b) { b.classList.toggle('active', b.dataset.settingsTheme === theme); });
        updatePillIndicator('settingsThemeIndicator', ['system', 'light', 'dark'], theme);
      });
    });
    updatePillIndicator('settingsThemeIndicator', ['system', 'light', 'dark'], currentTheme);

    // Wire up width pill clicks
    pane.querySelectorAll('[data-settings-width]').forEach(function(btn) {
      btn.addEventListener('click', function() {
        const w = btn.dataset.settingsWidth;
        applyWidth(w);
        pane.querySelectorAll('[data-settings-width]').forEach(function(b) { b.classList.toggle('active', b.dataset.settingsWidth === w); });
        updatePillIndicator('settingsWidthIndicator', ['compact', 'default', 'wide'], w);
      });
    });
    updatePillIndicator('settingsWidthIndicator', ['compact', 'default', 'wide'], currentWidth);

    // Wire up copy buttons
    pane.querySelectorAll('.config-card-copy').forEach(function(btn) {
      btn.addEventListener('click', function() {
        const text = btn.dataset.copy;
        navigator.clipboard.writeText(text).then(function() {
          btn.textContent = '\u2713 Copied';
          btn.setAttribute('aria-label', 'Copied');
          announceCopy();
          btn.classList.add('copied');
          setTimeout(function() {
            btn.textContent = 'Copy';
            btn.setAttribute('aria-label', 'Copy');
            btn.classList.remove('copied');
          }, 1500);
        });
      });
    });
  }

  function renderShortcutsPane() {
    const pane = document.getElementById('shortcutsPane');
    let html = '';

    const groups = [
      { label: 'Navigation', shortcuts: [
        { key: '<kbd>j</kbd>', action: 'Next block' },
        { key: '<kbd>k</kbd>', action: 'Previous block' },
        { key: '<kbd>]</kbd>', action: 'Next comment' },
        { key: '<kbd>[</kbd>', action: 'Previous comment' },
        { key: '<kbd>n</kbd>', action: 'Next change', mode: 'file mode' },
        { key: '<kbd>N</kbd>', action: 'Previous change', mode: 'file mode' },
      ]},
      { label: 'Comments', shortcuts: [
        { key: '<kbd>c</kbd>', action: 'Comment on focused block' },
        { key: '<kbd>e</kbd>', action: 'Edit comment on focused block' },
        { key: '<kbd>d</kbd>', action: 'Delete comment on focused block' },
        { key: '<kbd>G</kbd>', action: 'General comment' },
        { key: '<kbd>Ctrl</kbd>+<kbd>Enter</kbd>', action: 'Submit comment' },
      ]},
      { label: 'Review', shortcuts: [
        { key: '<kbd>Shift</kbd>+<kbd>F</kbd>', action: 'Finish review' },
        { key: '<kbd>Shift</kbd>+<kbd>C</kbd>', action: 'Toggle comments panel' },
        { key: '<kbd>Shift</kbd>+<kbd>1</kbd>/<kbd>2</kbd>/<kbd>3</kbd>/<kbd>4</kbd>', action: 'Switch scope', mode: 'git mode' },
      ]},
      { label: 'View', shortcuts: [
        { key: '<kbd>t</kbd>', action: 'Toggle table of contents', mode: 'file mode' },
        { key: '<kbd>Esc</kbd>', action: 'Cancel / clear focus' },
        { key: '<kbd>?</kbd>', action: 'Toggle this panel' },
      ]},
    ];

    groups.forEach(function(group) {
      html += '<div class="shortcuts-group-label">' + group.label + '</div>';
      html += '<table class="shortcuts-table">';
      group.shortcuts.forEach(function(s) {
        const modeTag = s.mode ? '<span class="shortcut-mode-badge">' + s.mode + '</span>' : '';
        html += '<tr><td>' + s.key + '</td><td>' + s.action + modeTag + '</td></tr>';
      });
      html += '</table>';
    });

    pane.innerHTML = html;
  }

  function renderAboutPane(cfg) {
    const pane = document.getElementById('aboutPane');
    let html = '';

    // Version header
    html += '<div class="about-header">';
    html += '<h2>Crit</h2>';
    const ver = cfg.version || 'dev';
    html += '<div class="about-version">' + escapeHtml(ver) + '</div>';
    if (!cfg.no_update_check) {
      if (cfg.latest_version && cfg.version && cfg.latest_version !== cfg.version) {
        html += '<div class="about-badge about-badge--update">Update available: ' + escapeHtml(cfg.latest_version) + '</div>';
      } else if (cfg.version && cfg.version !== 'dev') {
        html += '<div class="about-badge about-badge--current">Up to date</div>';
      }
    }
    html += '</div>';

    // Session info
    html += '<div class="settings-section-label">Current Session</div>';
    html += '<div class="about-session"><div class="about-session-grid">';
    html += '<span class="about-session-label">Mode</span><span class="about-session-value">' + (session.mode || 'unknown') + '</span>';
    if (session.mode === 'git' && session.branch) {
      html += '<span class="about-session-label">Branch</span><span class="about-session-value">' + escapeHtml(session.branch) + '</span>';
    }
    if (session.base_ref) {
      html += '<span class="about-session-label">Base</span><span class="about-session-value">' + escapeHtml(session.base_branch_name || session.base_ref) + '</span>';
    }
    html += '<span class="about-session-label">Round</span><span class="about-session-value">' + (session.review_round || 1) + '</span>';
    html += '<span class="about-session-label">Files</span><span class="about-session-value">' + (session.files ? session.files.length : 0) + ' changed</span>';
    if (cfg.review_path) {
      html += '<span class="about-session-label">Review file</span><span class="about-session-value"><code>' + escapeHtml(cfg.review_path) + '</code></span>';
    }
    html += '</div></div>';

    // Links
    html += '<div class="settings-section-label">Links</div>';
    html += '<div class="about-links">';
    html += '<a class="about-link" href="https://crit.md" target="_blank" rel="noopener"><svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M8 1v4M5.5 3h5M3 7h10v6.5a.5.5 0 0 1-.5.5h-9a.5.5 0 0 1-.5-.5V7Z"/></svg>Homepage</a>';
    html += '<a class="about-link" href="https://github.com/tomasz-tomczyk/crit" target="_blank" rel="noopener"><svg viewBox="0 0 16 16" fill="currentColor"><path d="M8 0c4.42 0 8 3.58 8 8a8.013 8.013 0 0 1-5.45 7.59c-.4.08-.55-.17-.55-.38 0-.27.01-1.13.01-2.2 0-.75-.25-1.23-.54-1.48 1.78-.2 3.65-.88 3.65-3.95 0-.88-.31-1.59-.82-2.15.08-.2.36-1.02-.08-2.12 0 0-.67-.22-2.2.82-.64-.18-1.32-.27-2-.27-.68 0-1.36.09-2 .27-1.53-1.03-2.2-.82-2.2-.82-.44 1.1-.16 1.92-.08 2.12-.51.56-.82 1.28-.82 2.15 0 3.06 1.86 3.75 3.64 3.95-.23.2-.44.55-.51 1.07-.46.21-1.61.55-2.33-.66-.15-.24-.6-.83-1.23-.82-.67.01-.27.38.01.53.34.19.73.9.82 1.13.16.45.68 1.31 2.69.94 0 .67.01 1.3.01 1.49 0 .21-.15.45-.55.38A7.995 7.995 0 0 1 0 8c0-4.42 3.58-8 8-8Z"/></svg>GitHub</a>';
    html += '<a class="about-link" href="https://github.com/tomasz-tomczyk/crit/releases" target="_blank" rel="noopener"><svg viewBox="0 0 16 16" fill="currentColor"><path d="M1 7.775V2.75C1 1.784 1.784 1 2.75 1h5.025c.464 0 .91.184 1.238.513l6.25 6.25a1.75 1.75 0 0 1 0 2.474l-5.026 5.026a1.75 1.75 0 0 1-2.474 0l-6.25-6.25A1.752 1.752 0 0 1 1 7.775Zm1.5 0c0 .066.026.13.073.177l6.25 6.25a.25.25 0 0 0 .354 0l5.025-5.025a.25.25 0 0 0 0-.354l-6.25-6.25a.25.25 0 0 0-.177-.073H2.75a.25.25 0 0 0-.25.25ZM6 5a1 1 0 1 1 0 2 1 1 0 0 1 0-2Z"/></svg>Changelog</a>';
    html += '</div>';

    pane.innerHTML = html;
  }

  // Gear icon opens Settings tab
  document.getElementById('settingsToggle').addEventListener('click', function() {
    if (settingsPanelOpen) closeSettingsPanel();
    else openSettingsPanel('settings');
  });

  // Close button
  document.getElementById('settingsClose').addEventListener('click', closeSettingsPanel);

  // Click outside to close
  document.getElementById('settingsOverlay').addEventListener('click', function(e) {
    if (e.target === this) closeSettingsPanel();
  });

  // Tab switching
  document.querySelectorAll('.settings-tab[data-tab]').forEach(function(tab) {
    tab.addEventListener('click', function() { switchSettingsTab(tab.dataset.tab); });
  });

  // Arrow key navigation for ARIA tabs pattern
  document.querySelector('.settings-tabs[role="tablist"]').addEventListener('keydown', function(e) {
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
    const tabs = Array.from(this.querySelectorAll('.settings-tab[data-tab]'));
    const current = tabs.findIndex(function(t) { return t.getAttribute('aria-selected') === 'true'; });
    if (current === -1) return;
    let next = e.key === 'ArrowRight' ? current + 1 : current - 1;
    if (next < 0) next = tabs.length - 1;
    if (next >= tabs.length) next = 0;
    e.preventDefault();
    switchSettingsTab(tabs[next].dataset.tab);
    tabs[next].focus();
  });

  document.getElementById('noChangesOverlay').addEventListener('click', function(e) {
    if (e.target === this) hideNoChangesConfirm();
  });

  document.addEventListener('keydown', function(e) {
    const tag = document.activeElement.tagName;
    if (tag === 'TEXTAREA' || tag === 'INPUT' || document.activeElement.isContentEditable) {
      if (e.key === 'Escape' && activeForms.length > 0) {
        e.preventDefault();
        const ta = document.activeElement;
        if (ta && ta.dataset && ta.dataset.formKey) {
          const form = activeForms.find(function(f) { return f.formKey === ta.dataset.formKey; });
          if (form) cancelComment(form);
        }
      }
      return;
    }

    if (document.getElementById('noChangesOverlay').classList.contains('active')) {
      if (e.key === 'Escape') {
        e.preventDefault();
        hideNoChangesConfirm();
      }
      return;
    }

    if (settingsPanelOpen) {
      if (e.key === 'Escape') {
        e.preventDefault();
        closeSettingsPanel();
      } else if (e.key === '?') {
        e.preventDefault();
        if (settingsPanelTab === 'shortcuts') closeSettingsPanel();
        else switchSettingsTab('shortcuts');
      }
      return;
    }

    if (e.metaKey || e.ctrlKey || e.altKey) return;

    switch (e.key) {
      case 'j': case 'k': {
        e.preventDefault();
        const allNav = navElements;
        if (allNav.length === 0) return;
        let curIdx = focusedElement ? allNav.indexOf(focusedElement) : -1;
        if (curIdx === -1 && focusedElement) {
          // Stale ref after re-render — find nearest match by data attributes
          let fp = focusedElement.dataset.filePath || focusedElement.dataset.diffFilePath;
          let bi = focusedElement.dataset.blockIndex;
          const dln = focusedElement.dataset.diffLineNum;
          for (let ni = 0; ni < allNav.length; ni++) {
            const n = allNav[ni];
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
          let fp = focusedElement.dataset.filePath;
          const bi = parseInt(focusedElement.dataset.blockIndex);
          const file = getFileByPath(fp);
          if (!file || !file.lineBlocks) return;
          let block = file.lineBlocks[bi];
          openForm({ filePath: fp, afterBlockIndex: bi, startLine: block.startLine, endLine: block.endLine, editingId: null });
        }
        // Diff line
        else if (focusedElement.dataset.diffFilePath && focusedElement.dataset.diffLineNum) {
          const dfp = focusedElement.dataset.diffFilePath;
          const lineNum = parseInt(focusedElement.dataset.diffLineNum);
          const side = focusedElement.dataset.diffSide || '';
          openForm({ filePath: dfp, afterBlockIndex: null, startLine: lineNum, endLine: lineNum, editingId: null, side: side || undefined });
        }
        break;
      }
      case 'e':
      case 'd': {
        e.preventDefault();
        if (!focusedElement) return;
        const fp = focusedElement.dataset.filePath || focusedElement.dataset.diffFilePath;
        if (!fp) return;
        const file = getFileByPath(fp);
        if (!file || !file.comments || file.comments.length === 0) return;
        // Find comments for the focused line
        let comment = null;
        if (focusedElement.dataset.blockIndex != null) {
          const block = file.lineBlocks[parseInt(focusedElement.dataset.blockIndex)];
          if (block) {
            comment = file.comments.find(function(c) { return c.end_line >= block.startLine && c.end_line <= block.endLine; });
          }
        } else if (focusedElement.dataset.diffLineNum) {
          let ln = parseInt(focusedElement.dataset.diffLineNum);
          const sd = focusedElement.dataset.diffSide || '';
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
      case 'G': {
        e.preventDefault();
        openReviewCommentForm();
        break;
      }
      case 'C': {
        e.preventDefault();
        toggleCommentsPanel();
        break;
      }
      case 't': {
        const tocBtn = document.getElementById('tocToggle');
        if (tocBtn.style.display === 'none') return;
        e.preventDefault();
        tocBtn.click();
        break;
      }
      case ']': {
        e.preventDefault();
        navigateToComment(1);
        break;
      }
      case '[': {
        e.preventDefault();
        navigateToComment(-1);
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
      case '!': case '@': case '#': case '$': {
        if (session.mode !== 'git') break;
        const scopeMap = { '!': 'all', '@': 'branch', '#': 'staged', '$': 'unstaged' };
        const scope = scopeMap[e.key];
        const btn = document.querySelector('#scopeToggle .toggle-btn[data-scope="' + scope + '"]');
        if (btn && !btn.disabled && !btn.classList.contains('active')) {
          e.preventDefault();
          btn.click();
        }
        break;
      }
      case '?': {
        e.preventDefault();
        openSettingsPanel('shortcuts');
        break;
      }
      case 'Escape': {
        e.preventDefault();
        if (reviewCommentFormActive) cancelReviewCommentForm();
        else if (activeForms.length > 0) cancelComment(activeForms[activeForms.length - 1]);
        else if (selectionStart !== null) {
          const clearPath = activeFilePath;
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

  // ===== Select-to-Comment: open comment form on text selection =====
  document.addEventListener('mouseup', function(e) {
    // Don't interfere with gutter interactions (drag-to-select, + button clicks).
    if (dragState || diffDragState) return;
    if (e.target.closest('.line-comment-gutter') || e.target.closest('.diff-comment-btn')) return;

    // Small delay to let the browser finalize the selection
    requestAnimationFrame(function() {
      const selection = window.getSelection();
      const range = getLineRangeFromSelection(selection);
      if (!range) return;

      // If any comment form is already open, don't hijack text selection —
      // the user is selecting text to copy, not to open another comment.
      if (activeForms.length > 0) return;

      // Capture the selected text before clearing, for the quote field.
      // If the selection covers the full text of the line range, skip it — redundant.
      let quote = null;
      let quoteOffset = null;
      try {
        let selectedText = selection.toString().trim();
        if (selectedText) {
          // Strip diff gutter markers (+/-) from the start of each line
          selectedText = selectedText.replace(/^[+\-]/gm, '').trim();

          // Get the full text content of the lines in this range to compare.
          // Try both document view (.line-block) and diff view elements.
          // Also collect content elements in order for offset computation.
          let fullText = '';
          const contentEls = [];
          for (let ln = range.startLine; ln <= range.endLine; ln++) {
            // Document view
            document.querySelectorAll('.line-block[data-file-path]').forEach(function(el) {
              if (el.dataset.filePath !== range.filePath) return;
              const s = parseInt(el.dataset.startLine), e = parseInt(el.dataset.endLine);
              if (s <= ln && e >= ln) {
                let content = el.querySelector('.line-content');
                if (content && contentEls.indexOf(content) === -1) {
                  fullText += (fullText ? '\n' : '') + content.textContent.trim();
                  contentEls.push(content);
                }
              }
            });
            // Diff view — filter by side so unified diff doesn't double-count
            const selSide = range.side || '';
            document.querySelectorAll('[data-diff-file-path][data-diff-line-num="' + ln + '"]').forEach(function(el) {
              if (el.dataset.diffFilePath !== range.filePath) return;
              if (el.dataset.diffSide !== selSide) return;
              const content = el.querySelector('.diff-content');
              if (content && contentEls.indexOf(content) === -1) {
                fullText += (fullText ? '\n' : '') + content.textContent.trim();
                contentEls.push(content);
              }
            });
          }
          // Only include quote if it's a partial selection (not the full line content)
          const normalizedSelected = selectedText.replace(/\s+/g, ' ');
          const normalizedFull = fullText.trim().replace(/\s+/g, ' ');
          if (normalizedSelected !== normalizedFull && selectedText.length <= 300) {
            quote = selectedText;

            // Compute quote_offset: character index of the selection start
            // within the normalized full text.  Disambiguates duplicate
            // substrings (e.g. "foo foo foo" — selecting the last "foo").
            try {
              // Determine which end of the selection comes first in document order
              const selRange = selection.getRangeAt(0);
              const startContainer = selRange.startContainer;
              const startOff = selRange.startOffset;

              // Walk content elements to find total chars before selection start
              let charsBefore = 0;
              let foundEl = false;
              for (let ci = 0; ci < contentEls.length; ci++) {
                if (contentEls[ci].contains(startContainer)) {
                  // Walk text nodes in this element up to the start node
                  const walker = document.createTreeWalker(contentEls[ci], NodeFilter.SHOW_TEXT, null);
                  let tn;
                  while ((tn = walker.nextNode())) {
                    if (tn === startContainer) {
                      charsBefore += startOff;
                      break;
                    }
                    charsBefore += tn.textContent.length;
                  }
                  foundEl = true;
                  break;
                }
                charsBefore += contentEls[ci].textContent.length;
              }

              if (foundEl) {
                // Build the raw text up to charsBefore, then normalize to get offset
                let rawAll = '';
                const rawUpTo = charsBefore;
                for (let ri = 0; ri < contentEls.length; ri++) {
                  rawAll += contentEls[ri].textContent;
                  if (contentEls[ri].contains(startContainer)) break;
                }
                const textBefore = rawAll.slice(0, rawUpTo);
                quoteOffset = textBefore.replace(/\s+/g, ' ').trimStart().length;
              }
            } catch (_) { /* offset is a nice-to-have */ }
          }
        }
      } catch (_) { /* quote is a nice-to-have, don't break form opening */ }

      // Clear the browser selection — the form is the interaction now
      selection.removeAllRanges();

      // Open the comment form using the same flow as gutter click / 'c' key.
      openForm({
        filePath: range.filePath,
        afterBlockIndex: range.afterBlockIndex,
        startLine: range.startLine,
        endLine: range.endLine,
        editingId: null,
        side: range.side,
        quote: quote,
        quoteOffset: quoteOffset
      });
    });
  });

  // ===== Start =====
  init().then(connectSSE).catch(function(err) {
    console.error('Init failed:', err.message);
  });

})();
