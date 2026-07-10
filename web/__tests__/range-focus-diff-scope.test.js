'use strict';
// Regression: #701 — crit pr showed correct line counts but "No changes"
// because diffScope defaulted to "branch" while range focus diffs are pinned
// to BaseSHA..HeadSHA (working-tree scopes return empty hunks).

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const appJs = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');

test('range focus forces diffScope to all on init', () => {
  assert.match(
    appJs,
    /inRangeFocus\)\s*\{\s*diffScope\s*=\s*'all'/s,
    'init must reset diffScope when session is in range focus'
  );
});

test('file diff loads use the story-aware file data scope helper', () => {
  assert.match(
    appJs,
    /function effectiveDiffScope\(\)\s*\{\s*return sessionInRangeFocus\(\) \? 'all' : diffScope;/,
    'effectiveDiffScope must bypass working-tree scope in range focus'
  );
  assert.match(
    appJs,
    /loadAllFileData\(session\.files[^)]*currentFileDataScope\(\)/,
    'loadAllFileData must use currentFileDataScope so story and range scopes agree'
  );
});
