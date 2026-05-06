'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { composeRoundTooltip } = require('../design-mode-round-tooltip.js');

const pins = (specs) => specs.map((s, i) => ({
  id: 'p' + i,
  resolved: !!s.resolved,
  drifted: !!s.drifted,
  drifted_on_round: s.dor || 0,
}));

test('composes round / total / resolved / drifted / carried counts', () => {
  const got = composeRoundTooltip({
    round: 3,
    pins: pins([
      { resolved: true },
      { drifted: true, dor: 3 },
      { drifted: true, dor: 1 },
      {},
    ]),
  });
  assert.deepEqual(got, {
    round: 3, total: 4, resolved: 1, drifted: 2, driftedThisRound: 1, carried: 3,
  });
});

test('zero pins yields zeros', () => {
  assert.deepEqual(
    composeRoundTooltip({ round: 1, pins: [] }),
    { round: 1, total: 0, resolved: 0, drifted: 0, driftedThisRound: 0, carried: 0 },
  );
});
