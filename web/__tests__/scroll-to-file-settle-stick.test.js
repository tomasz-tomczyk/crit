'use strict';
// scrollToFile stick contract: arm pending once; FileListVirtualizer owns
// settle. Must not define a timed settleRepin loop. (Virtualizer intent and
// settle behavior is covered in crit-file-list-virtualizer-lifecycle.test.js.)

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const appSrc = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');

function extractFunctionBody(src, name) {
  const marker = 'function ' + name + '(';
  const idx = src.indexOf(marker);
  if (idx === -1) return null;
  const start = src.indexOf('{', idx);
  if (start === -1) return null;
  let depth = 0;
  for (let i = start; i < src.length; i++) {
    const c = src[i];
    if (c === '{') depth++;
    else if (c === '}') {
      depth--;
      if (depth === 0) return src.slice(idx, i + 1);
    }
  }
  return null;
}

function stickToKeyCallCount(body) {
  const matches = body.match(/\bstickToKey\s*\(/g) || [];
  return matches.length;
}

function assertScrollToFileStick(src) {
  const scrollToFile = extractFunctionBody(src, 'scrollToFile');
  assert.ok(scrollToFile, 'scrollToFile must exist');

  assert.equal(
    scrollToFile.includes('function settleRepin('),
    false,
    'scrollToFile must not define settleRepin',
  );
  assert.equal(
    scrollToFile.includes('function repin('),
    false,
    'scrollToFile must not define nested repin',
  );
  assert.doesNotMatch(scrollToFile, /Date\.now\(\)\s*\+\s*4000/);

  const finish = extractFunctionBody(scrollToFile, 'finishWithSection');
  assert.ok(finish, 'finishWithSection must exist');
  assert.equal(stickToKeyCallCount(finish), 0, 'finishWithSection must not call stickToKey');
  assert.match(finish, /setItemHeight/);
  assert.match(finish, /pinKeyToViewportTop|scrollIntoView/);
  assert.match(scrollToFile, /\.stickToKey\(\s*filePath\s*\)/);
}

const BUGGY_SCROLL_TO_FILE = `
function scrollToFile(filePath) {
  if (fileListController && !storyActive()) {
    fileListController.stickToKey(filePath);
    function repin(section) {
      fileListController.stickToKey(filePath);
      fileListController.pinKeyToViewportTop(filePath);
    }
    function settleRepin(section, deadline, stableCount) {
      repin(section);
      if (Date.now() >= deadline) return;
      requestAnimationFrame(function() { settleRepin(section, deadline, stableCount); });
    }
    function finishWithSection(sectionEl) {
      settleRepin(sectionEl, Date.now() + 4000, 0);
    }
  }
}
`;

test('detector rejects a settleRepin stick re-arm loop', function() {
  assert.throws(
    function() { assertScrollToFileStick(BUGGY_SCROLL_TO_FILE); },
    /settleRepin|repin|4000/,
  );
});

test('app.js scrollToFile sticks without settleRepin', function() {
  assertScrollToFileStick(appSrc);
});

test('comment jump uses nearest alignment and drops a pending stick', function() {
  const ensure = extractFunctionBody(appSrc, 'ensureFileVisibleForComment');
  assert.ok(ensure);
  assert.match(ensure, /releasePendingScrollTarget\(\)/);
  assert.match(ensure, /scrollToItem\(\s*filePath\s*,\s*'nearest'\s*\)/);
  assert.doesNotMatch(ensure, /scrollToItem\(\s*filePath\s*,\s*'start'\s*\)/);
});
