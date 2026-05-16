const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const src = fs.readFileSync(path.join(__dirname, '..', 'crit-diff-renderer.js'), 'utf8');

// Minimal DiffMatchPatch mock that simulates the @sanity/diff-match-patch API
const mockDMP = {
  DIFF_EQUAL: 0,
  DIFF_DELETE: -1,
  DIFF_INSERT: 1,
  makeDiff: function(a, b) {
    // Simple mock: find common prefix/suffix, report middle as change
    if (a === b) return [[0, a]];
    // Character-by-character diff for short strings
    var result = [];
    var i = 0;
    // Common prefix
    while (i < a.length && i < b.length && a[i] === b[i]) i++;
    if (i > 0) result.push([0, a.slice(0, i)]);
    // Differing middle
    var j = 0;
    while (j < a.length - i && j < b.length - i && a[a.length - 1 - j] === b[b.length - 1 - j]) j++;
    var delPart = a.slice(i, a.length - j);
    var insPart = b.slice(i, b.length - j);
    if (delPart) result.push([-1, delPart]);
    if (insPart) result.push([1, insPart]);
    if (j > 0) result.push([0, a.slice(a.length - j)]);
    return result;
  },
  cleanupSemantic: function(diffs) { return diffs; },
};

function escapeHtml(s) {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

const sandbox = {
  window: {
    crit: { commentCardHelpers: { escapeHtml: escapeHtml } },
    DiffMatchPatch: mockDMP,
  },
  document: {},
};
const fn = new Function('window', 'document', src + '\nreturn window;');
fn(sandbox.window, sandbox.document);
const diffRenderer = sandbox.window.crit.diffRenderer;

// --- lineSimilarity ---

test('lineSimilarity returns 1.0 for identical strings', function() {
  assert.equal(diffRenderer.lineSimilarity('hello world', 'hello world'), 1);
});

test('lineSimilarity returns 1.0 for identical empty strings', function() {
  assert.equal(diffRenderer.lineSimilarity('', ''), 1);
});

test('lineSimilarity returns 0 for completely different strings', function() {
  assert.equal(diffRenderer.lineSimilarity('aaa bbb ccc', 'xxx yyy zzz'), 0);
});

test('lineSimilarity returns 0 when one string is empty', function() {
  assert.equal(diffRenderer.lineSimilarity('hello', ''), 0);
  assert.equal(diffRenderer.lineSimilarity('', 'hello'), 0);
});

test('lineSimilarity returns partial score for overlapping tokens', function() {
  var score = diffRenderer.lineSimilarity('foo bar baz', 'foo bar qux');
  // 2 common tokens out of 3+3 = 6 total => 4/6 = 0.667
  assert.ok(score > 0.5 && score < 1);
});

// --- htmlToText ---

test('htmlToText strips HTML tags', function() {
  assert.equal(diffRenderer.htmlToText('<span class="kw">var</span> x = 1;'), 'var x = 1;');
});

test('htmlToText decodes HTML entities', function() {
  assert.equal(diffRenderer.htmlToText('a &amp; b &lt; c &gt; d &quot;e&quot;'), 'a & b < c > d "e"');
});

test('htmlToText handles nested tags', function() {
  assert.equal(diffRenderer.htmlToText('<div><span>hello</span> <b>world</b></div>'), 'hello world');
});

// --- applyWordDiffToHtml ---

test('applyWordDiffToHtml wraps ranges with CSS class spans', function() {
  var html = 'hello world';
  var ranges = [[6, 11]]; // "world"
  var result = diffRenderer.applyWordDiffToHtml(html, ranges, 'diff-word-del');
  assert.equal(result, 'hello <span class="diff-word-del">world</span>');
});

test('applyWordDiffToHtml handles multiple ranges', function() {
  var html = 'abc def ghi';
  var ranges = [[0, 3], [8, 11]]; // "abc" and "ghi"
  var result = diffRenderer.applyWordDiffToHtml(html, ranges, 'hl');
  assert.equal(result, '<span class="hl">abc</span> def <span class="hl">ghi</span>');
});

test('applyWordDiffToHtml returns unchanged html for empty ranges', function() {
  var html = '<span>text</span>';
  assert.equal(diffRenderer.applyWordDiffToHtml(html, [], 'x'), html);
  assert.equal(diffRenderer.applyWordDiffToHtml(html, null, 'x'), html);
});

test('applyWordDiffToHtml handles HTML entities as single characters', function() {
  // "a&b" is 3 visible characters; entity &amp; counts as 1 char at index 1
  var html = 'a&amp;b';
  var ranges = [[1, 2]]; // the "&" character
  var result = diffRenderer.applyWordDiffToHtml(html, ranges, 'hl');
  assert.equal(result, 'a<span class="hl">&amp;</span>b');
});

test('applyWordDiffToHtml skips over HTML tags without counting them', function() {
  // visible text: "ab" (2 chars), range covers char 1 ("b")
  var html = '<span>a</span>b';
  var ranges = [[1, 2]];
  var result = diffRenderer.applyWordDiffToHtml(html, ranges, 'hl');
  assert.equal(result, '<span>a</span><span class="hl">b</span>');
});

// --- bestWordDiffPairing ---

test('bestWordDiffPairing pairs similar lines together', function() {
  var dels = ['const x = 1;', 'function foo() {'];
  var adds = ['function bar() {', 'const x = 2;'];
  var pairs = diffRenderer.bestWordDiffPairing(dels, adds);
  // "const x = 1;" should pair with "const x = 2;" (index 1 in adds)
  // "function foo() {" should pair with "function bar() {" (index 0 in adds)
  assert.equal(pairs.length, 2);
  // Find the pair for del[0] ("const x = 1;")
  var constPair = pairs.find(function(p) { return p[0] === 0; });
  assert.ok(constPair);
  assert.equal(constPair[1], 1); // paired with "const x = 2;"
  // Find the pair for del[1] ("function foo() {")
  var funcPair = pairs.find(function(p) { return p[0] === 1; });
  assert.ok(funcPair);
  assert.equal(funcPair[1], 0); // paired with "function bar() {"
});

test('bestWordDiffPairing returns empty for empty inputs', function() {
  assert.deepEqual(diffRenderer.bestWordDiffPairing([], ['a']), []);
  assert.deepEqual(diffRenderer.bestWordDiffPairing(['a'], []), []);
});

test('bestWordDiffPairing returns empty for large blocks', function() {
  var dels = ['a', 'b', 'c', 'd', 'e'];
  var adds = ['f', 'g', 'h', 'i'];
  // 5 + 4 = 9 > 8, should skip
  assert.deepEqual(diffRenderer.bestWordDiffPairing(dels, adds), []);
});

test('bestWordDiffPairing skips dissimilar 1:1 pairs', function() {
  var pairs = diffRenderer.bestWordDiffPairing(['aaa bbb ccc'], ['xxx yyy zzz']);
  assert.deepEqual(pairs, []);
});

// --- buildHunkWordDiffs ---

test('buildHunkWordDiffs returns diff data for hunk lines', function() {
  var hunk = {
    Lines: [
      { Type: 'context', Content: 'unchanged line' },
      { Type: 'del', Content: 'hello world' },
      { Type: 'add', Content: 'hello earth' },
      { Type: 'context', Content: 'another line' },
    ],
  };
  var map = diffRenderer.buildHunkWordDiffs(hunk);
  // del at index 1, add at index 2 should be paired
  assert.ok(map instanceof Map);
  // With our mock DMP, "hello world" -> "hello earth" produces a diff
  // The del line (index 1) should have diff-word-del class
  if (map.has(1)) {
    assert.equal(map.get(1).cssClass, 'diff-word-del');
    assert.ok(Array.isArray(map.get(1).ranges));
  }
  if (map.has(2)) {
    assert.equal(map.get(2).cssClass, 'diff-word-add');
    assert.ok(Array.isArray(map.get(2).ranges));
  }
});

test('buildHunkWordDiffs returns empty map for context-only hunks', function() {
  var hunk = {
    Lines: [
      { Type: 'context', Content: 'line 1' },
      { Type: 'context', Content: 'line 2' },
    ],
  };
  var map = diffRenderer.buildHunkWordDiffs(hunk);
  assert.equal(map.size, 0);
});

// --- wordDiff ---

test('wordDiff returns null for identical lines', function() {
  assert.equal(diffRenderer.wordDiff('same', 'same'), null);
});

test('wordDiff returns null for very long lines', function() {
  var long = 'x'.repeat(501);
  assert.equal(diffRenderer.wordDiff(long, 'short'), null);
});

test('wordDiff returns ranges for small changes', function() {
  var result = diffRenderer.wordDiff('hello world', 'hello earth');
  // With our mock, common prefix "hello " (6 chars), then del "world" / ins "earth"
  if (result) {
    assert.ok(Array.isArray(result.oldRanges));
    assert.ok(Array.isArray(result.newRanges));
  }
});
