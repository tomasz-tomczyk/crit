const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');

// Load crit-line-blocks.js in a fake-browser shim.
const src = fs.readFileSync(path.join(__dirname, '..', 'crit-line-blocks.js'), 'utf8');
const fn = new Function('window', 'document', src + '\nreturn window;');
const sandbox = {
  window: {
    crit: {
      commentCardHelpers: {
        escapeHtml: function(s) {
          return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
        }
      }
    },
    hljs: {
      getLanguage: function() { return null; },
      highlight: function() { return { value: '' }; }
    }
  },
  document: {}
};
fn(sandbox.window, sandbox.document);
const lineBlocks = sandbox.window.crit.lineBlocks;

// --- splitHighlightedCode ---

test('splitHighlightedCode splits multiline HTML preserving spans across lines', () => {
  const html = '<span class="hljs-keyword">if</span> (x)\n  <span class="hljs-built_in">console</span>.log(y)';
  const result = lineBlocks.splitHighlightedCode(html);
  assert.equal(result.length, 2);
  // First line: span opens and closes within the line
  assert.equal(result[0], '<span class="hljs-keyword">if</span> (x)');
  // Second line: no span wrapping needed since the span was closed on line 1
  assert.equal(result[1], '  <span class="hljs-built_in">console</span>.log(y)');
});

test('splitHighlightedCode reopens spans that cross line boundaries', () => {
  // A span that opens on line 1 but does NOT close until line 2
  const html = '<span class="hljs-string">"hello\nworld"</span>';
  const result = lineBlocks.splitHighlightedCode(html);
  assert.equal(result.length, 2);
  // Line 1: span opened, closed at end of line (synthetic close)
  assert.equal(result[0], '<span class="hljs-string">"hello</span>');
  // Line 2: span reopened at start, then closed properly
  assert.equal(result[1], '<span class="hljs-string">world"</span>');
});

test('splitHighlightedCode handles plain text without spans', () => {
  const result = lineBlocks.splitHighlightedCode('line1\nline2\nline3');
  assert.equal(result.length, 3);
  assert.equal(result[0], 'line1');
  assert.equal(result[1], 'line2');
  assert.equal(result[2], 'line3');
});

// --- findCloseToken ---

test('findCloseToken finds matching close token at depth 1', () => {
  const tokens = [
    { type: 'paragraph_open' },
    { type: 'inline' },
    { type: 'paragraph_close' }
  ];
  assert.equal(lineBlocks.findCloseToken(tokens, 0), 2);
});

test('findCloseToken handles nested tokens correctly', () => {
  const tokens = [
    { type: 'blockquote_open' },
    { type: 'blockquote_open' },
    { type: 'blockquote_close' },
    { type: 'blockquote_close' }
  ];
  assert.equal(lineBlocks.findCloseToken(tokens, 0), 3);
  assert.equal(lineBlocks.findCloseToken(tokens, 1), 2);
});

test('findCloseToken returns openIdx when no close found', () => {
  const tokens = [
    { type: 'paragraph_open' },
    { type: 'inline' }
  ];
  assert.equal(lineBlocks.findCloseToken(tokens, 0), 0);
});

// --- buildCodeLineBlocks ---

test('buildCodeLineBlocks produces one block per line with correct line numbers', () => {
  const file = { content: 'line1\nline2\nline3' };
  const blocks = lineBlocks.buildCodeLineBlocks(file);
  assert.equal(blocks.length, 3);
  assert.equal(blocks[0].startLine, 1);
  assert.equal(blocks[0].endLine, 1);
  assert.equal(blocks[1].startLine, 2);
  assert.equal(blocks[2].startLine, 3);
  // Each block should have code-line class
  assert.equal(blocks[0].cssClass, 'code-line');
});

test('buildCodeLineBlocks marks empty lines', () => {
  const file = { content: 'hello\n\nworld' };
  const blocks = lineBlocks.buildCodeLineBlocks(file);
  assert.equal(blocks[0].isEmpty, false);
  assert.equal(blocks[1].isEmpty, true);
  assert.equal(blocks[2].isEmpty, false);
});

test('buildCodeLineBlocks uses highlight cache when available', () => {
  const file = {
    content: 'var x = 1;',
    highlightCache: { 1: { raw: 'var x = 1;', html: '<span>var</span> x = 1;' } }
  };
  const blocks = lineBlocks.buildCodeLineBlocks(file);
  assert.equal(blocks[0].html, '<code class="hljs"><span>var</span> x = 1;</code>');
});

// --- addGapLineBlocks ---

test('addGapLineBlocks fills gaps between covered ranges', () => {
  const blocks = [];
  const sourceLines = ['alpha', 'beta', 'gamma', 'delta'];
  const result = lineBlocks.addGapLineBlocks(blocks, sourceLines, 1, 3);
  assert.equal(result, 3);
  assert.equal(blocks.length, 2);
  assert.equal(blocks[0].startLine, 2);
  assert.equal(blocks[0].html, 'beta');
  assert.equal(blocks[1].startLine, 3);
  assert.equal(blocks[1].html, 'gamma');
});

test('addGapLineBlocks marks empty lines as isEmpty', () => {
  const blocks = [];
  const sourceLines = ['hello', '', '  ', 'world'];
  lineBlocks.addGapLineBlocks(blocks, sourceLines, 0, 4);
  assert.equal(blocks[0].isEmpty, false);
  assert.equal(blocks[1].isEmpty, true);
  assert.equal(blocks[2].isEmpty, true);
  assert.equal(blocks[3].isEmpty, false);
});

// --- buildLineBlocks ---

test('buildLineBlocks with simple paragraph tokens produces correct blocks', () => {
  const content = 'Hello world\n\nSecond paragraph';
  const tokens = [
    { type: 'paragraph_open', map: [0, 1], nesting: 1, hidden: false },
    { type: 'inline', map: [0, 1], content: 'Hello world', nesting: 0, hidden: false },
    { type: 'paragraph_close', map: null, nesting: -1, hidden: false },
    { type: 'paragraph_open', map: [2, 3], nesting: 1, hidden: false },
    { type: 'inline', map: [2, 3], content: 'Second paragraph', nesting: 0, hidden: false },
    { type: 'paragraph_close', map: null, nesting: -1, hidden: false }
  ];

  const mockMd = {
    options: {},
    renderer: {
      render: function(toks) {
        return '<p>' + toks.filter(function(t) { return t.type === 'inline'; }).map(function(t) { return t.content; }).join('') + '</p>';
      }
    }
  };

  const blocks = lineBlocks.buildLineBlocks(tokens, mockMd, content);
  // Should have: paragraph block (line 1), gap block (line 2 empty), paragraph block (line 3)
  assert.equal(blocks.length, 3);
  assert.equal(blocks[0].startLine, 1);
  assert.equal(blocks[0].endLine, 1);
  assert.ok(blocks[0].html.includes('Hello world'));
  assert.equal(blocks[1].startLine, 2); // gap line (empty)
  assert.equal(blocks[1].isEmpty, true);
  assert.equal(blocks[2].startLine, 3);
  assert.equal(blocks[2].endLine, 3);
  assert.ok(blocks[2].html.includes('Second paragraph'));
});

test('buildLineBlocks skips hidden tokens', () => {
  const content = 'visible line';
  const tokens = [
    { type: 'paragraph_open', map: [0, 1], nesting: 1, hidden: true },
    { type: 'inline', map: [0, 1], content: 'visible line', nesting: 0, hidden: true },
    { type: 'paragraph_close', map: null, nesting: -1, hidden: true }
  ];

  const mockMd = {
    options: {},
    renderer: { render: function() { return '<p>x</p>'; } }
  };

  const blocks = lineBlocks.buildLineBlocks(tokens, mockMd, content);
  // Hidden tokens skipped, but gap-line blocks cover all source lines
  assert.equal(blocks.length, 1);
  assert.equal(blocks[0].startLine, 1);
  // Gap block for the uncovered line
  assert.equal(blocks[0].html, 'visible line');
});
