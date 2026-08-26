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
  // Setting open before the listener does still deliver a synthetic toggle:
  // it is queued, not fired. Gate the lazy fetch on the open state actually
  // changing, not on an event count — an initialToggle flag would drop the
  // first real user toggle, and ungated it loads every file in the review.
  assert.doesNotMatch(appSrc, /initialToggle/);
  assert.match(appSrc, /if \(readerToggled\) loadLazyFile\(section, file\);/);
  // Eager files must still mount, so a review under the threshold renders full.
  assert.match(appSrc, /\} else \{\s*ensureFileBodyMounted\(section, file\);/s);
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
    /replaceWith\(newSection\);[\s\S]{0,250}ensureFileBodyMounted\(newSection,\s*file\)/s,
  );
  // Concurrent callers must queue onLoaded, not drop it while _lazyLoading.
  assert.match(appSrc, /_lazyLoadCallbacks/);
});

test('viewport mounters skip collapsed sections so collapse keeps bodies deferred', () => {
  assert.match(appSrc, /function setupBodyMountObserver\s*\(/);
  assert.match(appSrc, /function mountVisibleDeferredBodies\s*\(/);
  assert.match(appSrc, /if \(!section\.open\) continue;/);
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

test('navigateToComment drives from the comment model and mounts before highlight', () => {
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

  const highlightMarker = 'function highlightNavComment(';
  const hIdx = appSrc.indexOf(highlightMarker);
  const hNext = appSrc.indexOf('\n  function ', hIdx + highlightMarker.length);
  const hBody = appSrc.slice(hIdx, hNext === -1 ? undefined : hNext);
  assert.match(hBody, /ensureFileBodyMounted/);
  assert.match(hBody, /loadLazyFile/);
});

test('j/k and change nav mount deferred file bodies at section boundaries', () => {
  assert.match(appSrc, /function navigateBlock\s*\(/);
  assert.match(appSrc, /function findDeferredFileForNav\s*\(/);
  assert.match(appSrc, /function mountFileForNav\s*\(/);
  assert.match(appSrc, /function fileSectionNeedsMount\s*\(/);

  const blockMarker = 'function navigateBlock(';
  const bIdx = appSrc.indexOf(blockMarker);
  const bNext = appSrc.indexOf('\n  function ', bIdx + blockMarker.length);
  const bBody = appSrc.slice(bIdx, bNext === -1 ? undefined : bNext);
  assert.match(bBody, /findDeferredFileForNav/);
  assert.match(bBody, /mountFileForNav/);

  const changeMarker = 'function navigateToChange(';
  const cIdx = appSrc.indexOf(changeMarker);
  const cNext = appSrc.indexOf('\n  function ', cIdx + changeMarker.length);
  const cBody = appSrc.slice(cIdx, cNext === -1 ? undefined : cNext);
  assert.match(cBody, /findDeferredFileForNav/);
  assert.match(cBody, /mountFileForNav/);
  assert.match(cBody, /fileLikelyHasChanges/);
  // Change-nav search must filter inside findDeferredFileForNav so a deferred
  // no-hunk file cannot block a later lazy/deferred file with changes.
  assert.match(cBody, /findDeferredFileForNav\([^)]*fileLikelyHasChanges\)/);
});

test('fileLikelyHasChanges treats lazy placeholders with additions/deletions as having changes', () => {
  const marker = 'function fileLikelyHasChanges(';
  const idx = appSrc.indexOf(marker);
  assert.notEqual(idx, -1, 'fileLikelyHasChanges must exist');
  const nextFn = appSrc.indexOf('\n  function ', idx + marker.length);
  const body = appSrc.slice(idx, nextFn === -1 ? undefined : nextFn);
  // Must not rely only on diffHunks — lazy placeholders keep diffHunks: [] until load.
  assert.match(body, /file\.lazy/);
  assert.match(body, /additions/);
  assert.match(body, /deletions/);
  assert.match(body, /diffHunks/);

  // findDeferredFileForNav must accept a predicate and skip non-matching mounts.
  const findMarker = 'function findDeferredFileForNav(';
  const fIdx = appSrc.indexOf(findMarker);
  const fNext = appSrc.indexOf('\n  function ', fIdx + findMarker.length);
  const fBody = appSrc.slice(fIdx, fNext === -1 ? undefined : fNext);
  assert.match(fBody, /predicate/);
  assert.match(fBody, /if \(predicate && !predicate\(files\[i\]\)\) continue;/);
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
    /if len\(changes\) > lazyFileThreshold && i >= lazyFileThreshold \{\s*populateLazyFile\(fe, fc, numstats, false\)/s,
  );
});

test('session loads lazy range files via SHA-aware path', () => {
  assert.match(sessionSrc, /func \(s \*Session\) ensureFileLoaded\(/);
  assert.match(sessionSrc, /ensureLoadedAtRange|readFileAtSHA/);
  // GetFileSnapshot / GetFileDiffSnapshot must use session-aware loader.
  assert.match(sessionSrc, /ensureFileLoaded\(f\)/);
});
