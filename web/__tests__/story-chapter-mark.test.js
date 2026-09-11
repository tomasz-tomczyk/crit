'use strict';
// Source-contract tests for the story chapter mark button (#fix/story-chapter-mark-button).
// Bug: per-file Viewed checkboxes only called toggleViewed + renderStoryRail,
// leaving `.crit-story-chapter__mark` stale (wrong label + stale click polarity).

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const appSrc = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');

function sliceFunction(src, name, nextName) {
  const start = src.indexOf('function ' + name);
  assert.notEqual(start, -1, name + ' must exist');
  const end = nextName ? src.indexOf('function ' + nextName, start + 1) : start + 8000;
  assert.notEqual(end, -1, nextName + ' must follow ' + name);
  return src.slice(start, end);
}

test('updateStoryMarkButton helper exists and syncs label from storyPageProgress', () => {
  const body = sliceFunction(appSrc, 'updateStoryMarkButton', 'storyStatusClass');
  assert.match(body, /storyPageProgress\s*\(\s*page\s*\)/);
  assert.match(body, /crit-story-chapter__mark/);
  assert.match(body, /Chapter viewed/);
  assert.match(body, /Mark chapter viewed/);
});

test('chapter mark click reads live progress instead of closing over render-time allViewed', () => {
  const body = sliceFunction(appSrc, 'renderStoryPage', 'renderStoryFileGroup');
  assert.match(body, /crit-story-chapter__mark/);
  // No stale closure: handler must recompute allViewed at click time.
  assert.doesNotMatch(body, /markPageViewed\s*\(\s*page\s*,\s*!allViewed\s*\)/);
  assert.match(body, /storyPageProgress\s*\(\s*page\s*\)[\s\S]{0,200}markPageViewed\s*\(\s*page\s*,/s);
});

test('per-file Viewed toggle refreshes the chapter mark button in place', () => {
  const body = sliceFunction(appSrc, 'renderStoryFileGroup', 'storySupportReasonForFile');
  assert.match(body, /toggleViewed\s*\(\s*filePath\s*\)/);
  assert.match(body, /renderStoryRail\s*\(\s*\)/);
  assert.match(body, /updateStoryMarkButton\s*\(\s*page\s*\)/);
});

test('renderStoryFileByPath refreshes the chapter mark button after partial re-render', () => {
  const body = sliceFunction(appSrc, 'renderStoryFileByPath', 'markPageViewed');
  assert.match(body, /renderStoryRail\s*\(\s*\)/);
  assert.match(body, /updateStoryMarkButton\s*\(\s*page\s*\)/);
});
