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
    PIN_RESOLUTION_RESULT: 'pin-resolution-result',
    VIEWPORT_APPLIED:   'viewport-applied',
    HOVERED_ANCESTOR_LEVEL: 'hovered-ancestor-level',
  };

  // Chrome → Agent
  const C2A = {
    SET_MODE:           'set-mode',
    COMMIT_ANCESTOR_SELECTION: 'commit-ancestor-selection',
    CANCEL_ANCESTOR_SELECTION: 'cancel-ancestor-selection',
    SET_PINS:           'set-pins',
    REQUEST_RESOLUTION: 'request-resolution',
    ENTER_REANCHOR_MODE: 'enter-reanchor-mode',
    SET_VIEWPORT:       'set-viewport',
    FLASH_MARKER:       'flash-marker',
    CANCEL_REANCHOR:    'cancel-reanchor',
    SET_MARKER_TABINDEX: 'set-marker-tabindex',
    KEEP_HIGHLIGHT:     'keep-highlight',
    CLEAR_HIGHLIGHT:    'clear-highlight',
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
        // NOTE: a.screenshot was required here until commits 3d29c41 +
        // b522df7 removed the screenshot field from DOMAnchor and stopped
        // populating it in the agent. The validator was not updated, so
        // every selection message after b522df7 was silently rejected by
        // the dispatcher (see design-mode.dispatch.js:19) — composer never
        // opened. Field dropped from validation; do NOT re-add without
        // also re-emitting from crit-agent.js.
        if (!isFiniteNumber(a.viewport_width) || !isFiniteNumber(a.viewport_height)) return { ok: false, reason: 'selection.viewport' };
        if (msg.reanchor_for !== undefined && !isString(msg.reanchor_for)) {
          return { ok: false, reason: 'selection.reanchor_for' };
        }
        return { ok: true };
      }
      case A2C.PIN_RESOLUTION_RESULT: {
        if (!isString(msg.pin_id)) return { ok: false, reason: 'pin-resolution-result.pin_id' };
        if (msg.status !== 'resolved' && msg.status !== 'drifted-recoverable' && msg.status !== 'drifted') {
          return { ok: false, reason: 'pin-resolution-result.status' };
        }
        if (msg.rect !== undefined) {
          const r = msg.rect;
          if (!r || !isFiniteNumber(r.x) || !isFiniteNumber(r.y) || !isFiniteNumber(r.w) || !isFiniteNumber(r.h)) {
            return { ok: false, reason: 'pin-resolution-result.rect' };
          }
        }
        if (msg.recovered_via !== undefined && !isString(msg.recovered_via)) {
          return { ok: false, reason: 'pin-resolution-result.recovered_via' };
        }
        return { ok: true };
      }
      case A2C.VIEWPORT_APPLIED:
        if (!isFiniteNumber(msg.width) || !isFiniteNumber(msg.height)) return { ok: false, reason: 'viewport-applied.size' };
        return { ok: true };
      case C2A.REQUEST_RESOLUTION:
        return { ok: true };
      case C2A.ENTER_REANCHOR_MODE:
        if (!isString(msg.pin_id)) return { ok: false, reason: 'enter-reanchor-mode.pin_id' };
        return { ok: true };
      case C2A.SET_VIEWPORT: {
        if (!isFiniteNumber(msg.width) || !isFiniteNumber(msg.height)) return { ok: false, reason: 'set-viewport.size' };
        if (msg.width <= 0 || msg.height <= 0) return { ok: false, reason: 'set-viewport.nonpositive' };
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
      case A2C.HOVERED_ANCESTOR_LEVEL:
        if (!isFiniteNumber(msg.level)) return { ok: false, reason: 'hovered-ancestor-level.level' };
        return { ok: true };
      case C2A.FLASH_MARKER:
        if (!isString(msg.pin_id)) return { ok: false, reason: 'flash-marker.pin_id' };
        return { ok: true };
      case C2A.CANCEL_REANCHOR:
        return { ok: true };
      case C2A.SET_MARKER_TABINDEX:
        if (!isFiniteNumber(msg.value)) return { ok: false, reason: 'set-marker-tabindex.value' };
        return { ok: true };
      case C2A.KEEP_HIGHLIGHT:
        if (!isString(msg.selector) || msg.selector.length === 0) {
          return { ok: false, reason: 'keep-highlight.selector' };
        }
        return { ok: true };
      case C2A.CLEAR_HIGHLIGHT:
        return { ok: true };
      default:
        return { ok: false, reason: 'unknown-type' };
    }
  }

  return { MESSAGE_TYPES, A2C, C2A, validateMessage };
});
