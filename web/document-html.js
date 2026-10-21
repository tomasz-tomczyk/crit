(function (root, factory) {
  root.crit = root.crit || {};
  root.crit.documentHtml = factory(root.DOMPurify, root);
})(typeof window !== 'undefined' ? window : globalThis, function (DOMPurify, root) {
  'use strict';

  if (!DOMPurify) throw new Error('DOMPurify must load before document-html.js');

  // Isolated instance so the comment allowlist hooks never apply here.
  // Keep this in sync with crit-web/assets/js/document-html.js.
  var purify = typeof DOMPurify === 'function' ? DOMPurify(root) : DOMPurify;

  // Reviewed documents may carry raw HTML (<details>, <img width>, <kbd>...).
  // DOMPurify's default profile keeps that markup and drops scripts, event
  // handlers and javascript: URLs. <style>, <form> and style attributes would
  // let a document restyle the review page, cover it, or post from it.
  var RAW_CONFIG = { FORBID_TAGS: ['style', 'form'], FORBID_ATTR: ['style'] };
  // Rendered blocks also hold crit's own inline styles (Shiki token colours,
  // table column widths), so the second pass keeps style attributes.
  var RENDERED_CONFIG = { FORBID_TAGS: ['style', 'form'] };

  // Raw HTML from the document.
  function sanitize(html) {
    return purify.sanitize(html, RAW_CONFIG);
  }

  // Block HTML that crit changed as a string after the first pass (word-diff
  // spans), cleaned again before it reaches the page.
  function sanitizeRendered(html) {
    return purify.sanitize(html, RENDERED_CONFIG);
  }

  function hasHtmlInline(tokens) {
    for (var i = 0; i < tokens.length; i++) {
      if (tokens[i].type === 'html_inline') return true;
    }
    return false;
  }

  // Sanitize only the raw HTML a document brings in: html_block tokens, and
  // inline runs that contain html_inline tokens. Cleaning the whole inline run
  // keeps <a href>text</a> together, since its open and close tags are
  // separate tokens. Markdown-it already escapes everything else, so Shiki
  // fences and crit's own markup skip DOMPurify.
  function install(md) {
    var renderInline = md.renderer.renderInline;
    md.renderer.renderInline = function (tokens, options, env) {
      var html = renderInline.call(this, tokens, options, env);
      return hasHtmlInline(tokens) ? sanitize(html) : html;
    };
    md.renderer.rules.html_block = function (tokens, idx) {
      return sanitize(tokens[idx].content);
    };
    return md;
  }

  return { sanitize: sanitize, sanitizeRendered: sanitizeRendered, install: install };
});
