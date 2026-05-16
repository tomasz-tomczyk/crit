(function () {
  'use strict';

  /**
   * createSSE(url, handlers, opts) → { close, source }
   *
   * Thin wrapper around EventSource that:
   * - Registers typed event listeners from a handler map
   * - Parses JSON data for each event (passes raw string on parse failure)
   * - Provides a close() method for cleanup
   *
   * EventSource handles reconnection natively — no custom retry logic needed.
   *
   * handlers: { 'event-name': function(data) { ... }, ... }
   * opts.onOpen:  called when connection opens
   * opts.onError: called on connection error (before native retry)
   */
  function createSSE(url, handlers, opts) {
    opts = opts || {};
    var source = new EventSource(url);
    var closed = false;

    if (typeof opts.onOpen === 'function') {
      source.onopen = opts.onOpen;
    }
    if (typeof opts.onError === 'function') {
      source.onerror = function (e) {
        if (!closed) opts.onError(e);
      };
    }

    var types = Object.keys(handlers);
    for (var i = 0; i < types.length; i++) {
      (function (type) {
        source.addEventListener(type, function (event) {
          var data = event.data;
          try { data = JSON.parse(event.data); } catch (_) {}
          handlers[type](data);
        });
      })(types[i]);
    }

    return {
      close: function () {
        closed = true;
        source.close();
      },
      source: source,
    };
  }

  var api = { createSSE: createSSE };

  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.sse = api;
  }

  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
