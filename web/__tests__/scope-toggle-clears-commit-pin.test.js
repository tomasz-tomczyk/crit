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

function functionBody(name) {
  const start = appJs.indexOf('function ' + name + '(');
  assert.notEqual(start, -1, name + ' must exist');
  const bodyStart = appJs.indexOf('{', start);
  let depth = 0;
  for (let i = bodyStart; i < appJs.length; i++) {
    if (appJs[i] === '{') depth++;
    if (appJs[i] === '}') {
      depth--;
      if (depth === 0) return appJs.slice(bodyStart, i + 1);
    }
  }
  throw new Error('could not extract ' + name);
}

test('a pinned commit marks the scope toggle so the dead state is visible', () => {
  assert.match(
    functionBody('recomputeDiffCommit'),
    /updateScopePinIndicator\(\);\s*\}$/,
    'recomputeDiffCommit must refresh the indicator after every branch'
  );
  const indicator = functionBody('updateScopePinIndicator');
  assert.match(indicator, /classList\.toggle\('commit-pinned', pinned\)/);
  assert.match(indicator, /\.toggle-btn/, 'the hint goes on the buttons, whose own titles win');
});

test('the scope shortcut releases a pinned commit on the active scope too', () => {
  assert.match(
    appJs,
    /btn && !btn\.disabled && \(!btn\.classList\.contains\('active'\) \|\| diffCommit\)/
  );
});

test('file diffs send scope=all so they match the live file list', () => {
  const body = functionBody('loadSingleFile');
  assert.doesNotMatch(body, /scope !== 'all'/, '"all" without a scope param is served from the round snapshot');
  assert.match(body, /if \(scope\) \{\s*diffUrl \+= '&scope=' \+ enc\(scope\);/);
  assert.match(body, /fileHash: diffRes\.file_hash \|\| fileRes\.file_hash/);
});

test('the pinned marker reuses theme variables', () => {
  const css = fs.readFileSync(path.join(__dirname, '..', 'style.css'), 'utf8');
  const rule = css.match(/\.scope-toggle\.commit-pinned\s*\{[^}]*\}/);
  assert.ok(rule, '.scope-toggle.commit-pinned rule must exist');
  assert.doesNotMatch(rule[0], /#[0-9a-f]{3,8}\b/i, 'use theme variables, not literals');
});
