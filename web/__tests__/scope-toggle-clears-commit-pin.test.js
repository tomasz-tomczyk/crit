'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const appJs = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');

function scopeToggleHandler() {
  const start = appJs.indexOf("document.getElementById('scopeToggle').addEventListener('click'");
  assert.notEqual(start, -1, 'scope toggle click handler must exist');
  const bodyStart = appJs.indexOf('{', appJs.indexOf('=>', start));
  let depth = 0;
  for (let i = bodyStart; i < appJs.length; i++) {
    if (appJs[i] === '{') depth++;
    if (appJs[i] === '}') {
      depth--;
      if (depth === 0) return appJs.slice(bodyStart, i + 1);
    }
  }
  throw new Error('could not extract scope toggle handler');
}

const handler = scopeToggleHandler();

test('scope toggle clears any pinned commit', () => {
  assert.match(
    handler,
    /clearCommitPins\(\)/,
    'every scope click must clear the commit pin, otherwise the pinned commit wins server-side'
  );
  assert.doesNotMatch(
    handler,
    /scope\s*!==\s*'all'\s*&&\s*scope\s*!==\s*'branch'[\s\S]*clearCommitPins/,
    'clearCommitPins must not be restricted to staged/unstaged'
  );
});

test('clicking the already-active scope still releases a pinned commit', () => {
  assert.doesNotMatch(
    handler,
    /if \(!btn \|\| btn\.disabled \|\| btn\.classList\.contains\('active'\)\) return;/,
    'an early return on the active scope makes the click a no-op while a commit is pinned'
  );
  assert.match(
    handler,
    /btn\.classList\.contains\('active'\)\s*&&\s*!diffCommit/,
    'the active-scope early return must be skipped while a commit is pinned'
  );
});

test('a pinned commit marks the scope toggle so the dead state is visible', () => {
  assert.match(appJs, /function updateScopePinIndicator\(\)/);
  assert.match(
    appJs,
    /updateScopePinIndicator\(\);\s*\}\s*\n\n  function updateScopePinIndicator/,
    'recomputeDiffCommit must refresh the indicator on every path'
  );
  assert.match(appJs, /classList\.toggle\('commit-pinned', pinned\)/);
});

test('the pinned marker reuses theme variables', () => {
  const css = fs.readFileSync(path.join(__dirname, '..', 'style.css'), 'utf8');
  const rule = css.match(/\.scope-toggle\.commit-pinned\s*\{[^}]*\}/);
  assert.ok(rule, '.scope-toggle.commit-pinned rule must exist');
  assert.doesNotMatch(rule[0], /#[0-9a-f]{3,8}\b/i, 'use theme variables, not literals');
});