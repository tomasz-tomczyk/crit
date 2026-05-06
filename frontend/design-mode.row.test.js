'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { renderDesignPinRowHTML } = require('./design-mode.row.js');

test('design pin row uses screenshot when present', () => {
  const html = renderDesignPinRowHTML({
    id: 'c1', body: 'looks off', dom_anchor: {
      pathname: '/x', screenshot: 'data:image/jpeg;base64,abc',
      outer_html: '<b>', css_selector: 'body',
    },
  });
  assert.ok(html.includes('data:image/jpeg;base64,abc'));
});

test('design pin row falls back to outer_html preview', () => {
  const html = renderDesignPinRowHTML({
    id: 'c1', body: 'looks off', dom_anchor: {
      pathname: '/x', screenshot: '',
      outer_html: '<b>', css_selector: 'body',
    },
  });
  assert.ok(!html.includes('<img'));
  assert.ok(html.includes('&lt;b&gt;'));
});
