const test = require('node:test');
const assert = require('node:assert/strict');

const virtualizer = require('../crit-diff-virtualizer.js');

function fixture() {
  return {
    hunks: [
      {
        OldStart: 10,
        OldCount: 2,
        NewStart: 10,
        NewCount: 2,
        Header: '@@ -10,2 +10,2 @@',
        Lines: [
          { Type: 'del', OldNum: 10, NewNum: 0, Content: 'old' },
          { Type: 'add', OldNum: 0, NewNum: 10, Content: 'new' },
          { Type: 'context', OldNum: 11, NewNum: 11, Content: 'same' },
        ],
      },
      {
        OldStart: 40,
        OldCount: 1,
        NewStart: 40,
        NewCount: 1,
        Header: '@@ -40,1 +40,1 @@',
        Lines: [{ Type: 'add', OldNum: 0, NewNum: 40, Content: 'later' }],
      },
    ],
    commentsMap: {
      '10:old': [{ id: 'c_old', start_line: 10, end_line: 10, side: 'old' }],
      '10:': [{ id: 'c_new', start_line: 10, end_line: 10 }],
      '30:': [{ id: 'c_outdated', start_line: 30, end_line: 30 }],
      '31:': [{ id: 'c_resolved', start_line: 31, end_line: 31, resolved: true }],
    },
    forms: [{ formKey: 'file:10:10:', startLine: 10, endLine: 10, side: '' }],
    totalLines: 50,
  };
}

test('buildUnifiedRows emits stable semantic rows in visual order', function() {
  const rows = virtualizer.buildUnifiedRows(fixture());
  assert.equal(rows[0].kind, 'gap');
  assert.equal(rows[0].gapKind, 'leading');

  const lineRows = rows.filter(function(row) { return row.kind === 'line'; });
  assert.deepEqual(lineRows.map(function(row) { return row.visualIdx; }), [0, 1, 2, 3]);
  assert.deepEqual(lineRows.map(function(row) { return [row.lineNum, row.side]; }), [
    [10, 'old'], [10, ''], [11, ''], [40, ''],
  ]);

  const newLineIndex = rows.findIndex(function(row) {
    return row.kind === 'line' && row.lineNum === 10 && row.side === '';
  });
  assert.equal(rows[newLineIndex + 1].key, 'comment:c_new');
  assert.equal(rows[newLineIndex + 2].key, 'form:file:10:10:');
  assert.ok(rows.some(function(row) { return row.key === 'comment:c_old'; }));
  assert.ok(rows.some(function(row) { return row.key === 'outdated:c_outdated'; }));
  assert.ok(rows.some(function(row) { return row.key === 'outdated:c_resolved'; }));
  assert.equal(rows.at(-1).gapKind, undefined, 'outdated comments follow the trailing gap');

  const rebuiltKeys = virtualizer.buildUnifiedRows(fixture()).map(function(row) { return row.key; });
  assert.deepEqual(rebuiltKeys, rows.map(function(row) { return row.key; }));
});

test('buildUnifiedRows removes resolved comment rows when hide-resolved is active', function() {
  const options = fixture();
  options.hideResolved = true;
  const rows = virtualizer.buildUnifiedRows(options);
  assert.equal(rows.some(function(row) { return row.key === 'outdated:c_resolved'; }), false);
  assert.equal(rows.some(function(row) { return row.key === 'outdated:c_outdated'; }), true);
});

test('HeightIndex supports prefix lookup and batched measurement changes', function() {
  const rows = [
    { key: 'a', kind: 'line' },
    { key: 'b', kind: 'header' },
    { key: 'c', kind: 'form' },
  ];
  const index = new virtualizer.HeightIndex(rows);
  assert.deepEqual(index.offsets, [0, 20, 54, 244]);
  assert.equal(index.indexAt(0), 0);
  assert.equal(index.indexAt(19.9), 0);
  assert.equal(index.indexAt(20), 1);
  assert.equal(index.indexAt(243), 2);

  assert.equal(index.updateMany(new Map([[0, 25], [2, 200]])), 0);
  assert.deepEqual(index.offsets, [0, 25, 59, 259]);
  assert.equal(index.indexAt(58), 1);
  assert.equal(index.indexAt(59), 2);
});

test('mergeIntervals clamps, sorts, and joins adjacent pinned islands', function() {
  assert.deepEqual(
    virtualizer.mergeIntervals([[10, 12], [2, 4], [5, 5], [-3, 0], [99, 101]], 20),
    [[0, 0], [2, 5], [10, 12]]
  );
});

test('overscan follows the viewport policy bounds', function() {
  assert.equal(virtualizer.overscanForViewport(300), 800);
  assert.equal(virtualizer.overscanForViewport(900), 1350);
  assert.equal(virtualizer.overscanForViewport(2000), 2400);
});

test('calculateWindow bounds the ordinary mounted code-row interval', function() {
  const rows = Array.from({ length: 1000 }, function(_, index) {
    return { key: String(index), kind: 'line' };
  });
  const heights = new virtualizer.HeightIndex(rows);
  assert.deepEqual(virtualizer.calculateWindow(heights, 0, 900), [0, 112]);
  const middle = virtualizer.calculateWindow(heights, 10000, 900);
  assert.deepEqual(middle, [432, 612]);
  assert.ok(middle[1] - middle[0] + 1 < 400);
});
