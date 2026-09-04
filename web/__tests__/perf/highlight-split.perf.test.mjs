// Perf: splitHighlightedCode on a 2000-line highlighted code block.
// Run: npm run test:perf.
//
// splitHighlightedCode tracks open <span> tags across lines to preserve
// syntax highlighting when fences are split per-line. A 2000-line fence is
// the realistic worst case (large generated code blocks in plans).

import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { medianMs } from './bench-utils.mjs';

const __dirname = dirname(fileURLToPath(import.meta.url));
const src = readFileSync(resolve(__dirname, '..', '..', 'crit-line-blocks.js'), 'utf8');

function loadLineBlocks() {
  const sandbox = {
    window: {
      crit: {
        commentCardHelpers: {
          escapeHtml: (s) =>
            String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'),
        },
      },
      hljs: null,
    },
    document: {},
  };
  const fn = new Function('window', 'document', src + '\nreturn window;');
  fn(sandbox.window, sandbox.document);
  return sandbox.window.crit.lineBlocks;
}

const lineBlocks = loadLineBlocks();

// Simulate hljs output: every line wrapped in spans, some spans left open
// across lines (multi-line strings/comments) to exercise the tag tracker.
let highlighted = '';
for (let i = 0; i < 2000; i++) {
  if (i % 50 === 0) highlighted += '<span class="hljs-comment">/* multi-line comment starts ' + i + '\n';
  else if (i % 50 === 25) highlighted += 'comment ends ' + i + ' */</span>\n';
  else
    highlighted +=
      '<span class="hljs-keyword">const</span> x' + i + ' = <span class="hljs-string">"value ' + i + '"</span>;\n';
}

test('perf: splitHighlightedCode on 2000-line block stays within budget', () => {
  let n = 0;
  const median = medianMs(() => {
    n = lineBlocks.splitHighlightedCode(highlighted).length;
  });
  console.log(`  splitHighlightedCode: median ${median.toFixed(1)}ms for ${n} lines (budget 1000ms)`);
  assert.ok(n >= 2000, `expected >= 2000 lines, got ${n}`);
  assert.ok(median < 1000, `splitHighlightedCode median ${median.toFixed(1)}ms exceeds 1000ms`);
});
