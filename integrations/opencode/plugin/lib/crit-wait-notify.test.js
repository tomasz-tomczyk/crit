'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { isCritWaitCommand, roundReadyToast } = require('./crit-wait-notify.js');

test('isCritWaitCommand accepts bare crit and file args', () => {
  assert.equal(isCritWaitCommand('crit'), true);
  assert.equal(isCritWaitCommand('crit plan.md'), true);
  assert.equal(isCritWaitCommand('./crit'), true);
  assert.equal(isCritWaitCommand('crit --pr 12'), true);
  assert.equal(isCritWaitCommand('CRIT_FOO=1 crit'), true);
});

test('isCritWaitCommand rejects non-wait subcommands', () => {
  assert.equal(isCritWaitCommand('crit comment --reply-to c_1 hi'), false);
  assert.equal(isCritWaitCommand('crit comments --json'), false);
  assert.equal(isCritWaitCommand('crit config'), false);
  assert.equal(isCritWaitCommand('crit share plan.md'), false);
  assert.equal(isCritWaitCommand('crit install opencode'), false);
  assert.equal(isCritWaitCommand('echo crit'), false);
  assert.equal(isCritWaitCommand(''), false);
});

test('roundReadyToast includes URL when provided', () => {
  const withURL = roundReadyToast('http://127.0.0.1:9');
  assert.equal(withURL.title, 'Crit');
  assert.match(withURL.message, /http:\/\/127\.0\.0\.1:9/);

  const without = roundReadyToast('');
  assert.match(without.message, /browser/i);
});
