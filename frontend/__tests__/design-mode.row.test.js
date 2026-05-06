'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { renderDesignPinRowHTML, chipLabel, renderRepliesHTML, renderReplyComposerHTML } = require('../design-mode.row.js');

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

test('design pin row renders Reply button next to Resolve', () => {
  const html = renderDesignPinRowHTML({
    id: 'c1', body: 'x',
    dom_anchor: { pathname: '/x', screenshot: '', outer_html: '', tag_chain: ['DIV'] },
  });
  assert.ok(html.includes('crit-design-comment-reply'));
  assert.ok(html.includes('crit-design-comment-resolve'));
  // Reply button carries comment id + pathname for the dispatch handler.
  assert.ok(html.includes('data-comment-id="c1"'));
  assert.ok(html.includes('data-pathname="/x"'));
});

test('design pin row renders existing replies with author + body', () => {
  const html = renderDesignPinRowHTML({
    id: 'c1', body: 'parent',
    replies: [
      { id: 'r1', author: 'alice', body: 'agree', created_at: '2026-05-06T12:00:00Z' },
      { id: 'r2', author: 'bob', body: 'nope' },
    ],
    dom_anchor: { pathname: '/x', tag_chain: ['DIV'] },
  });
  assert.ok(html.includes('crit-design-comment-replies'));
  assert.ok(html.includes('@alice'));
  assert.ok(html.includes('@bob'));
  assert.ok(html.includes('agree'));
  assert.ok(html.includes('nope'));
  assert.ok(html.includes('data-reply-id="r1"'));
});

test('renderRepliesHTML returns empty string when no replies', () => {
  assert.equal(renderRepliesHTML(undefined), '');
  assert.equal(renderRepliesHTML([]), '');
});

test('design pin row renders inline reply composer when _replyOpen is true', () => {
  const html = renderDesignPinRowHTML({
    id: 'c1', body: 'x', _replyOpen: true, _replyDraft: 'thinking',
    dom_anchor: { pathname: '/x', tag_chain: ['DIV'] },
  });
  assert.ok(html.includes('crit-design-reply-composer'));
  assert.ok(html.includes('crit-design-reply-textarea'));
  assert.ok(html.includes('crit-design-reply-save'));
  assert.ok(html.includes('crit-design-reply-cancel'));
  // Draft preserved in textarea body.
  assert.ok(html.includes('thinking'));
});

test('design pin row hides reply composer when _replyOpen is falsy', () => {
  const html = renderDesignPinRowHTML({
    id: 'c1', body: 'x',
    dom_anchor: { pathname: '/x', tag_chain: ['DIV'] },
  });
  assert.ok(!html.includes('crit-design-reply-composer'));
});

test('renderReplyComposerHTML escapes draft text', () => {
  const html = renderReplyComposerHTML('c1', '/x', '<script>alert(1)</script>');
  assert.ok(!html.includes('<script>alert(1)</script>'));
  assert.ok(html.includes('&lt;script&gt;'));
});
