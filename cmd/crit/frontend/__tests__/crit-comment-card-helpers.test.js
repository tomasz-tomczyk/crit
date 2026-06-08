'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const helpers = require('../crit-comment-card-helpers.js');

test('escapeHtml escapes &, <, >, ", and single quotes', () => {
  assert.equal(helpers.escapeHtml('a & b'), 'a &amp; b');
  assert.equal(helpers.escapeHtml('<script>'), '&lt;script&gt;');
  assert.equal(helpers.escapeHtml('"x"'), '&quot;x&quot;');
  assert.equal(helpers.escapeHtml("it's"), 'it&#39;s');
});

test('escapeHtml handles null/undefined', () => {
  assert.equal(helpers.escapeHtml(null), '');
  assert.equal(helpers.escapeHtml(undefined), '');
});

test('escapeHtml stringifies non-strings', () => {
  assert.equal(helpers.escapeHtml(42), '42');
  assert.equal(helpers.escapeHtml(true), 'true');
});

test('relativeTime returns "just now" under a minute', () => {
  const dateStr = new Date(Date.now() - 5 * 1000).toISOString();
  assert.equal(helpers.relativeTime(dateStr), 'just now');
});

test('relativeTime returns minutes', () => {
  const dateStr = new Date(Date.now() - 5 * 60 * 1000).toISOString();
  assert.equal(helpers.relativeTime(dateStr), '5m ago');
});

test('relativeTime returns hours', () => {
  const dateStr = new Date(Date.now() - 3 * 3600 * 1000).toISOString();
  assert.equal(helpers.relativeTime(dateStr), '3h ago');
});

test('relativeTime returns days', () => {
  const dateStr = new Date(Date.now() - 2 * 86400 * 1000).toISOString();
  assert.equal(helpers.relativeTime(dateStr), '2d ago');
});

test('relativeTime returns weeks', () => {
  const dateStr = new Date(Date.now() - 3 * 604800 * 1000).toISOString();
  assert.equal(helpers.relativeTime(dateStr), '3w ago');
});

test('formatTime returns empty string on falsy input', () => {
  assert.equal(helpers.formatTime(''), '');
  assert.equal(helpers.formatTime(null), '');
  assert.equal(helpers.formatTime(undefined), '');
});

test('formatTime returns hh:mm style string for an ISO date', () => {
  const out = helpers.formatTime('2024-01-15T14:30:00Z');
  // Locale output varies by platform; just sanity-check it contains digits + ":"
  assert.match(out, /\d{1,2}:\d{2}/);
});

test('formKeyFor produces convention-based key from id + kind', () => {
  assert.equal(helpers.formKeyFor('abc123', 'edit'), 'comment:edit:abc123');
  assert.equal(helpers.formKeyFor('abc123', 'reply'), 'comment:reply:abc123');
});

test('formKeyFor produces distinct keys for different kinds', () => {
  const id = 'X';
  assert.notEqual(helpers.formKeyFor(id, 'edit'), helpers.formKeyFor(id, 'reply'));
});

test('authorColorIndex is deterministic and bounded by AUTHOR_COLOR_COUNT', () => {
  assert.ok(helpers.AUTHOR_COLOR_COUNT >= 1);
  const a = helpers.authorColorIndex('alice');
  const b = helpers.authorColorIndex('alice');
  assert.equal(a, b);
  assert.ok(a >= 0 && a < helpers.AUTHOR_COLOR_COUNT);
});

test('authorColorIndex produces different slots for different names (usually)', () => {
  // Not strictly guaranteed for all pairs, but these two diverge in practice.
  assert.notEqual(helpers.authorColorIndex('alice'), helpers.authorColorIndex('bob'));
});

test('authorColorIndex tolerates empty / null', () => {
  assert.equal(helpers.authorColorIndex(''), 0);
  assert.equal(helpers.authorColorIndex(null), 0);
  assert.equal(helpers.authorColorIndex(undefined), 0);
});

test('class constants are stable strings', () => {
  assert.equal(helpers.BTN_CLASS, 'btn');
  assert.equal(helpers.BTN_SM_CLASS, 'btn btn-sm');
  assert.equal(helpers.COMMENT_CARD_CLASS, 'comment-card');
  assert.equal(helpers.COMMENT_BODY_CLASS, 'comment-body');
  assert.equal(helpers.COMMENT_ACTIONS_CLASS, 'comment-actions');
});
