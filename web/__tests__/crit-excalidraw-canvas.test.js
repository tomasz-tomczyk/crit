'use strict';
// Excalidraw canvas page (preview mode): zoom limits, the view a scene opens
// at, zooming at the pointer, and the grid. The page itself (drawing,
// panning, islands, agent pins) is checked in a browser.

const test = require('node:test');
const assert = require('node:assert/strict');

const canvas = require('../crit-excalidraw-canvas.js');

const viewport = { width: 1000, height: 800 };

test('zoom stays between 10% and 1000%', () => {
  assert.equal(canvas.clampZoom(0.01), 0.1);
  assert.equal(canvas.clampZoom(99), 10);
  assert.equal(canvas.clampZoom(1.5), 1.5);
});

test('a scene opens fitted, never above 100%', () => {
  assert.equal(canvas.fitZoom({ width: 400, height: 300 }, viewport), 1, 'small scenes open at 100%');
  // 2000 × 1000: the width limits it, (1000 - 64) / 2000
  assert.equal(canvas.fitZoom({ width: 2000, height: 1000 }, viewport), 0.468);
  // A very tall scene opens fitted to the width, to stay readable ...
  assert.equal(canvas.fitZoom({ width: 2000, height: 4000 }, viewport), 0.468);
  // ... while Fit shows all of it, (800 - 64) / 4000 ...
  assert.equal(canvas.fitZoom({ width: 2000, height: 4000 }, viewport, true), 0.184);
  // ... down to the 10% floor.
  assert.equal(canvas.fitZoom({ width: 2000, height: 9000 }, viewport, true), 0.1);
});

test('a fitted scene is centred, or shows its top when taller than the view', () => {
  assert.deepEqual(canvas.fitPan({ width: 400, height: 300 }, 1, viewport), { x: 300, y: 250 });
  assert.deepEqual(canvas.fitPan({ width: 2000, height: 4000 }, 0.468, viewport), { x: 32, y: 32 });
});

test('zooming keeps the point under the pointer in place', () => {
  const pan = { x: 120, y: -40 };
  const pointer = { x: 300, y: 200 };
  const next = canvas.zoomAt(pan, 0.5, 2, pointer);
  // The drawing point under the pointer, before and after.
  const before = { x: (pointer.x - pan.x) / 0.5, y: (pointer.y - pan.y) / 0.5 };
  const after = { x: (pointer.x - next.x) / 2, y: (pointer.y - next.y) / 2 };
  assert.ok(Math.abs(before.x - after.x) < 1e-9 && Math.abs(before.y - after.y) < 1e-9);
});

test('grid dots sit on scene multiples of the grid size, thinning when zoomed out', () => {
  // The drawing's top-left corner is scene point (-30, 10), drawn at (100, 50).
  const g = canvas.gridFor(20, 1, { x: 100, y: 50 }, { x: -30, y: 10 });
  assert.equal(g.step, 20);
  // Scene x = 0 is at 100 + 30 = 130 → 130 mod 20 = 10; scene y = 0 is at 40.
  assert.deepEqual([g.x, g.y], [10, 0]);
  assert.equal(canvas.gridFor(20, 0.25, { x: 0, y: 0 }, { x: 0, y: 0 }).step, 25, '5px dots thin to every 5th');
});
