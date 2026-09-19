'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const liveModeSrc = fs.readFileSync(require.resolve('../live-mode.js'), 'utf8');
const agentSrc = fs.readFileSync(require.resolve('../crit-agent.js'), 'utf8');
const indexSrc = fs.readFileSync(require.resolve('../index.html'), 'utf8');

function includes(substr, msg) {
  assert.ok(liveModeSrc.includes(substr), msg || 'live-mode.js should include: ' + substr);
}

test('live-mode.js defaults agentConnectionState to connecting', () => {
  includes('agentConnectionState: \'connecting\'');
});

test('live-mode.js declares connection controller variable', () => {
  includes('var connectionCtl = null;');
});

test('live-mode.js has connection UI update function', () => {
  includes('function updateConnectionUI()');
});

test('live-mode.js renders the connection banner in buildShell', () => {
  includes('id="liveConnectionBanner"');
  includes('id="liveConnectionText"');
  includes('id="liveConnectionRetry"');
  includes('id="liveConnectionHelp"');
});

test('live-mode.js starts connection tracking on iframe load', () => {
  includes('function startConnectionTracking()');
  includes("addEventListener('load', startConnectionTracking)");
});

test('live-mode.js wires the retry button', () => {
  includes('function installConnectionRetry()');
  includes('liveConnectionRetry');
});

test('live-mode.js transitions to ready in handleAgentReady', () => {
  includes("state.agentConnectionState = 'ready'");
});

test('live-mode.js treats bootstrap errors as connection unavailable', () => {
  includes('isBootstrapErrorKind(e.kind)');
  includes("state.agentConnectionState = 'unavailable'");
});

test('live-mode.js gates pin mode on connection state', () => {
  includes("state.agentConnectionState !== 'ready'");
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
