'use strict';
// Pierre-aligned scrollToFile stick: arm pending once, let FileListVirtualizer
// re-apply scrollFix until device-pixel settle / user clear. No Crit settleRepin
// rAF/deadline loop (that re-armed stickToKey and fought scroll).

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const appSrc = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');
const flSrc = fs.readFileSync(
  path.join(__dirname, '..', 'crit-file-list-virtualizer.js'),
  'utf8',
);

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

function assertScrollToFilePierreStick(src) {
  const scrollToFile = extractFunctionBody(src, 'scrollToFile');
  assert.ok(scrollToFile, 'scrollToFile must exist');

  // Pierre: one pendingScrollTarget arm — not a timed settleRepin loop.
  assert.equal(
    scrollToFile.includes('function settleRepin('),
    false,
    'scrollToFile must not define settleRepin (Crit deadline loop removed)',
  );
  assert.equal(
    scrollToFile.includes('function repin('),
    false,
    'scrollToFile must not define nested repin',
  );
  assert.doesNotMatch(
    scrollToFile,
    /Date\.now\(\)\s*\+\s*4000/,
    'no 4s settle deadline',
  );

  const finish = extractFunctionBody(scrollToFile, 'finishWithSection');
  assert.ok(finish, 'finishWithSection must exist');
  assert.equal(
    stickToKeyCallCount(finish),
    0,
    'finishWithSection must not call stickToKey (virt owns pending target)',
  );
  assert.match(finish, /setItemHeight/);
  assert.match(finish, /pinKeyToViewportTop|scrollIntoView/);

  // Initial arm once at scrollToFile entry.
  assert.match(scrollToFile, /\.stickToKey\(\s*filePath\s*\)/);
}

// Old buggy shape — detector must still reject it.
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

test('detector rejects the old settleRepin stickToKey re-arm loop', function() {
  assert.throws(
    function() { assertScrollToFilePierreStick(BUGGY_SCROLL_TO_FILE); },
    /settleRepin|repin|4000/,
  );
});

test('app.js scrollToFile uses Pierre-style stick without settleRepin', function() {
  assertScrollToFilePierreStick(appSrc);
});

test('FileListVirtualizer clears stick on Pierre user intents', function() {
  // Pierre: wheel, touchstart, pointerdown, keydown → clearPendingScroll.
  assert.match(flSrc, /addEventListener\(\s*'wheel'/);
  assert.match(flSrc, /addEventListener\(\s*'touchstart'/);
  assert.match(flSrc, /addEventListener\(\s*'pointerdown'/);
  assert.match(flSrc, /clearStickToKey\s*\(/);
  assert.match(flSrc, /removeEventListener\(\s*'pointerdown'/);
  assert.match(flSrc, /removeEventListener\(\s*'touchstart'/);
});

// Behavioral: clear must leave stick cleared (no re-arm from a settle frame).
test('clearStick leaves stick cleared without settle re-arm', function() {
  let stick = 'web/app.js';
  const ctrl = {
    stickToKey: function(key) { stick = key; },
    stickKey: function() { return stick; },
    clearStickToKey: function() { stick = null; },
    pinKeyToViewportTop: function() {},
  };

  // Old Crit settle frame re-armed after clear.
  function oldSettleFrame(key) {
    ctrl.stickToKey(key);
    ctrl.pinKeyToViewportTop(key);
  }
  // Pierre / fixed: after clear, only pin if still pending — and finishWithSection
  // never calls stickToKey.
  function virtStyleFrame(key) {
    if (ctrl.stickKey() !== key) return;
    ctrl.pinKeyToViewportTop(key);
  }

  ctrl.clearStickToKey();
  oldSettleFrame('web/app.js');
  assert.equal(ctrl.stickKey(), 'web/app.js', 'old settle re-armed (the bug)');

  ctrl.clearStickToKey();
  virtStyleFrame('web/app.js');
  assert.equal(ctrl.stickKey(), null, 'Pierre-style frame leaves stick cleared');
});
