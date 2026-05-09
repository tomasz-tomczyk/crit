'use strict';
// Regression: drift was a code-review concept; design mode no longer
// surfaces drifted-pin UI. The drift-tray module, its index.html script
// tag, and design-mode's drift-tray host must all be gone so a future
// rebase can't quietly resurrect the dead surface area.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const ROOT = path.resolve(__dirname, '..');

test('design-mode-drift-tray.js module file is removed', () => {
  assert.equal(
    fs.existsSync(path.join(ROOT, 'design-mode-drift-tray.js')),
    false,
    'design-mode-drift-tray.js should be deleted',
  );
});

test('index.html does not load design-mode-drift-tray.js', () => {
  const html = fs.readFileSync(path.join(ROOT, 'index.html'), 'utf8');
  assert.equal(
    html.includes('design-mode-drift-tray'),
    false,
    'index.html should not reference design-mode-drift-tray',
  );
});

test('design-mode.js no longer wires a drift tray host or PUT path', () => {
  const src = fs.readFileSync(path.join(ROOT, 'design-mode.js'), 'utf8');
  // Tray rendering surface — host element + module lookup.
  assert.equal(src.includes('crit-design-drifted-tray-host'), false,
    'drift tray host element should be removed');
  assert.equal(src.includes('renderDriftTray'), false,
    'renderDriftTray function should be removed');
  assert.equal(src.includes('driftTray'), false,
    'driftTray module reference should be removed');
  // Client-side drift PUT — daemon no longer sets the bit, so the
  // route-change scan path that PUTs `drifted_on_round` must go too.
  assert.equal(src.includes('drifted_on_round'), false,
    'design-mode.js should no longer PUT drifted_on_round');
});
