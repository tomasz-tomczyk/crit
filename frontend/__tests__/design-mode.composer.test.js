'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { renderComposerHTML, chipLabel } = require('../design-mode.composer.js');

test('renderComposerHTML escapes user-controlled fields', () => {
  const html = renderComposerHTML({
    pathname: '/x', css_selector: '<script>',
    tag_chain: [], outer_html: '<b>hi</b>', screenshot: '',
    viewport_width: 1, viewport_height: 1,
    accessible_name: '"<x>', role: '', landmark: '',
  });
  assert.ok(!html.includes('<script>'));
  assert.ok(!html.includes('<x>'), 'accessible_name must be escaped');
  // selector must NOT appear in the composer chrome (drop verbose CSS path).
  assert.ok(!html.includes('css_selector'));
});

test('renderComposerHTML shows screenshot thumbnail when present', () => {
  const html = renderComposerHTML({
    pathname: '/x', css_selector: 'body', tag_chain: ['BODY'],
    outer_html: '<body></body>',
    screenshot: 'data:image/jpeg;base64,abc',
    viewport_width: 1, viewport_height: 1,
    accessible_name: 'Hi', role: '', landmark: '',
  });
  assert.ok(html.includes('data:image/jpeg;base64,abc'));
});

test('renderComposerHTML omits outerHTML preview when no screenshot', () => {
  const html = renderComposerHTML({
    pathname: '/x', css_selector: 'body', tag_chain: ['BODY'],
    outer_html: '<body>hello world</body>',
    screenshot: '',
    viewport_width: 1, viewport_height: 1,
    accessible_name: 'Greeting', role: '', landmark: '',
  });
  // No html-preview <pre>; just the chip label drives identification.
  assert.ok(!html.includes('crit-design-composer-html-preview'));
  assert.ok(html.includes('Greeting'));
});

test('renderComposerHTML uses Cancel/Save with crit btn classes', () => {
  const html = renderComposerHTML({
    pathname: '/', css_selector: 'body', tag_chain: ['BODY'],
    outer_html: '', screenshot: '',
    viewport_width: 1, viewport_height: 1,
    accessible_name: '', role: '', landmark: '',
  });
  assert.ok(html.includes('class="btn btn-sm crit-design-composer-cancel"'));
  assert.ok(html.includes('class="btn btn-sm btn-primary crit-design-composer-save"'));
  assert.ok(html.includes('class="comment-form-actions"'));
});

test('chipLabel prefers accessible_name', () => {
  assert.equal(
    chipLabel({ accessible_name: 'Submit', outer_html: '<button>x</button>', tag_chain: ['BUTTON'] }),
    'Submit',
  );
});

test('chipLabel falls back to text content slice', () => {
  assert.equal(
    chipLabel({ accessible_name: '', outer_html: '<p>Hello there</p>', tag_chain: ['P'] }),
    'Hello there',
  );
});

test('chipLabel falls back to tag name when nothing else', () => {
  assert.equal(
    chipLabel({ accessible_name: '', outer_html: '', tag_chain: ['DIV'] }),
    '<div>',
  );
});

test('chipLabel truncates long names', () => {
  const long = 'x'.repeat(120);
  const out = chipLabel({ accessible_name: long, outer_html: '', tag_chain: [] });
  assert.ok(out.length <= 61); // 60 + ellipsis
  assert.ok(out.endsWith('…'));
});
