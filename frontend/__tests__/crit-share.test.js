'use strict';
// Unit tests for crit-share.js (window.crit.share). These exercise the
// create() factory contract and the reveal() button-gating logic — the parts
// that are pure enough to test without a real browser. The full share flow
// (modals, popup relay, fetch) is covered by the integration suite
// (share_integration_test.go) and the Playwright e2e tests.
//
// crit-share.js queries document.getElementById('shareBtn') and attaches a
// click listener in create(), then reveal() toggles the button's display.
// Node has no DOM, so we install a minimal stub covering exactly the surface
// the module touches: getElementById, a fake button element with a style
// object, classList, and addEventListener.

const { test, beforeEach } = require('node:test');
const assert = require('node:assert/strict');

function makeButton() {
  return {
    style: { display: 'none' },
    disabled: false,
    textContent: 'Share',
    _classes: new Set(),
    classList: {
      add(c) { this._set.add(c); },
      remove(c) { this._set.delete(c); },
      contains(c) { return this._set.has(c); },
    },
    _listeners: {},
    addEventListener(type, fn) { (this._listeners[type] = this._listeners[type] || []).push(fn); },
    focus() {},
  };
}

function installDomStub(elements) {
  global.window = global.window || {};
  global.document = {
    getElementById(id) { return elements[id] || null; },
    createElement() { return { style: {}, setAttribute() {}, appendChild() {}, addEventListener() {}, querySelector() { return null; }, querySelectorAll() { return []; } }; },
    body: { appendChild() {} },
  };
}

// Wire classList.add/remove against the same Set the button carries.
function wireButton(btn) {
  btn.classList._set = btn._classes;
  return btn;
}

let share;
beforeEach(() => {
  // Fresh module each time (it caches window.crit.share on require, but the
  // factory itself is stateless — re-requiring is cheap and isolates globals).
  delete require.cache[require.resolve('../crit-share.js')];
  installDomStub({});
  share = require('../crit-share.js');
});

test('module exports a create() factory', () => {
  assert.equal(typeof share.create, 'function');
});

test('create() returns a controller with reveal/setButtonState/openModal/closeModal', () => {
  const btn = wireButton(makeButton());
  installDomStub({ shareBtn: btn });
  share = require('../crit-share.js');
  const ctl = share.create({ shareBtnEl: btn, shareURL: 'https://crit.md', canShare: true });
  assert.equal(typeof ctl.reveal, 'function');
  assert.equal(typeof ctl.setButtonState, 'function');
  assert.equal(typeof ctl.openModal, 'function');
  assert.equal(typeof ctl.closeModal, 'function');
});

test('create() wires a click handler onto shareBtnEl', () => {
  const btn = wireButton(makeButton());
  installDomStub({ shareBtn: btn });
  share = require('../crit-share.js');
  share.create({ shareBtnEl: btn, shareURL: 'https://crit.md', canShare: true });
  assert.equal((btn._listeners.click || []).length, 1);
});

test('reveal() shows the button when shareURL && canShare', () => {
  const btn = wireButton(makeButton());
  installDomStub({ shareBtn: btn });
  share = require('../crit-share.js');
  const ctl = share.create({ shareBtnEl: btn, shareURL: 'https://crit.md', canShare: true });
  assert.equal(btn.style.display, 'none');
  ctl.reveal();
  assert.equal(btn.style.display, '');
});

test('reveal() does NOT show the button when canShare is false (e.g. git mode)', () => {
  const btn = wireButton(makeButton());
  installDomStub({ shareBtn: btn });
  share = require('../crit-share.js');
  const ctl = share.create({ shareBtnEl: btn, shareURL: 'https://crit.md', canShare: false });
  ctl.reveal();
  assert.equal(btn.style.display, 'none');
});

test('reveal() does NOT show the button when shareURL is empty', () => {
  const btn = wireButton(makeButton());
  installDomStub({ shareBtn: btn });
  share = require('../crit-share.js');
  const ctl = share.create({ shareBtnEl: btn, shareURL: '', canShare: true });
  ctl.reveal();
  assert.equal(btn.style.display, 'none');
});

test('reveal() sets the button to the shared state when a hostedURL already exists', () => {
  const btn = wireButton(makeButton());
  installDomStub({ shareBtn: btn });
  share = require('../crit-share.js');
  const ctl = share.create({
    shareBtnEl: btn,
    shareURL: 'https://crit.md',
    hostedURL: 'https://crit.md/r/abc',
    canShare: true,
  });
  ctl.reveal();
  assert.equal(btn.style.display, '');
  assert.equal(btn.textContent, 'Shared');
  assert.equal(btn.classList.contains('btn-success'), true);
  assert.equal(btn.disabled, false);
});

test('setButtonState toggles label/disabled/btn-success for sharing/shared/default', () => {
  const btn = wireButton(makeButton());
  installDomStub({ shareBtn: btn });
  share = require('../crit-share.js');
  const ctl = share.create({ shareBtnEl: btn, shareURL: 'https://crit.md', canShare: true });

  ctl.setButtonState('sharing');
  assert.equal(btn.disabled, true);
  assert.equal(btn.classList.contains('btn-success'), false);

  ctl.setButtonState('shared');
  assert.equal(btn.textContent, 'Shared');
  assert.equal(btn.disabled, false);
  assert.equal(btn.classList.contains('btn-success'), true);

  ctl.setButtonState('default');
  assert.equal(btn.textContent, 'Share');
  assert.equal(btn.disabled, false);
  assert.equal(btn.classList.contains('btn-success'), false);
});
