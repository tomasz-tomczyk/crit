'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { renderDesignPinRowHTML, chipLabel } = require('../design-mode.row.js');

test('design pin row uses screenshot when present', () => {
  const html = renderDesignPinRowHTML({
    id: 'c1', body: 'looks off', dom_anchor: {
      pathname: '/x', screenshot: 'data:image/jpeg;base64,abc',
      outer_html: '<b>', css_selector: 'body',
    },
  });
  assert.ok(html.includes('data:image/jpeg;base64,abc'));
});

test('design pin row omits image when no screenshot, prefers chip label', () => {
  const html = renderDesignPinRowHTML({
    id: 'c1', body: 'looks off', dom_anchor: {
      pathname: '/x', screenshot: '',
      outer_html: '<b>hello</b>', css_selector: 'body',
      accessible_name: 'Submit', tag_chain: ['BUTTON'],
    },
  });
  assert.ok(!html.includes('<img'));
  // Chip label should contain accessible_name; CSS selector should not appear.
  assert.ok(html.includes('Submit'));
  assert.ok(!html.includes('crit-design-comment-selector'));
});

test('design pin row carries author and resolve action', () => {
  const html = renderDesignPinRowHTML({
    id: 'c1', body: 'looks off', author: 'tomek', resolved: false,
    dom_anchor: { pathname: '/x', screenshot: '', outer_html: '', tag_chain: ['DIV'] },
  });
  assert.ok(html.includes('@tomek'));
  assert.ok(html.includes('crit-design-comment-resolve'));
  assert.ok(html.includes('>Resolve<'));
});

test('design pin row says Reopen when resolved', () => {
  const html = renderDesignPinRowHTML({
    id: 'c1', body: 'x', resolved: true,
    dom_anchor: { pathname: '/x', screenshot: '', outer_html: '', tag_chain: ['DIV'] },
  });
  assert.ok(html.includes('>Reopen<'));
  assert.ok(html.includes('data-resolved="true"'));
});

test('chipLabel handles missing fields', () => {
  assert.equal(chipLabel({ tag_chain: ['DIV'] }), '<div>');
  assert.equal(chipLabel({ accessible_name: 'X', tag_chain: ['DIV'] }), 'X');
});
