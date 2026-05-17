// crit-settings-panes.js — shared renderers for the Settings overlay.
// Owns ALL three tabs (Settings / Shortcuts / About) so code review and
// design mode mount the same panes without duplication. Mode-specific
// behaviour is injected via the `hooks` and `show` options on
// renderSettingsTab — see below.
//
// Exports on window.crit.settingsPanes:
//   renderShortcutsPane(pane, opts)
//     opts.mode : 'code-review' | 'design' (default: 'code-review')
//                  Filters entries by their `modes` array so design users
//                  don't see code-review-only bindings (j/k, ]/[, c/e/d, …).
//   renderAboutPane(pane, cfg, sessionInfo)
//   renderSettingsTab(pane, opts)
//     opts.mode    : 'code-review' | 'design'
//     opts.cfg     : /api/config response or {}
//     opts.show    : { width, hideResolved, update, account, agent,
//                       integration, share } — booleans, defaulted from mode
//     opts.hooks   : {
//                      applyTheme(choice),                   // required
//                      applyWidth(choice),                   // required if show.width
//                      getHideResolved(), setHideResolved(v),// required if show.hideResolved
//                      onHideResolvedChange(),               // optional, called after toggle
//                      hasActivePendingUpdates(),            // optional, default false
//                      announceCopy(),                       // optional
//                    }

(function () {
  'use strict';

  // escapeHTML — delegates to the canonical window.crit.shared.escapeHTML.
  // crit-shared.js loads before this file per index.html script order.
  var escapeHTML = window.crit.shared.escapeHTML;

  // Each shortcut declares which modes it actually fires in. The renderer
  // filters out entries that don't apply to the current mode so design-mode
  // users aren't shown bindings that do nothing for them. Investigated
  // bindings:
  //   - design-mode: Esc, Ctrl+Enter, ? (and `p/P` for pin mode — design-only)
  //   - code-review: j, k, ], [, n, N, c, e, d, G, Shift+F, Shift+C,
  //                  Shift+1/2/3/4, t, h, Esc, Ctrl+Enter, ?
  var BOTH = ['code-review', 'design'];
  var CODE_REVIEW_ONLY = ['code-review'];
  var DESIGN_ONLY = ['design'];

  function renderShortcutsPane(pane, opts) {
    if (!pane) return;
    opts = opts || {};
    var mode = opts.mode || 'code-review';
    var html = '';

    var groups = [
      { label: 'Navigation', shortcuts: [
        { key: '<kbd>j</kbd>', action: 'Next block', modes: CODE_REVIEW_ONLY },
        { key: '<kbd>k</kbd>', action: 'Previous block', modes: CODE_REVIEW_ONLY },
        { key: '<kbd>Shift</kbd>+<kbd>V</kbd>', action: 'Visual line mode (extend with j/k, then c to comment)', modes: CODE_REVIEW_ONLY },
        { key: '<kbd>]</kbd>', action: 'Next comment', modes: CODE_REVIEW_ONLY },
        { key: '<kbd>[</kbd>', action: 'Previous comment', modes: CODE_REVIEW_ONLY },
        { key: '<kbd>n</kbd>', action: 'Next change', mode: 'file mode', modes: CODE_REVIEW_ONLY },
        { key: '<kbd>N</kbd>', action: 'Previous change', mode: 'file mode', modes: CODE_REVIEW_ONLY },
      ]},
      { label: 'Comments', shortcuts: [
        { key: '<kbd>c</kbd>', action: 'Comment on focused block (or text selection, with quote)', modes: CODE_REVIEW_ONLY },
        { key: '<kbd>e</kbd>', action: 'Edit comment on focused block', modes: CODE_REVIEW_ONLY },
        { key: '<kbd>d</kbd>', action: 'Delete comment on focused block', modes: CODE_REVIEW_ONLY },
        { key: '<kbd>G</kbd>', action: 'General comment', modes: CODE_REVIEW_ONLY },
        { key: '<kbd>Ctrl</kbd>+<kbd>Enter</kbd>', action: 'Comment', modes: BOTH },
      ]},
      { label: 'Review', shortcuts: [
        { key: '<kbd>Shift</kbd>+<kbd>F</kbd>', action: 'Finish review', modes: BOTH },
        { key: '<kbd>Shift</kbd>+<kbd>C</kbd>', action: 'Toggle comments panel', modes: CODE_REVIEW_ONLY },
        { key: '<kbd>Shift</kbd>+<kbd>1</kbd>/<kbd>2</kbd>/<kbd>3</kbd>/<kbd>4</kbd>', action: 'Switch scope', mode: 'vcs mode', modes: CODE_REVIEW_ONLY },
      ]},
      { label: 'Design', shortcuts: [
        { key: '<kbd>p</kbd>', action: 'Toggle pin mode', modes: DESIGN_ONLY },
      ]},
      { label: 'View', shortcuts: [
        { key: '<kbd>t</kbd>', action: 'Toggle table of contents', mode: 'file mode', modes: CODE_REVIEW_ONLY },
        { key: '<kbd>h</kbd>', action: 'Toggle hide resolved', modes: CODE_REVIEW_ONLY },
        { key: '<kbd>Esc</kbd>', action: 'Cancel / clear focus', modes: BOTH },
        { key: '<kbd>?</kbd>', action: 'Toggle this panel', modes: BOTH },
      ]},
    ];

    groups.forEach(function (group) {
      var visible = group.shortcuts.filter(function (s) {
        return s.modes && s.modes.indexOf(mode) !== -1;
      });
      if (visible.length === 0) return;
      html += '<div class="shortcuts-group-label">' + group.label + '</div>';
      html += '<table class="shortcuts-table">';
      visible.forEach(function (s) {
        var modeTag = s.mode ? '<span class="shortcut-mode-badge">' + s.mode + '</span>' : '';
        html += '<tr><td>' + s.key + '</td><td>' + s.action + modeTag + '</td></tr>';
      });
      html += '</table>';
    });

    pane.innerHTML = html;
  }

  function renderAboutPane(pane, cfg, sessionInfo) {
    if (!pane) return;
    cfg = cfg || {};
    var session = sessionInfo || {};
    var html = '';

    // Version header
    html += '<div class="about-header">';
    html += '<h2>Crit</h2>';
    var ver = cfg.version || 'dev';
    html += '<div class="about-version">' + escapeHTML(ver) + '</div>';
    if (!cfg.no_update_check) {
      if (cfg.latest_version && cfg.version && cfg.latest_version !== cfg.version) {
        html += '<div class="about-badge about-badge--update">Update available: ' + escapeHTML(cfg.latest_version) + '</div>';
      } else if (cfg.version && cfg.version !== 'dev') {
        html += '<div class="about-badge about-badge--current">Up to date</div>';
      }
    }
    html += '</div>';

    // Session info
    html += '<div class="settings-section-label">Current Session</div>';
    html += '<div class="about-session"><div class="about-session-grid">';
    var modeLabel = session.vcs_name || session.mode || 'design';
    html += '<span class="about-session-label">Mode</span><span class="about-session-value">' + escapeHTML(modeLabel) + '</span>';
    if (session.mode === 'git' && session.branch) {
      html += '<span class="about-session-label">Branch</span><span class="about-session-value">' + escapeHTML(session.branch) + '</span>';
    }
    if (session.base_ref) {
      html += '<span class="about-session-label">Base</span><span class="about-session-value">' + escapeHTML(session.base_branch_name || session.base_ref) + '</span>';
    }
    if (session.upstream_url) {
      html += '<span class="about-session-label">Upstream</span><span class="about-session-value"><code>' + escapeHTML(session.upstream_url) + '</code></span>';
    }
    html += '<span class="about-session-label">Round</span><span class="about-session-value">' + (session.review_round || 1) + '</span>';
    if (session.files !== undefined) {
      html += '<span class="about-session-label">Files</span><span class="about-session-value">' + (session.files ? session.files.length : 0) + ' changed</span>';
    }
    if (cfg.review_path) {
      html += '<span class="about-session-label">Review file</span><span class="about-session-value"><code>' + escapeHTML(cfg.review_path) + '</code></span>';
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

  // ============================================================
  // Settings tab (Display + Configuration cards). Both modes call this.
  // mode-specific differences are confined to defaults() and hooks; the
  // markup, copy buttons, dismiss buttons, pill indicators are shared.
  // ============================================================

  function defaultsForMode(mode) {
    if (mode === 'design') {
      return {
        width: false,         // width pill is file-mode only
        hideResolved: true,
        update: true,
        account: true,
        agent: true,
        integration: true,
        share: true,
      };
    }
    // code-review default
    return {
      width: true,
      hideResolved: true,
      update: true,
      account: true,
      agent: true,
      integration: true,
      share: true,
    };
  }

  function getSetting(key, fallback) {
    var s = window.crit && window.crit.shared;
    return s && s.getSetting ? s.getSetting(key, fallback) : fallback;
  }
  function setSetting(key, value) {
    var s = window.crit && window.crit.shared;
    if (s && s.setSetting) s.setSetting(key, value);
  }

  function updatePillIndicator(pane, indicatorId, values, current) {
    var indicator = pane.querySelector('#' + indicatorId);
    if (!indicator) return;
    var idx = values.indexOf(current);
    if (idx >= 0) {
      indicator.style.left = (idx * (100 / values.length)) + '%';
      indicator.style.width = (100 / values.length) + '%';
    }
  }

  var THEME_ICONS = {
    system: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor"><path fill-rule="evenodd" d="M2 4.25A2.25 2.25 0 0 1 4.25 2h7.5A2.25 2.25 0 0 1 14 4.25v5.5A2.25 2.25 0 0 1 11.75 12h-1.312c.1.128.21.248.328.36a.75.75 0 0 1 .234.545v.345a.75.75 0 0 1-.75.75h-4.5a.75.75 0 0 1-.75-.75v-.345a.75.75 0 0 1 .234-.545c.118-.111.228-.232.328-.36H4.25A2.25 2.25 0 0 1 2 9.75v-5.5Zm2.25-.75a.75.75 0 0 0-.75.75v4.5c0 .414.336.75.75.75h7.5a.75.75 0 0 0 .75-.75v-4.5a.75.75 0 0 0-.75-.75h-7.5Z" clip-rule="evenodd"/></svg>',
    light: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor"><path d="M8 1a.75.75 0 0 1 .75.75v1.5a.75.75 0 0 1-1.5 0v-1.5A.75.75 0 0 1 8 1ZM10.5 8a2.5 2.5 0 1 1-5 0 2.5 2.5 0 0 1 5 0ZM12.95 4.11a.75.75 0 1 0-1.06-1.06l-1.062 1.06a.75.75 0 0 0 1.061 1.062l1.06-1.061ZM15 8a.75.75 0 0 1-.75.75h-1.5a.75.75 0 0 1 0-1.5h1.5A.75.75 0 0 1 15 8ZM11.89 12.95a.75.75 0 0 0 1.06-1.06l-1.06-1.062a.75.75 0 0 0-1.062 1.061l1.061 1.06ZM8 12a.75.75 0 0 1 .75.75v1.5a.75.75 0 0 1-1.5 0v-1.5A.75.75 0 0 1 8 12ZM5.172 11.89a.75.75 0 0 0-1.061-1.062L3.05 11.89a.75.75 0 1 0 1.06 1.06l1.06-1.06ZM4 8a.75.75 0 0 1-.75.75h-1.5a.75.75 0 0 1 0-1.5h1.5A.75.75 0 0 1 4 8ZM4.11 5.172A.75.75 0 0 0 5.173 4.11L4.11 3.05a.75.75 0 1 0-1.06 1.06l1.06 1.06Z"/></svg>',
    dark: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor"><path d="M14.438 10.148c.19-.425-.321-.787-.748-.601A5.5 5.5 0 0 1 6.453 2.31c.186-.427-.176-.938-.6-.748a6.501 6.501 0 1 0 8.585 8.586Z"/></svg>',
  };

  function renderSettingsTab(pane, opts) {
    if (!pane) return;
    opts = opts || {};
    var cfg = opts.cfg || {};
    var hooks = opts.hooks || {};
    var defaults = defaultsForMode(opts.mode);
    var show = Object.assign({}, defaults, opts.show || {});
    var esc = (hooks.escape) || escapeHTML;

    var currentTheme = getSetting('theme', 'system');
    var currentWidth = getSetting('width', 'default');

    var html = '';

    // ---------- Display ----------
    html += '<div class="settings-section-label">Display</div>';
    html += '<div class="settings-display-group">';

    // Theme row (always shown)
    html += '<div class="settings-display-row">';
    html += '<span class="settings-display-label">Theme</span>';
    html += '<div class="settings-pill settings-pill--theme" id="settingsThemePill" role="group" aria-label="Theme">';
    html += '<div class="settings-pill-indicator" id="settingsThemeIndicator"></div>';
    ['system', 'light', 'dark'].forEach(function (theme) {
      var active = theme === currentTheme ? ' active' : '';
      var label = theme.charAt(0).toUpperCase() + theme.slice(1);
      html += '<button type="button" class="settings-pill-btn' + active +
        '" data-settings-theme="' + theme + '" aria-pressed="' + (theme === currentTheme) +
        '" title="' + label + ' theme">' + THEME_ICONS[theme] + '</button>';
    });
    html += '</div></div>';

    // Width row (file-mode in code review; off in design)
    if (show.width) {
      html += '<div class="settings-display-row">';
      html += '<span class="settings-display-label">Content Width <span style="font-weight:400;color:var(--crit-editor-fg-muted)">(file mode)</span></span>';
      html += '<div class="settings-pill settings-pill--width" id="settingsWidthPill" role="group" aria-label="Content width">';
      html += '<div class="settings-pill-indicator" id="settingsWidthIndicator"></div>';
      ['compact', 'default', 'wide'].forEach(function (w) {
        var active = w === currentWidth ? ' active' : '';
        html += '<button type="button" class="settings-pill-btn' + active +
          '" data-settings-width="' + w + '">' + w.charAt(0).toUpperCase() + w.slice(1) + '</button>';
      });
      html += '</div></div>';
    }

    // Hide resolved row
    if (show.hideResolved && hooks.getHideResolved) {
      var hideResolved = !!hooks.getHideResolved();
      html += '<div class="settings-display-row">';
      html += '<span class="settings-display-label">Hide resolved comments</span>';
      html += '<label class="comments-panel-switch">';
      html += '<input type="checkbox" id="hideResolvedToggle" aria-label="Hide resolved comments"' + (hideResolved ? ' checked' : '') + '>';
      html += '<span class="comments-panel-switch-track"><span class="comments-panel-switch-thumb"></span></span>';
      html += '</label>';
      html += '</div>';
    }

    html += '</div>'; // close settings-display-group

    // ---------- Configuration ----------
    var anyConfigCard = show.update || show.account || show.agent || show.integration || show.share;
    if (anyConfigCard) {
      html += '<div class="settings-section-label">Configuration</div>';
      html += '<div class="config-cards">';

      // Update card
      if (show.update && cfg.latest_version && cfg.version && cfg.latest_version !== cfg.version && !cfg.no_update_check) {
        var upgradeCmd = 'brew update && brew upgrade crit';
        var releaseUrl = 'https://github.com/tomasz-tomczyk/crit/releases/tag/v' + esc(cfg.latest_version);
        var alreadyDismissed = getSetting('updatesDismissed', '') === cfg.latest_version;
        html += '<div class="config-card config-card--orange"><div class="config-card-header">';
        html += '<span class="config-card-icon" style="color:var(--crit-yellow)">&#11014;</span>';
        html += '<span class="config-card-title">Update available</span>';
        html += '<span class="config-card-value">v' + esc(cfg.latest_version) + '</span>';
        html += '</div>';
        html += '<div class="config-card-cmd"><span>$ ' + esc(upgradeCmd) + '</span><button class="config-card-copy" data-copy="' + esc(upgradeCmd) + '">Copy</button></div>';
        html += '<div class="config-card-body" id="updateCardBody">';
        html += '<div class="config-card-actions">';
        html += '<a class="about-link" href="' + releaseUrl + '" target="_blank" rel="noopener">Release notes</a>';
        if (alreadyDismissed) {
          html += '<span class="config-card-dismissed" id="updateDismissedNote">Dismissed — will remind you on next version</span>';
        } else {
          html += '<button type="button" class="config-card-dismiss" id="updateDismissBtn" data-dismiss-version="' + esc(cfg.latest_version) + '">Don\'t remind me until next version</button>';
        }
        html += '</div></div></div>';
      }

      // Account card
      if (show.account && cfg.share_url) {
        if (cfg.auth_logged_in) {
          var display = cfg.auth_user_email || cfg.auth_user_name || 'Logged in';
          html += '<div class="config-card config-card--green"><div class="config-card-header">';
          html += '<span class="config-card-icon" style="color:var(--crit-green)">&#10003;</span>';
          html += '<span class="config-card-title">Account</span>';
          html += '<span class="config-card-value">' + esc(display) + '</span>';
          html += '</div></div>';
        } else {
          html += '<div class="config-card config-card--red config-card--unconfigured"><div class="config-card-header">';
          html += '<span class="config-card-icon" style="color:var(--crit-red)">&#9675;</span>';
          html += '<span class="config-card-title">Account</span>';
          html += '</div>';
          html += '<div class="config-card-body">Not logged in. Sign in to link reviews to your account and track review history.</div>';
          html += '<div class="config-card-cmd"><span>$ crit auth login</span><button class="config-card-copy" data-copy="crit auth login">Copy</button></div>';
          html += '</div>';
        }
      }

      // Agent Command card
      if (show.agent) {
        if (cfg.agent_cmd_enabled) {
          html += '<div class="config-card config-card--green"><div class="config-card-header">';
          html += '<span class="config-card-icon" style="color:var(--crit-green)">&#10003;</span>';
          html += '<span class="config-card-title">Agent Command</span>';
          html += '</div>';
          html += '<div class="config-card-cmd-value"><code>' + esc(cfg.agent_cmd || cfg.agent_name || '') + '</code></div>';
          html += '</div>';
        } else {
          html += '<div class="config-card config-card--orange config-card--unconfigured"><div class="config-card-header">';
          html += '<span class="config-card-icon" style="color:var(--crit-yellow)">&#9675;</span>';
          html += '<span class="config-card-title">Agent Command</span>';
          html += '</div>';
          html += '<div class="config-card-body">Edit <code>~/.crit.config.json</code> and set <code>agent_cmd</code> to send comments directly to your AI agent. <a href="https://github.com/tomasz-tomczyk/crit#send-to-agent-experimental" target="_blank" rel="noopener" style="color:var(--crit-brand)">Learn more</a></div>';
          html += '<div class="config-card-snippet">{"agent_cmd": "claude -p"}\n// Also: "opencode ask", "aider --message"</div>';
          html += '</div>';
        }
      }

      // Integration card
      if (show.integration && !cfg.no_integration_check) {
        var integrations = cfg.integrations || [];
        var anyInstalled = cfg.any_integration_installed;
        if (anyInstalled) {
          var current = integrations.filter(function (i) { return i.status === 'current'; });
          var stale = integrations.filter(function (i) { return i.status === 'stale'; });
          if (stale.length > 0) {
            var si = stale[0];
            var name = si.agent.replace(/\b\w/g, function (c) { return c.toUpperCase(); }).replace(/-/g, ' ');
            var dismissedMap = getSetting('dismissedIntegrations', {}) || {};
            var intAlreadyDismissed = !!si.hash && dismissedMap[si.agent] === si.hash;
            html += '<div class="config-card config-card--yellow"><div class="config-card-header">';
            html += '<span class="config-card-icon" style="color:var(--crit-yellow)">&#9888;</span>';
            html += '<span class="config-card-title">AI Integration</span>';
            html += '<span class="config-card-value">' + esc(name) + ' (update available)</span>';
            html += '</div>';
            var hintLines = (si.hint || '').split('\n').map(function (l) { return l.trim(); }).filter(Boolean);
            hintLines.forEach(function (line) {
              var parts = line.split('|');
              var lbl = '';
              var cmd = line.replace(/^Run:\s*/i, '');
              if (parts.length === 2) { lbl = parts[0]; cmd = parts[1]; }
              html += '<div class="config-card-cmd">';
              if (lbl) html += '<span class="config-card-cmd-label">' + esc(lbl) + '</span>';
              html += '<span>$ ' + esc(cmd) + '</span><button class="config-card-copy" data-copy="' + esc(cmd) + '">Copy</button></div>';
            });
            if (si.hash) {
              html += '<div class="config-card-body" id="integrationCardBody">';
              html += '<div class="config-card-actions config-card-actions--end">';
              if (intAlreadyDismissed) {
                html += '<span class="config-card-dismissed" id="integrationDismissedNote">Dismissed — will remind you when this integration changes</span>';
              } else {
                html += '<button type="button" class="config-card-dismiss" id="integrationDismissBtn" data-agent="' + esc(si.agent) + '" data-hash="' + esc(si.hash) + '">Don\'t remind me until next version</button>';
              }
              html += '</div></div>';
            }
            html += '</div>';
          } else if (current.length > 0) {
            var nm = current[0].agent.replace(/\b\w/g, function (c) { return c.toUpperCase(); }).replace(/-/g, ' ');
            html += '<div class="config-card config-card--green"><div class="config-card-header">';
            html += '<span class="config-card-icon" style="color:var(--crit-green)">&#10003;</span>';
            html += '<span class="config-card-title">AI Integration</span>';
            html += '<span class="config-card-value">' + esc(nm) + ' (up to date)</span>';
            html += '</div></div>';
          }
        } else {
          var available = (cfg.integrations_available || []).join(' · ');
          html += '<div class="config-card config-card--blue config-card--unconfigured"><div class="config-card-header">';
          html += '<span class="config-card-icon" style="color:var(--crit-brand)">&#128161;</span>';
          html += '<span class="config-card-title">AI Integration</span>';
          html += '<span class="config-card-badge">Recommended</span>';
          html += '</div>';
          html += '<div class="config-card-body">Install a plugin so your AI agent can launch crit, read comments, and iterate.</div>';
          html += '<div class="config-card-cmd"><span>$ crit install claude-code</span><button class="config-card-copy" data-copy="crit install claude-code">Copy</button></div>';
          if (available) html += '<div class="config-card-agents">Also: ' + esc(available) + '</div>';
          html += '</div>';
        }
      }

      // Share card
      if (show.share) {
        if (cfg.share_url) {
          var hostname;
          try { hostname = new URL(cfg.share_url).hostname; } catch (_) { hostname = cfg.share_url; }
          html += '<div class="config-card config-card--green"><div class="config-card-header">';
          html += '<span class="config-card-icon" style="color:var(--crit-green)">&#10003;</span>';
          html += '<span class="config-card-title">Sharing enabled</span>';
          html += '<span class="config-card-value">' + esc(hostname) + '</span>';
          html += '</div></div>';
        } else {
          html += '<div class="config-card config-card--gray config-card--unconfigured"><div class="config-card-header">';
          html += '<span class="config-card-icon" style="color:var(--crit-editor-fg-muted)">&mdash;</span>';
          html += '<span class="config-card-title">Share</span>';
          html += '<span class="config-card-value">Disabled</span>';
          html += '</div></div>';
        }
      }

      html += '</div>'; // close config-cards
    }

    pane.innerHTML = html;

    // ---------- Wire-up ----------
    pane.querySelectorAll('[data-settings-theme]').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var t = btn.dataset.settingsTheme;
        if (hooks.applyTheme) hooks.applyTheme(t);
        else { setSetting('theme', t); var s = window.crit && window.crit.shared; if (s && s.applyThemeFromCookie) s.applyThemeFromCookie(); }
        pane.querySelectorAll('[data-settings-theme]').forEach(function (b) {
          var on = b.dataset.settingsTheme === t;
          b.classList.toggle('active', on);
          b.setAttribute('aria-pressed', String(on));
        });
        updatePillIndicator(pane, 'settingsThemeIndicator', ['system', 'light', 'dark'], t);
      });
    });
    updatePillIndicator(pane, 'settingsThemeIndicator', ['system', 'light', 'dark'], currentTheme);

    if (show.width) {
      pane.querySelectorAll('[data-settings-width]').forEach(function (btn) {
        btn.addEventListener('click', function () {
          var w = btn.dataset.settingsWidth;
          if (hooks.applyWidth) hooks.applyWidth(w);
          pane.querySelectorAll('[data-settings-width]').forEach(function (b) {
            b.classList.toggle('active', b.dataset.settingsWidth === w);
          });
          updatePillIndicator(pane, 'settingsWidthIndicator', ['compact', 'default', 'wide'], w);
        });
      });
      updatePillIndicator(pane, 'settingsWidthIndicator', ['compact', 'default', 'wide'], currentWidth);
    }

    if (show.hideResolved && hooks.getHideResolved) {
      var hrToggle = pane.querySelector('#hideResolvedToggle');
      if (hrToggle) {
        hrToggle.addEventListener('change', function () {
          if (hooks.setHideResolved) hooks.setHideResolved(hrToggle.checked);
          if (hooks.onHideResolvedChange) hooks.onHideResolvedChange();
        });
      }
    }

    var dismissBtn = pane.querySelector('#updateDismissBtn');
    if (dismissBtn) {
      dismissBtn.addEventListener('click', function () {
        var version = dismissBtn.dataset.dismissVersion || '';
        setSetting('updatesDismissed', version);
        var updateBtn = document.getElementById('updateBtn');
        var pending = hooks.hasActivePendingUpdates ? !!hooks.hasActivePendingUpdates() : false;
        if (updateBtn && !pending) updateBtn.style.display = 'none';
        var body = pane.querySelector('#updateCardBody');
        if (body) {
          dismissBtn.outerHTML = '<span class="config-card-dismissed" id="updateDismissedNote">Dismissed — will remind you on next version</span>';
        }
      });
    }

    var integrationDismissBtn = pane.querySelector('#integrationDismissBtn');
    if (integrationDismissBtn) {
      integrationDismissBtn.addEventListener('click', function () {
        var agent = integrationDismissBtn.dataset.agent || '';
        var hash = integrationDismissBtn.dataset.hash || '';
        if (!agent || !hash) return;
        var map = getSetting('dismissedIntegrations', {}) || {};
        map[agent] = hash;
        setSetting('dismissedIntegrations', map);
        var updateBtn = document.getElementById('updateBtn');
        var pending = hooks.hasActivePendingUpdates ? !!hooks.hasActivePendingUpdates() : false;
        if (updateBtn && !pending) updateBtn.style.display = 'none';
        integrationDismissBtn.outerHTML = '<span class="config-card-dismissed" id="integrationDismissedNote">Dismissed — will remind you when this integration changes</span>';
      });
    }

    pane.querySelectorAll('.config-card-copy').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var text = btn.dataset.copy;
        navigator.clipboard.writeText(text).then(function () {
          btn.textContent = '✓ Copied';
          btn.setAttribute('aria-label', 'Copied');
          if (hooks.announceCopy) hooks.announceCopy();
          btn.classList.add('copied');
          setTimeout(function () {
            btn.textContent = 'Copy';
            btn.setAttribute('aria-label', 'Copy');
            btn.classList.remove('copied');
          }, 1500);
        });
      });
    });
  }

  window.crit = window.crit || {};
  window.crit.settingsPanes = {
    renderShortcutsPane: renderShortcutsPane,
    renderAboutPane: renderAboutPane,
    renderSettingsTab: renderSettingsTab,
  };
})();
