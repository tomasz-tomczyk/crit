'use strict';
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.agent = root.crit.agent || {};
    root.crit.agent.reanchorState = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  class ReanchorState {
    constructor() { this._pinId = null; }
    get armed() { return this._pinId !== null; }
    get pinId() { return this._pinId; }
    arm(pinId) { this._pinId = pinId; }
    consume() { const v = this._pinId; this._pinId = null; return v; }
    // disarm clears any armed state. Idempotent: safe to call when not armed.
    disarm() { this._pinId = null; }
  }
  function create() { return new ReanchorState(); }
  return { ReanchorState, create };
});
