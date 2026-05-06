'use strict';
const test = require('node:test');
const assert = require('node:assert');
const { ResolutionGate } = require('../design-mode-resolution-gate.js');

test('gate fires immediately when no pending viewport', () => {
  let fired = 0;
  const g = new ResolutionGate(() => fired++);
  g.requestResolution();
  assert.equal(fired, 1);
});

test('gate defers when viewport pending; fires on viewport-applied', () => {
  let fired = 0;
  const g = new ResolutionGate(() => fired++);
  g.beginViewportChange();
  g.requestResolution();
  assert.equal(fired, 0);
  g.onViewportApplied();
  assert.equal(fired, 1);
});

test('gate dedups multiple requestResolution while pending', () => {
  let fired = 0;
  const g = new ResolutionGate(() => fired++);
  g.beginViewportChange();
  g.requestResolution();
  g.requestResolution();
  g.requestResolution();
  g.onViewportApplied();
  assert.equal(fired, 1);
});
