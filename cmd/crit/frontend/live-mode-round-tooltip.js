'use strict';
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.live = root.crit.live || {};
    root.crit.live.roundTooltip = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  function composeRoundTooltip({ round, pins } = {}) {
    let resolved = 0, drifted = 0, driftedThisRound = 0;
    const list = pins || [];
    for (const p of list) {
      if (p && p.resolved) resolved++;
      if (p && p.drifted) drifted++;
      if (p && p.drifted_on_round === round) driftedThisRound++;
    }
    const total = list.length;
    const carried = total - resolved;
    return { round, total, resolved, drifted, driftedThisRound, carried };
  }
  return { composeRoundTooltip };
});
