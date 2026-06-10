'use strict';
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.live = root.crit.live || {};
    root.crit.live.pinState = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {

  class PinState {
    constructor() { this._byId = new Map(); }
    setComments(list) {
      this._byId.clear();
      for (const c of list || []) {
        if (c && c.dom_anchor) {
          this._byId.set(c.id, {
            id: c.id,
            body: c.body,
            pathname: c.dom_anchor.pathname,
            status: undefined,
            recovered_via: undefined,
            drifted_on_round: c.drifted_on_round || 0,
          });
        }
      }
    }
    applyResolution(msg) {
      const r = this._byId.get(msg.pin_id);
      if (!r) return;
      r.status = msg.status;
      r.recovered_via = msg.recovered_via;
    }
    statusOf(id) { const r = this._byId.get(id); return r && r.status; }
    driftedRows() {
      const out = [];
      this._byId.forEach(v => { if (v.status && v.status !== 'resolved') out.push(v); });
      return out;
    }
  }
  return { PinState };
});
