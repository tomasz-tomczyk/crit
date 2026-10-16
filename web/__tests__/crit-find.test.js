const { describe, it } = require('node:test');
const assert = require('node:assert/strict');
const { collectMatches, sameMatch } = require('../crit-find.js');

const rowsOf = (file) => file.rows;
const files = [
  { path: 'a.js', rows: [{ side: '', line: 1, text: 'foo bar foo' }, { side: 'old', line: 4, text: 'Foo' }] },
  { path: 'b.js', rows: [{ side: '', line: 9, text: 'nothing' }, { side: '', line: 10, text: 'xfoox' }] },
];

describe('collectMatches', () => {
  it('finds every occurrence in list order, case-insensitive by default', () => {
    const m = collectMatches(files, 'foo', rowsOf, false);
    assert.deepEqual(m.map((x) => [x.path, x.side, x.line, x.nth, x.col]), [
      ['a.js', '', 1, 0, 0],
      ['a.js', '', 1, 1, 8],
      ['a.js', 'old', 4, 0, 0],
      ['b.js', '', 10, 0, 1],
    ]);
  });

  it('respects case sensitivity', () => {
    assert.equal(collectMatches(files, 'Foo', rowsOf, true).length, 1);
  });

  it('does not overlap matches', () => {
    const m = collectMatches([{ path: 'c', rows: [{ side: '', line: 1, text: 'aaaa' }] }], 'aa', rowsOf, false);
    assert.equal(m.length, 2);
  });

  it('returns nothing for an empty query', () => {
    assert.deepEqual(collectMatches(files, '', rowsOf, false), []);
  });
});

describe('sameMatch', () => {
  it('compares path, side, line and occurrence', () => {
    const a = { path: 'a', side: '', line: 1, nth: 0, col: 3 };
    assert.ok(sameMatch(a, { path: 'a', side: '', line: 1, nth: 0, col: 9 }));
    assert.ok(!sameMatch(a, { path: 'a', side: 'old', line: 1, nth: 0 }));
    assert.ok(!sameMatch(a, null));
  });
});
