'use strict';
// Regression: #701 — crit pr showed correct line counts but "No changes"
// because diffScope defaulted to "branch" while range focus diffs are pinned
// to BaseSHA..HeadSHA (working-tree scopes return empty hunks).

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const appJs = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');

function extractFunction(name) {
  const start = appJs.indexOf(`function ${name}(`);
  assert.notEqual(start, -1, `${name} must exist`);
  const bodyStart = appJs.indexOf('{', start);
  let depth = 0;
  for (let i = bodyStart; i < appJs.length; i++) {
    if (appJs[i] === '{') depth++;
    if (appJs[i] === '}') {
      depth--;
      if (depth === 0) return appJs.slice(start, i + 1);
    }
  }
  throw new Error(`could not extract ${name}`);
}

const loadSingleFile = new Function(`
  let diffCommit = '';
  let ignoreWhitespace = false;
  const session = { mode: 'git' };
  function enc(value) { return value; }
  function preHighlightFile() { return null; }
  function langFromPath() { return ''; }
  function buildCodeLineBlocks() { return []; }
  function parseMarkdown() { return { blocks: [], tocItems: [] }; }
  ${extractFunction('loadSingleFile').replace(/^function /, 'async function ')}
  return loadSingleFile;
`)();

async function loadWithDiff(diff) {
  const originalFetch = global.fetch;
  global.fetch = async function(url) {
    if (url.startsWith('/api/file?')) return { ok: true, json: async function() { return { content: 'worktree' }; } };
    if (url.startsWith('/api/file/comments')) return { ok: true, json: async function() { return []; } };
    return { ok: true, json: async function() { return diff; } };
  };
  try {
    return await loadSingleFile({ path: 'main.go', status: 'modified', file_type: 'code' }, 'staged');
  } finally {
    global.fetch = originalFetch;
  }
}

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

test('scoped diff content overrides worktree content for context expansion', async () => {
  assert.equal((await loadWithDiff({ hunks: [], content: 'index' })).content, 'index');
  assert.equal((await loadWithDiff({ hunks: [] })).content, 'worktree');
  assert.equal((await loadWithDiff({ hunks: [], content: '' })).content, '');
});
