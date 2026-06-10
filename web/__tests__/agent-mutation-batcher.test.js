'use strict';
const test = require('node:test');
const assert = require('node:assert');
const { MutationBatcher } = require('../agent-mutation-batcher.js');

function makeFakeRaf() {
  const queue = [];
  return {
    request: (cb) => { queue.push(cb); return queue.length; },
    flush: () => { const q = queue.slice(); queue.length = 0; q.forEach(cb => cb(0)); },
  };
}

test('batches multiple mutations into a single rAF call', () => {
  const raf = makeFakeRaf();
  let drained = 0;
  const b = new MutationBatcher({
    raf: raf.request,
    onDrain: (count) => { drained = count; },
  });
  b.enqueue([{ type: 'childList' }, { type: 'childList' }]);
  b.enqueue([{ type: 'childList' }]);
  assert.equal(drained, 0);
  raf.flush();
  assert.equal(drained, 3);
});

test('budget exceeded triggers fullReresolve flag', () => {
  const raf = makeFakeRaf();
  let lastFull = null;
  const b = new MutationBatcher({
    raf: raf.request,
    budget: 5,
    onDrain: (c, full) => { lastFull = full; },
  });
  const recs = Array.from({ length: 10 }, () => ({ type: 'childList' }));
  b.enqueue(recs);
  raf.flush();
  assert.equal(lastFull, true);
});

test('budget under threshold does not flag fullReresolve', () => {
  const raf = makeFakeRaf();
  let lastFull = null;
  const b = new MutationBatcher({ raf: raf.request, budget: 5, onDrain: (c, f) => (lastFull = f) });
  b.enqueue([{}, {}]);
  raf.flush();
  assert.equal(lastFull, false);
});

test('pause window suppresses drains, then forces fullReresolve on next post-pause drain', async () => {
  const raf = makeFakeRaf();
  const drains = [];
  const b = new MutationBatcher({ raf: raf.request, onDrain: (count, full) => drains.push({ count, full }) });
  b.pause(50);
  b.enqueue([{}, {}, {}]);
  raf.flush();
  assert.deepEqual(drains, []);

  await new Promise(r => setTimeout(r, 60));
  b.scheduleCatchUpIfNeeded();
  raf.flush();
  assert.equal(drains.length, 1);
  assert.equal(drains[0].full, true);
  assert.equal(drains[0].count, 0);

  b.enqueue([{}]);
  raf.flush();
  assert.equal(drains.length, 2);
  assert.equal(drains[1].full, false);
});
