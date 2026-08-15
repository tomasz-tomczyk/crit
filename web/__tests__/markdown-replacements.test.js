const { describe, it } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const markdownit = require('markdown-it');

// Regression test for #820: markdown-it's typographer rule turns literal
// (c)/(r)/(tm) into ©/®/™, which mangles enumerated options in review docs.
// We disable only the replacements rule so smart quotes still work.

function makeDocumentMd() {
  const md = markdownit({
    html: true,
    typographer: true,
    linkify: true,
    highlight() { return ''; }
  });
  md.disable('replacements');
  return md;
}

function makeCommentMd() {
  const md = markdownit({
    html: false,
    linkify: true,
    typographer: true,
    highlight() { return ''; }
  });
  md.disable('replacements');
  return md;
}

describe('markdown-it replacements disabled (#820)', () => {
  it('preserves literal (c), (r), (tm) in document renderer', () => {
    const md = makeDocumentMd();
    const html = md.render('Three ways: (a) keep, (b) drop, (c) split. (r) (tm)');
    assert.match(html, /\(c\)/);
    assert.match(html, /\(r\)/);
    assert.match(html, /\(tm\)/);
    assert.doesNotMatch(html, /©/);
    assert.doesNotMatch(html, /®/);
    assert.doesNotMatch(html, /™/);
  });

  it('preserves literal (c), (r), (tm) in comment renderer', () => {
    const md = makeCommentMd();
    const html = md.render('Option (c) is best. (r) (tm)');
    assert.match(html, /\(c\)/);
    assert.match(html, /\(r\)/);
    assert.match(html, /\(tm\)/);
    assert.doesNotMatch(html, /©/);
    assert.doesNotMatch(html, /®/);
    assert.doesNotMatch(html, /™/);
  });

  it('still converts straight quotes to smart quotes', () => {
    const md = makeDocumentMd();
    const html = md.render('"hello"');
    assert.match(html, /“hello”/);
  });

  it('has .disable("replacements") applied in web/app.js for both renderers', () => {
    const appJsPath = path.join(__dirname, '..', 'app.js');
    const appJs = fs.readFileSync(appJsPath, 'utf8');

    // Each markdown-it instance in app.js should be followed by the disable call.
    const documentMdBlock = appJs.indexOf('const documentMd = window.markdownit({');
    const commentMdBlock = appJs.indexOf('const commentMd = window.markdownit({');
    assert.notEqual(documentMdBlock, -1, 'documentMd instance not found');
    assert.notEqual(commentMdBlock, -1, 'commentMd instance not found');

    const documentDisable = appJs.indexOf("documentMd.disable('replacements')", documentMdBlock);
    const commentDisable = appJs.indexOf("commentMd.disable('replacements')", commentMdBlock);
    assert.notEqual(documentDisable, -1, 'documentMd.disable("replacements") not found');
    assert.notEqual(commentDisable, -1, 'commentMd.disable("replacements") not found');
  });
});
