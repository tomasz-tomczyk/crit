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

test('files stay open by default and bodies mount via viewport observer', () => {
  assert.match(appSrc, /function setupBodyMountObserver\s*\(/);
  assert.match(appSrc, /function mountDeferredBody\s*\(/);
  assert.match(appSrc, /function mountVisibleDeferredBodies\s*\(/);
  assert.doesNotMatch(appSrc, /applyDefaultFileCollapse\s*\(/);
  assert.doesNotMatch(appSrc, /DEFAULT_COLLAPSE_FILE_THRESHOLD/);
});

test('file body mount/unmount helpers are wired into details toggle without initialToggle gate', () => {
  assert.match(appSrc, /function ensureFileBodyMounted\s*\(/);
  assert.match(appSrc, /function deferFileBody\s*\(/);
  assert.match(appSrc, /function populateFileBody\s*\(/);
  assert.match(appSrc, /data-body-deferred/);
  // Setting open before the listener means there is no synthetic first toggle
  // to swallow — an initialToggle flag would drop the first real user toggle.
  assert.doesNotMatch(appSrc, /initialToggle/);
  assert.match(appSrc, /if \(section\.open\) \{\s*if \(file\.lazy\) loadLazyFile\(section, file\);\s*else ensureFileBodyMounted\(section, file\);/s);
  assert.match(appSrc, /else if \(!fileHasOpenLineForms\(file\.path\)\) \{\s*deferFileBody\(section\);/s);
});

test('scrollToFile mounts deferred body or loads lazy file before scrolling', () => {
  assert.match(appSrc, /function scrollToFile\s*\(/);
  assert.match(appSrc, /if \(file\.lazy\)[\s\S]{0,400}loadLazyFile\(sectionEl, file, function onLoaded\(newSection\)/s);
  assert.match(appSrc, /ensureFileBodyMounted\(sectionEl, file\)/);
});

test('loadLazyFile mounts body after re-render so observer suppress cannot leave empty section', () => {
  assert.match(appSrc, /function loadLazyFile\s*\(/);
  // After replaceWith(renderFileSection(...)), body must be mounted when open.
  assert.match(
    appSrc,
    /section\.replaceWith\(newSection\);[\s\S]{0,250}ensureFileBodyMounted\(newSection,\s*file\)/s,
  );
});

test('body-mount observer suppress schedules a remount when the window expires', () => {
  assert.match(appSrc, /ignoreBodyMountObserverUntil\s*=/);
  assert.match(appSrc, /mountVisibleDeferredBodies\(\)/);
  // Suppress helper must re-run viewport mount after the quiet period.
  assert.match(
    appSrc,
    /function suppressBodyMountObserver\s*\(\s*ms\s*\)[\s\S]{0,400}setTimeout\([\s\S]{0,200}mountVisibleDeferredBodies/s,
  );
});

test('scrollToComment mounts deferred/lazy body before looking up the card', () => {
  const marker = 'function scrollToComment(';
  const scrollToCommentIdx = appSrc.indexOf(marker);
  assert.notEqual(scrollToCommentIdx, -1, 'scrollToComment must exist');
  const nextFn = appSrc.indexOf('\n  function ', scrollToCommentIdx + marker.length);
  const body = appSrc.slice(scrollToCommentIdx, nextFn === -1 ? undefined : nextFn);
  assert.match(body, /ensureFileBodyMounted/);
  assert.match(body, /loadLazyFile/);
});

test('renderFileByPath remounts the body for the replaced open section', () => {
  assert.match(appSrc, /function renderFileByPath\s*\(/);
  const idx = appSrc.indexOf('function renderFileByPath');
  const nextFn = appSrc.indexOf('\n  function ', idx + 1);
  const body = appSrc.slice(idx, nextFn === -1 ? undefined : nextFn);
  assert.match(body, /ensureFileBodyMounted|mountVisibleDeferredBodies/);
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
    /if len\(changes\) > lazyFileThreshold && i >= lazyFileThreshold \{\s*populateLazyFile\(fe, fc, numstats\)/s,
  );
});

test('session loads lazy range files via SHA-aware path', () => {
  assert.match(sessionSrc, /func \(s \*Session\) ensureFileLoaded\(/);
  assert.match(sessionSrc, /ensureLoadedAtRange|readFileAtSHA/);
  // GetFileSnapshot / GetFileDiffSnapshot must use session-aware loader.
  assert.match(sessionSrc, /ensureFileLoaded\(f\)/);
});
