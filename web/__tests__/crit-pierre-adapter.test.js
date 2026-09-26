'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const a = require('../crit-pierre-adapter.js');

function hunk(oldStart, oldCount, newStart, newCount, lines, header) {
  return {
    OldStart: oldStart, OldCount: oldCount, NewStart: newStart, NewCount: newCount,
    Header: header || '',
    Lines: lines.map(function(l) { return { Type: l[0], Content: l[1] }; }),
  };
}

test('hunksToPatch emits a git unified diff Pierre can parse', function() {
  const patch = a.hunksToPatch({
    path: 'src/app.go',
    status: 'modified',
    diffHunks: [hunk(3, 3, 3, 4, [['context', 'a'], ['del', 'b'], ['add', 'B'], ['add', 'B2'], ['context', 'c']], '@@ -3,3 +3,4 @@ func main() {')],
  });
  assert.equal(patch, [
    'diff --git a/src/app.go b/src/app.go',
    '--- a/src/app.go',
    '+++ b/src/app.go',
    '@@ -3,3 +3,4 @@ func main() {',
    ' a', '-b', '+B', '+B2', ' c', '',
  ].join('\n'));
});

test('hunksToPatch: new, deleted and renamed files', function() {
  const added = a.hunksToPatch({ path: 'n.txt', status: 'added', diffHunks: [hunk(0, 0, 1, 1, [['add', 'x']])] });
  assert.match(added, /new file mode 100644\n--- \/dev\/null\n\+\+\+ b\/n\.txt\n@@ -0,0 \+1 @@\n\+x\n$/);
  const deleted = a.hunksToPatch({ path: 'd.txt', status: 'deleted', diffHunks: [hunk(1, 1, 0, 0, [['del', 'x']])] });
  assert.match(deleted, /deleted file mode 100644\n--- a\/d\.txt\n\+\+\+ \/dev\/null\n@@ -1 \+0,0 @@\n-x\n$/);
  const renamed = a.hunksToPatch({ path: 'new.go', old_path: 'old.go', status: 'renamed', diffHunks: [] });
  assert.match(renamed, /^diff --git a\/old\.go b\/new\.go\nrename from old\.go\nrename to new\.go\n--- a\/old\.go\n\+\+\+ b\/new\.go\n$/);
});

test('reconstructOldContent reverses the hunks against the new file', function() {
  const newContent = '1\n2\nB\nB2\n4\n5\n6\n7\nX\n9\n';
  const hunks = [
    hunk(2, 3, 2, 4, [['context', '2'], ['del', '3'], ['add', 'B'], ['add', 'B2'], ['context', '4']]),
    hunk(7, 3, 8, 2, [['context', '7'], ['del', '8'], ['del', 'eight'], ['add', 'X']]),
  ];
  hunks[1].Lines.push({ Type: 'context', Content: '9' });
  hunks[1].OldCount = 4; hunks[1].NewCount = 3;
  assert.equal(a.reconstructOldContent(newContent, hunks), '1\n2\n3\n4\n5\n6\n7\n8\neight\n9\n');
});

test('reconstructOldContent handles added and deleted files and missing trailing newline', function() {
  assert.equal(a.reconstructOldContent('x\ny\n', [hunk(0, 0, 1, 2, [['add', 'x'], ['add', 'y']])]), '');
  assert.equal(a.reconstructOldContent('', [hunk(1, 2, 0, 0, [['del', 'x'], ['del', 'y']])]), 'x\ny\n');
  assert.equal(a.reconstructOldContent('a\nb', [hunk(1, 2, 1, 2, [['context', 'a'], ['del', 'c'], ['add', 'b']])]), 'a\nc');
});

test('reconstructOldContent returns null when content and hunks disagree', function() {
  // Stale content (file changed after the diff was computed) must not
  // produce a wrong old file — the caller falls back to a partial diff.
  assert.equal(a.reconstructOldContent('1\nZ\n3\n', [hunk(2, 1, 2, 1, [['del', 'b'], ['add', 'B']])]), null);
  assert.equal(a.reconstructOldContent('1\n', [hunk(5, 1, 5, 1, [['del', 'b'], ['add', 'B']])]), null);
  assert.equal(a.reconstructOldContent(undefined, []), null);
});

test('annotations map comments and forms to Pierre sides and lines', function() {
  assert.deepEqual(a.annotationForComment({ id: 'c1', end_line: 12, side: '' }),
    { side: 'additions', lineNumber: 12, metadata: { kind: 'thread', id: 'c1' } });
  assert.deepEqual(a.annotationForComment({ id: 'c2', end_line: 4, side: 'old' }),
    { side: 'deletions', lineNumber: 4, metadata: { kind: 'thread', id: 'c2' } });
  assert.deepEqual(a.annotationForComment({ id: 'c3', scope: 'file' }),
    { side: 'additions', lineNumber: 0, metadata: { kind: 'thread', id: 'c3' } });
  assert.deepEqual(a.annotationForForm({ formKey: 'p:1:3:', endLine: 3, side: '' }),
    { side: 'additions', lineNumber: 3, metadata: { kind: 'form', id: 'p:1:3:' } });
});

test('formRangeFromSelection normalizes direction and side', function() {
  assert.deepEqual(a.formRangeFromSelection({ start: 9, end: 4, side: 'additions' }), { startLine: 4, endLine: 9, side: '' });
  assert.deepEqual(a.formRangeFromSelection({ start: 2, end: 2, side: 'deletions' }), { startLine: 2, endLine: 2, side: 'old' });
  assert.deepEqual(a.formRangeFromSelection({ start: 2, end: 5, side: 'additions', endSide: 'deletions' }), { startLine: 2, endLine: 5, side: 'old' });
  assert.equal(a.formRangeFromSelection(null), null);
});

test('themeTypeFor and estimatedLineCount', function() {
  assert.equal(a.themeTypeFor('light'), 'light');
  assert.equal(a.themeTypeFor('dark'), 'dark');
  assert.equal(a.themeTypeFor('system'), 'system');
  assert.equal(a.themeTypeFor(undefined), 'system');
  assert.equal(a.estimatedLineCount({ additions: 10, deletions: 3 }), 14);
  assert.equal(a.estimatedLineCount({}), 2);
});

test('navRowsForHunks: unified visits every line; split pairs change rows', function() {
  const h = [hunk(1, 4, 1, 4, [['context', 'a'], ['del', 'b'], ['del', 'c'], ['add', 'B'], ['context', 'd'], ['add', 'e']])];
  h[0].Lines[0].OldNum = 1; h[0].Lines[0].NewNum = 1;
  h[0].Lines[1].OldNum = 2; h[0].Lines[2].OldNum = 3;
  h[0].Lines[3].NewNum = 2;
  h[0].Lines[4].OldNum = 4; h[0].Lines[4].NewNum = 3;
  h[0].Lines[5].NewNum = 4;
  assert.deepEqual(a.navRowsForHunks(h, 'unified'), [
    { line: 1, side: '' }, { line: 2, side: 'old' }, { line: 3, side: 'old' }, { line: 2, side: '' },
    { line: 3, side: '' }, { line: 4, side: '' },
  ]);
  // Split: del b pairs with add B (one row, focus new side); del c alone.
  assert.deepEqual(a.navRowsForHunks(h, 'split'), [
    { line: 1, side: '' }, { line: 2, side: '' }, { line: 3, side: 'old' },
    { line: 3, side: '' }, { line: 4, side: '' },
  ]);
  assert.deepEqual(a.navRowsForHunks([], 'split'), []);
});

test('buildFileDiff: a subset with an omitted earlier hunk falls back to the patch alone', function() {
  const seen = [];
  const P = { processFile(patch, opts) { seen.push({ patch, full: !!(opts && opts.oldFile) }); return {}; } };
  const newContent = '1\nB\nB2\n3\n4\n5\nF\n7\n';
  const all = [
    hunk(2, 1, 2, 2, [['del', 'b'], ['add', 'B'], ['add', 'B2']]),
    hunk(6, 1, 7, 1, [['del', 'f'], ['add', 'F']]),
  ];
  const file = { path: 'x.txt', status: 'modified', content: newContent };
  // Chapter shows only the second hunk: the first (which adds a line) is
  // omitted before it, so full contents cannot line up.
  a.buildFileDiff(P, file, [all[1]], 'k', all);
  assert.equal(seen[0].full, false);
  assert.match(seen[0].patch, /@@ -6 \+7 @@/, 'real line numbers kept');
  // Chapter shows only the first hunk: nothing omitted before it.
  a.buildFileDiff(P, file, [all[0]], 'k2', all);
  assert.equal(seen[1].full, true);
});


test('display settings use defaults and reject unsupported persisted values', function() {
  const defaults = {
    overflow: 'scroll', hunkSeparators: 'line-info', lineDiffType: 'word-alt',
    diffIndicators: 'bars', expandUnchanged: false, disableLineNumbers: false,
  };
  function read(settings) {
    return function(key, fallback) { return Object.hasOwn(settings, key) ? settings[key] : fallback; };
  }
  assert.deepEqual(a.displayOptions(read({})), defaults);
  assert.deepEqual(a.displayOptions(read({
    codeOverflow: 'invalid', inlineDiff: 'invalid', changeIndicators: null,
    unchangedContext: 'invalid', lineNumbers: 'invalid',
  })), defaults);
  for (const inlineDiff of ['word-alt', 'word', 'char', 'none']) {
    for (const changeIndicators of ['classic', 'bars', 'none']) {
      assert.deepEqual(a.displayOptions(read({
        codeOverflow: 'wrap', inlineDiff, changeIndicators,
        unchangedContext: 'expanded', lineNumbers: 'off',
      })), {
        overflow: 'wrap', hunkSeparators: 'line-info', lineDiffType: inlineDiff,
        diffIndicators: changeIndicators, expandUnchanged: true, disableLineNumbers: true,
      });
    }
  }
});
