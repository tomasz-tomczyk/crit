const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const css = fs.readFileSync(path.join(__dirname, '..', 'theme.css'), 'utf8');

// R2 + Phase C + Phase D tokens.
const REQUIRED_VARS = [
  '--crit-design-iframe-frame',
  '--crit-design-iframe-bg',
  '--crit-design-composer-bg',
  '--crit-design-composer-border',
  '--crit-design-composer-input-bg',
  '--crit-design-composer-error-fg',
  '--crit-design-ancestor-menu-bg',
  '--crit-design-ancestor-menu-fg',
  '--crit-design-ancestor-menu-hover-bg',
  '--crit-design-mode-btn-active-bg',
  '--crit-design-mode-btn-active-fg',
  // Phase D markers + re-anchor
  '--crit-design-marker-bg',
  '--crit-design-marker-fg',
  '--crit-design-marker-border',
  '--crit-design-marker-shadow',
  '--crit-design-marker-focus-ring',
  '--crit-design-reanchor-active-outline',
];

const BLOCKS = [
  { name: ':root',                              re: /:root\s*\{([\s\S]*?)\n\}/m },
  { name: 'prefers-color-scheme: light',        re: /@media \(prefers-color-scheme: light\)\s*\{[\s\S]*?html:not\(\[data-theme\]\)\s*\{([\s\S]*?)\n\s*\}/ },
  { name: '[data-theme="dark"]',                re: /\[data-theme="dark"\]\s*\{([\s\S]*?)\n\}/ },
  { name: '[data-theme="light"]',               re: /\[data-theme="light"\]\s*\{([\s\S]*?)\n\}/ },
];

for (const b of BLOCKS) {
  test(`block ${b.name} defines all --crit-design-* vars`, () => {
    const m = b.re.exec(css);
    assert.ok(m, 'block not found: ' + b.name);
    const body = m[1];
    for (const v of REQUIRED_VARS) {
      assert.match(body, new RegExp(v + '\\s*:'), `missing ${v} in ${b.name}`);
    }
  });
}
