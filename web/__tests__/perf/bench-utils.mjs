// Shared helpers for frontend perf microbenchmarks (node --test).
//
// Pattern: deterministic seeded fixture -> warmup -> median-of-N -> budget
// assert. Budgets are generous (10-50x measured) on purpose: these guard
// against algorithmic regressions (accidental quadratic, lost early-out),
// not against 5% noise. Tight timing gates belong in local benchstat-style
// comparison, not in CI asserts.

import test from 'node:test';
import assert from 'node:assert/strict';

// Deterministic PRNG (mulberry32) so fixtures are stable across runs without
// checking in multi-thousand-line files.
export function mulberry32(seed) {
  let a = seed;
  return function () {
    a |= 0;
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

// Run `fn` (sync) after `warmup` untimed iterations, then return the median
// of `iters` timed iterations in milliseconds.
export function medianMs(fn, { warmup = 3, iters = 5 } = {}) {
  for (let i = 0; i < warmup; i++) fn();
  const samples = [];
  for (let i = 0; i < iters; i++) {
    const t0 = performance.now();
    fn();
    samples.push(performance.now() - t0);
  }
  samples.sort((a, b) => a - b);
  return samples[Math.floor(samples.length / 2)];
}

// Define a budget test: measures medianMs(fn) and fails if it exceeds budgetMs.
export function budgetTest(name, fn, budgetMs, opts) {
  test(name, () => {
    const median = medianMs(fn, opts);
    console.log(`  ${name}: median ${median.toFixed(1)}ms (budget ${budgetMs}ms)`);
    assert.ok(
      median < budgetMs,
      `${name}: median ${median.toFixed(1)}ms exceeds budget ${budgetMs}ms`
    );
  });
}
