const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const css = fs.readFileSync(path.join(__dirname, '..', 'theme.css'), 'utf8');

// R2 + Phase C + Phase D tokens.
const REQUIRED_VARS = [
  '--crit-live-iframe-frame',
  '--crit-live-iframe-bg',
  '--crit-live-composer-bg',
  '--crit-live-composer-border',
  '--crit-live-composer-input-bg',
  '--crit-live-composer-error-fg',
  '--crit-live-ancestor-menu-bg',
  '--crit-live-ancestor-menu-fg',
  '--crit-live-ancestor-menu-hover-bg',
  // Phase D markers + re-anchor
  '--crit-live-marker-bg',
  '--crit-live-marker-fg',
  '--crit-live-marker-border',
  '--crit-live-marker-shadow',
  '--crit-live-marker-focus-ring',
  '--crit-live-reanchor-active-outline',
];

const BLOCKS = [
  { name: ':root',                              re: /:root\s*\{([\s\S]*?)\n\}/m },
  { name: 'prefers-color-scheme: light',        re: /@media \(prefers-color-scheme: light\)\s*\{[\s\S]*?html:not\(\[data-theme\]\)\s*\{([\s\S]*?)\n\s*\}/ },
  { name: '[data-theme="dark"]',                re: /\[data-theme="dark"\]\s*\{([\s\S]*?)\n\}/ },
  { name: '[data-theme="light"]',               re: /\[data-theme="light"\]\s*\{([\s\S]*?)\n\}/ },
];

for (const b of BLOCKS) {
  test(`block ${b.name} defines all --crit-live-* vars`, () => {
    const m = b.re.exec(css);
    assert.ok(m, 'block not found: ' + b.name);
    const body = m[1];
    for (const v of REQUIRED_VARS) {
      assert.match(body, new RegExp(v + '\\s*:'), `missing ${v} in ${b.name}`);
    }
  });
}
