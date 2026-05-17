'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { makeOriginGuard } = require('../live-mode.origin.js');

test('guard accepts matching source and origin', () => {
  const g = makeOriginGuard({
    expectSource: 'iframeWin',
    expectOrigin: 'http://localhost:54322',
  });
  assert.equal(g({ source: 'iframeWin', origin: 'http://localhost:54322' }), true);
});

test('guard rejects mismatched source', () => {
  const g = makeOriginGuard({
    expectSource: 'iframeWin', expectOrigin: 'http://localhost:54322',
  });
  assert.equal(g({ source: 'attacker', origin: 'http://localhost:54322' }), false);
});

test('guard rejects mismatched origin', () => {
  const g = makeOriginGuard({
    expectSource: 'iframeWin', expectOrigin: 'http://localhost:54322',
  });
  assert.equal(g({ source: 'iframeWin', origin: 'https://evil.example' }), false);
});
