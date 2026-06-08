(function () {
  'use strict';

  var active = null;

  function anchorFromComment(comment) {
    if (comment.domAnchor || comment.dom_anchor) {
      var da = comment.domAnchor || comment.dom_anchor;
      return {
        type: 'dom',
        pathname: da.pathname,
        selector: da.css_selector || da.selector,
        tagChain: da.tag_chain,
        accessibleName: da.accessible_name,
        role: da.role,
        landmark: da.landmark,
      };
    }
    return {
      type: 'line',
      filePath: comment.filePath || comment.file_path,
      startLine: comment.startLine || comment.start_line,
      endLine: comment.endLine || comment.end_line,
      side: comment.side,
    };
  }

  function register(renderer) {
    var required = [
      'scrollToAnchor', 'highlightAnchor', 'clearHighlight',
      'onAnnotationIntent', 'getMode', 'getAnchorType'
    ];
    for (var i = 0; i < required.length; i++) {
      if (typeof renderer[required[i]] !== 'function') {
        throw new Error('ContentRenderer missing: ' + required[i]);
      }
    }
    active = renderer;
  }

  function deregister() {
    active = null;
  }

  function current() {
    return active;
  }

  var api = {
    register: register,
    deregister: deregister,
    current: current,
    anchorFromComment: anchorFromComment,
  };

  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.renderer = api;
  }

  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
