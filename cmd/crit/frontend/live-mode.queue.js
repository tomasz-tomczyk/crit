'use strict';
(function (root, factory) {
  var api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.live = root.crit.live || {};
    root.crit.live.queue = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  function makeAgentSender(opts) {
    var post = opts.post;
    var ready = false;
    var queue = [];
    function send(msg) {
      if (!ready) { queue.push(msg); return; }
      post(msg);
    }
    function requeue(msg) { queue.unshift(msg); }
    function markReady() {
      ready = true;
      while (queue.length) post(queue.shift());
    }
    return { send: send, markReady: markReady, requeue: requeue };
  }
  return { makeAgentSender };
});
