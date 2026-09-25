const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');
const markdownit = require('markdown-it');

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
      },
      // Stand-in for crit-code-highlight.js: yaml "grammar" colours keys.
      codeHighlight: {
        lines: function(content, lang) {
          if (lang !== 'yaml') return null;
          return content.replace(/\n$/, '').split('\n').map(function(l) {
            return l.replace(/^(\w+):/, '<span style="--diffs-token-light:#a00;--diffs-token-dark:#f66">$1</span>:');
          });
        }
      }
    }
  },
  document: {}
};
fn(sandbox.window, sandbox.document);
const lineBlocks = sandbox.window.crit.lineBlocks;

// --- rewriteFrontmatterAsYamlFence ---

test('rewriteFrontmatterAsYamlFence converts only valid opening and closing delimiters', () => {
  const content = '---\nname: demo\n...\n\n# Body';
  assert.equal(
    lineBlocks.rewriteFrontmatterAsYamlFence(content),
    '```yaml\nname: demo\n```\n\n# Body'
  );
});

test('rewriteFrontmatterAsYamlFence allows trailing spaces on delimiters', () => {
  const content = '---  \nname: demo\n---\t\n\n# Body';
  assert.equal(
    lineBlocks.rewriteFrontmatterAsYamlFence(content),
    '```yaml\nname: demo\n```\n\n# Body'
  );
});

test('rewriteFrontmatterAsYamlFence preserves content without a closing delimiter', () => {
  const content = '---\nname: demo\n# Not a closing delimiter';
  assert.equal(lineBlocks.rewriteFrontmatterAsYamlFence(content), content);
});

test('rewriteFrontmatterAsYamlFence ignores delimiters that are not on the first line', () => {
  const content = 'Intro\n---\nname: demo\n---';
  assert.equal(lineBlocks.rewriteFrontmatterAsYamlFence(content), content);
});

test('rewriteFrontmatterAsYamlFence preserves line count and document maps', () => {
  const content = [
    '\uFEFF---',
    'name: demo',
    '# not a heading',
    'paths:',
    '  - "src/**/*.go"',
    '---',
    '',
    '# Skill Body'
  ].join('\n');
  const rewritten = lineBlocks.rewriteFrontmatterAsYamlFence(content);
  const tokens = [
    { type: 'fence', map: [0, 6], info: 'yaml', content: 'name: demo\n# not a heading\npaths:\n  - "src/**/*.go"\n' },
    { type: 'heading_open', map: [7, 8], nesting: 1, tag: 'h1' },
    { type: 'inline', map: [7, 8], nesting: 0, content: 'Skill Body' },
    { type: 'heading_close', map: null, nesting: -1, tag: 'h1' }
  ];
  const md = {
    options: {},
    renderer: {
      render: function(renderedTokens) {
        return renderedTokens.some(token => token.type === 'heading_open')
          ? '<h1>Skill Body</h1>'
          : '';
      }
    }
  };
  const blocks = lineBlocks.buildLineBlocks(tokens, md, content);

  assert.equal(rewritten.split('\n').length, content.split('\n').length);
  assert.equal(tokens[0].info, 'yaml');
  assert.equal(tokens.some(token => token.type === 'heading_open' && token.map[0] === 2), false);

  for (let line = 1; line <= 8; line++) {
    assert.ok(blocks.some(block => block.startLine === line), `line ${line} is commentable`);
  }
  assert.equal(blocks[0].html, '<span class="fence-marker">﻿---</span>');
  assert.match(blocks[1].html, /code class="crit-code"/);
  assert.match(blocks[1].html, /--diffs-token-light/);
  assert.equal(blocks[5].html, '<span class="fence-marker">---</span>');
  assert.ok(blocks.some(block => block.startLine === 8 && /<h1/.test(block.html)));
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

// --- rendered tables ---

test('buildLineBlocks groups table rows for native table rendering', () => {
  const md = markdownit();
  const source = '| Label | Status |\n' +
    '| --- | --- |\n' +
    '| [x](https://example.com/a/very/long/hidden/path) | available |';
  const blocks = lineBlocks.buildLineBlocks(md.parse(source, {}), md, source);

  assert.equal(blocks.length, 3);
  assert.equal(blocks[0].tableId, blocks[1].tableId);
  assert.equal(blocks[1].tableId, blocks[2].tableId);
  assert.equal(blocks[0].tableSection, 'thead');
  assert.equal(blocks[1].cssClass, 'table-separator');
  assert.equal(blocks[2].tableSection, 'tbody');
  assert.match(blocks[0].nativeRowHtml, /^<tr>/);
  assert.doesNotMatch(blocks[0].nativeRowHtml, /<table|<colgroup/);
  assert.match(blocks[0].html, /^<table class="split-table" data-table-id="table-0">/);
  assert.match(blocks[0].html, /<col style="width:35\.71%"><col style="width:64\.29%">/);
  assert.match(blocks[2].html, /<col style="width:35\.71%"><col style="width:64\.29%">/);
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
