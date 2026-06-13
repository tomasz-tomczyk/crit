const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');

// crit-glob-match.js exports via module.exports (dual-export pattern), so it
// can be required directly in Node.
const { matchOne, matchAny } = require(path.join(__dirname, '..', 'crit-glob-match.js'));

test('*.ext — basename glob', () => {
  assert.ok(matchOne('yarn.lock', '*.lock'));
  assert.ok(matchOne('deps/yarn.lock', '*.lock'), 'matches basename at any depth');
  assert.ok(matchOne('a/b/c/Cargo.lock', '*.lock'));
  assert.ok(!matchOne('lockfile', '*.lock'), 'no extension → no match');
  assert.ok(!matchOne('foo.locked', '*.lock'), 'partial extension → no match');
});

test('dir/ — directory prefix', () => {
  assert.ok(matchOne('generated/api.ts', 'generated/'));
  assert.ok(matchOne('generated/nested/deep.ts', 'generated/'));
  assert.ok(matchOne('src/generated/api.ts', 'generated/'), 'matches at a path boundary');
  assert.ok(!matchOne('generatedX/api.ts', 'generated/'), 'prefix must end on a slash boundary');
  assert.ok(!matchOne('mygenerated/api.ts', 'generated/'));
});

test('exact.file — exact path or basename', () => {
  assert.ok(matchOne('PLAN.md', 'PLAN.md'), 'exact basename at root');
  assert.ok(matchOne('docs/PLAN.md', 'PLAN.md'), 'matches basename at any depth');
  assert.ok(matchOne('a/b/PLAN.md', 'a/b/PLAN.md'), 'exact full path');
  assert.ok(!matchOne('PLAN.markdown', 'PLAN.md'));
  assert.ok(!matchOne('MYPLAN.md', 'PLAN.md'), 'basename must match fully');
});

test('path/*.ext — glob with directory (single segment)', () => {
  assert.ok(matchOne('migrations/001.sql', 'migrations/*.sql'));
  assert.ok(matchOne('migrations/big_name.sql', 'migrations/*.sql'));
  assert.ok(!matchOne('migrations/sub/001.sql', 'migrations/*.sql'), '* does not cross /');
  assert.ok(!matchOne('other/001.sql', 'migrations/*.sql'), 'wrong directory');
  assert.ok(!matchOne('migrations/001.txt', 'migrations/*.sql'), 'wrong extension');
});

test('non-matches and edge cases', () => {
  assert.ok(!matchOne('', '*.lock'));
  assert.ok(!matchOne('foo.lock', ''));
  assert.ok(!matchOne('foo.lock', null));
  assert.ok(!matchOne(null, '*.lock'));
});

test('matchAny — true if ANY pattern matches', () => {
  const patterns = ['*.lock', 'generated/', 'PLAN.md'];
  assert.ok(matchAny('yarn.lock', patterns));
  assert.ok(matchAny('generated/x.ts', patterns));
  assert.ok(matchAny('docs/PLAN.md', patterns));
  assert.ok(!matchAny('src/index.ts', patterns));
});

test('matchAny — empty / nullish pattern list', () => {
  assert.ok(!matchAny('yarn.lock', []));
  assert.ok(!matchAny('yarn.lock', null));
  assert.ok(!matchAny('yarn.lock', undefined));
});
