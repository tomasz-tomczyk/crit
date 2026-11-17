// Host matching for remote_images must follow CSP host-source rules, since
// the server puts the same patterns in the page's img-src: a placeholder for
// a host the policy allows (or an <img> for one it blocks) would disagree
// with what the browser does. rewriteHtml needs a DOM and is covered by
// test/e2e/tests/remote-images.filemode.spec.ts.

const test = require('node:test');
const assert = require('node:assert/strict');
const ri = require('../crit-remote-images.js');

test('hostOf reads http(s) and protocol-relative hosts', () => {
  assert.equal(ri.hostOf('https://Img.Shields.io/badge.svg'), 'img.shields.io');
  assert.equal(ri.hostOf('http://example.com:8080/x.png'), 'example.com');
  assert.equal(ri.hostOf('//cdn.example.com/x.png'), 'cdn.example.com');
  assert.equal(ri.hostOf('data:image/png;base64,AAAA'), '');
  assert.equal(ri.hostOf('not a url'), '');
});

test('hostAllowed follows CSP host-source rules', () => {
  const cases = [
    ['img.shields.io', ['img.shields.io'], true],
    ['evil.img.shields.io', ['img.shields.io'], false],
    ['raw.githubusercontent.com', ['*.githubusercontent.com'], true],
    ['a.b.githubusercontent.com', ['*.githubusercontent.com'], true],
    // *.example.com does not match example.com itself
    ['githubusercontent.com', ['*.githubusercontent.com'], false],
    ['evilgithubusercontent.com', ['*.githubusercontent.com'], false],
    ['anything.example', ['*'], true],
    ['example.com', [], false],
    ['', ['*'], false],
  ];
  for (const [host, list, want] of cases) {
    assert.equal(ri.hostAllowed(host, list), want, host + ' vs ' + JSON.stringify(list));
  }
});

test('isBlocked only blocks remote URLs outside the allowlist', () => {
  const list = ['img.shields.io'];
  assert.equal(ri.isBlocked('https://attacker.example/p.png?d=secret', list), true);
  assert.equal(ri.isBlocked('//attacker.example/p.png', list), true);
  assert.equal(ri.isBlocked('HTTPS://attacker.example/p.png', list), true);
  assert.equal(ri.isBlocked('https://img.shields.io/badge/x-y-green', list), false);
  assert.equal(ri.isBlocked('/files/docs/diagram.png', list), false);
  assert.equal(ri.isBlocked('/api/attachments/abc.png', list), false);
  assert.equal(ri.isBlocked('data:image/png;base64,AAAA', list), false);
  assert.equal(ri.isBlocked('images/local.png', list), false);
  assert.equal(ri.isBlocked(null, list), false);
});

test('normalizePatterns lowercases and drops non-strings', () => {
  assert.deepEqual(ri.normalizePatterns([' Img.Shields.IO ', 3, '', '*']), ['img.shields.io', '*']);
  assert.deepEqual(ri.normalizePatterns('img.shields.io'), []);
});

test('rewriteHtml leaves HTML without remote URLs untouched without a DOM', () => {
  const html = '<p><img src="/files/a.png" alt="a"></p>';
  assert.equal(ri.rewriteHtml(html), html);
});
