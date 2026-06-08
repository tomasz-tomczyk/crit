'use strict';
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.agent = root.crit.agent || {};
    root.crit.agent.batcher = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {

  const DEFAULT_BUDGET = 200;

  class MutationBatcher {
    constructor(opts) {
      opts = opts || {};
      this.raf = opts.raf || (function (cb) { return requestAnimationFrame(cb); });
      this.onDrain = opts.onDrain || function () {};
      this.budget = opts.budget || DEFAULT_BUDGET;
      this._queue = [];
      this._scheduled = false;
      this._suspendUntil = 0;
      this._catchUpPending = false;
    }
    pause(ms) {
      this._suspendUntil = (typeof performance !== 'undefined' ? performance.now() : Date.now()) + ms;
      this._catchUpPending = true;
    }
    enqueue(records) {
      if (records && records.length) {
        for (const r of records) this._queue.push(r);
      }
      // Even an empty enqueue may trigger a catch-up drain when pending.
      this._scheduleDrain();
    }
    scheduleCatchUpIfNeeded() {
      if (this._catchUpPending) this._scheduleDrain();
    }
    _scheduleDrain() {
      if (this._scheduled) return;
      // No work and no catch-up pending → don't bother scheduling.
      if (this._queue.length === 0 && !this._catchUpPending) return;
      this._scheduled = true;
      this.raf(() => this._drain());
    }
    _drain() {
      this._scheduled = false;
      const now = (typeof performance !== 'undefined' ? performance.now() : Date.now());
      if (now < this._suspendUntil) {
        this._queue.length = 0;
        return;
      }
      if (this._catchUpPending) {
        this._catchUpPending = false;
        this._queue.length = 0;
        this.onDrain(0, true);
        return;
      }
      const count = this._queue.length;
      const fullReresolve = count > this.budget;
      this._queue.length = 0;
      this.onDrain(count, fullReresolve);
    }
  }

  return { MutationBatcher };
});
