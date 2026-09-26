(function() {
  'use strict';

  // Async boundaries for review data and the optional highlight worker pool.
  function createLoader(options) {
    let generation = 0;
    const pending = new Map();
    function invalidate() {
      generation++;
      pending.forEach(entry => entry.controller.abort());
      pending.clear();
    }
    function load(path) {
      const file = options.getFile(path);
      if (!file || !file.lazy) return Promise.resolve(file);
      const existing = pending.get(path);
      if (existing && existing.file === file) return existing.promise;
      if (existing) existing.controller.abort();
      const entry = { file, generation, controller: new AbortController() };
      entry.promise = Promise.resolve().then(() => options.load(file, entry.controller.signal)).then(loaded => {
        if (entry.controller.signal.aborted || entry.generation !== generation || options.getFile(path) !== file) return null;
        if (!loaded) return null;
        const keep = { viewed: file.viewed, collapsed: file.collapsed, viewMode: file.viewMode };
        Object.assign(file, loaded, keep, { lazy: false });
        if (options.changed) options.changed(path);
        return file;
      }).catch(error => {
        if (entry.controller.signal.aborted || error.name === 'AbortError') return null;
        throw error;
      }).finally(() => {
        if (pending.get(path) === entry) pending.delete(path);
      });
      pending.set(path, entry);
      return entry.promise;
    }
    return { load, invalidate };
  }

  function createWorkerController(options) {
    let pool;
    let disabled = false;
    let timer;
    let unsubscribe;
    const schedule = options.schedule || setTimeout;
    const cancel = options.cancel || clearTimeout;
    function stopWatching() {
      if (timer !== undefined) cancel(timer);
      timer = undefined;
      if (unsubscribe) unsubscribe();
      unsubscribe = undefined;
    }
    function fail(error) {
      if (disabled) return;
      disabled = true;
      stopWatching();
      if (pool) pool.terminate();
      pool = undefined;
      options.onFailure(error);
    }
    function get() {
      if (disabled) return undefined;
      if (pool) return pool;
      try {
        pool = options.create();
        if (!pool.isInitialized()) timer = schedule(() => fail(new Error('Highlight workers did not initialize')), options.timeout || 5000);
        const off = pool.subscribeToStatChanges(stats => {
          if (stats.workersFailed) fail(new Error('Highlight worker failed'));
          else if (stats.managerState === 'initialized' && timer !== undefined) {
            cancel(timer);
            timer = undefined;
          }
        });
        // Subscription may report failure synchronously.
        if (disabled) off(); else unsubscribe = off;
      } catch (error) { fail(error); }
      return pool;
    }
    return { get, dispose: stopWatching };
  }

  const api = { createLoader, createWorkerController };
  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.pierreRuntime = api;
  }
  if (typeof module === 'object' && module.exports) module.exports = api;
})();
