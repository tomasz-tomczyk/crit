'use strict';
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.live = root.crit.live || {};
    root.crit.live.resolutionGate = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  class ResolutionGate {
    constructor(fire) {
      this._fire = fire;
      this._pendingViewport = false;
      this._queued = false;
    }
    beginViewportChange() { this._pendingViewport = true; }
    onViewportApplied() {
      this._pendingViewport = false;
      if (this._queued) { this._queued = false; this._fire(); }
    }
    requestResolution() {
      if (this._pendingViewport) { this._queued = true; return; }
      this._fire();
    }
  }
  return { ResolutionGate };
});
