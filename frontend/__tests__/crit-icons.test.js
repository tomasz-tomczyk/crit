const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');

// Load crit-icons.js in a fake-browser sandbox.
const src = fs.readFileSync(path.join(__dirname, '..', 'crit-icons.js'), 'utf8');
const fn = new Function('window', 'document', src + '\nreturn window;');
const sandbox = { window: {}, document: {} };
fn(sandbox.window, sandbox.document);
const icons = sandbox.window.crit.icons;

test('all exported icon values are non-empty strings containing <svg', () => {
  const keys = Object.keys(icons);
  assert.ok(keys.length > 0, 'should export at least one icon');
  for (const key of keys) {
    assert.equal(typeof icons[key], 'string', key + ' should be a string');
    assert.ok(icons[key].length > 0, key + ' should be non-empty');
    assert.ok(icons[key].includes('<svg'), key + ' should contain <svg');
  }
});

test('well-known icons exist: ICON_CHEVRON, ICON_EDIT, ICON_DELETE, ICON_RESOLVE', () => {
  assert.ok(icons.ICON_CHEVRON, 'ICON_CHEVRON should exist');
  assert.ok(icons.ICON_EDIT, 'ICON_EDIT should exist');
  assert.ok(icons.ICON_DELETE, 'ICON_DELETE should exist');
  assert.ok(icons.ICON_RESOLVE, 'ICON_RESOLVE should exist');
});

test('ICON_COMMENT uses a 16x16 viewBox', () => {
  assert.ok(icons.ICON_COMMENT.includes('viewBox="0 0 16 16"'),
    'ICON_COMMENT should have a 16x16 viewBox');
});

test('module.exports matches window.crit.icons', () => {
  const mod = require(path.join(__dirname, '..', 'crit-icons.js'));
  assert.deepEqual(Object.keys(mod).sort(), Object.keys(icons).sort());
  for (const key of Object.keys(mod)) {
    assert.equal(mod[key], icons[key]);
  }
});
