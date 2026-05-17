// crit-settings-panes.test.js — exercise the shared Settings overlay
// renderer used by both code-review (app.js) and live-mode.js.
//
// The renderer writes its markup as a single `pane.innerHTML = html`
// assignment, then runs querySelectorAll to wire events. We stub a
// minimal DOM where `innerHTML=` records the raw string (assertable via
// regex) and querySelectorAll returns a small set of stub nodes for the
// wire-up calls (so renderSettingsTab doesn't throw). We do not exercise
// click wiring here — that path requires a real DOM and is covered by
// E2E. We DO verify the option contract: which sections render under
// which (mode, cfg, show) combinations.
'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');

function makeStubNode() {
  return {
    classList: {
      _s: new Set(),
      add(...c) { c.forEach((x) => this._s.add(x)); },
      remove(...c) { c.forEach((x) => this._s.delete(x)); },
      contains(c) { return this._s.has(c); },
      toggle(c, force) {
        if (typeof force === 'boolean') { force ? this._s.add(c) : this._s.delete(c); return force; }
        if (this._s.has(c)) { this._s.delete(c); return false; }
        this._s.add(c); return true;
      },
    },
    dataset: {},
    style: {},
    setAttribute() {},
    addEventListener() {},
    querySelector() { return null },
    querySelectorAll() { return []; },
  };
}

function makePane() {
  const pane = makeStubNode();
  pane._html = '';
  Object.defineProperty(pane, 'innerHTML', {
    set(v) { this._html = String(v); },
    get() { return this._html; },
  });
  pane.querySelectorAll = () => [];
  pane.querySelector = () => null;
  return pane;
}

function loadShared() {
  const sandbox = { window: {}, document: { cookie: '', getElementById: () => null } };
  const sharedSrc = fs.readFileSync(path.join(__dirname, '..', 'crit-shared.js'), 'utf8');
  new Function('window', 'document', sharedSrc)(sandbox.window, sandbox.document);
  const panesSrc = fs.readFileSync(path.join(__dirname, '..', 'crit-settings-panes.js'), 'utf8');
  // navigator.clipboard is referenced inside copy-button click handlers but
  // those handlers don't run during render.
  new Function('window', 'document', 'navigator', panesSrc)(sandbox.window, sandbox.document, {});
  return sandbox.window.crit.settingsPanes;
}

test('renderSettingsTab: code-review mode renders theme + width + hide-resolved', () => {
  const sp = loadShared();
  const pane = makePane();

  sp.renderSettingsTab(pane, {
    mode: 'code-review',
    cfg: {},
    hooks: {
      applyTheme: () => {},
      applyWidth: () => {},
      getHideResolved: () => false,
      setHideResolved: () => {},
    },
  });

  const html = pane.innerHTML;
  assert.match(html, /data-settings-theme="system"/);
  assert.match(html, /data-settings-theme="light"/);
  assert.match(html, /data-settings-theme="dark"/);
  assert.match(html, /data-settings-width="compact"/);
  assert.match(html, /data-settings-width="default"/);
  assert.match(html, /data-settings-width="wide"/);
  assert.match(html, /id="hideResolvedToggle"/);
});

test('renderSettingsTab: live mode hides width pill but keeps theme + hide-resolved', () => {
  const sp = loadShared();
  const pane = makePane();

  sp.renderSettingsTab(pane, {
    mode: 'live',
    cfg: {},
    hooks: {
      applyTheme: () => {},
      getHideResolved: () => false,
      setHideResolved: () => {},
    },
  });

  const html = pane.innerHTML;
  assert.match(html, /data-settings-theme="system"/);
  assert.doesNotMatch(html, /data-settings-width=/, 'no width pill in live mode');
  assert.match(html, /id="hideResolvedToggle"/);
});

test('renderSettingsTab: pre-checks hide-resolved checkbox when getHideResolved returns true', () => {
  const sp = loadShared();
  const pane = makePane();

  sp.renderSettingsTab(pane, {
    mode: 'live',
    cfg: {},
    hooks: { applyTheme: () => {}, getHideResolved: () => true, setHideResolved: () => {} },
  });

  assert.match(pane.innerHTML, /id="hideResolvedToggle"[^>]*checked/);
});

test('renderSettingsTab: update card appears when latest_version > version', () => {
  const sp = loadShared();
  const pane = makePane();
  sp.renderSettingsTab(pane, {
    mode: 'code-review',
    cfg: { version: '1.0.0', latest_version: '1.1.0' },
    hooks: { applyTheme: () => {}, applyWidth: () => {}, getHideResolved: () => false, setHideResolved: () => {} },
  });

  assert.match(pane.innerHTML, /Update available/);
  assert.match(pane.innerHTML, /v1\.1\.0/);
  assert.match(pane.innerHTML, /brew update &amp;&amp; brew upgrade crit/);
});

test('renderSettingsTab: no update card when no_update_check', () => {
  const sp = loadShared();
  const pane = makePane();
  sp.renderSettingsTab(pane, {
    mode: 'code-review',
    cfg: { version: '1.0.0', latest_version: '1.1.0', no_update_check: true },
    hooks: { applyTheme: () => {}, applyWidth: () => {}, getHideResolved: () => false, setHideResolved: () => {} },
  });
  assert.doesNotMatch(pane.innerHTML, /Update available/);
});

test('renderSettingsTab: agent card unconfigured snippet when agent_cmd_enabled false', () => {
  const sp = loadShared();
  const pane = makePane();
  sp.renderSettingsTab(pane, {
    mode: 'live',
    cfg: { agent_cmd_enabled: false, no_integration_check: true },
    hooks: { applyTheme: () => {}, getHideResolved: () => false, setHideResolved: () => {} },
  });
  assert.match(pane.innerHTML, /Agent Command/);
  assert.match(pane.innerHTML, /config-card--unconfigured/);
});

test('renderSettingsTab: agent card configured shows agent_cmd value', () => {
  const sp = loadShared();
  const pane = makePane();
  sp.renderSettingsTab(pane, {
    mode: 'live',
    cfg: { agent_cmd_enabled: true, agent_cmd: 'claude -p', no_integration_check: true },
    hooks: { applyTheme: () => {}, getHideResolved: () => false, setHideResolved: () => {} },
  });
  assert.match(pane.innerHTML, /<code>claude -p<\/code>/);
});

test('renderSettingsTab: integration card omitted when no_integration_check', () => {
  const sp = loadShared();
  const pane = makePane();
  sp.renderSettingsTab(pane, {
    mode: 'live',
    cfg: { no_integration_check: true },
    hooks: { applyTheme: () => {}, getHideResolved: () => false, setHideResolved: () => {} },
  });
  assert.doesNotMatch(pane.innerHTML, /AI Integration/);
});

test('renderSettingsTab: integration card unconfigured CTA when nothing installed', () => {
  const sp = loadShared();
  const pane = makePane();
  sp.renderSettingsTab(pane, {
    mode: 'live',
    cfg: { integrations: [], integrations_available: ['claude-code', 'cursor'] },
    hooks: { applyTheme: () => {}, getHideResolved: () => false, setHideResolved: () => {} },
  });
  assert.match(pane.innerHTML, /AI Integration/);
  assert.match(pane.innerHTML, /crit install claude-code/);
  assert.match(pane.innerHTML, /Also: claude-code/);
});

test('renderSettingsTab: share card disabled when no share_url', () => {
  const sp = loadShared();
  const pane = makePane();
  sp.renderSettingsTab(pane, {
    mode: 'live',
    cfg: { no_integration_check: true },
    hooks: { applyTheme: () => {}, getHideResolved: () => false, setHideResolved: () => {} },
  });
  assert.match(pane.innerHTML, /Share[\s\S]*Disabled/);
});

test('renderSettingsTab: share card enabled shows hostname', () => {
  const sp = loadShared();
  const pane = makePane();
  sp.renderSettingsTab(pane, {
    mode: 'live',
    cfg: { share_url: 'https://example.com/api', no_integration_check: true },
    hooks: { applyTheme: () => {}, getHideResolved: () => false, setHideResolved: () => {} },
  });
  assert.match(pane.innerHTML, /Sharing enabled/);
  assert.match(pane.innerHTML, /example\.com/);
});

test('renderSettingsTab: account card present when share_url + auth_logged_in', () => {
  const sp = loadShared();
  const pane = makePane();
  sp.renderSettingsTab(pane, {
    mode: 'live',
    cfg: { share_url: 'https://example.com', auth_logged_in: true, auth_user_email: 'a@b.com', no_integration_check: true },
    hooks: { applyTheme: () => {}, getHideResolved: () => false, setHideResolved: () => {} },
  });
  assert.match(pane.innerHTML, /a@b\.com/);
});

test('renderSettingsTab: opts.show overrides defaults', () => {
  const sp = loadShared();
  const pane = makePane();
  sp.renderSettingsTab(pane, {
    mode: 'code-review',
    cfg: {},
    show: { width: false, hideResolved: false, update: false, account: false, agent: false, integration: false, share: false },
    hooks: { applyTheme: () => {}, applyWidth: () => {}, getHideResolved: () => false, setHideResolved: () => {} },
  });
  // Only theme pill remains.
  assert.match(pane.innerHTML, /data-settings-theme/);
  assert.doesNotMatch(pane.innerHTML, /data-settings-width=/);
  assert.doesNotMatch(pane.innerHTML, /id="hideResolvedToggle"/);
  assert.doesNotMatch(pane.innerHTML, /Configuration/);
});

test('renderShortcutsPane / renderAboutPane still exposed', () => {
  const sp = loadShared();
  assert.equal(typeof sp.renderShortcutsPane, 'function');
  assert.equal(typeof sp.renderAboutPane, 'function');
});

test('renderShortcutsPane: code-review mode shows code-review-only shortcuts', () => {
  const sp = loadShared();
  const pane = makePane();
  sp.renderShortcutsPane(pane, { mode: 'code-review' });
  const html = pane.innerHTML;
  // Code-review-only bindings present
  assert.match(html, /<kbd>j<\/kbd>/);
  assert.match(html, /<kbd>k<\/kbd>/);
  assert.match(html, /<kbd>\]<\/kbd>/);
  assert.match(html, /<kbd>\[<\/kbd>/);
  assert.match(html, /<kbd>c<\/kbd>/);
  assert.match(html, /<kbd>e<\/kbd>/);
  assert.match(html, /<kbd>d<\/kbd>/);
  assert.match(html, /<kbd>G<\/kbd>/);
  assert.match(html, /<kbd>t<\/kbd>/);
  assert.match(html, /<kbd>h<\/kbd>/);
  assert.match(html, /Shift<\/kbd>\+<kbd>F/);
  assert.match(html, /Shift<\/kbd>\+<kbd>C/);
  assert.match(html, /Switch scope/);
  // Shared bindings present
  assert.match(html, /<kbd>Esc<\/kbd>/);
  assert.match(html, /<kbd>\?<\/kbd>/);
  assert.match(html, /Ctrl<\/kbd>\+<kbd>Enter/);
  // Live-only binding absent
  assert.doesNotMatch(html, /Toggle pin mode/);
});

test('renderShortcutsPane: live mode omits code-review-only shortcuts', () => {
  const sp = loadShared();
  const pane = makePane();
  sp.renderShortcutsPane(pane, { mode: 'live' });
  const html = pane.innerHTML;
  // Code-review-only bindings absent
  assert.doesNotMatch(html, /<kbd>j<\/kbd>/);
  assert.doesNotMatch(html, /<kbd>k<\/kbd>/);
  assert.doesNotMatch(html, /<kbd>\]<\/kbd>/);
  assert.doesNotMatch(html, /<kbd>\[<\/kbd>/);
  assert.doesNotMatch(html, /<kbd>n<\/kbd>/);
  assert.doesNotMatch(html, /<kbd>c<\/kbd>/);
  assert.doesNotMatch(html, /<kbd>e<\/kbd>/);
  assert.doesNotMatch(html, /<kbd>d<\/kbd>/);
  assert.doesNotMatch(html, /<kbd>G<\/kbd>/);
  assert.doesNotMatch(html, /<kbd>t<\/kbd>/);
  assert.doesNotMatch(html, /<kbd>h<\/kbd>/);
  // Shift+F (Finish review) is shared with code-review now — live mode
  // wires it up via live-mode.shortcut.handleShortcut.
  assert.match(html, /Finish review/);
  assert.match(html, /Shift<\/kbd>\+<kbd>F/);
  assert.doesNotMatch(html, /Toggle comments panel/);
  assert.doesNotMatch(html, /Switch scope/);
  // Shared bindings present
  assert.match(html, /<kbd>Esc<\/kbd>/);
  assert.match(html, /<kbd>\?<\/kbd>/);
  assert.match(html, /Ctrl<\/kbd>\+<kbd>Enter/);
  // Live-only binding present
  assert.match(html, /<kbd>p<\/kbd>/);
  assert.match(html, /Toggle pin mode/);
});

test('renderShortcutsPane: defaults to code-review when no opts passed', () => {
  const sp = loadShared();
  const pane = makePane();
  sp.renderShortcutsPane(pane);
  // Sanity: code-review-only entries appear (legacy behaviour preserved)
  assert.match(pane.innerHTML, /<kbd>j<\/kbd>/);
  assert.doesNotMatch(pane.innerHTML, /Toggle pin mode/);
});
