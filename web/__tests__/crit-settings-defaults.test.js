const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const SHARED_SRC = fs.readFileSync(path.join(__dirname, '..', 'crit-shared.js'), 'utf8');
const PALETTE_SRC = fs.readFileSync(path.join(__dirname, '..', 'crit-theme-palette.js'), 'utf8');
const SHORTCUTS_SRC = fs.readFileSync(path.join(__dirname, '..', 'crit-shortcuts.js'), 'utf8');
const PANES_SRC = fs.readFileSync(path.join(__dirname, '..', 'crit-settings-panes.js'), 'utf8');
const APP_SRC = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');
const LIVE_SRC = fs.readFileSync(path.join(__dirname, '..', 'live-mode.js'), 'utf8');

function makeSandbox({ settingsDefaults = {}, cookieObj = null, darkPreferred = true } = {}) {
  let cookieJar = '';
  if (cookieObj) {
    cookieJar = 'crit-settings=' + encodeURIComponent(JSON.stringify(cookieObj));
  }
  const attrs = new Map();
  const props = new Map();
  const rootEl = {
    dataset: {},
    setAttribute(k, v) { attrs.set(k, String(v)); },
    getAttribute(k) { return attrs.has(k) ? attrs.get(k) : null; },
    removeAttribute(k) { attrs.delete(k); },
    style: {
      setProperty(k, v) { props.set(k, String(v)); },
      getPropertyValue(k) { return props.get(k) || ''; },
      removeProperty(k) { props.delete(k); },
    },
  };
  const doc = {
    get cookie() { return cookieJar; },
    set cookie(v) {
      const part = String(v).split(';')[0];
      const eq = part.indexOf('=');
      if (eq === -1) return;
      const name = part.slice(0, eq).trim();
      const val = part.slice(eq + 1);
      const pairs = cookieJar ? cookieJar.split('; ').filter(Boolean) : [];
      const filtered = pairs.filter((p) => !p.startsWith(name + '='));
      filtered.push(name + '=' + val);
      cookieJar = filtered.join('; ');
    },
    documentElement: rootEl,
    head: { appendChild() {} },
    body: { classList: { toggle() {}, add() {}, remove() {} } },
    getElementById() { return null; },
    querySelector() { return null; },
    querySelectorAll() { return []; },
    createElement(tag) {
      return {
        tagName: tag.toUpperCase(),
        style: {},
        classList: { toggle() {}, add() {}, remove() {} },
        setAttribute() {},
        getAttribute() { return null; },
        addEventListener() {},
        appendChild() {},
      };
    },
  };
  const sandbox = {
    window: null,
    globalThis: null,
    document: doc,
    console,
    setTimeout,
    clearTimeout,
    setInterval,
    clearInterval,
    encodeURIComponent,
    decodeURIComponent,
    matchMedia: (q) => ({ matches: q.includes('light') ? !darkPreferred : darkPreferred, addEventListener() {} }),
    crit: {
      settingsDefaults,
      palettes: [
        { id: 'github-light', displayName: 'GitHub Light', type: 'light', colors: { bg: '#fff' } },
        { id: 'min-light', displayName: 'Min Light', type: 'light', colors: { bg: '#fafafa' } },
        { id: 'tokyo-night', displayName: 'Tokyo Night', type: 'dark', colors: { bg: '#1a1b26' } },
        { id: 'catppuccin-mocha', displayName: 'Catppuccin Mocha', type: 'dark', colors: { bg: '#1e1e2e' } },
      ],
    },
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  vm.createContext(sandbox);
  vm.runInContext(PALETTE_SRC, sandbox);
  vm.runInContext(SHARED_SRC, sandbox);
  vm.runInContext(SHORTCUTS_SRC, sandbox);
  return {
    sandbox,
    attrs,
    props,
    rootEl,
    readCookie() {
      const m = doc.cookie.match(/(?:^|;\s*)crit-settings=([^;]*)/);
      return m ? JSON.parse(decodeURIComponent(m[1])) : {};
    },
  };
}

test('getSetting precedence: cookie > window.crit.settingsDefaults > fallback', () => {
  const { sandbox } = makeSandbox({
    settingsDefaults: { theme: 'dark', width: 'wide', diffMode: 'unified', ignoreWhitespace: true },
    cookieObj: { width: 'compact' },
  });
  const shared = sandbox.crit.shared;

  // Cookie wins over config
  assert.equal(shared.getSetting('width', 'default'), 'compact');
  // Config wins over hardcoded fallback
  assert.equal(shared.getSetting('theme', 'system'), 'dark');
  assert.equal(shared.getSetting('diffMode', 'split'), 'unified');
  assert.equal(shared.getSetting('ignoreWhitespace', false), true);
  // Hardcoded fallback used when neither cookie nor config sets key
  assert.equal(shared.getSetting('inlineDiff', 'word'), 'word');
  assert.equal(shared.readThemeFromSettings(), 'dark');
});

test('crit-theme-palette.js applySaved reads theme and palettes from window.crit.settingsDefaults before cookie', () => {
  const { sandbox, rootEl, props } = makeSandbox({
    settingsDefaults: { theme: 'light', lightPalette: 'min-light', darkPalette: 'catppuccin-mocha' },
    darkPreferred: true,
  });

  const applied = sandbox.crit.themePalette.applySaved();
  assert.equal(applied.id, 'min-light');
  assert.equal(props.get('--crit-palette-bg'), '#fafafa');
  assert.equal(rootEl.dataset.critPalette, 'min-light');

  // Cookie overrides individual palette without losing config theme
  sandbox.crit.shared.setSetting('lightPalette', 'github-light');
  const overridden = sandbox.crit.themePalette.applySaved();
  assert.equal(overridden.id, 'github-light');
  assert.equal(props.get('--crit-palette-bg'), '#fff');
});

test('shortcuts merge per binding (config under cookie) and setBinding never stamps untouched config bindings into cookie', () => {
  const { sandbox, readCookie } = makeSandbox({
    settingsDefaults: {
      shortcuts: {
        next_block: 'ArrowDown',
        previous_block: 'ArrowUp',
      },
    },
    cookieObj: {
      shortcuts: {
        next_block: 'Alt+j',
      },
    },
  });
  const shortcuts = sandbox.crit.shortcuts;
  const shared = sandbox.crit.shared;

  // Cookie overrides next_block while config still supplies previous_block
  assert.equal(shortcuts.getBinding('next_block'), 'Alt+j');
  assert.equal(shortcuts.getBinding('previous_block'), 'ArrowUp');
  assert.deepEqual(Object.assign({}, shared.getSetting('shortcuts', {})), {
    next_block: 'Alt+j',
    previous_block: 'ArrowUp',
  });

  // Changing another binding only writes cookie overrides, not config bindings
  assert.equal(shortcuts.setBinding('toggle_toc', 'Alt+m'), true);
  assert.deepEqual(readCookie().shortcuts, { next_block: 'Alt+j', toggle_toc: 'Alt+m' });
  assert.equal(shortcuts.getBinding('previous_block'), 'ArrowUp');

  // Setting next_block back to its config default deletes it from the cookie
  assert.equal(shortcuts.setBinding('next_block', 'ArrowDown'), true);
  assert.deepEqual(readCookie().shortcuts, { toggle_toc: 'Alt+m' });
  assert.equal(shortcuts.getBinding('next_block'), 'ArrowDown');

  // Setting previous_block back to built-in default ('k') overrides config in cookie
  assert.equal(shortcuts.setBinding('previous_block', 'k'), true);
  assert.equal(shortcuts.getBinding('previous_block'), 'k');
  assert.equal(shortcuts.isCustomized('previous_block'), false);

  // resetAll clears cookie overrides so config defaults take effect again
  shortcuts.resetAll();
  assert.deepEqual(readCookie().shortcuts, {});
  assert.equal(shortcuts.getBinding('next_block'), 'ArrowDown');
  assert.equal(shortcuts.getBinding('previous_block'), 'ArrowUp');
  assert.equal(shortcuts.getBinding('toggle_toc'), 't');
});

test('resetConfigurableSettings clears only the 20 configurable keys and preserves unrelated cookie keys', () => {
  const { sandbox, readCookie } = makeSandbox({
    settingsDefaults: { theme: 'dark', width: 'wide' },
    cookieObj: {
      theme: 'light',
      lightPalette: 'min-light',
      darkPalette: 'catppuccin-mocha',
      boostContrast: 'on',
      codeFont: 'monospace',
      lineNumbers: 'off',
      codeOverflow: 'wrap',
      width: 'compact',
      diffMode: 'unified',
      inlineDiff: 'char',
      ignoreWhitespace: true,
      unchangedContext: 'all',
      diffScope: 'unstaged',
      hideResolved: true,
      live_hideResolved: true,
      live_viewport: 'mobile',
      fileTree: 'collapsed',
      toc: 'open',
      commentsPanel: 'open',
      shortcuts: { next_block: 'n' },
      // Unrelated keys that MUST be preserved:
      customCSS: false,
      fileTreeWidth: 320,
      commentsPanelWidth: 360,
      copyPromptOnFinish: true,
      selectionToolbarEnabled: false,
    },
  });
  const shared = sandbox.crit.shared;

  assert.equal(shared.CONFIGURABLE_SETTINGS.length, 20);
  assert.equal(shared.resetConfigurableSettings(), true);

  assert.deepEqual(readCookie(), {
    customCSS: false,
    fileTreeWidth: 320,
    commentsPanelWidth: 360,
    copyPromptOnFinish: true,
    selectionToolbarEnabled: false,
  });
  // Config defaults now apply again
  assert.equal(shared.getSetting('theme', 'system'), 'dark');
  assert.equal(shared.getSetting('width', 'default'), 'wide');
  // Calling again when nothing configurable is in the cookie is a no-op
  assert.equal(shared.resetConfigurableSettings(), false);
});

test('renderSettingsTab wires #resetSettingsBtn to clear configurable keys and invoke hooks.onResetSettings', () => {
  const { sandbox, readCookie } = makeSandbox({
    settingsDefaults: { theme: 'dark' },
    cookieObj: { theme: 'light', width: 'compact', fileTreeWidth: 300 },
  });
  let toastMsg = null;
  sandbox.crit.shared.showToast = (msg) => { toastMsg = msg; };
  vm.runInContext(PANES_SRC, sandbox);

  let html = '';
  const listeners = new Map();
  const resetBtn = {
    addEventListener(ev, fn) { listeners.set(ev, fn); },
  };
  const pane = {
    set innerHTML(v) { html = v; },
    get innerHTML() { return html; },
    querySelector(sel) {
      if (sel === '#resetSettingsBtn') return resetBtn;
      return null;
    },
    querySelectorAll() { return []; },
  };

  let resetHookCalled = false;
  sandbox.crit.settingsPanes.renderSettingsTab(pane, {
    mode: 'code-review',
    cfg: { global_config_path: '~/.crit.config.json' },
    hooks: {
      onResetSettings: () => { resetHookCalled = true; },
    },
  });

  assert.match(html, /id="resetSettingsBtn"/);
  assert.match(html, /Reset to defaults/);
  assert.equal(typeof listeners.get('click'), 'function');

  listeners.get('click')();

  assert.equal(resetHookCalled, true);
  assert.equal(toastMsg, 'Settings reset to defaults');
  assert.deepEqual(readCookie(), { fileTreeWidth: 300 });
  assert.equal(sandbox.crit.shared.getSetting('theme', 'system'), 'dark');
});

test('initial page hydration does not persist config defaults into the crit-settings cookie', () => {
  assert.match(APP_SRC, /applyTheme\(saved,\s*\{\s*persist:\s*false\s*\}\)/);
  assert.match(APP_SRC, /applyWidth\(saved,\s*\{\s*persist:\s*false\s*\}\)/);
  assert.match(APP_SRC, /setFileTreeCollapsed\(getSetting\('fileTree',\s*'open'\)\s*===\s*'collapsed',\s*false,\s*\{\s*persist:\s*false\s*\}\)/);
  assert.match(APP_SRC, /setCommentsPanelCollapsed\(getSetting\('commentsPanel',\s*'collapsed'\)\s*!==\s*'open'\)/);
  assert.match(LIVE_SRC, /applyViewport\(initial,\s*\{\s*persist:\s*false\s*\}\)/);
});
