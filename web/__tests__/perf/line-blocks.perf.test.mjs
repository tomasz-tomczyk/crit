// Perf: markdown parse + buildLineBlocks on a 10k-line plan fixture.
// Run: npm run test:perf (also runs under plain node --test discovery).
//
// This is the core document-render path for large AI-generated plans.
// Budgets are ~50x measured (parse ~35ms, blocks ~10ms on M4) so CI only
// fails on algorithmic regressions, never on runner noise.

import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { createRequire } from 'node:module';
import { mulberry32, medianMs } from './bench-utils.mjs';

const __dirname = dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const markdownit = require('markdown-it');

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
      hljs: {
        getLanguage: () => false,
        highlight: (content) => ({ value: content }),
      },
    },
    document: {},
  };
  const fn = new Function('window', 'document', src + '\nreturn window;');
  fn(sandbox.window, sandbox.document);
  return sandbox.window.crit.lineBlocks;
}

// Deterministic 10k-line markdown mixing every block type the splitter drills
// into: headings, paragraphs, lists (nested), fences, tables, blockquotes.
function buildFixture() {
  const rand = mulberry32(42);
  const pick = (arr) => arr[Math.floor(rand() * arr.length)];
  const words = [
    'review', 'session', 'diff', 'hunk', 'comment', 'focus', 'range', 'stack',
    'markdown', 'render', 'lazy', 'threshold', 'viewport', 'gutter', 'syntax',
    'highlight', 'token', 'block', 'quote', 'table',
  ];
  const sentence = () => {
    const n = 5 + Math.floor(rand() * 12);
    const out = [];
    for (let i = 0; i < n; i++) out.push(pick(words));
    return out.join(' ') + '.';
  };
  const lines = [];
  let i = 0;
  while (i < 10000) {
    const r = rand();
    if (r < 0.35) {
      lines.push('## Section ' + i + ' ' + sentence());
      i++;
      const n = 3 + Math.floor(rand() * 6);
      for (let k = 0; k < n && i < 10000; k++, i++) lines.push(sentence());
    } else if (r < 0.5) {
      lines.push('- ' + sentence());
      i++;
      const n = 2 + Math.floor(rand() * 6);
      for (let k = 0; k < n && i < 10000; k++, i++)
        lines.push((rand() < 0.3 ? '  - ' : '- ') + sentence());
    } else if (r < 0.6) {
      lines.push('```js');
      i++;
      const n = 5 + Math.floor(rand() * 20);
      for (let k = 0; k < n && i < 10000; k++, i++)
        lines.push('const x' + k + ' = "' + sentence() + '";');
      lines.push('```');
      i++;
    } else if (r < 0.68) {
      lines.push('| a | b | c |');
      lines.push('|---|---|---|');
      i += 2;
      const n = 3 + Math.floor(rand() * 5);
      for (let k = 0; k < n && i < 10000; k++, i++)
        lines.push('| ' + sentence() + ' | ' + sentence() + ' | ' + sentence() + ' |');
    } else if (r < 0.74) {
      lines.push('> ' + sentence());
      i++;
      if (rand() < 0.5) {
        lines.push('> ' + sentence());
        i++;
      }
    } else {
      lines.push(sentence());
      i++;
      if (rand() < 0.3) {
        lines.push('');
        i++;
      }
    }
  }
  return lines.slice(0, 10000).join('\n');
}

const content = buildFixture();
const md = markdownit({ html: true });
const lineBlocks = loadLineBlocks();

test('perf: markdown-it parse of 10k-line fixture stays within budget', () => {
  const median = medianMs(() => md.parse(content, {}));
  console.log(`  parse: median ${median.toFixed(1)}ms (budget 2000ms)`);
  assert.ok(median < 2000, `parse median ${median.toFixed(1)}ms exceeds 2000ms`);
});

test('perf: buildLineBlocks on 10k-line fixture stays within budget', () => {
  const tokens = md.parse(content, {});
  let nBlocks = 0;
  const median = medianMs(() => {
    const blocks = lineBlocks.buildLineBlocks(tokens, md, content);
    nBlocks = blocks.length;
  });
  console.log(`  buildLineBlocks: median ${median.toFixed(1)}ms for ${nBlocks} blocks (budget 2000ms)`);
  assert.ok(nBlocks > 1000, `expected thousands of blocks, got ${nBlocks}`);
  assert.ok(median < 2000, `buildLineBlocks median ${median.toFixed(1)}ms exceeds 2000ms`);
});
