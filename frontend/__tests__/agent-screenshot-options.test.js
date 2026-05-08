'use strict';
// Regression test for Bug 2: pin screenshots came back pure white on dark
// themes because html2canvas defaults backgroundColor to '#ffffff' when the
// captured element has no own background. Logic in agent-screenshot-options.js
// mirrors the inline copy in crit-agent.js — they must agree.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const so = require('../agent-screenshot-options.js');

test('isTransparent recognises common no-background values', () => {
  assert.equal(so.isTransparent(''), true);
  assert.equal(so.isTransparent(null), true);
  assert.equal(so.isTransparent('transparent'), true);
  assert.equal(so.isTransparent('rgba(0, 0, 0, 0)'), true);
  assert.equal(so.isTransparent('rgba(255,255,255,0)'), true);
});

test('isTransparent treats any non-zero alpha as a real background', () => {
  assert.equal(so.isTransparent('rgb(0,0,0)'), false);
  assert.equal(so.isTransparent('#ffffff'), false);
  assert.equal(so.isTransparent('rgba(0,0,0,1)'), false);
  assert.equal(so.isTransparent('rgba(0,0,0,0.01)'), false);
});

// Tiny element-tree mock — we only need parentElement chain walking and a
// getComputedStyle injector.
function mkEl(bg, parent) {
  return { nodeType: 1, _bg: bg, parentElement: parent || null };
}
function mockDoc(body, html) {
  return { body: body || null, documentElement: html || null };
}
function gcsFactory() {
  return (el) => ({ backgroundColor: el && el._bg ? el._bg : '' });
}

test('resolvePageBackground walks ancestors and returns first opaque bg', () => {
  const html = mkEl('rgb(20, 20, 20)');
  const body = mkEl('transparent', html);
  const parent = mkEl('rgba(0,0,0,0)', body);
  const target = mkEl('', parent);
  const bg = so.resolvePageBackground(target, mockDoc(body, html), gcsFactory());
  assert.equal(bg, 'rgb(20, 20, 20)');
});

test('resolvePageBackground prefers nearest non-transparent ancestor', () => {
  const html = mkEl('rgb(255, 255, 255)');
  const body = mkEl('rgb(10, 10, 10)', html);
  const target = mkEl('', body);
  const bg = so.resolvePageBackground(target, mockDoc(body, html), gcsFactory());
  assert.equal(bg, 'rgb(10, 10, 10)');
});

test('resolvePageBackground returns null when nothing has a background', () => {
  const html = mkEl('transparent');
  const body = mkEl('', html);
  const target = mkEl('', body);
  const bg = so.resolvePageBackground(target, mockDoc(body, html), gcsFactory());
  assert.equal(bg, null);
});

test('buildCaptureOptions never lets html2canvas default to white', () => {
  // Critical regression: opts.backgroundColor must always be defined. If we
  // omit it, html2canvas falls back to '#ffffff' and floods dark themes.
  const html = mkEl('rgb(15, 15, 15)');
  const body = mkEl('transparent', html);
  const target = mkEl('', body);
  const rect = { left: 10, top: 20, width: 100, height: 200 };
  const opts = so.buildCaptureOptions(target, rect, { x: 0, y: 0 }, mockDoc(body, html), gcsFactory());
  assert.equal(opts.backgroundColor, 'rgb(15, 15, 15)');
  assert.equal(opts.scale, 1);
  assert.equal(opts.logging, false);
  assert.equal(opts.x, 10);
  assert.equal(opts.y, 20);
  assert.equal(opts.width, 100);
  assert.equal(opts.height, 200);
});

test('buildCaptureOptions falls back to transparent (null) when no bg found', () => {
  // Explicit `null` tells html2canvas to render a transparent backdrop —
  // strictly better than white on a dark theme, even if we couldn't pick
  // up the real colour.
  const html = mkEl('transparent');
  const body = mkEl('', html);
  const target = mkEl('', body);
  const opts = so.buildCaptureOptions(target, null, { x: 0, y: 0 }, mockDoc(body, html), gcsFactory());
  assert.equal(opts.backgroundColor, null);
  assert.ok(!Object.prototype.hasOwnProperty.call(opts, 'x'));
});

test('buildCaptureOptions applies scroll offset to crop coordinates', () => {
  const html = mkEl('rgb(0,0,0)');
  const body = mkEl('', html);
  const target = mkEl('', body);
  const rect = { left: 5, top: 7, width: 50, height: 60 };
  const opts = so.buildCaptureOptions(target, rect, { x: 100, y: 200 }, mockDoc(body, html), gcsFactory());
  assert.equal(opts.x, 105);
  assert.equal(opts.y, 207);
});
