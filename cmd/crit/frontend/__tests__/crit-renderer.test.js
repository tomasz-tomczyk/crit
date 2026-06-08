const { describe, it } = require('node:test');
const assert = require('node:assert/strict');

const renderer = require('../crit-renderer.js');

describe('crit-renderer', () => {
  it('register and current', () => {
    const mock = {
      scrollToAnchor: () => Promise.resolve(),
      highlightAnchor: () => Promise.resolve(),
      clearHighlight: () => {},
      onAnnotationIntent: () => () => {},
      getMode: () => 'test',
      getAnchorType: () => 'line',
    };
    renderer.register(mock);
    assert.strictEqual(renderer.current(), mock);
    renderer.deregister();
    assert.strictEqual(renderer.current(), null);
  });

  it('register throws on missing method', () => {
    assert.throws(() => renderer.register({}), /ContentRenderer missing/);
  });

  it('anchorFromComment with line anchor', () => {
    const a = renderer.anchorFromComment({ file_path: 'a.go', start_line: 1, end_line: 5 });
    assert.equal(a.type, 'line');
    assert.equal(a.filePath, 'a.go');
    assert.equal(a.startLine, 1);
    assert.equal(a.endLine, 5);
  });

  it('anchorFromComment with DOM anchor', () => {
    const a = renderer.anchorFromComment({ dom_anchor: { pathname: '/', css_selector: '#btn', tag_chain: ['button'] } });
    assert.equal(a.type, 'dom');
    assert.equal(a.pathname, '/');
    assert.equal(a.selector, '#btn');
  });
});
