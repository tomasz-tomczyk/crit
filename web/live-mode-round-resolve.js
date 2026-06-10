'use strict';
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.live = root.crit.live || {};
    root.crit.live.roundResolve = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  function pinsToResolveAtRoundStart(pins, currentPath) {
    const out = [];
    for (const p of pins || []) {
      if (!p || !p.dom_anchor) continue;
      if (p.dom_anchor.pathname !== currentPath) continue;
      if (p._roundResolved) continue;
      out.push(p.id);
    }
    return out;
  }
  function classifyPinForRound(prev, resolution, round) {
    const wasDrifted = !!(prev && prev.drifted);
    if (!resolution) return { driftedOnRound: null, drifted: wasDrifted };
    switch (resolution.status) {
      case 'resolved':
        return { driftedOnRound: null, drifted: false };
      case 'drifted':
        if (wasDrifted) return { driftedOnRound: null, drifted: true };
        return { driftedOnRound: round, drifted: true };
      case 'drifted-recoverable':
      default:
        return { driftedOnRound: null, drifted: wasDrifted };
    }
  }
  return { pinsToResolveAtRoundStart, classifyPinForRound };
});
