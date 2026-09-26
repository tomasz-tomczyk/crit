'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const { createLoader, createWorkerController } = require('../crit-pierre-runtime.js');

test('lazy loader deduplicates and rejects responses after scope or file replacement', async () => {
  let file = { path: 'a.go', lazy: true, content: 'current', viewed: true, collapsed: false, viewMode: 'diff' };
  let resolve;
  let signal;
  const loader = createLoader({ getFile: () => file, load: (_file, s) => { signal = s; return new Promise(r => { resolve = r; }); } });
  const first = loader.load('a.go');
  assert.equal(loader.load('a.go'), first);
  await Promise.resolve();
  loader.invalidate();
  assert.equal(signal.aborted, true);
  file = { path: 'a.go', lazy: true, content: 'new scope' };
  resolve({ content: 'stale scope' });
  assert.equal(await first, null);
  assert.equal(file.content, 'new scope');
  const second = loader.load('a.go');
  await Promise.resolve();
  file = { path: 'a.go', lazy: true, content: 'new file identity' };
  resolve({ content: 'stale file' });
  assert.equal(await second, null);
  assert.equal(file.content, 'new file identity');
});

test('lazy hydration preserves current user state and aborts resolve harmlessly', async () => {
  const file = { path: 'a.go', lazy: true, viewed: false, collapsed: false, viewMode: 'diff' };
  let finish;
  const loader = createLoader({ getFile: () => file, load: () => new Promise(r => { finish = r; }) });
  const promise = loader.load('a.go');
  await Promise.resolve();
  file.viewed = true;
  file.collapsed = true;
  finish({ content: 'loaded', viewed: false, collapsed: false, viewMode: 'document' });
  assert.equal(await promise, file);
  assert.equal(file.content, 'loaded');
  assert.equal(file.lazy, false);
  assert.equal(file.viewed, true);
  assert.equal(file.collapsed, true);
  assert.equal(file.viewMode, 'diff');
});

function harness(initialized = false) {
  let timeout;
  let listener;
  let failures = 0;
  let terminations = 0;
  let unsubscribed = 0;
  const pool = {
    isInitialized: () => initialized,
    subscribeToStatChanges(fn) { listener = fn; return () => { unsubscribed++; }; },
    terminate() { terminations++; },
  };
  const controller = createWorkerController({
    create: () => pool,
    onFailure: () => { failures++; },
    schedule: fn => { timeout = fn; return 1; },
    cancel: () => { timeout = undefined; },
  });
  return { pool, controller, timeout: () => timeout?.(), notify: stats => listener(stats), counts: () => ({ failures, terminations, unsubscribed }) };
}

test('dead workers time out, terminate once and stay disabled', () => {
  const h = harness();
  assert.equal(h.controller.get(), h.pool);
  h.timeout();
  assert.equal(h.controller.get(), undefined);
  assert.deepEqual(h.counts(), { failures: 1, terminations: 1, unsubscribed: 1 });
});

test('initialized workers cancel the deadline but later failure still degrades safely', () => {
  const h = harness();
  h.controller.get();
  h.notify({ managerState: 'initialized' });
  h.timeout();
  assert.equal(h.counts().failures, 0);
  h.notify({ workersFailed: 1 });
  h.notify({ workersFailed: 1 });
  assert.equal(h.controller.get(), undefined);
  assert.equal(h.counts().failures, 1);
});

test('worker creation failures never escape into the render path', () => {
  let failures = 0;
  const controller = createWorkerController({ create: () => { throw new Error('blocked'); }, onFailure: () => { failures++; } });
  assert.equal(controller.get(), undefined);
  assert.equal(controller.get(), undefined);
  assert.equal(failures, 1);
});
