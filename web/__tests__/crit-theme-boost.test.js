'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const boost = require('../crit-theme-boost.js');

function tokenColors(theme) {
  const out = [];
  for (const rule of theme.settings || []) {
    if (rule.settings && rule.settings.foreground) out.push(rule.settings.foreground);
  }
  return out;
}

test('boosted copies of every bundled theme make all token colours readable on code backgrounds', async () => {
  const { bundledThemesInfo, bundledThemes, normalizeTheme } = await import('shiki');
  for (const info of bundledThemesInfo) {
    const theme = normalizeTheme((await bundledThemes[info.id]()).default);
    const before = JSON.stringify(theme);
    const copy = boost.boostTheme(theme, info.id + '--crit-boost');
    assert.equal(JSON.stringify(theme), before, `${info.id} original was mutated`);
    assert.equal(copy.name, info.id + '--crit-boost');
    const backgrounds = boost.codeBackgrounds(theme);
    const originals = tokenColors(theme);
    tokenColors(copy).forEach((value, i) => {
      const hex = boost.parse(value, backgrounds[0]);
      for (const bg of backgrounds) {
        assert.ok(boost.contrast(hex, bg) >= 4.5, `${info.id} token ${originals[i]} -> ${hex} on ${bg}`);
      }
      // Readable colours keep their exact value; others keep their hue.
      const original = boost.parse(originals[i], backgrounds[0]);
      if (backgrounds.every(bg => boost.contrast(original, bg) >= boost.TOKEN_CONTRAST)) {
        assert.equal(hex, original, `${info.id} readable token ${original} changed`);
      } else if (boost.oklch(original)[1] > 0.05 && boost.oklch(hex)[1] > 0.05) {
        const hue = Math.abs(boost.oklch(original)[2] - boost.oklch(hex)[2]) % 360;
        assert.ok(Math.min(hue, 360 - hue) < 12, `${info.id} ${original} -> ${hex} changed hue`);
      }
    });
  }
});

test('min-light comments get darker but stay grey; tokyo-night comments get lighter but stay blue', async () => {
  const { bundledThemes, normalizeTheme } = await import('shiki');
  const comment = theme => theme.settings.find(r => [].concat(r.scope || []).includes('comment') && r.settings && r.settings.foreground).settings.foreground;
  const light = normalizeTheme((await bundledThemes['min-light']()).default);
  const lightCopy = boost.boostTheme(light, 'min-light--crit-boost');
  assert.ok(boost.oklch(comment(lightCopy))[0] < boost.oklch(boost.parse(comment(light), '#ffffff'))[0]);
  const dark = normalizeTheme((await bundledThemes['tokyo-night']()).default);
  const darkCopy = boost.boostTheme(dark, 'tokyo-night--crit-boost');
  const [l0, , h0] = boost.oklch(boost.parse(comment(dark), '#000000'));
  const [l1, , h1] = boost.oklch(comment(darkCopy));
  assert.ok(l1 > l0);
  assert.ok(Math.abs(h1 - h0) < 12);
});

test('themes() registers each boosted theme once and leaves the pair alone when off', () => {
  const registered = [];
  const P = { registerCustomTheme: (name, loader) => registered.push([name, loader]), resolveTheme: () => Promise.resolve({}) };
  const pair = { light: 'min-light', dark: 'nord' };
  assert.deepEqual(boost.themes(P, pair, false), pair);
  assert.deepEqual(boost.themes(P, pair, true), { light: 'min-light--crit-boost', dark: 'nord--crit-boost' });
  boost.themes(P, pair, true);
  assert.deepEqual(registered.map(r => r[0]), ['min-light--crit-boost', 'nord--crit-boost']);
});
