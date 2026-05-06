'use strict';
(function (root, factory) {
  var protocol = (typeof require === 'function')
    ? require('./agent-protocol.js')
    : (root.crit && root.crit.agentProtocol);
  var api = factory(protocol);
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.design = root.crit.design || {};
    root.crit.design.dispatch = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function (protocol) {
  var A2C = protocol.A2C;
  var validateMessage = protocol.validateMessage;
  function makeMessageDispatcher(handlers) {
    return function dispatch(msg) {
      var v = validateMessage(msg);
      if (!v.ok) return;
      switch (msg.type) {
        case A2C.AGENT_READY: if (handlers.onAgentReady) handlers.onAgentReady(); break;
        case A2C.AGENT_ERROR: if (handlers.onAgentError) handlers.onAgentError({ kind: msg.kind, message: msg.message }); break;
        case A2C.SELECTION: if (handlers.onSelection) handlers.onSelection(msg.dom_anchor, msg.pointer, msg.reanchor_for); break;
        case A2C.REQUEST_ANCESTOR_MENU: if (handlers.onRequestAncestorMenu) handlers.onRequestAncestorMenu(msg.options, msg.pointer); break;
        case A2C.FOCUS_STATE: if (handlers.onFocusState) handlers.onFocusState(msg.in_input); break;
        case A2C.ROUTE_CHANGE: if (handlers.onRouteChange) handlers.onRouteChange(msg); break;
        case A2C.PIN_CLICKED: if (handlers.onPinClicked) handlers.onPinClicked(msg.pin_id); break;
        case A2C.PIN_RESOLUTION_RESULT: if (handlers.onPinResolutionResult) handlers.onPinResolutionResult(msg); break;
        case A2C.VIEWPORT_APPLIED: if (handlers.onViewportApplied) handlers.onViewportApplied(msg); break;
        case A2C.HOVERED_ANCESTOR_LEVEL: if (handlers.onHoveredAncestorLevel) handlers.onHoveredAncestorLevel(msg.level); break;
        default: break;
      }
    };
  }
  return { makeMessageDispatcher };
});
