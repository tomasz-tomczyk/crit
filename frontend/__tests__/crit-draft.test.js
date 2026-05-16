const { describe, it, beforeEach } = require('node:test');
const assert = require('node:assert/strict');

const store = {};
global.localStorage = {
  getItem: (k) => store[k] ?? null,
  setItem: (k, v) => { store[k] = v; },
  removeItem: (k) => { delete store[k]; },
  get length() { return Object.keys(store).length; },
  key: (i) => Object.keys(store)[i] ?? null,
};

global.window = { crit: {} };

const draft = require('../crit-draft.js');

describe('crit-draft', () => {
  beforeEach(() => {
    Object.keys(store).forEach(k => delete store[k]);
  });

  it('saveDraftImmediate writes to localStorage', () => {
    draft.saveDraftImmediate('test-key', { body: 'hello' });
    const loaded = draft.loadDraft('test-key');
    assert.deepEqual(loaded, { body: 'hello' });
  });

  it('clearDraft removes from localStorage', () => {
    draft.saveDraftImmediate('test-key', { body: 'hello' });
    draft.clearDraft('test-key');
    assert.equal(draft.loadDraft('test-key'), null);
  });

  it('clearAllDrafts removes matching keys', () => {
    draft.saveDraftImmediate('file-a', { body: 'a' });
    draft.saveDraftImmediate('file-b', { body: 'b' });
    draft.saveDraftImmediate('design-c', { body: 'c' });
    draft.clearAllDrafts('file-');
    assert.equal(draft.loadDraft('file-a'), null);
    assert.equal(draft.loadDraft('file-b'), null);
    assert.deepEqual(draft.loadDraft('design-c'), { body: 'c' });
  });

  it('loadDraft returns null for missing key', () => {
    assert.equal(draft.loadDraft('nonexistent'), null);
  });
});
