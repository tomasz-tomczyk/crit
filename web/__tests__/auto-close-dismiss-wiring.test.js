'use strict';
// Regression: dismissing the waiting overlay ("Back to editing", backdrop)
// must call clearAutoCloseTimers — otherwise the Approve countdown keeps
// running and closes the tab while the reviewer is editing again.

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const appJs = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');
const liveJs = fs.readFileSync(path.join(__dirname, '..', 'live-mode.js'), 'utf8');

test('setUIState("reviewing") clears any auto-close countdown', () => {
  const start = appJs.indexOf('function setUIState(state)');
  assert.ok(start >= 0, 'app.js setUIState must exist');
  assert.match(
    appJs.slice(start, start + 600),
    /state === 'reviewing'[\s\S]*?clearAutoCloseTimers\(\)/,
    'app.js: dismissing the waiting overlay must stop the countdown'
  );

  const liveStart = liveJs.indexOf('function setUIState(s)');
  assert.ok(liveStart >= 0, 'live-mode.js setUIState must exist');
  assert.match(
    liveJs.slice(liveStart, liveStart + 600),
    /s === 'reviewing'[\s\S]*?clearAutoCloseTimers\(\)/,
    'live-mode.js: dismissing the waiting overlay must stop the countdown'
  );
});
