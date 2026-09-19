'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { makeConnectionState, isBootstrapErrorKind, BOOTSTRAP_KINDS } = require('../live-mode.connection.js');

test('starts in connecting state and emits onChange', () => {
  const events = [];
  const ctl = makeConnectionState({ onChange: (s, r) => events.push([s, r]) });
  assert.equal(ctl.state(), 'connecting');
  assert.deepEqual(events, [['connecting', undefined]]);
  ctl.destroy();
});

test('setReady transitions to ready and stops timeout', () => {
  const events = [];
  const ctl = makeConnectionState({
    timeoutMs: 50,
    onChange: (s, r) => events.push([s, r]),
  });
  ctl.setReady();
  assert.equal(ctl.state(), 'ready');
  assert.deepEqual(events, [['connecting', undefined], ['ready', undefined]]);
  // Timeout should not fire after ready.
  return new Promise((resolve) => setTimeout(resolve, 80)).then(() => {
    assert.equal(ctl.state(), 'ready');
    assert.deepEqual(events, [['connecting', undefined], ['ready', undefined]]);
    ctl.destroy();
  });
});

test('timeout transitions to unavailable with reason', () => {
  const events = [];
  const ctl = makeConnectionState({
    timeoutMs: 30,
    onChange: (s, r) => events.push([s, r]),
  });
  return new Promise((resolve) => setTimeout(resolve, 80)).then(() => {
    assert.equal(ctl.state(), 'unavailable');
    assert.deepEqual(events, [['connecting', undefined], ['unavailable', 'timeout']]);
    ctl.destroy();
  });
});

test('explicit setUnavailable carries the reason', () => {
  const events = [];
  const ctl = makeConnectionState({
    timeoutMs: 9999,
    onChange: (s, r) => events.push([s, r]),
  });
  ctl.setUnavailable('csp-violation');
  assert.equal(ctl.state(), 'unavailable');
  assert.deepEqual(events, [['connecting', undefined], ['unavailable', 'csp-violation']]);
  ctl.destroy();
});

test('retry from unavailable returns to connecting and calls onRetry', () => {
  const events = [];
  let retries = 0;
  const ctl = makeConnectionState({
    timeoutMs: 9999,
    onChange: (s, r) => events.push([s, r]),
    onRetry: () => retries++,
  });
  ctl.setUnavailable('timeout');
  assert.equal(ctl.state(), 'unavailable');
  ctl.retry();
  assert.equal(ctl.state(), 'connecting');
  assert.equal(retries, 1);
  assert.deepEqual(events, [
    ['connecting', undefined],
    ['unavailable', 'timeout'],
    ['connecting', undefined],
  ]);
  ctl.destroy();
});

test('retry is ignored when not unavailable', () => {
  let retries = 0;
  const ctl = makeConnectionState({
    timeoutMs: 9999,
    onRetry: () => retries++,
  });
  ctl.retry();
  assert.equal(ctl.state(), 'connecting');
  assert.equal(retries, 0);
  ctl.setReady();
  ctl.retry();
  assert.equal(ctl.state(), 'ready');
  assert.equal(retries, 0);
  ctl.destroy();
});

test('setUnavailable is ignored once ready', () => {
  const events = [];
  const ctl = makeConnectionState({
    timeoutMs: 9999,
    onChange: (s, r) => events.push([s, r]),
  });
  ctl.setReady();
  ctl.setUnavailable('timeout');
  assert.equal(ctl.state(), 'ready');
  assert.deepEqual(events, [['connecting', undefined], ['ready', undefined]]);
  ctl.destroy();
});

test('bootstrap error kinds are recognized', () => {
  assert.equal(isBootstrapErrorKind('protocol-missing'), true);
  assert.equal(isBootstrapErrorKind('origin-guess-failed'), true);
  assert.equal(isBootstrapErrorKind('already-booted'), true);
  assert.equal(isBootstrapErrorKind('incompatible-response'), true);
  assert.equal(isBootstrapErrorKind('csp-violation'), true);
});

test('non-bootstrap error kinds are not treated as connection failures', () => {
  assert.equal(isBootstrapErrorKind('capture-failed'), false);
  assert.equal(isBootstrapErrorKind('shadow-dom'), false);
  assert.equal(isBootstrapErrorKind('selection-too-large'), false);
  assert.equal(isBootstrapErrorKind(''), false);
  assert.equal(isBootstrapErrorKind(null), false);
  assert.equal(isBootstrapErrorKind(undefined), false);
});

test('BOOTSTRAP_KINDS set is exposed and frozen in shape', () => {
  assert.equal(BOOTSTRAP_KINDS['capture-failed'], undefined);
  assert.equal(BOOTSTRAP_KINDS['protocol-missing'], true);
});

test('destroy clears timeout so it does not leak', () => {
  const events = [];
  const ctl = makeConnectionState({
    timeoutMs: 30,
    onChange: (s, r) => events.push([s, r]),
  });
  ctl.destroy();
  return new Promise((resolve) => setTimeout(resolve, 80)).then(() => {
    assert.equal(ctl.state(), 'connecting');
    assert.deepEqual(events, [['connecting', undefined]]);
  });
});
