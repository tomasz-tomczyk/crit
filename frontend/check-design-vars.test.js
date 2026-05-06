const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const css = fs.readFileSync(path.join(__dirname, 'theme.css'), 'utf8');

// R2: only --crit-design-iframe-frame and --crit-design-iframe-bg.
// Marker tokens deferred to Phase D; everything else reuses existing tokens.
const REQUIRED_VARS = [
  '--crit-design-iframe-frame',
  '--crit-design-iframe-bg',
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
