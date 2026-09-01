const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const markdownit = require('markdown-it');

// Keep in sync with web/comment-html.js
const BOLD_BLOCKQUOTE_LINE = /^\*\*\s*>\s*(.+?)\s*\*\*$/gm;

function normalizeCommentMarkdown(src) {
  if (!src) return src;
  return String(src).replace(BOLD_BLOCKQUOTE_LINE, '> $1');
}

function makeCommentMd() {
  const md = markdownit({
    html: true,
    linkify: true,
    typographer: true,
    highlight() { return ''; },
  });
  md.disable('replacements');
  return md;
}

test('normalizeCommentMarkdown converts **> question** lines to blockquotes', () => {
  const md = makeCommentMd();
  const body = [
    'Intro paragraph.',
    '',
    '**> are the 280 jobs actually staggered or do they all fire at once anyway?**',
    '',
    'Answer paragraph.',
  ].join('\n');

  const html = md.render(normalizeCommentMarkdown(body));
  assert.match(html, /<blockquote>\s*<p>are the 280 jobs/);
  assert.doesNotMatch(html, /&gt; are the 280 jobs/);
  assert.doesNotMatch(html, /<strong>/);
});

test('normalizeCommentMarkdown leaves proper blockquotes unchanged', () => {
  const md = makeCommentMd();
  const html = md.render(normalizeCommentMarkdown('> already a blockquote'));
  assert.match(html, /<blockquote>\s*<p>already a blockquote<\/p>\s*<\/blockquote>/);
});

test('comment-html.js exports normalizeCommentMarkdown', () => {
  const src = fs.readFileSync(path.join(__dirname, '..', 'comment-html.js'), 'utf8');
  assert.match(src, /function normalizeCommentMarkdown/);
  assert.match(src, /normalizeCommentMarkdown: normalizeCommentMarkdown/);
});

test('style.css: comment and reply blockquotes use document blockquote tokens', () => {
  const css = fs.readFileSync(path.join(__dirname, '..', 'style.css'), 'utf8');
  assert.match(css, /\.comment-body blockquote[\s\S]*?var\(--crit-blockquote-border\)/);
  assert.match(css, /\.comment-body blockquote[\s\S]*?var\(--crit-blockquote-bg\)/);
  assert.match(css, /\.reply-body blockquote[\s\S]*?var\(--crit-blockquote-border\)/);
});
