(function() {
  'use strict';

  // Theme preview page (/themes). Fixed samples in Crit's own markup (file
  // header, diff with a comment, file-level thread and form, rendered
  // Markdown) so a reader can flip through the bundled themes and pick one.
  // The sample diff follows the reader's display settings, and the Settings
  // dialog works here too. Reads window.crit.palettes, window.PierreDiffs
  // (FileDiff, processFile), window.crit.themePalette, window.crit.shared,
  // window.crit.settingsOverlay, window.crit.settingsPanes, window.crit.pierreDOM,
  // window.crit.themeBoost, window.crit.pierreAdapter.

  var TP = window.crit.themePalette;
  var shared = window.crit.shared;

  var ICON_CHEVRON = '<svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor"><path d="M12.78 5.22a.749.749 0 0 1 0 1.06l-4.25 4.25a.749.749 0 0 1-1.06 0L3.22 6.28a.749.749 0 1 1 1.06-1.06L8 8.939l3.72-3.719a.749.749 0 0 1 1.06 0Z"></path></svg>';
  var ICON_FILE = '<svg class="file-header-icon" viewBox="0 0 16 16" fill="var(--crit-editor-fg-muted)"><path fill-rule="evenodd" d="M3.75 1.5a.25.25 0 0 0-.25.25v11.5c0 .138.112.25.25.25h8.5a.25.25 0 0 0 .25-.25V6H9.75A1.75 1.75 0 0 1 8 4.25V1.5H3.75zm5.75.56v2.19c0 .138.112.25.25.25h2.19L9.5 2.06zM2 1.75C2 .784 2.784 0 3.75 0h5.086c.464 0 .909.184 1.237.513l3.414 3.414c.329.328.513.773.513 1.237v8.086A1.75 1.75 0 0 1 12.25 15h-8.5A1.75 1.75 0 0 1 2 13.25V1.75z"></path></svg>';
  var ICON_COMMENT = '<svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor"><path d="M1 2.75C1 1.784 1.784 1 2.75 1h10.5c.966 0 1.75.784 1.75 1.75v7.5A1.75 1.75 0 0 1 13.25 12H9.06l-2.573 2.573A1.458 1.458 0 0 1 4 13.543V12H2.75A1.75 1.75 0 0 1 1 10.25Zm1.75-.25a.25.25 0 0 0-.25.25v7.5c0 .138.112.25.25.25h2a.75.75 0 0 1 .75.75v2.19l2.72-2.72a.749.749 0 0 1 .53-.22h4.5a.25.25 0 0 0 .25-.25v-7.5a.25.25 0 0 0-.25-.25Z"></path></svg>';
  var ICON_CHECK = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"></polyline></svg>';
  var ICON_EDIT = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17 3a2.85 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"></path><path d="m15 5 4 4"></path></svg>';
  var ICON_DELETE = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18"></path><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"></path><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"></path></svg>';

  var PATCH = [
    'diff --git a/server.go b/server.go',
    '--- a/server.go',
    '+++ b/server.go',
    '@@ -1,16 +1,18 @@',
    ' package main',
    ' ',
    ' import (',
    '+\t"errors"',
    ' \t"net/http"',
    ' )',
    ' ',
    '-// authMiddleware checks the session cookie.',
    '+// authMiddleware checks the API key header, then the session cookie.',
    ' func authMiddleware(next http.Handler) http.Handler {',
    ' \treturn http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {',
    '-\t\tif r.Header.Get("Cookie") == "" {',
    '-\t\t\thttp.Error(w, "forbidden", 403)',
    '+\t\tif key := r.Header.Get("X-API-Key"); key != "" && !validKey(key) {',
    '+\t\t\thttp.Error(w, errors.New("invalid key").Error(), http.StatusUnauthorized)',
    ' \t\t\treturn',
    ' \t\t}',
    '+\t\tconst retries = 3 // TODO: make configurable',
    ' \t\tnext.ServeHTTP(w, r)',
    ' \t})',
    ' }',
    '',
  ].join('\n');

  // badge: [label, class] as the review page uses them (e.g. ['New', 'added']).
  function fileHeader(dir, name, badge, stats) {
    return '<div class="file-header crit-review-file-header pierre-file-header">' +
      '<div class="file-header-chevron">' + ICON_CHEVRON + '</div>' + ICON_FILE +
      '<span class="file-header-name"><span class="dir">' + dir + '</span><span class="filename">' + name + '</span></span>' +
      '<span class="file-header-badge ' + badge[1] + '">' + badge[0] + '</span>' +
      '<span class="file-header-stats">' + stats + '</span>' +
      '<button class="file-comment-btn" aria-label="Add file-level comment">' + ICON_COMMENT + '</button>' +
      '<label class="file-header-viewed"><input type="checkbox"><span>Viewed</span></label></div>';
  }

  function commentCard(author, color, body, reply, resolved) {
    return '<div class="comment-card' + (resolved ? ' resolved-card' : '') + '"><div class="comment-header"><div class="comment-header-left">' +
      '<button class="comment-collapse-btn" aria-label="Collapse comment">' + ICON_CHEVRON + '</button>' +
      '<span class="comment-author-badge author-color-' + color + '">@' + author + '</span>' +
      '<span class="comment-round-badge round-current">R1</span><span class="comment-time">1:47 PM</span></div>' +
      '<div class="comment-actions"><button class="resolve-btn" aria-label="Resolve thread">' + ICON_CHECK + '<span>Resolve</span></button>' +
      '<button aria-label="Edit">' + ICON_EDIT + '</button><button class="delete-btn" aria-label="Delete">' + ICON_DELETE + '</button></div></div>' +
      '<div class="comment-body">' + body + '</div>' +
      (reply ? '<div class="comment-replies"><div class="comment-reply"><div class="reply-header"><div class="reply-meta">' +
        '<span class="comment-author-badge author-color-2">@Claude</span><span class="reply-time">1:52 PM</span></div></div>' +
        '<div class="reply-body">' + reply + '</div></div></div>' : '') +
      '<div class="reply-form"><input type="text" class="reply-input" placeholder="Write a reply…"></div></div>';
  }

  var COMMENT_FORM = '<div class="comment-form"><div class="comment-form-header">Comment</div>' +
    '<textarea placeholder="Leave a review comment... (Ctrl+Enter to submit, Escape to cancel)">Can we add a test for the empty-key case?</textarea>' +
    '<div class="comment-form-actions"><div class="comment-form-actions-left"><button class="btn btn-sm">± Suggest</button><button class="btn btn-sm">+ Template</button></div>' +
    '<button class="btn btn-sm">Cancel</button><button class="btn btn-sm btn-primary">Comment</button></div></div>';

  // Rendered Markdown uses Crit's line blocks: one block per source line.
  function block(n, html, cls, contentCls) {
    return '<div class="line-block' + (cls ? ' ' + cls : '') + '"><div class="line-gutter"><span class="line-num">' + n + '</span></div>' +
      '<div class="line-content' + (contentCls ? ' ' + contentCls : '') + '">' + html + '</div></div>';
  }

  function tableRow(n, tag, cells, head) {
    return '<tr class="line-block table-row' + (head ? ' table-first' : '') + '"><td class="native-table-gutter"><div class="line-gutter"><span class="line-num">' + n + '</span></div></td>' +
      cells.map(function(c) { return '<' + tag + ' class="line-content table-row' + (head ? ' table-first' : '') + '">' + c + '</' + tag + '>'; }).join('') + '</tr>';
  }

  var DOCUMENT =
    block(1, '<h1>API key authentication</h1>') +
    block(2, '', '', 'empty-line') +
    block(3, '<p>Requests may send an <code>X-API-Key</code> header. Keys are checked before the session cookie, see <a href="#">the auth guide</a>. Invalid keys get a <strong>401</strong>.</p>', 'has-comment') +
    block(4, '', '', 'empty-line') +
    block(5, '<h2>Rollout</h2>', 'focused') +
    block(6, '<ol start="1"><li>Ship the middleware behind a flag</li></ol>') +
    block(7, '<ol start="2"><li>Rotate existing keys</li></ol>', 'line-block-added') +
    block(8, '<blockquote><p>Keys never appear in logs.</p></blockquote>') +
    block(9, '', '', 'empty-line') +
    '<div class="native-table-wrapper"><table class="native-table"><thead>' +
    tableRow(10, 'th', ['Setting', 'Type', 'Default'], true) + '</thead><tbody>' +
    tableRow(12, 'td', ['<code>auth.keys</code>', 'string[]', '<code>[]</code>']) +
    tableRow(13, 'td', ['<code>auth.header</code>', 'string', '<code>"X-API-Key"</code>']) +
    '</tbody></table></div>';

  var state = { mode: 'dark', id: null, diff: null };
  var P = null;

  function palettes(mode) {
    return window.crit.palettes.filter(function(p) { return p.type === mode; })
      .sort(function(a, b) { return a.displayName.localeCompare(b.displayName); });
  }

  function savedId(mode) {
    return TP.pair({ lightPalette: shared.getSetting('lightPalette'), darkPalette: shared.getSetting('darkPalette') }, window.crit.palettes)[mode];
  }

  function swatch(color) {
    return '<span class="theme-preview-swatch" style="background:' + color + '"></span>';
  }

  function renderList() {
    var list = document.getElementById('themeList');
    var saved = savedId(state.mode);
    list.innerHTML = palettes(state.mode).map(function(p) {
      var c = p.colors;
      return '<div class="theme-preview-item" role="option" id="theme-' + p.id + '" data-id="' + p.id + '" aria-selected="' + (p.id === state.id) + '">' +
        '<span class="theme-preview-swatches" style="background:' + c.bg + ';border-color:' + c.border + '">' +
        swatch(c.fg) + swatch(c.accent) + swatch(c.green) + swatch(c.red) + '</span>' +
        '<span class="theme-preview-item-name">' + p.displayName + '</span>' +
        (p.id === saved ? '<span class="theme-preview-current">current</span>' : '') + '</div>';
    }).join('');
    list.setAttribute('aria-activedescendant', 'theme-' + state.id);
  }

  function renderSamples() {
    var root = document.getElementById('themeSamples');
    root.innerHTML =
      '<section class="theme-preview-file">' + fileHeader('internal/', 'server.go', ['Modified', 'modified'], '<span class="add">+6</span><span class="del">-3</span>') +
        '<div class="theme-preview-diff" id="previewDiff"></div></section>' +
      '<section class="theme-preview-file">' + fileHeader('docs/', 'auth.md', ['New file', 'added'], '<span class="add">+13</span>') +
        '<div class="comment-block pierre-file-level">' + commentCard('Ada', 4, '<p>Should the rollout mention <strong>key rotation</strong> for old clients?</p>', '<p>Added step 2.</p>') + '</div>' +
        '<div class="comment-form-wrapper pierre-file-level">' + COMMENT_FORM + '</div>' +
        '<div class="file-section pierre-document"><div class="file-body"><div class="document-wrapper">' + DOCUMENT + '</div></div></div></section>' +
      '<section class="theme-preview-controls">' +
        ['Ada', 'Claude', 'Grace', 'Linus', 'Margaret', 'Ken'].map(function(name, i) {
          return '<span class="comment-author-badge author-color-' + i + '">@' + name + '</span>';
        }).join('') +
      '</section><section class="theme-preview-controls">' +
        '<div class="scope-toggle"><button class="toggle-btn active">Split</button><button class="toggle-btn">Unified</button></div>' +
        '<button class="btn btn-sm">Default</button><button class="btn btn-sm btn-primary">Primary</button>' +
        '<span class="file-header-badge modified">Modified</span><span class="file-header-badge added">New file</span><span class="file-header-badge deleted">Deleted</span>' +
      '</section>';
  }

  // One Pierre FileDiff with a line comment, rebuilt per theme or setting.
  function renderDiff() {
    var container = document.getElementById('previewDiff');
    if (state.diff) state.diff.cleanUp();
    container.innerHTML = '';
    var theme = { light: savedId('light'), dark: savedId('dark') };
    theme[state.mode] = state.id;
    theme = window.crit.themeBoost.themes(P, theme, shared.getSetting('boostContrast', 'off') === 'on');
    state.diff = new P.FileDiff(Object.assign(window.crit.pierreAdapter.displayOptions(shared.getSetting), {
      theme: theme,
      themeType: state.mode,
      diffStyle: 'unified',
      disableFileHeader: true,
      unsafeCSS: window.crit.pierreDOM.unsafeCSS,
      renderAnnotation: function() {
        var el = document.createElement('div');
        el.className = 'comment-block pierre-annotation';
        el.innerHTML = commentCard('Ada', 4, '<p>Use <code>http.StatusUnauthorized</code> here too, and drop the unused <code>retries</code>.</p>');
        return el;
      },
    }));
    state.diff.render({
      fileDiff: P.processFile(PATCH, { cacheKey: 'theme-preview' }),
      lineAnnotations: [{ side: 'additions', lineNumber: 11, metadata: {} }],
      containerWrapper: container,
    });
  }

  function select(mode, id) {
    state.mode = mode;
    state.id = id;
    var html = document.documentElement;
    html.setAttribute('data-theme', mode);
    var settings = { theme: mode };
    settings[mode + 'Palette'] = id;
    var palette = TP.apply(settings, window.crit.palettes, html, mode === 'light');
    document.querySelectorAll('[data-preview-mode]').forEach(function(b) {
      b.classList.toggle('active', b.dataset.previewMode === mode);
      b.setAttribute('aria-pressed', String(b.dataset.previewMode === mode));
    });
    renderList();
    renderDiff();
    var saved = savedId(mode) === id;
    document.getElementById('themeName').textContent = palette.displayName;
    document.getElementById('themeStatus').textContent = saved ? 'Your current ' + mode + ' theme' : '';
    var use = document.getElementById('useTheme');
    use.textContent = saved ? 'In use' : 'Use as ' + mode + ' theme';
    use.disabled = saved;
    history.replaceState(null, '', '#' + mode + '/' + id);
    var item = document.getElementById('theme-' + id);
    if (item) item.scrollIntoView({ block: 'nearest' });
  }

  // Settings dialog: the review page's Settings tab, minus review-only rows.
  // Changes save to the same cookie and update the preview in place.
  var codeFonts = null;
  function renderSettingsPane() {
    window.crit.settingsPanes.renderSettingsTab(document.getElementById('settingsPane'), {
      mode: 'code-review',
      cfg: { code_fonts: codeFonts || undefined },
      show: { width: false, hideResolved: false, ignoreWhitespace: false, account: false, agent: false, share: false, themePreview: false },
      hooks: {
        applyTheme: function(choice) {
          shared.setSetting('theme', choice);
          var mode = choice === 'light' || choice === 'dark' ? choice
            : window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
          select(mode, savedId(mode));
        },
        themePalettes: window.crit.palettes,
        paletteDefaults: TP.pair({ lightPalette: shared.getSetting('lightPalette'), darkPalette: shared.getSetting('darkPalette') }, window.crit.palettes),
        onRendererSettingChange: function(key, value) {
          shared.setSetting(key, value);
          if (key === state.mode + 'Palette') select(state.mode, value);
          else if (key === 'lightPalette' || key === 'darkPalette') renderList();
          else renderDiff();
        },
      },
    });
  }

  function installSettings() {
    window.crit.settingsOverlay.install({
      overlay: document.getElementById('settingsOverlay'),
      toggle: document.getElementById('settingsToggle'),
      closeBtn: document.getElementById('settingsClose'),
      initialTab: 'settings',
      enableQuestionMarkShortcut: false,
      onOpen: function() {
        renderSettingsPane();
        if (codeFonts) return;
        fetch('/api/code-fonts').then(function(r) {
          if (!r.ok) throw new Error('Could not load code fonts');
          return r.json();
        }).then(function(data) {
          codeFonts = Array.isArray(data.code_fonts) ? data.code_fonts : [];
          renderSettingsPane();
        }).catch(function() { /* built-in fonts stay listed */ });
      },
      onClose: function() {
        // A palette may have been picked for the other mode; refresh the list.
        select(state.mode, state.id);
      },
    });
  }

  function move(delta) {
    var list = palettes(state.mode);
    var i = list.findIndex(function(p) { return p.id === state.id; });
    var next = list[Math.max(0, Math.min(list.length - 1, i + delta))];
    if (next) select(state.mode, next.id);
  }

  function start(pierre) {
    P = pierre;
    if (!P || !window.crit.palettes) {
      document.getElementById('themeName').textContent = 'Themes could not be loaded.';
      return;
    }
    var hash = location.hash.slice(1).split('/');
    var setting = shared.getSetting('theme', 'system');
    var mode = hash[0] === 'light' || hash[0] === 'dark' ? hash[0]
      : setting === 'light' || setting === 'dark' ? setting
      : window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
    var id = palettes(mode).some(function(p) { return p.id === hash[1]; }) ? hash[1] : savedId(mode);
    shared.applyCodeFontFromCookie();
    renderSamples();
    select(mode, id);
    installSettings();

    document.getElementById('themeList').addEventListener('click', function(e) {
      var item = e.target.closest('[data-id]');
      if (item) select(state.mode, item.dataset.id);
    });
    document.getElementById('themeList').addEventListener('keydown', function(e) {
      var step = { ArrowDown: 1, ArrowUp: -1, Home: -Infinity, End: Infinity }[e.key];
      if (step === undefined) return;
      e.preventDefault();
      move(step === Infinity ? 1e9 : step === -Infinity ? -1e9 : step);
    });
    document.getElementById('previewModeToggle').addEventListener('click', function(e) {
      var btn = e.target.closest('[data-preview-mode]');
      if (btn && btn.dataset.previewMode !== state.mode) select(btn.dataset.previewMode, savedId(btn.dataset.previewMode));
    });
    document.getElementById('useTheme').addEventListener('click', function() {
      shared.setSetting(state.mode + 'Palette', state.id);
      select(state.mode, state.id);
      document.getElementById('themeStatus').textContent = 'Saved. Open or reload a review to use it.';
    });
    document.getElementById('themeList').focus();
  }

  window.critPierreReady.then(start);
})();
