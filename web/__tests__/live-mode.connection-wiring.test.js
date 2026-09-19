'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');

const liveModeSrc = fs.readFileSync(require.resolve('../live-mode.js'), 'utf8');
const agentSrc = fs.readFileSync(require.resolve('../crit-agent.js'), 'utf8');
const indexSrc = fs.readFileSync(require.resolve('../index.html'), 'utf8');

function includes(substr, msg) {
  assert.ok(liveModeSrc.includes(substr), msg || 'live-mode.js should include: ' + substr);
}

function excludes(substr, msg) {
  assert.ok(!liveModeSrc.includes(substr), msg || 'live-mode.js should not include: ' + substr);
}

test('live-mode.js defaults agentConnectionState to connecting', () => {
  includes('agentConnectionState: \'connecting\'');
});

test('live-mode.js defaults to Comment (pin) mode', () => {
  includes('mode: \'pin\'');
});

test('live-mode.js declares connection controller variable', () => {
  includes('var connectionCtl = null;');
});

test('live-mode.js has connection UI update function', () => {
  includes('function updateConnectionUI()');
});

test('live-mode.js renders navbar status chip and context strips in buildShell', () => {
  includes('liveConnStatus');
  includes('liveConnStatusText');
  includes('id="liveModeHint"');
  includes('id="liveUnavailableFlash"');
  includes('id="liveUnavailableGuide"');
  includes('id="liveModeHintDismiss"');
  excludes('id="liveConnectionBanner"');
  excludes('id="liveConnectionRetry"');
});

test('live-mode.js starts connection tracking on iframe navigation', () => {
  includes('function startConnectionTracking()');
  includes('function onIframeLoad()');
  includes("addEventListener('load', onIframeLoad)");
  // agent-ready usually arrives before the load event; the load handler
  // must not blindly reset a successful ready state.
  includes('agentReadyForCurrentLoad');
});

test('live-mode.js does not wire a Retry button', () => {
  excludes('function installConnectionRetry()');
  includes('function installModeHintDismiss()');
});

test('live-mode.js transitions to ready in handleAgentReady', () => {
  includes("state.agentConnectionState = 'ready'");
});

test('agent-ready flushes cross-document pending pin focus after markers are pushed', () => {
  const start = liveModeSrc.indexOf('function handleAgentReady()');
  const end = liveModeSrc.indexOf('function handleAgentError', start);
  assert.ok(start >= 0 && end > start, 'handleAgentReady block must exist');
  const body = liveModeSrc.slice(start, end);
  assert.match(body, /pushPinsToAgent\(\);\s*focusPendingPinForCurrentRoute\(\);/);
});

test('SPA route changes use the same pending pin focus helper', () => {
  const start = liveModeSrc.indexOf('function handleRouteChange(msg)');
  const end = liveModeSrc.indexOf('function handlePinClicked', start);
  assert.ok(start >= 0 && end > start, 'handleRouteChange block must exist');
  assert.match(liveModeSrc.slice(start, end), /focusPendingPinForCurrentRoute\(\);/);
});

test('repeated card activation waits while the destination document is loading', () => {
  const start = liveModeSrc.indexOf('function activatePendingPinId()');
  const end = liveModeSrc.indexOf('state.activatePendingPinId = activatePendingPinId', start);
  assert.ok(start >= 0 && end > start, 'activatePendingPinId block must exist');
  assert.match(
    liveModeSrc.slice(start, end),
    /if \(!state\.agentReady\) \{\s*state\.pendingFlashOnLoad = true;\s*return;/,
  );
});

test('live-mode.js treats bootstrap errors as connection unavailable', () => {
  includes('isBootstrapErrorKind(e.kind)');
  includes("state.agentConnectionState = 'unavailable'");
});

test('live-mode.js gates pin mode on connection state', () => {
  includes("state.agentConnectionState !== 'ready'");
});

test('live-mode.js forces Browse when commenting is unavailable', () => {
  includes("state.mode = 'navigate'");
  includes('setActiveModeButton()');
});

test('live-mode.js clears the agent-ready latch on iframe navigations', () => {
  includes('function loadIframe(url)');
  includes('Consume the latch so the next navigation cannot inherit Ready');
});

test('crit-agent.js emits structured bootstrap errors', () => {
  assert.ok(agentSrc.includes("kind: 'protocol-missing'"));
  assert.ok(agentSrc.includes("kind: 'origin-guess-failed'"));
  assert.ok(agentSrc.includes("type: 'agent-error'"));
});

test('index.html loads the connection module before live-mode.js', () => {
  const connIdx = indexSrc.indexOf("'live-mode.connection.js'");
  const liveIdx = indexSrc.indexOf("s.src = 'live-mode.js'");
  assert.ok(connIdx > -1, 'index.html should load live-mode.connection.js');
  assert.ok(liveIdx > -1, 'index.html should load live-mode.js');
  assert.ok(connIdx < liveIdx, 'live-mode.connection.js must load before live-mode.js');
});
