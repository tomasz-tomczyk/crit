// Regression: app.js calls window.crit.shared.installSidebarResize(...) at
// init time (and uses other window.crit.shared.* helpers). crit-shared.js
// must be loaded AND parsed before app.js executes, otherwise:
//   TypeError: Cannot read properties of undefined (reading 'installSidebarResize')
//
// Two requirements for the non-design (code-review) branch in index.html:
//   1. crit-shared.js must actually be loaded (it's a dependency of app.js).
//   2. Dynamically-inserted <script> tags must set async=false to preserve
//      DOM execution order (the design branch already does this).

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');

const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');

function getElseBlock() {
  const elseMatch = html.match(/} else \{([\s\S]*?)\}\s*\)\(\)\;/);
  assert.ok(elseMatch, 'expected to find the non-design else { ... } branch');
  return elseMatch[1];
}

test('index.html non-design branch loads crit-shared.js (app.js depends on window.crit.shared)', () => {
  const elseBlock = getElseBlock();
  assert.match(
    elseBlock,
    /\.src\s*=\s*['"]crit-shared\.js['"]/,
    'crit-shared.js must be loaded in the non-design branch — app.js calls ' +
    'window.crit.shared.installSidebarResize and other shared helpers at init'
  );
});

test('index.html non-design branch sets async=false on dynamically inserted scripts', () => {
  const elseBlock = getElseBlock();

  // Find every `var X = document.createElement('script')` declaration in the
  // else block and assert each one is followed by an `X.async = false`.
  // Without this, dynamically-inserted scripts default to async=true and may
  // execute out of DOM order.
  const scriptDecls = [...elseBlock.matchAll(
    /var (\w+) = document\.createElement\('script'\);/g
  )].map(m => m[1]);

  assert.ok(scriptDecls.length > 0, 'expected at least one script tag in the non-design branch');

  for (const name of scriptDecls) {
    const asyncRe = new RegExp(`${name}\\.async\\s*=\\s*false`);
    assert.match(
      elseBlock,
      asyncRe,
      `script var "${name}" must set ${name}.async = false to preserve execution order`
    );
  }
});
