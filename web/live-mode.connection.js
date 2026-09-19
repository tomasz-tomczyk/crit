'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.live = root.crit.live || {};
    root.crit.live.connection = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  // Default readiness window. The agent typically posts agent-ready within
  // a few hundred ms of the iframe load event; pages with heavy scripts may
  // take longer, but 10s is long enough that a missing agent is almost always
  // a real bootstrap failure (CSP, non-HTML response, script injection blocked).
  var DEFAULT_TIMEOUT_MS = 10000;

  // Agent → chrome error kinds that mean the agent itself could not start.
  // Other agent-error kinds (e.g. capture-failed) are runtime best-effort
  // failures and must NOT disable commenting.
  var BOOTSTRAP_KINDS = {
    'protocol-missing': true,
    'origin-guess-failed': true,
    'already-booted': true,
    'incompatible-response': true,
    'csp-violation': true,
  };

  function isBootstrapErrorKind(kind) {
    return !!(kind && BOOTSTRAP_KINDS[kind]);
  }

  function makeConnectionState(opts) {
    opts = opts || {};
    var onChange = typeof opts.onChange === 'function' ? opts.onChange : function () {};
    var onRetry = typeof opts.onRetry === 'function' ? opts.onRetry : function () {};
    var timeoutMs = (typeof opts.timeoutMs === 'number' && opts.timeoutMs > 0)
      ? opts.timeoutMs
      : DEFAULT_TIMEOUT_MS;
    var state = 'connecting';
    var timer = null;
    var destroyed = false;

    function clearTimer() {
      if (timer) { clearTimeout(timer); timer = null; }
    }

    function startTimer() {
      clearTimer();
      timer = setTimeout(function () { setUnavailable('timeout'); }, timeoutMs);
    }

    function setConnecting() {
      if (destroyed) return;
      var changed = state !== 'connecting';
      state = 'connecting';
      startTimer();
      if (changed) onChange('connecting');
    }

    function setReady() {
      if (destroyed) return;
      if (state === 'ready') return;
      state = 'ready';
      clearTimer();
      onChange('ready');
    }

    function setUnavailable(reason) {
      if (destroyed) return;
      if (state === 'ready') return;
      state = 'unavailable';
      clearTimer();
      onChange('unavailable', reason);
    }

    function retry() {
      if (destroyed) return;
      if (state !== 'unavailable') return;
      setConnecting();
      onRetry();
    }

    function destroy() {
      destroyed = true;
      clearTimer();
    }

    state = 'connecting';
    startTimer();
    onChange('connecting');

    return {
      state: function () { return state; },
      setReady: setReady,
      setUnavailable: setUnavailable,
      retry: retry,
      destroy: destroy,
    };
  }

  return {
    makeConnectionState: makeConnectionState,
    isBootstrapErrorKind: isBootstrapErrorKind,
    BOOTSTRAP_KINDS: BOOTSTRAP_KINDS,
  };
});
