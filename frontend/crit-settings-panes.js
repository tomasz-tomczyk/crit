// crit-settings-panes.js — shared renderers for the Settings overlay's
// Shortcuts and About panes. Loaded by both app.js (code review) and
// design-mode.js so the overlay shows the same content in either mode.
//
// Settings (the first tab) is NOT extracted here: its action wiring is
// tightly coupled to app.js module-scope state (applyTheme, applyWidth,
// renderAllFiles, hasActivePendingUpdates, etc.). Design mode renders a
// minimal Theme pill itself; code review continues to use app.js's
// renderSettingsPane.
//
// Exports on window.crit.settingsPanes:
//   renderShortcutsPane(pane)
//     pane: target HTMLElement (typically #shortcutsPane)
//
//   renderAboutPane(pane, cfg, sessionInfo)
//     pane: target HTMLElement (typically #aboutPane)
//     cfg:  /api/config response or {} (uses version, latest_version,
//           no_update_check, review_path)
//     sessionInfo: {mode, vcs_name, branch, base_ref, base_branch_name,
//                   review_round, files} or {} for design mode.

(function () {
  'use strict';

  function escapeHTML(s) {
    if (s === null || s === undefined) return '';
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function renderShortcutsPane(pane) {
    if (!pane) return;
    var html = '';

    var groups = [
      { label: 'Navigation', shortcuts: [
        { key: '<kbd>j</kbd>', action: 'Next block' },
        { key: '<kbd>k</kbd>', action: 'Previous block' },
        { key: '<kbd>]</kbd>', action: 'Next comment' },
        { key: '<kbd>[</kbd>', action: 'Previous comment' },
        { key: '<kbd>n</kbd>', action: 'Next change', mode: 'file mode' },
        { key: '<kbd>N</kbd>', action: 'Previous change', mode: 'file mode' },
      ]},
      { label: 'Comments', shortcuts: [
        { key: '<kbd>c</kbd>', action: 'Comment on focused block (or text selection, with quote)' },
        { key: '<kbd>e</kbd>', action: 'Edit comment on focused block' },
        { key: '<kbd>d</kbd>', action: 'Delete comment on focused block' },
        { key: '<kbd>G</kbd>', action: 'General comment' },
        { key: '<kbd>Ctrl</kbd>+<kbd>Enter</kbd>', action: 'Comment' },
      ]},
      { label: 'Review', shortcuts: [
        { key: '<kbd>Shift</kbd>+<kbd>F</kbd>', action: 'Finish review' },
        { key: '<kbd>Shift</kbd>+<kbd>C</kbd>', action: 'Toggle comments panel' },
        { key: '<kbd>Shift</kbd>+<kbd>1</kbd>/<kbd>2</kbd>/<kbd>3</kbd>/<kbd>4</kbd>', action: 'Switch scope', mode: 'vcs mode' },
      ]},
      { label: 'View', shortcuts: [
        { key: '<kbd>t</kbd>', action: 'Toggle table of contents', mode: 'file mode' },
        { key: '<kbd>h</kbd>', action: 'Toggle hide resolved' },
        { key: '<kbd>Esc</kbd>', action: 'Cancel / clear focus' },
        { key: '<kbd>?</kbd>', action: 'Toggle this panel' },
      ]},
    ];

    groups.forEach(function (group) {
      html += '<div class="shortcuts-group-label">' + group.label + '</div>';
      html += '<table class="shortcuts-table">';
      group.shortcuts.forEach(function (s) {
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

  window.crit = window.crit || {};
  window.crit.settingsPanes = {
    renderShortcutsPane: renderShortcutsPane,
    renderAboutPane: renderAboutPane,
  };
})();
