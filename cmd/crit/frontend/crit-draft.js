(function () {
  'use strict';

  var DEBOUNCE_MS = 500;
  var timers = {};

  function storageKey(formKey) {
    return 'crit-draft-' + formKey;
  }

  function saveDraft(formKey, data) {
    if (timers[formKey]) clearTimeout(timers[formKey]);
    timers[formKey] = setTimeout(function () {
      try {
        localStorage.setItem(storageKey(formKey), JSON.stringify(data));
      } catch (e) {}
      delete timers[formKey];
    }, DEBOUNCE_MS);
  }

  function saveDraftImmediate(formKey, data) {
    if (timers[formKey]) {
      clearTimeout(timers[formKey]);
      delete timers[formKey];
    }
    try {
      localStorage.setItem(storageKey(formKey), JSON.stringify(data));
    } catch (e) {}
  }

  function loadDraft(formKey) {
    try {
      var raw = localStorage.getItem(storageKey(formKey));
      return raw ? JSON.parse(raw) : null;
    } catch (e) {
      return null;
    }
  }

  function clearDraft(formKey) {
    if (timers[formKey]) {
      clearTimeout(timers[formKey]);
      delete timers[formKey];
    }
    try {
      localStorage.removeItem(storageKey(formKey));
    } catch (e) {}
  }

  function clearAllDrafts(prefix) {
    try {
      var toRemove = [];
      for (var i = 0; i < localStorage.length; i++) {
        var key = localStorage.key(i);
        if (key && key.indexOf('crit-draft-' + (prefix || '')) === 0) {
          toRemove.push(key);
        }
      }
      for (var j = 0; j < toRemove.length; j++) {
        localStorage.removeItem(toRemove[j]);
      }
    } catch (e) {}
  }

  function flushAll() {
    var keys = Object.keys(timers);
    for (var i = 0; i < keys.length; i++) {
      var formKey = keys[i];
      clearTimeout(timers[formKey]);
      delete timers[formKey];
    }
  }

  var api = {
    saveDraft: saveDraft,
    saveDraftImmediate: saveDraftImmediate,
    loadDraft: loadDraft,
    clearDraft: clearDraft,
    clearAllDrafts: clearAllDrafts,
    flushAll: flushAll,
  };

  window.crit = window.crit || {};
  window.crit.draft = api;

  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
