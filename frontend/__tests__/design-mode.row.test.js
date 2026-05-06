'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { chipLabel, renderDesignPinRow } = require('../design-mode.row.js');

test('chipLabel handles missing fields', () => {
  assert.equal(chipLabel({ tag_chain: ['DIV'] }), '<div>');
  assert.equal(chipLabel({ accessible_name: 'X', tag_chain: ['DIV'] }), 'X');
});

test('chipLabel prefers accessible_name over outer_html', () => {
  assert.equal(
    chipLabel({ accessible_name: 'Submit', outer_html: '<b>noise</b>', tag_chain: ['BUTTON'] }),
    'Submit',
  );
});

test('chipLabel falls back to outer_html text when no accessible_name', () => {
  assert.equal(
    chipLabel({ accessible_name: '', outer_html: '<b>hello</b>', tag_chain: ['B'] }),
    'hello',
  );
});

test('chipLabel falls back to tag name as a last resort', () => {
  assert.equal(chipLabel({ tag_chain: ['SECTION'] }), '<section>');
  assert.equal(chipLabel({}), 'element');
});

test('chipLabel truncates long values to 60 chars + ellipsis', () => {
  const long = 'x'.repeat(80);
  const out = chipLabel({ accessible_name: long, tag_chain: ['DIV'] });
  assert.equal(out.length, 61); // 60 + ellipsis
  assert.ok(out.endsWith('…'));
});

test('renderDesignPinRow returns a fallback element when buildCommentCard is unavailable', () => {
  // Simulate a Node-side environment where window.crit.commentCard is not
  // wired. The row falls back to a minimal div so design mode still renders
  // even if the shared module is missing.
  const origDocument = global.document;
  const origWindow = global.window;
  global.document = {
    createElement(tag) {
      const el = {
        tagName: tag.toUpperCase(), className: '', textContent: '',
        dataset: {}, children: [],
        appendChild(c) { this.children.push(c); return c; },
      };
      return el;
    },
  };
  global.window = { crit: {} };
  try {
    const out = renderDesignPinRow(
      { id: 'c1', body: 'looks off', dom_anchor: { pathname: '/x' } },
      {},
    );
    assert.equal(out.dataset.id, 'c1');
    assert.equal(out.dataset.commentId, 'c1');
    assert.equal(out.dataset.designRoute, '/x');
    assert.equal(out.textContent, 'looks off');
  } finally {
    if (origDocument === undefined) delete global.document; else global.document = origDocument;
    if (origWindow === undefined) delete global.window; else global.window = origWindow;
  }
});
