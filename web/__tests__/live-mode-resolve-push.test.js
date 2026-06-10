'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const src = fs.readFileSync(require.resolve('../live-mode.js'), 'utf8');

// Regression: local resolve-click handler must call pushPinsToAgent() on the
// fetch success path. Without it, the live-mode-pin-filter set-pins payload
// is not re-pushed and the resolved (or just-reopened) pin's marker stays
// painted in the iframe overlay until the next chrome boot.
//
// Cross-tab/CLI-driven resolves are covered by the server-side SSE broadcast
// on /resolve. This test guards the originating-tab path only.
test('local resolve handler pushes pins to agent on success', () => {
  // Isolate the resolve PUT block: starts at the fetch to /api/comment/.../resolve
  // and ends at the matching `.finally(...)` that releases the in-flight guard.
  const m = src.match(/fetch\('\/api\/comment\/' \+ encodeURIComponent\(id\) \+ '\/resolve[\s\S]*?\}\);\s*\}\);/);
  assert.ok(m, 'resolve fetch block not found');
  const block = m[0];
  // The .then() success path runs refreshPanel and must also push the new
  // (filtered) pin set to the agent so the marker overlay updates in-place.
  assert.match(block, /refreshPanel\(\);[\s\S]*pushPinsToAgent\(\);/);
});
