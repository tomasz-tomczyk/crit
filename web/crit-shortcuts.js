// crit-shortcuts.js — shared keyboard shortcut registry and persistence.
//
// User overrides live under `shortcuts` in the crit-settings cookie:
//   { "shortcuts": { "next_block": "ArrowDown" } }
// Empty strings intentionally disable an action. Defaults stay in this module so
// the settings pane and keyboard handlers cannot drift apart.
'use strict';
(function (root, factory) {
  var api = factory(root);
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.shortcuts = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function (root) {
  var BOTH = ['code-review', 'live'];
  var CODE_REVIEW_ONLY = ['code-review'];
  var LIVE_ONLY = ['live'];

  var groups = [
    { label: 'Navigation', shortcuts: [
      { id: 'next_block', binding: 'j', action: 'Next block', modes: CODE_REVIEW_ONLY },
      { id: 'previous_block', binding: 'k', action: 'Previous block', modes: CODE_REVIEW_ONLY },
      { id: 'visual_mode', binding: 'Shift+V', action: 'Visual line mode (extend with next/previous block, then comment)', modes: CODE_REVIEW_ONLY },
      { id: 'next_comment', binding: ']', action: 'Next comment', modes: CODE_REVIEW_ONLY },
      { id: 'previous_comment', binding: '[', action: 'Previous comment', modes: CODE_REVIEW_ONLY },
      { id: 'next_change', binding: 'n', action: 'Next change', mode: 'file mode', modes: CODE_REVIEW_ONLY },
      { id: 'previous_change', binding: 'Shift+N', action: 'Previous change', mode: 'file mode', modes: CODE_REVIEW_ONLY },
    ]},
    { label: 'Comments', shortcuts: [
      { id: 'comment', binding: 'c', action: 'Comment on focused block (or text selection, with quote)', modes: CODE_REVIEW_ONLY },
      { id: 'edit_comment', binding: 'e', action: 'Edit comment on focused block', modes: CODE_REVIEW_ONLY },
      { id: 'delete_comment', binding: 'd', action: 'Delete comment on focused block', modes: CODE_REVIEW_ONLY },
      { id: 'general_comment', binding: 'Shift+G', action: 'General comment', modes: CODE_REVIEW_ONLY },
      { binding: 'Ctrl+Enter', action: 'Comment', modes: BOTH, fixed: true },
    ]},
    { label: 'Review', shortcuts: [
      { id: 'finish_review', binding: 'Shift+F', action: 'Finish review', modes: BOTH },
      { id: 'toggle_comments', binding: 'Shift+C', action: 'Toggle comments panel', modes: CODE_REVIEW_ONLY },
      { id: 'scope_all', binding: 'Shift+1', action: 'Switch to all changes', mode: 'vcs mode', modes: CODE_REVIEW_ONLY },
      { id: 'scope_branch', binding: 'Shift+2', action: 'Switch to branch changes', mode: 'vcs mode', modes: CODE_REVIEW_ONLY },
      { id: 'scope_staged', binding: 'Shift+3', action: 'Switch to staged changes', mode: 'vcs mode', modes: CODE_REVIEW_ONLY },
      { id: 'scope_unstaged', binding: 'Shift+4', action: 'Switch to unstaged changes', mode: 'vcs mode', modes: CODE_REVIEW_ONLY },
    ]},
    { label: 'Story', shortcuts: [
      { id: 'story_next', binding: 'Shift+J', action: 'Next story chapter', mode: 'story mode', modes: CODE_REVIEW_ONLY },
      { id: 'story_previous', binding: 'Shift+K', action: 'Previous story chapter', mode: 'story mode', modes: CODE_REVIEW_ONLY },
      { id: 'story_prologue', binding: 'Shift+O', action: 'Story prologue', mode: 'story mode', modes: CODE_REVIEW_ONLY },
      { id: 'story_support', binding: 'Shift+S', action: 'Story support', mode: 'story mode', modes: CODE_REVIEW_ONLY },
      { binding: '1–9', action: 'Jump to story chapter', mode: 'story mode', modes: CODE_REVIEW_ONLY, fixed: true },
      { id: 'story_toggle_list', binding: '\\', action: 'Toggle story chapter list', mode: 'story mode', modes: CODE_REVIEW_ONLY },
    ]},
    { label: 'Live', shortcuts: [
      { id: 'toggle_pin_mode', binding: 'p', action: 'Toggle pin mode', modes: LIVE_ONLY },
    ]},
    { label: 'View', shortcuts: [
      { id: 'toggle_toc', binding: 't', action: 'Toggle table of contents', mode: 'file mode', modes: CODE_REVIEW_ONLY },
      { id: 'toggle_resolved', binding: 'h', action: 'Toggle hide resolved', modes: CODE_REVIEW_ONLY },
      { binding: 'Esc', action: 'Cancel / clear focus', modes: BOTH, fixed: true },
      { binding: '?', action: 'Toggle this panel', modes: BOTH, fixed: true },
    ]},
  ];

  var byID = {};
  groups.forEach(function (group) {
    group.shortcuts.forEach(function (shortcut) {
      if (shortcut.id) byID[shortcut.id] = shortcut;
    });
  });

  function shared() {
    return root.crit && root.crit.shared;
  }

  function isReservedBinding(binding) {
    if (['Esc', 'Tab', 'Enter', '1', '2', '3', '4', '5', '6', '7', '8', '9'].indexOf(binding) !== -1) return true;
    var parts = binding ? binding.split('+') : [];
    return parts[parts.length - 1] === '/' && parts.indexOf('Shift') !== -1 ||
      parts[parts.length - 1] === 'Enter' && (parts.indexOf('Ctrl') !== -1 || parts.indexOf('Meta') !== -1);
  }

  function overrides() {
    var helpers = shared();
    var value = helpers && helpers.getSetting ? helpers.getSetting('shortcuts', {}) : {};
    if (!value || typeof value !== 'object' || Array.isArray(value)) return {};
    var candidates = {};
    var valid = {};
    Object.keys(value).forEach(function (id) {
      if (byID[id] && typeof value[id] === 'string' && !isReservedBinding(value[id])) candidates[id] = value[id];
    });
    Object.keys(candidates).forEach(function (id) {
      var binding = candidates[id];
      var conflict = false;
      groups.some(function (group) {
        return group.shortcuts.some(function (other) {
          if (!other.id || other.id === id || other.modes.every(function (mode) { return byID[id].modes.indexOf(mode) === -1; })) return false;
          var otherBinding = Object.prototype.hasOwnProperty.call(candidates, other.id) ? candidates[other.id] : other.binding;
          if (otherBinding === binding) { conflict = true; return true; }
          return false;
        });
      });
      if (!conflict) valid[id] = binding;
    });
    return valid;
  }

  function getBinding(id) {
    var shortcut = byID[id];
    if (!shortcut) return '';
    var saved = overrides();
    return Object.prototype.hasOwnProperty.call(saved, id) ? saved[id] : shortcut.binding;
  }

  function setBinding(id, binding) {
    if (!byID[id]) return false;
    if (binding && isReservedBinding(binding)) return false;
    if (binding && findConflict(id, binding)) return false;
    var saved = overrides();
    if (binding === byID[id].binding) delete saved[id];
    else saved[id] = binding;
    var helpers = shared();
    if (!helpers || !helpers.setSetting) return false;
    helpers.setSetting('shortcuts', saved);
    return true;
  }

  function resetAll() {
    var helpers = shared();
    if (helpers && helpers.setSetting) helpers.setSetting('shortcuts', {});
  }

  function isCustomized(id) {
    return Object.prototype.hasOwnProperty.call(overrides(), id);
  }

  function eventToBinding(ev) {
    var key = ev.key;
    if (!key || key === 'Shift' || key === 'Control' || key === 'Alt' || key === 'Meta') return '';
    var names = {
      ' ': 'Space', Escape: 'Esc', Control: 'Ctrl', ArrowUp: 'ArrowUp',
      ArrowDown: 'ArrowDown', ArrowLeft: 'ArrowLeft', ArrowRight: 'ArrowRight',
    };
    key = names[key] || key;

    // Some synthetic keyboard APIs report the produced glyph but omit
    // shiftKey. Canonicalize those glyphs to the physical chord shown in UI.
    var shiftedGlyphs = {
      '!': '1', '@': '2', '#': '3', '$': '4', '%': '5', '^': '6',
      '&': '7', '*': '8', '(': '9', ')': '0', '_': '-', '+': '=',
      '{': '[', '}': ']', ':': ';', '"': "'", '<': ',', '>': '.',
      '?': '/', '|': '\\', '~': '`',
    };
    var shift = !!ev.shiftKey;
    if (shiftedGlyphs[key]) {
      key = shiftedGlyphs[key];
      shift = true;
    }

    // Shifted number/punctuation keys report the produced glyph in `key`.
    // Prefer `code` so capture and matching both represent Shift+1 as such.
    if (shift && /^Digit[0-9]$/.test(ev.code || '')) key = ev.code.slice(5);
    else if (shift) {
      var shiftedCodeKeys = {
        BracketLeft: '[', BracketRight: ']', Semicolon: ';', Quote: "'",
        Comma: ',', Period: '.', Slash: '/', Backslash: '\\', Backquote: '`',
        Minus: '-', Equal: '=',
      };
      if (shiftedCodeKeys[ev.code]) key = shiftedCodeKeys[ev.code];
    }
    if (key.length === 1 && /^[a-zA-Z]$/.test(key)) {
      key = shift ? key.toUpperCase() : key.toLowerCase();
    }

    var parts = [];
    if (ev.ctrlKey) parts.push('Ctrl');
    if (ev.altKey) parts.push('Alt');
    if (shift) parts.push('Shift');
    if (ev.metaKey) parts.push('Meta');
    parts.push(key);
    return parts.join('+');
  }

  function actionForEvent(ev, mode) {
    var binding = eventToBinding(ev);
    if (!binding) return '';
    var found = '';
    groups.some(function (group) {
      return group.shortcuts.some(function (shortcut) {
        if (!shortcut.id || shortcut.modes.indexOf(mode) === -1) return false;
        if (getBinding(shortcut.id) !== binding) return false;
        found = shortcut.id;
        return true;
      });
    });
    return found;
  }

  function findConflict(id, binding) {
    if (!binding) return null;
    var target = byID[id];
    if (!target) return null;
    var conflict = null;
    groups.some(function (group) {
      return group.shortcuts.some(function (shortcut) {
        if (!shortcut.id || shortcut.id === id) return false;
        var sharesMode = shortcut.modes.some(function (mode) { return target.modes.indexOf(mode) !== -1; });
        if (!sharesMode) return false;
        if (getBinding(shortcut.id) !== binding) return false;
        conflict = shortcut;
        return true;
      });
    });
    return conflict;
  }

  return {
    groups: groups,
    getBinding: getBinding,
    setBinding: setBinding,
    resetAll: resetAll,
    isCustomized: isCustomized,
    eventToBinding: eventToBinding,
    actionForEvent: actionForEvent,
    findConflict: findConflict,
    isReservedBinding: isReservedBinding,
  };
});
