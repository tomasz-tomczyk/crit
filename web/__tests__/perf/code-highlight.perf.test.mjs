// Perf: main-thread cost of highlighted fences (crit-code-highlight.js).
// Run: npm run test:perf (also runs under plain node --test discovery).
//
// Shiki tokenizes fenced code in Pierre's worker pool; what stays on the main
// thread is turning the worker's per-line hast into HTML. This benches that
// conversion for a 2000-line fence with ~8 tokens per line, the shape Pierre
// returns. Budget is generous so CI only fails on algorithmic regressions.

import test from 'node:test';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { mulberry32, medianMs } from './bench-utils.mjs';

const require = createRequire(import.meta.url);
const codeHighlight = require('../../crit-code-highlight.js');

const rand = mulberry32(11);
const colors = ['#D568EA', '#FFAB16', '#08C0EF', '#9D6AFB', '#5ECC71', '#A631BE'];
const code = [];
for (let i = 0; i < 2000; i++) {
  const spans = [];
  for (let t = 0; t < 8; t++) {
    const c = colors[Math.floor(rand() * colors.length)];
    spans.push({
      type: 'element', tagName: 'span',
      properties: { style: `--diffs-token-dark:${c};--diffs-token-light:${c}` },
      children: [{ type: 'text', value: t === 0 ? '  const' : ` tok${t}<${i}>` }],
    });
  }
  code.push({ type: 'element', tagName: 'div', properties: { 'data-line': i + 1 }, children: spans });
}
code.push({ type: 'element', tagName: 'div', properties: {}, children: [] });
const source = 'x\n'.repeat(2000);

test('converting a 2000-line highlighted fence to HTML stays within budget', () => {
  const out = codeHighlight.linesFromResult({ code }, source);
  // Real work: every line converted, trailing empty line dropped, text escaped.
  assert.equal(out.length, 2000);
  assert.match(out[3], /^<span style="--diffs-token-dark:#[0-9A-F]+;--diffs-token-light:#[0-9A-F]+">  const<\/span>/);
  assert.match(out[3], /tok1&lt;3&gt;/);

  const ms = medianMs(() => codeHighlight.linesFromResult({ code }, source));
  console.log(`[perf] linesFromResult 2000-line fence: ${ms.toFixed(1)}ms`);
  assert.ok(ms < 250, `took ${ms.toFixed(1)}ms`);
});
