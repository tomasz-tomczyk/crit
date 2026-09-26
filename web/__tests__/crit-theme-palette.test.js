'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { pathToFileURL } = require('node:url');
const path = require('node:path');
const palette = require('../crit-theme-palette.js');

const paletteModule = pathToFileURL(path.resolve(__dirname, '../../scripts/crit-theme-palette.mjs')).href;

function contrast(a, b) {
  const luminance = hex => {
    const rgb = [1, 3, 5].map(i => {
      const value = parseInt(hex.slice(i, i + 2), 16) / 255;
      return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
    });
    return rgb[0] * 0.2126 + rgb[1] * 0.7152 + rgb[2] * 0.0722;
  };
  return (Math.max(luminance(a), luminance(b)) + 0.05) / (Math.min(luminance(a), luminance(b)) + 0.05);
}

function mixHex(a, b, share) {
  const ch = hex => [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16));
  const x = ch(a), y = ch(b);
  return '#' + x.map((v, i) => Math.round(v * share + y[i] * (1 - share)).toString(16).padStart(2, '0')).join('');
}

// OKLCH chroma: how colourful a colour is, independent of lightness.
function chroma(hex) {
  const [r, g, b] = [1, 3, 5].map(i => {
    const value = parseInt(hex.slice(i, i + 2), 16) / 255;
    return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  });
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  return Math.hypot(1.9779984951 * l - 2.4285922050 * m + 0.4505937099 * s, 0.0259040371 * l + 0.7827717662 * m - 0.8086757660 * s);
}

test('normalizes all bundled Shiki themes into complete UI palettes without changing token data', async () => {
  const { bundledThemesInfo, bundledThemes, normalizeTheme } = await import('shiki');
  const { themePalette, distance, AUTHOR_MIN_DISTANCE } = await import(paletteModule);
  assert.ok(bundledThemesInfo.length > 0);

  const requiredRoles = [
    'bg', 'fg', 'accent', 'on-accent', 'muted', 'border', 'surface', 'elevated',
    'red', 'green', 'yellow', 'orange', 'purple', 'blue', 'cyan', 'shadow', 'overlay',
    'author-0', 'author-1', 'author-2', 'author-3', 'author-4', 'author-5',
  ];
  for (const info of bundledThemesInfo) {
    const theme = normalizeTheme((await bundledThemes[info.id]()).default);
    const tokenSnapshot = structuredClone(theme.settings);
    const colorsSnapshot = structuredClone(theme.colors);
    const result = themePalette({ ...theme, name: info.id, displayName: info.displayName });

    assert.equal(result.id, info.id);
    assert.equal(result.type, theme.type);
    assert.deepEqual(Object.keys(result.colors).sort(), [...requiredRoles].sort(), info.id);
    for (const role of requiredRoles) {
      assert.match(result.colors[role], /^(#[\da-f]{6}|rgba\()/i, `${info.id}.${role}`);
    }
    // Preserve opaque editor colours, including shorthand, in canonical form.
    const opaque = value => typeof value === 'string' && /^#(?:[\da-f]{3}|[\da-f]{6})$/i.test(value);
    const canonical = value => (value.length === 4 ? '#' + [...value.slice(1)].map(digit => digit + digit).join('') : value).toLowerCase();
    if (opaque(theme.bg)) assert.equal(result.colors.bg, canonical(theme.bg), info.id);
    for (const role of ['fg', 'muted', 'accent', 'red', 'green', 'yellow', 'orange', 'purple', 'blue', 'cyan',
      'author-0', 'author-1', 'author-2', 'author-3', 'author-4', 'author-5']) {
      for (const background of ['bg', 'surface', 'elevated']) {
        assert.ok(contrast(result.colors[role], result.colors[background]) >= 4.5, `${info.id}.${role} on ${background}`);
      }
    }
    // Card and file borders: neutral, faint, and visible on every surface.
    // Theme panel borders can be an accent colour or match the background.
    const borderMax = /high-contrast/.test(info.id) ? 2.6 : 1.8;
    const onBg = contrast(result.colors.border, result.colors.bg);
    assert.ok(onBg >= 1.4 && onBg <= borderMax, `${info.id}.border contrast ${onBg.toFixed(2)} on bg`);
    for (const background of ['surface', 'elevated']) {
      assert.ok(contrast(result.colors.border, result.colors[background]) >= 1.15, `${info.id}.border on ${background}`);
    }
    assert.ok(chroma(result.colors.border) - chroma(result.colors.bg) <= 0.03, `${info.id}.border is tinted`);
    // Author tags: readable on their own tint, and no two look alike.
    const authors = [0, 1, 2, 3, 4, 5].map(i => result.colors['author-' + i]);
    for (const tag of authors) {
      const tint = mixHex(tag, result.colors.elevated, 0.15);
      assert.ok(contrast(tag, tint) >= 4.5, `${info.id} author tag ${tag} on its tint`);
    }
    for (let i = 0; i < authors.length; i++) {
      for (let j = i + 1; j < authors.length; j++) {
        assert.ok(distance(authors[i], authors[j]) >= AUTHOR_MIN_DISTANCE, `${info.id} author-${i}/author-${j} look alike`);
      }
    }
    assert.deepEqual(theme.settings, tokenSnapshot, `${info.id} token settings were mutated`);
    assert.deepEqual(theme.colors, colorsSnapshot, `${info.id} theme colors were mutated`);
  }
});

test('contrasting VS Code sidebars do not invert Crit cards (Slack Ochin regression)', async () => {
  const { bundledThemes, normalizeTheme } = await import('shiki');
  const { themePalette } = await import(paletteModule);
  const theme = normalizeTheme((await bundledThemes['slack-ochin']()).default);
  const { colors } = themePalette(theme);
  assert.equal(colors.bg, '#ffffff');
  assert.equal(colors.fg, '#002339');
  assert.notEqual(colors.surface, theme.colors['sideBar.background'].toLowerCase());
  assert.equal(colors.surface, '#f6f7f8');
  assert.equal(colors.elevated, '#edf0f1');
  const withoutSidebar = themePalette({ ...theme, colors: { ...theme.colors, 'sideBar.background': '#ff0000', 'input.background': '#00ff00' } });
  assert.deepEqual(withoutSidebar.colors, colors);
});

test('short hex and alpha colours are expanded and composited on the editor', async () => {
  const { themePalette } = await import(paletteModule);
  const { colors } = themePalette({ name: 'short-alpha', type: 'light', bg: '#fff', fg: '#123', colors: {
    'textLink.foreground': '#123f', 'terminal.ansiRed': '#112233ff',
  } });
  assert.equal(colors.bg, '#ffffff');
  assert.equal(colors.fg, '#112233');
  assert.equal(colors.accent, '#112233');
  assert.equal(colors.red, '#112233');
  const transparent = themePalette({ name: 'transparent', type: 'light', bg: '#0000', fg: '#00000080' });
  assert.equal(transparent.colors.bg, '#ffffff');
  assert.match(transparent.colors.fg, /^#[\da-f]{6}$/);
  assert.notEqual(transparent.colors.fg, '#000000');
});

test('sparse themes use mode-aware usable defaults', async () => {
  const { themePalette } = await import(paletteModule);
  for (const [type, bg, fg] of [
    ['light', '#ffffff', '#24292f'],
    ['dark', '#1a1b26', '#c0caf5'],
  ]) {
    const result = themePalette({ name: `sparse-${type}`, type });
    assert.equal(result.colors.bg, bg);
    assert.equal(result.colors.fg, fg);
    for (const [role, value] of Object.entries(result.colors)) {
      assert.match(value, /^(#[\da-f]{6}|rgba\()/i, `${type}.${role}`);
    }
  }
});

test('pair filters selected palettes by mode and falls back to defaults', () => {
  const themes = [
    { id: 'bright', type: 'light' },
    { id: 'midnight', type: 'dark' },
  ];
  assert.deepEqual(palette.pair({ lightPalette: 'midnight', darkPalette: 'bright' }, themes), {
    light: palette.DEFAULTS.light,
    dark: palette.DEFAULTS.dark,
  });
  assert.deepEqual(palette.pair({}, themes), {
    light: palette.DEFAULTS.light,
    dark: palette.DEFAULTS.dark,
  });
  assert.deepEqual(palette.pair({ lightPalette: 'bright', darkPalette: 'midnight' }, themes), {
    light: 'bright',
    dark: 'midnight',
  });
});

test('apply selects explicit and system light/dark modes on a fake root', () => {
  const themes = [
    { id: 'paper', type: 'light', colors: { bg: '#ffffff', fg: '#222222', accent: '#0066cc' } },
    { id: 'ink', type: 'dark', colors: { bg: '#111111', fg: '#eeeeee', accent: '#6699ff' } },
  ];
  function fakeRoot() {
    const values = new Map();
    return {
      dataset: {},
      style: {
        colorScheme: '',
        setProperty(key, value) { values.set(key, value); },
      },
      values,
    };
  }

  for (const [settings, systemLight, id, mode] of [
    [{ theme: 'light', lightPalette: 'paper', darkPalette: 'ink' }, false, 'paper', 'light'],
    [{ theme: 'dark', lightPalette: 'paper', darkPalette: 'ink' }, true, 'ink', 'dark'],
    [{ theme: 'system', lightPalette: 'paper', darkPalette: 'ink' }, true, 'paper', 'light'],
    [{ theme: 'system', lightPalette: 'paper', darkPalette: 'ink' }, false, 'ink', 'dark'],
  ]) {
    const root = fakeRoot();
    const selected = palette.apply(settings, themes, root, systemLight);
    assert.equal(selected.id, id);
    assert.equal(root.dataset.critPalette, id);
    assert.equal(root.style.colorScheme, mode);
    for (const [role, value] of Object.entries(selected.colors)) {
      assert.equal(root.values.get(`--crit-palette-${role}`), value);
    }
  }
});
