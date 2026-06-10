'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const src = fs.readFileSync(require.resolve('../live-mode.js'), 'utf8');

test('closeComposer does not mutate state.mode', () => {
  // Source-level guard: the closeComposer body must not assign to state.mode.
  const m = src.match(/function closeComposer\(\)[\s\S]*?\n  \}/);
  assert.ok(m, 'closeComposer not found');
  assert.ok(!/state\.mode\s*=/.test(m[0]), 'closeComposer must not mutate mode');
});
