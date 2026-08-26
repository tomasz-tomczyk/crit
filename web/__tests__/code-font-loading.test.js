'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

test('code-review settings load code fonts lazily from the dedicated endpoint', () => {
  const webDir = path.join(__dirname, '..');
  const app = fs.readFileSync(path.join(webDir, 'app.js'), 'utf8');
  const live = fs.readFileSync(path.join(webDir, 'live-mode.js'), 'utf8');

  assert.match(app, /fetch\('\/api\/code-fonts'\)/);
  assert.match(app, /if \(!r\.ok\) throw new Error\('Could not load code fonts'\)/);
  assert.doesNotMatch(live, /code-fonts/);
  assert.doesNotMatch(live, /applyCodeFont/);
});
