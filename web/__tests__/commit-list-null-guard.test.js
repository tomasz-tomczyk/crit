'use strict';
// Regression: #692 — /api/commits JSON null left commitList null; clearCommitPins
// during file-changed then threw in normalizeCommitPins and stuck the Waiting overlay.

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const appJs = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');

test('normalizeCommitPins coalesces null commitList before map', () => {
  const fnStart = appJs.indexOf('function normalizeCommitPins()');
  assert.ok(fnStart >= 0, 'normalizeCommitPins must exist');
  const fnBody = appJs.slice(fnStart, fnStart + 400);
  assert.match(
    fnBody,
    /if\s*\(\s*!commitList\s*\)\s*commitList\s*=\s*\[\s*\]/,
    'normalizeCommitPins must guard null commitList before .map'
  );
});

test('fetchCommits coalesces null JSON response to empty array', () => {
  assert.match(
    appJs,
    /commitList\s*=\s*\(\s*await\s+res\.json\(\)\s*\)\s*\|\|\s*\[\s*\]/,
    'fetchCommits must not assign null to commitList'
  );
});
