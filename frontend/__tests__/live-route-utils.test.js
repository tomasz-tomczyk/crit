const test = require('node:test');
const assert = require('node:assert/strict');
const utils = require('../live-route-utils.js');

test('extractPathname strips query and hash', () => {
  assert.equal(utils.extractPathname('http://localhost:54322/dash?x=1#y'), '/dash');
  assert.equal(utils.extractPathname('http://localhost:54322/'), '/');
});

test('extractPathname returns "/" for malformed URL', () => {
  assert.equal(utils.extractPathname('not a url'), '/');
  assert.equal(utils.extractPathname(''), '/');
  assert.equal(utils.extractPathname(null), '/');
});

test('normaliseRoute collapses trailing slash except for root', () => {
  assert.equal(utils.normaliseRoute('/dash/'), '/dash');
  assert.equal(utils.normaliseRoute('/'), '/');
  assert.equal(utils.normaliseRoute(''), '/');
  assert.equal(utils.normaliseRoute('/a/b/'), '/a/b');
});

test('isSameOrigin compares scheme+host+port', () => {
  assert.equal(utils.isSameOrigin('http://localhost:3000/x', 'http://localhost:3000/y'), true);
  assert.equal(utils.isSameOrigin('http://localhost:3000', 'http://localhost:3001'), false);
  assert.equal(utils.isSameOrigin('http://localhost:3000', 'https://localhost:3000'), false);
});

test('groupCommentsByRoute groups by path field', () => {
  const cs = [
    { id: 1, path: '/a', body: 'one' },
    { id: 2, path: '/a', body: 'two' },
    { id: 3, path: '/b', body: 'three' },
  ];
  const g = utils.groupCommentsByRoute(cs);
  assert.equal(g.size, 2);
  assert.equal(g.get('/a').length, 2);
  assert.equal(g.get('/b').length, 1);
});

test('groupCommentsByRoute preserves first-seen route order', () => {
  const cs = [
    { id: 1, path: '/b' },
    { id: 2, path: '/a' },
    { id: 3, path: '/b' },
  ];
  assert.deepEqual(Array.from(utils.groupCommentsByRoute(cs).keys()), ['/b', '/a']);
});
