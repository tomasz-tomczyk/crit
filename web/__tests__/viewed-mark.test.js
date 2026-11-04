'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const app = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');
const source = app.match(/  function viewedMarkIsCurrent\([\s\S]*?\n  }/)[0];
const isCurrent = (mark, file, head) => new Function('session', `${source}; return viewedMarkIsCurrent;`)(
  { focus: head ? { head_sha: head } : undefined })(mark, file);

test('a mark on the same content is current', () => {
  assert.equal(isCurrent({ file_hash: 'a', head_sha: 'old' }, { fileHash: 'a' }, 'new'), true);
});

test('a mark on different content is stale', () => {
  assert.equal(isCurrent({ file_hash: 'a', head_sha: 'same' }, { fileHash: 'b' }, 'same'), false);
});

test('a lazy file falls back to the revision', () => {
  assert.equal(isCurrent({ file_hash: 'a', head_sha: 'old' }, { fileHash: '' }, 'new'), false);
  assert.equal(isCurrent({ file_hash: 'a', head_sha: 'same' }, { fileHash: '' }, 'same'), true);
});

test('a mark with nothing to compare is kept', () => {
  assert.equal(isCurrent({}, { fileHash: 'a' }, 'new'), true);
  assert.equal(isCurrent({ head_sha: 'old' }, { fileHash: '' }, undefined), true);
});
