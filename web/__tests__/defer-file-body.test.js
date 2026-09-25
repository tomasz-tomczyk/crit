'use strict';
// Source-contract tests for deferred file-body mounting (crit-web#318 /
// multi-file markdown PR perf). app.js is a monolithic IIFE, so we assert
// the critical helpers and call sites stay wired rather than spinning a DOM.

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const appSrc = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');
const sessionSrc = fs.readFileSync(
  path.join(__dirname, '..', '..', 'internal', 'session', 'session.go'),
  'utf8',
);

test('navigateToComment drives from the comment model and loads before highlight', () => {
  assert.match(appSrc, /function collectNavigableComments\s*\(/);
  assert.match(appSrc, /function highlightNavComment\s*\(/);
  const marker = 'function navigateToComment(';
  const idx = appSrc.indexOf(marker);
  assert.notEqual(idx, -1, 'navigateToComment must exist');
  const nextFn = appSrc.indexOf('\n  function ', idx + marker.length);
  const body = appSrc.slice(idx, nextFn === -1 ? undefined : nextFn);
  assert.match(body, /collectNavigableComments\(\)/);
  assert.match(body, /highlightNavComment\(/);
  // Must not rely solely on mounted .comment-card query for the nav plan.
  assert.doesNotMatch(body, /container\.querySelectorAll\('\.comment-card'\)/);

  // The highlight goes through the Pierre jump, which loads a lazy file
  // before scrolling (the card may not be mounted yet).
  const highlightMarker = 'function highlightNavComment(';
  const hIdx = appSrc.indexOf(highlightMarker);
  const hNext = appSrc.indexOf('\n  function ', hIdx + highlightMarker.length);
  const hBody = appSrc.slice(hIdx, hNext === -1 ? undefined : hNext);
  assert.match(hBody, /pierreJumpToComment\(/);
  const jIdx = appSrc.indexOf('function pierreJumpToComment(');
  const jBody = appSrc.slice(jIdx, appSrc.indexOf('\n  function ', jIdx + 10));
  assert.match(jBody, /ensureLoaded\(/);
});

test('n/N shortcuts call navigateToChange even when changeGroups is empty', () => {
  // Keyboard n/N must reach navigateToChange's empty-groups mount path;
  // header buttons already did. Guarding on changeGroups.length === 0 blocks that.
  const nextCase = appSrc.indexOf("case 'next_change':");
  const prevCase = appSrc.indexOf("case 'previous_change':");
  assert.notEqual(nextCase, -1);
  assert.notEqual(prevCase, -1);
  const nextBody = appSrc.slice(nextCase, appSrc.indexOf('case ', nextCase + 10));
  const prevBody = appSrc.slice(prevCase, appSrc.indexOf('case ', prevCase + 10));
  assert.match(nextBody, /navigateToChange\(1\)/);
  assert.match(prevBody, /navigateToChange\(-1\)/);
  assert.doesNotMatch(nextBody, /changeGroups\.length\s*===\s*0/);
  assert.doesNotMatch(prevBody, /changeGroups\.length\s*===\s*0/);
});

test('buildChangeGroups rebinds currentChangeIdx after rebuild instead of always clearing', () => {
  assert.match(appSrc, /function changeNavAnchorFromIdx\s*\(/);
  assert.match(appSrc, /function findChangeIdxForAnchor\s*\(/);
  const marker = 'function buildChangeGroups(';
  const idx = appSrc.indexOf(marker);
  assert.notEqual(idx, -1);
  const nextFn = appSrc.indexOf('\n  function ', idx + marker.length);
  const body = appSrc.slice(idx, nextFn === -1 ? undefined : nextFn);
  assert.match(body, /changeNavAnchorFromIdx\(currentChangeIdx\)/);
  assert.match(body, /findChangeIdxForAnchor\(prevAnchor\)/);
  // Must not unconditionally wipe the index when groups were rebuilt with content.
  assert.doesNotMatch(body, /changeGroups\.push\(group\);[\s\S]*currentChangeIdx\s*=\s*-1;/);
});

test('change-nav button titles use shortcut bindings, not hardcoded n/N', () => {
  assert.match(appSrc, /getBinding\('previous_change'\)/);
  assert.match(appSrc, /getBinding\('next_change'\)/);
  assert.doesNotMatch(appSrc, /title="Previous change \(N\)"/);
  assert.doesNotMatch(appSrc, /title="Next change \(n\)"/);
});

test('agent-pending reply form disables Cancel and Reply buttons', () => {
  const marker = 'function createReplyInput(';
  const idx = appSrc.indexOf(marker);
  assert.notEqual(idx, -1);
  const nextFn = appSrc.indexOf('\n  function ', idx + marker.length);
  const body = appSrc.slice(idx, nextFn === -1 ? undefined : nextFn);
  assert.match(body, /cancelBtn\.disabled\s*=\s*!!isPending/);
  assert.match(body, /submitBtn\.disabled\s*=\s*!!isPending/);
});

test('backend eager-load threshold is 25', () => {
  assert.match(sessionSrc, /const lazyFileThreshold = 25/);
});

test('range/PR focus applies lazyFileThreshold via populateLazyFile', () => {
  const focusSrc = fs.readFileSync(
    path.join(__dirname, '..', '..', 'internal', 'session', 'session_focus.go'),
    'utf8',
  );
  assert.match(focusSrc, /len\(changes\) > lazyFileThreshold && i >= lazyFileThreshold/);
  // Range path must populate lazy stats the same way as working-tree.
  assert.match(
    focusSrc,
    /if len\(changes\) > lazyFileThreshold && i >= lazyFileThreshold \{\s*populateLazyFile\(fe, fc, numstats, false\)/s,
  );
});

test('session loads lazy range files via SHA-aware path', () => {
  assert.match(sessionSrc, /func \(s \*Session\) ensureFileLoaded\(/);
  assert.match(sessionSrc, /ensureLoadedAtRange|readFileAtSHA/);
  // GetFileSnapshot / GetFileDiffSnapshot must use session-aware loader.
  assert.match(sessionSrc, /ensureFileLoaded\(f\)/);
});
