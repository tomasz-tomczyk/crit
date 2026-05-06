'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { renderComposerHTML } = require('./design-mode.composer.js');

test('renderComposerHTML escapes user-controlled fields', () => {
  const html = renderComposerHTML({
    pathname: '/x', css_selector: '<script>',
    tag_chain: [], outer_html: '<b>', screenshot: '',
    viewport_width: 1, viewport_height: 1,
    accessible_name: '"', role: '', landmark: '',
  });
  assert.ok(!html.includes('<script>'));
  assert.ok(html.includes('&lt;script&gt;'));
});

test('renderComposerHTML shows screenshot when present', () => {
  const html = renderComposerHTML({
    pathname: '/x', css_selector: 'body', tag_chain: ['BODY'],
    outer_html: '<body></body>',
    screenshot: 'data:image/jpeg;base64,abc',
    viewport_width: 1, viewport_height: 1,
    accessible_name: '', role: '', landmark: '',
  });
  assert.ok(html.includes('data:image/jpeg;base64,abc'));
});

test('renderComposerHTML shows outerHTML preview when no screenshot', () => {
  const html = renderComposerHTML({
    pathname: '/x', css_selector: 'body', tag_chain: ['BODY'],
    outer_html: '<body></body>',
    screenshot: '',
    viewport_width: 1, viewport_height: 1,
    accessible_name: '', role: '', landmark: '',
  });
  assert.ok(html.includes('&lt;body&gt;&lt;/body&gt;'));
});
