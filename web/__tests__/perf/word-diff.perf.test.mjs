// Perf: word-diff pairing + intra-line diff on realistic hunk shapes.
// Run: npm run test:perf.
//
// Production caps bound this path (pairing skipped past 8 del+add lines,
// wordDiff skipped past 500 chars), so these benches lock the capped worst
// case: a 4x4 pairing set and 500-char lines. If someone loosens a cap, the
// bench — not a user's tab — finds out first.

import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { mulberry32, medianMs } from './bench-utils.mjs';

const __dirname = dirname(fileURLToPath(import.meta.url));
const src = readFileSync(resolve(__dirname, '..', '..', 'crit-diff-renderer.js'), 'utf8');

function loadDiffRenderer() {
  const mockDMP = {
    DIFF_EQUAL: 0,
    DIFF_DELETE: -1,
    DIFF_INSERT: 1,
    makeDiff(a, b) {
      if (a === b) return [[0, a]];
      let i = 0;
      while (i < a.length && i < b.length && a[i] === b[i]) i++;
      let j = 0;
      while (j < a.length - i && j < b.length - i && a[a.length - 1 - j] === b[b.length - 1 - j]) j++;
      const out = [];
      if (i > 0) out.push([0, a.slice(0, i)]);
      const del = a.slice(i, a.length - j);
      const ins = b.slice(i, b.length - j);
      if (del) out.push([-1, del]);
      if (ins) out.push([1, ins]);
      if (j > 0) out.push([0, a.slice(a.length - j)]);
      return out;
    },
    cleanupSemantic(diffs) {
      return diffs;
    },
  };
  const sandbox = {
    window: {
      crit: {
        commentCardHelpers: {
          escapeHtml: (s) =>
            String(s)
              .replace(/&/g, '&amp;')
              .replace(/</g, '&lt;')
              .replace(/>/g, '&gt;')
              .replace(/"/g, '&quot;'),
        },
      },
      DiffMatchPatch: mockDMP,
    },
    document: {},
    NodeFilter: { SHOW_TEXT: 4 },
  };
  const fn = new Function('window', 'document', 'NodeFilter', src + '\nreturn window;');
  fn(sandbox.window, sandbox.document, sandbox.NodeFilter);
  return sandbox.window.crit.diffRenderer;
}

const diffRenderer = loadDiffRenderer();
const rand = mulberry32(7);
const words = ['function', 'return', 'const', 'process', 'value', 'offset', 'delta', 'result', 'index', 'length'];

function codeLine(n) {
  const parts = [];
  while (parts.join(' ').length < n) parts.push(words[Math.floor(rand() * words.length)] + '_' + Math.floor(rand() * 1000));
  return parts.join(' ').slice(0, n);
}

// Max-capped pairing shape: 4 removed + 4 added lines (del+add == 8).
const removed = [codeLine(120), codeLine(120), codeLine(120), codeLine(120)];
const added = removed.map((l) => l.replace(/_\d+/, '_changed'));

test('perf: bestWordDiffPairing on capped 4x4 hunk stays within budget', () => {
  let pairs = 0;
  const median = medianMs(() => {
    for (let i = 0; i < 500; i++) pairs = diffRenderer.bestWordDiffPairing(removed, added).length;
  });
  console.log(`  bestWordDiffPairing x500: median ${median.toFixed(1)}ms (budget 1000ms)`);
  assert.ok(pairs > 0, 'expected non-empty pairing');
  assert.ok(median < 1000, `pairing median ${median.toFixed(1)}ms exceeds 1000ms`);
});

test('perf: wordDiff on capped 500-char lines stays within budget', () => {
  // 480 chars: stays under the 500-char wordDiff perf guard even after the
  // single-token substitution below makes `b` a few chars longer.
  const a = codeLine(480);
  const b = a.replace(/_\d+/, '_changed');
  // Guard the bench itself: at least one call must do real diff work
  // (non-null) or the budget asserts against an early-out path.
  assert.ok(diffRenderer.wordDiff(a, b) !== null, 'fixture lines must produce a real word diff');
  const median = medianMs(() => {
    for (let i = 0; i < 100; i++) diffRenderer.wordDiff(a, b);
  });
  console.log(`  wordDiff x100: median ${median.toFixed(1)}ms (budget 1000ms)`);
  assert.ok(median < 1000, `wordDiff median ${median.toFixed(1)}ms exceeds 1000ms`);
});
