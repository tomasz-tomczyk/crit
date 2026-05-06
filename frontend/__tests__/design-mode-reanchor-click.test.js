'use strict';
const test = require('node:test');
const assert = require('node:assert');
const { handleTrayClick, armReanchor, disarmReanchor } = require('../design-mode-reanchor-click.js');

test('clicks on Re-anchor btn dispatch enter-reanchor-mode', () => {
  let posted = null;
  const target = { matches: sel => sel === '.crit-design-reanchor-btn', getAttribute: () => 'p1' };
  handleTrayClick({ target }, m => (posted = m));
  assert.deepEqual(posted, { type: 'enter-reanchor-mode', pin_id: 'p1' });
});

test('clicks elsewhere are ignored', () => {
  let posted = null;
  const target = { matches: () => false, getAttribute: () => null };
  handleTrayClick({ target }, m => (posted = m));
  assert.equal(posted, null);
});

test('armReanchor disables the button and sets a 30s timeout', () => {
  const btn = { disabled: false };
  const ctx = {
    state: {},
    post: () => {},
    setTimeout: (cb, ms) => { ctx.scheduledMs = ms; return 7; },
    clearTimeout: () => {},
  };
  armReanchor(ctx, 'p1', btn);
  assert.equal(btn.disabled, true);
  assert.equal(ctx.scheduledMs, 30000);
  assert.equal(ctx.state.reanchorPending, 'p1');
  disarmReanchor(ctx, 'completed');
  assert.equal(btn.disabled, false);
  assert.equal(ctx.state.reanchorPending, null);
});

test('armReanchor is no-op when already armed for some pin', () => {
  const btn1 = { disabled: false };
  const btn2 = { disabled: false };
  const ctx = {
    state: {},
    post: () => {},
    setTimeout: () => 1,
    clearTimeout: () => {},
  };
  armReanchor(ctx, 'p1', btn1);
  armReanchor(ctx, 'p2', btn2);
  assert.equal(ctx.state.reanchorPending, 'p1');
  assert.equal(btn2.disabled, false);
});
