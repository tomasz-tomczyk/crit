'use strict';
// Source-contract tests for story mode + lazy file loading (#869).
// Large reviews mark most files lazy (empty diffHunks until opened). Story
// chapters must hydrate those diffs before claiming hunks are gone.

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const appSrc = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');

function sliceFunction(src, name, nextName) {
  const start = src.indexOf('function ' + name);
  assert.notEqual(start, -1, name + ' must exist');
  const end = nextName ? src.indexOf('function ' + nextName, start + 1) : start + 2500;
  assert.notEqual(end, -1, nextName + ' must follow ' + name);
  return src.slice(start, end);
}

test('ensureStoryLazyFile hydrates a lazy file via loadSingleFile', () => {
  assert.match(appSrc, /function ensureStoryLazyFile\s*\(/);
  const body = sliceFunction(appSrc, 'ensureStoryLazyFile', 'cloneFileForHunks');
  assert.match(body, /loadSingleFile\s*\(/);
  // Story-visible loads must use the same scope as eager file hydration.
  assert.match(body, /currentFileDataScope\s*\(/);
  assert.doesNotMatch(body, /effectiveDiffScope\s*\(/);
  assert.match(body, /file\.lazy\s*=\s*false/);
  assert.match(body, /file\.diffHunks\s*=\s*loaded\.diffHunks/);
  // Cached story clones keyed by empty lazy placeholders must not stick.
  assert.match(body, /storyExpandedFileCache/);
});

test('renderStoryFileGroup loads lazy files instead of claiming hunks are gone', () => {
  const body = sliceFunction(appSrc, 'renderStoryFileGroup', 'storySupportReasonForFile');
  assert.match(body, /ensureStoryLazyFile\s*\(/);
  // Lazy placeholders show a loading state; true drift keeps the old copy.
  assert.match(
    body,
    /file\.lazy[\s\S]{0,120}Loading diff[\s\S]{0,80}These hunks are no longer in the diff\./,
  );
  assert.match(body, /ensureStoryLazyFile\(file\)\.then/);
  // After hydrate, refresh nav / hide-resolved / mermaid like renderStoryFileByPath.
  assert.match(body, /replaceWith\(replacement\);\s*renderMermaidBlocks\(\);\s*rebuildNavList\(\);\s*applyHideResolved\(\);\s*renderStoryRail\(\);/s);
});