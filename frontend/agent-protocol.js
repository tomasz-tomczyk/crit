'use strict';
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.agentProtocol = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {

  // Agent → Chrome
  const A2C = {
    AGENT_READY:        'agent-ready',
    AGENT_ERROR:        'agent-error',
    SELECTION:          'selection',
    REQUEST_ANCESTOR_MENU: 'request-ancestor-menu',
    PIN_CLICKED:        'pin-clicked',
    FOCUS_STATE:        'focus-state',
    ROUTE_CHANGE:       'route-change',
  };

  // Chrome → Agent
  const C2A = {
    SET_MODE:           'set-mode',
    COMMIT_ANCESTOR_SELECTION: 'commit-ancestor-selection',
    CANCEL_ANCESTOR_SELECTION: 'cancel-ancestor-selection',
    SET_PINS:           'set-pins',
  };

  const MESSAGE_TYPES = Object.freeze({ ...A2C, ...C2A });

  function isFiniteNumber(n) { return typeof n === 'number' && Number.isFinite(n); }
  function isString(v) { return typeof v === 'string'; }
  function isBool(v) { return typeof v === 'boolean'; }

  function validateMessage(msg) {
    if (!msg || typeof msg !== 'object') return { ok: false, reason: 'not-object' };
    if (!isString(msg.type)) return { ok: false, reason: 'no-type' };
    switch (msg.type) {
      case A2C.AGENT_READY:
        return { ok: true };
      case A2C.AGENT_ERROR:
        if (!isString(msg.kind)) return { ok: false, reason: 'agent-error.kind' };
        if (!isString(msg.message)) return { ok: false, reason: 'agent-error.message' };
        return { ok: true };
      case A2C.SELECTION: {
        const a = msg.dom_anchor;
        if (!a || typeof a !== 'object') return { ok: false, reason: 'selection.dom_anchor' };
        if (!isString(a.pathname) || !isString(a.css_selector)) return { ok: false, reason: 'selection.fields' };
        if (!Array.isArray(a.tag_chain)) return { ok: false, reason: 'selection.tag_chain' };
        if (!isString(a.outer_html)) return { ok: false, reason: 'selection.outer_html' };
        if (!isString(a.screenshot)) return { ok: false, reason: 'selection.screenshot' };
        if (!isFiniteNumber(a.viewport_width) || !isFiniteNumber(a.viewport_height)) return { ok: false, reason: 'selection.viewport' };
        return { ok: true };
      }
      case A2C.REQUEST_ANCESTOR_MENU:
        if (!Array.isArray(msg.options) || msg.options.length === 0) return { ok: false, reason: 'menu.options' };
        if (!msg.pointer || !isFiniteNumber(msg.pointer.x) || !isFiniteNumber(msg.pointer.y)) return { ok: false, reason: 'menu.pointer' };
        return { ok: true };
      case A2C.PIN_CLICKED:
        if (!isString(msg.pin_id)) return { ok: false, reason: 'pin-clicked.id' };
        return { ok: true };
      case A2C.FOCUS_STATE:
        if (!isBool(msg.in_input)) return { ok: false, reason: 'focus-state.in_input' };
        return { ok: true };
      case A2C.ROUTE_CHANGE:
        if (!isString(msg.pathname)) return { ok: false, reason: 'route-change.pathname' };
        return { ok: true };
      case C2A.SET_MODE:
        if (msg.value !== 'navigate' && msg.value !== 'pin') return { ok: false, reason: 'set-mode.value' };
        return { ok: true };
      case C2A.COMMIT_ANCESTOR_SELECTION:
        if (!Number.isInteger(msg.level) || msg.level < 0) return { ok: false, reason: 'commit-ancestor.level' };
        return { ok: true };
      case C2A.CANCEL_ANCESTOR_SELECTION:
        return { ok: true };
      case C2A.SET_PINS:
        if (!Array.isArray(msg.pins)) return { ok: false, reason: 'set-pins.pins' };
        return { ok: true };
      default:
        return { ok: false, reason: 'unknown-type' };
    }
  }

  return { MESSAGE_TYPES, A2C, C2A, validateMessage };
});
