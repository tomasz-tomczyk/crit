'use strict';
// Regression: per-pin base64 JPEGs were dropped from DOMAnchor (Go side
// removed the field too). The agent must stop capturing them, the
// composer must stop rendering the thumbnail preview, the pin row card
// must stop rendering the <img> thumbnail, and html2canvas must no
// longer be loaded.
//
// We assert against source text rather than spinning up jsdom because
// the agent module is an IIFE that mounts on a parent window — it has
// no exported entry point the test can call. Source-level assertions
// keep the contract explicit and rebase-safe.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const ROOT = path.resolve(__dirname, '..');
const read = (p) => fs.readFileSync(path.join(ROOT, p), 'utf8');

test('crit-agent.js does not capture or attach a screenshot to DOMAnchor', () => {
  const src = read('crit-agent.js');
  // Construction site (buildDOMAnchorFor) — no `screenshot:` field.
  assert.equal(/screenshot:\s*''/.test(src), false,
    "DOMAnchor builder should not declare a screenshot field");
  // Capture call site — captureScreenshot is the only thing that ever
  // populated sel.anchor.screenshot.
  assert.equal(src.includes('captureScreenshot'), false,
    'captureScreenshot helper must be removed');
  assert.equal(src.includes('html2canvas'), false,
    'html2canvas loader / references must be removed from crit-agent.js');
});

test('design-mode.composer.js does not render a screenshot thumbnail', () => {
  const src = read('design-mode.composer.js');
  assert.equal(src.includes('screenshot'), false,
    'composer thumbnail rendering should be removed');
});

test('design-mode.row.js does not render a pin screenshot <img>', () => {
  const src = read('design-mode.row.js');
  // Allow header/comment text but not screenshot rendering or property reads.
  assert.equal(/anchor\.screenshot/.test(src), false,
    'row card should not read anchor.screenshot');
  assert.equal(src.includes('crit-design-comment-thumb'), false,
    'row card should not append a .crit-design-comment-thumb image');
});

test('agent-screenshot-options.js module is removed', () => {
  assert.equal(fs.existsSync(path.join(ROOT, 'agent-screenshot-options.js')), false,
    'agent-screenshot-options.js should be deleted');
  assert.equal(fs.existsSync(path.join(ROOT, '__tests__/agent-screenshot-options.test.js')), false,
    'agent-screenshot-options.test.js should be deleted');
});

test('design-mode.panel-render.js comment signature drops screenshot key', () => {
  // The signature is what panel reuse decisions key off; including
  // anchor.screenshot in it just kept stale data alive across reuse.
  const src = read('design-mode.panel-render.js');
  assert.equal(/anchor\.screenshot/.test(src), false,
    'commentSignature should not reference anchor.screenshot');
});
