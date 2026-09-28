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
  NodeFilter: { SHOW_TEXT: 4 },
};
const fn = new Function('window', 'document', 'NodeFilter', src + '\nreturn window;');
fn(sandbox.window, sandbox.document, sandbox.NodeFilter);
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

test('applyWordDiffToHtml keeps highlight spans open across nested syntax spans', function() {
  // Syntax highlighting can nest many spans inside a single changed token (e.g. HEEx #{...}).
  // Closing/reopening word-diff spans at every tag boundary creates empty highlight
  // spans that render as phantom whitespace in the diff viewer.
  var oldLine = '                <span class="text-gray-500 sm:text-sm" id="price-currency-for-sms">USD</span>';
  var newLine = '                <span class="text-gray-500 sm:text-sm" id={"price-currency-for-feature-#{ef.index}"}>';
  var hlLine =
    '<span style="color:var(--t)"><span style="color:var(--t)">&lt;<span style="color:var(--t)">span</span> ' +
    '<span style="color:var(--t)">class</span>=<span style="color:var(--t)">"text-gray-500 sm:text-sm"</span> ' +
    '<span style="color:var(--t)">id</span>=<span style="color:var(--t)">{</span></span></span>' +
    '<span style="color:var(--t)"><span style="color:var(--t)"><span style="color:var(--t)">' +
    '<span style="color:var(--t)">"price-currency-for-feature-#{ef.index}"</span></span></span></span>' +
    '<span style="color:var(--t)"><span style="color:var(--t)">}&gt;</span></span>';
  var wd = diffRenderer.wordDiff(oldLine, newLine);
  assert.ok(wd && wd.newRanges.length > 0);
  var result = diffRenderer.applyWordDiffToHtml(hlLine, wd.newRanges, 'diff-word-add');
  assert.equal(result.match(/<span class="diff-word-add"><\/span>/g), null);
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

// --- resolveTextSelectionLineRange ---
// Markdown select-to-comment: the line blocks a selection touches resolve to
// one file's line range (Pierre diffs resolve their own selections).

test('resolveTextSelectionLineRange returns null for multi-file selection', function() {
  var candidates = [
    { filePath: 'a.md', startLine: 1, endLine: 1, blockIndex: 0 },
    { filePath: 'b.md', startLine: 2, endLine: 2, blockIndex: 0 },
  ];
  assert.equal(diffRenderer.resolveTextSelectionLineRange(candidates), null);
});

test('resolveTextSelectionLineRange returns null for empty candidates', function() {
  assert.equal(diffRenderer.resolveTextSelectionLineRange([]), null);
  assert.equal(diffRenderer.resolveTextSelectionLineRange(null), null);
});

test('resolveTextSelectionLineRange preserves markdown afterBlockIndex', function() {
  var candidates = [
    { filePath: 'doc.md', startLine: 10, endLine: 12, blockIndex: 3 },
    { filePath: 'doc.md', startLine: 13, endLine: 14, blockIndex: 4 },
  ];
  assert.deepEqual(
    diffRenderer.resolveTextSelectionLineRange(candidates),
    { filePath: 'doc.md', startLine: 10, endLine: 14, afterBlockIndex: 4 }
  );
});

// Wiring: markdown text selections resolve through resolveTextSelectionLineRange
// (Pierre diffs use getComposedRanges in pierreSelectionForComment instead).
test('app.js wires text selection through resolveTextSelectionLineRange', function() {
  var appJs = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');
  assert.match(
    appJs,
    /selectedTextWithinElements\(selection,\s*contentEls\)/,
    'quote capture must clip to contentEls'
  );
  assert.match(
    appJs,
    /resolveTextSelectionLineRange\(candidates\)/,
    'getLineRangeFromSelection must resolve via resolveTextSelectionLineRange'
  );
  assert.match(
    fs.readFileSync(path.join(__dirname, '..', 'crit-pierre-dom.js'), 'utf8'),
    /getComposedRanges\(\{\s*shadowRoots:/,
    'Pierre diff selections must read shadow-root ranges via getComposedRanges'
  );
});

test('selectedTextWithinElements joins only intersecting contentEls', function() {
  // Minimal Selection/Range stubs — no jsdom.
  var t1 = { textContent: 'old line', nodeType: 3 };
  var t2 = { textContent: 'new line', nodeType: 3 };
  var el1 = {
    _nodes: [t1],
    contains: function(n) { return n === t1 || n === this; },
  };
  var el2 = {
    _nodes: [t2],
    contains: function(n) { return n === t2 || n === this; },
  };
  // TreeWalker stub via document.createTreeWalker — inject via global in helper.
  // Instead exercise by monkeypatching: call with selection that only hits el2.
  var selRange = {
    startContainer: t2,
    startOffset: 0,
    endContainer: t2,
    endOffset: 8,
    intersectsNode: function(el) { return el === el2; },
  };
  var selection = {
    rangeCount: 1,
    getRangeAt: function() { return selRange; },
    containsNode: function(n, _partial) { return n === t2; },
  };
  // Mutate the sandbox document the module closed over (no jsdom).
  var prevTW = sandbox.document.createTreeWalker;
  sandbox.document.createTreeWalker = function(root, _what, _filter) {
    var nodes = root._nodes || [];
    var i = -1;
    return {
      nextNode: function() {
        i++;
        return i < nodes.length ? nodes[i] : null;
      },
    };
  };
  try {
    assert.equal(
      diffRenderer.selectedTextWithinElements(selection, [el1, el2]),
      'new line'
    );
  } finally {
    sandbox.document.createTreeWalker = prevTW;
  }
});
