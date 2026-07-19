'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { create } = require('../crit-tab-ready.js');

function fakeDocument(visibility) {
  return {
    title: 'Crit Review',
    visibilityState: visibility || 'visible',
  };
}

test('notifyRoundReady badges when tab is hidden', () => {
  const doc = fakeDocument('hidden');
  const ready = create({ document: doc, Notification: null, baseTitle: 'Crit Review' });
  const result = ready.notifyRoundReady({ body: 'Round 2 is ready' });
  assert.equal(result.badged, true);
  assert.equal(result.notified, false);
  assert.equal(ready.isBadgeActive(), true);
  assert.ok(doc.title.startsWith(ready.BADGE_PREFIX));
});

test('notifyRoundReady is a no-op when tab is visible', () => {
  const doc = fakeDocument('visible');
  const ready = create({ document: doc, Notification: null, baseTitle: 'Crit Review' });
  const result = ready.notifyRoundReady();
  assert.equal(result.badged, false);
  assert.equal(ready.isBadgeActive(), false);
  assert.equal(doc.title, 'Crit Review');
});

test('notifyRoundReady fires Notification when permission granted', () => {
  const constructed = [];
  function FakeNotification(title, opts) {
    constructed.push({ title, opts });
    this.title = title;
    this.opts = opts;
  }
  FakeNotification.permission = 'granted';

  const doc = fakeDocument('hidden');
  const ready = create({ document: doc, Notification: FakeNotification, baseTitle: 'Crit Review' });
  const result = ready.notifyRoundReady({ title: 'Crit', body: 'Round 3 is ready for review' });
  assert.equal(result.notified, true);
  assert.equal(constructed.length, 1);
  assert.equal(constructed[0].title, 'Crit');
  assert.equal(constructed[0].opts.body, 'Round 3 is ready for review');
  assert.equal(constructed[0].opts.tag, 'crit-round-ready');
});

test('clearTabBadge restores title', () => {
  const doc = fakeDocument('hidden');
  const ready = create({ document: doc, Notification: null, baseTitle: 'Crit Review' });
  ready.notifyRoundReady();
  ready.clearTabBadge();
  assert.equal(ready.isBadgeActive(), false);
  assert.equal(doc.title, 'Crit Review');
});
