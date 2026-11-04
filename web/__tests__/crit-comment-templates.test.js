const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');

// --- Sandbox setup ---
// The module reads its storage through configure(); by default the CLI's
// cookies via window.crit.shared. The cookie jar below keeps one value per
// name, like a browser, so both template cookies coexist.

function fakeElement(tag) {
  var el = {
    tagName: tag.toUpperCase(),
    className: '',
    textContent: '',
    title: '',
    style: {},
    children: [],
    attributes: {},
    _listeners: {},
    appendChild: function (child) { el.children.push(child); return child; },
    addEventListener: function (evt, fn) {
      if (!el._listeners[evt]) el._listeners[evt] = [];
      el._listeners[evt].push(fn);
    },
    setAttribute: function (name, value) { el.attributes[name] = String(value); },
  };
  Object.defineProperty(el, 'innerHTML', {
    get: function () { return ''; },
    set: function () { el.children = []; },
  });
  return el;
}

function makeSandbox() {
  var jar = {};
  var doc = {
    get cookie() {
      return Object.keys(jar).map(function (name) { return name + '=' + jar[name]; }).join('; ');
    },
    set cookie(v) {
      var pair = v.split(';')[0];
      var eq = pair.indexOf('=');
      jar[pair.slice(0, eq)] = pair.slice(eq + 1);
    },
    createElement: fakeElement,
  };
  var win = { crit: { shared: {} } };

  // Load crit-shared.js to get real getCookie/setCookie.
  var sharedSrc = fs.readFileSync(path.join(__dirname, '..', 'crit-shared.js'), 'utf8');
  new Function('window', 'document', sharedSrc)(win, doc);

  var src = fs.readFileSync(path.join(__dirname, '..', 'crit-comment-templates.js'), 'utf8');
  var mod = { exports: {} };
  new Function('window', 'document', 'module', src)(win, doc, mod);

  return { win: win, doc: doc, mod: mod, api: win.crit.commentTemplates, jar: jar };
}

function memoryStorage(initial) {
  var values = Object.assign({}, initial || {});
  return {
    values: values,
    get: function (key) { return Object.prototype.hasOwnProperty.call(values, key) ? values[key] : null; },
    set: function (key, value) { values[key] = value; },
  };
}

function chipLabels(bar) {
  return bar.children.map(function (chip) {
    var label = chip.children.filter(function (c) { return c.className === 'template-chip-label'; })[0];
    return label.textContent;
  });
}

// --- Storage and defaults ---

test('getTemplates returns empty array when no cookie', () => {
  var sb = makeSandbox();
  assert.deepEqual(sb.api.getTemplates(), []);
});

test('saveTemplates + getTemplates roundtrip through the cookie', () => {
  var sb = makeSandbox();
  sb.api.saveTemplates(['Fix typo', 'LGTM']);
  assert.deepEqual(sb.api.getTemplates(), ['Fix typo', 'LGTM']);
});

test('first use seeds Accepted and Rejected, pinned with their emoji and Alt+P / Alt+C', () => {
  var sb = makeSandbox();
  var items = sb.api.getTemplateItems();
  assert.deepEqual(items.map(function (i) { return [i.text, i.emoji, i.pinned, i.display, i.shortcut]; }), [
    ['Accepted', '👍', true, 'emoji', 'Alt+p'],
    ['Rejected', '👎', true, 'emoji', 'Alt+c'],
  ]);
  assert.equal(sb.api.findByShortcut('Alt+p').text, 'Accepted');
});

test('a template can hide its shortcut from the toolbar, and keeps the shortcut', () => {
  var sb = makeSandbox();
  sb.api.getTemplateItems();
  assert.equal(sb.api.getTemplateItems()[0].hideShortcut, false);
  sb.api.updateTemplate(0, { hideShortcut: true });
  var accepted = sb.api.getTemplateItems()[0];
  assert.equal(accepted.hideShortcut, true);
  assert.equal(accepted.shortcut, 'Alt+p');
  assert.equal(sb.api.findByShortcut('Alt+p').text, 'Accepted');
});

test('onChange listeners run after every change, once each', () => {
  var sb = makeSandbox();
  var calls = 0;
  function listener() { calls += 1; }
  sb.api.onChange(listener);
  sb.api.onChange(listener);
  sb.api.addTemplate('Nice!');
  sb.api.updateTemplate(0, { pinned: false });
  sb.api.moveTemplate(0, 1);
  sb.api.removeTemplate(0);
  assert.equal(calls, 4);
});

test('the template dialog is gone: templates are edited in the Settings tab', () => {
  var sb = makeSandbox();
  assert.equal(sb.api.openTemplateDialog, undefined);
  assert.equal(typeof sb.api.renderSettingsList, 'function');
});

test('seeding keeps saved templates and the crit-templates shape (texts only)', () => {
  var sb = makeSandbox();
  sb.api.saveTemplates(['Needs tests']);
  sb.api.getTemplateItems();
  assert.deepEqual(sb.api.getTemplates(), ['Needs tests', 'Accepted', 'Rejected']);
  assert.ok(JSON.parse(decodeURIComponent(sb.jar['crit-templates'])).every(function (t) { return typeof t === 'string'; }));
});

test('a removed default does not come back', () => {
  var sb = makeSandbox();
  sb.api.getTemplateItems();
  sb.api.removeTemplate(0);
  assert.deepEqual(sb.api.getTemplateItems().map(function (i) { return i.text; }), ['Rejected']);
});

test('configure() swaps the storage: crit-web passes localStorage', () => {
  var sb = makeSandbox();
  var storage = memoryStorage({ 'crit-templates': JSON.stringify(['From crit-web']) });
  sb.api.configure({ storage: storage });
  assert.deepEqual(sb.api.getTemplateItems().map(function (i) { return i.text; }), ['From crit-web', 'Accepted', 'Rejected']);
  assert.ok(storage.values['crit-template-meta']);
  assert.equal(sb.jar['crit-templates'], undefined);
});

// --- Metadata ---

test('addTemplate stores title, emoji, shortcut, display and pin', () => {
  var sb = makeSandbox();
  assert.equal(sb.api.addTemplate('  Please add tests  ', { title: ' Ask for tests ', emoji: '🧪', shortcut: 'x', display: 'both', pinned: true }), true);
  var item = sb.api.getTemplateItems().filter(function (i) { return i.text === 'Please add tests'; })[0];
  assert.deepEqual([item.title, item.emoji, item.shortcut, item.display, item.pinned], ['Ask for tests', '🧪', 'x', 'both', true]);
  assert.equal(sb.api.templateLabel(item), 'Ask for tests');
  assert.equal(sb.api.addTemplate('   '), false);
});

test('an unknown display falls back to emoji', () => {
  var sb = makeSandbox();
  sb.api.addTemplate('Plain', { display: 'banner' });
  assert.equal(sb.api.getTemplateItems().filter(function (i) { return i.text === 'Plain'; })[0].display, 'emoji');
});

test('updateTemplate renames a template with its metadata, and refuses an empty text', () => {
  var sb = makeSandbox();
  sb.api.getTemplateItems();
  assert.equal(sb.api.updateTemplate(0, { text: 'Approved', title: 'OK' }), true);
  var first = sb.api.getTemplateItems()[0];
  assert.deepEqual([first.text, first.title, first.emoji, first.pinned], ['Approved', 'OK', '👍', true]);
  assert.equal(sb.api.updateTemplate(0, { text: '  ' }), false);
  assert.equal(sb.api.updateTemplate(9, { pinned: false }), false);
});

test('moveTemplate reorders, and the pinned templates follow', () => {
  var sb = makeSandbox();
  sb.api.addTemplate('Out of scope', { emoji: '🚫', pinned: true });
  assert.equal(sb.api.moveTemplate(2, 0), true);
  assert.deepEqual(sb.api.getPinnedTemplates().map(function (i) { return i.emoji; }), ['🚫', '👍', '👎']);
  assert.equal(sb.api.moveTemplate(0, 3), false);
  assert.equal(sb.api.moveTemplate(1, 1), false);
});

// --- Shortcuts ---

test('findByShortcut returns the template a binding belongs to', () => {
  var sb = makeSandbox();
  sb.api.addTemplate('Out of scope', { shortcut: 'Alt+o' });
  assert.equal(sb.api.findByShortcut('Alt+o').text, 'Out of scope');
  assert.equal(sb.api.findByShortcut('z'), null);
  assert.equal(sb.api.findByShortcut(''), null);
});

test('shortcutConflict refuses browser chords, host bindings and other templates', () => {
  var sb = makeSandbox();
  sb.api.configure({
    shortcuts: {
      eventToBinding: function () { return ''; },
      refusal: function (binding) { return binding === 'c' ? 'c already does: Comment.' : ''; },
    },
  });
  sb.api.addTemplate('Out of scope', { shortcut: 'o' });
  var index = sb.api.findByShortcut('o').index;
  assert.match(sb.api.shortcutConflict('Ctrl+o', -1), /browser/);
  assert.match(sb.api.shortcutConflict('Meta+o', -1), /browser/);
  assert.equal(sb.api.shortcutConflict('c', -1), 'c already does: Comment.');
  assert.match(sb.api.shortcutConflict('o', -1), /Out of scope/);
  assert.equal(sb.api.shortcutConflict('o', index), '');
  assert.equal(sb.api.shortcutConflict('y', -1), '');
});

test('the CLI default refuses crit\'s own code-review bindings', () => {
  var sb = makeSandbox();
  var shortcutsSrc = fs.readFileSync(path.join(__dirname, '..', 'crit-shortcuts.js'), 'utf8');
  new Function('window', 'globalThis', 'module', shortcutsSrc)(sb.win, sb.win, undefined);
  assert.match(sb.api.shortcutConflict('j', -1), /Next block/);
  assert.match(sb.api.shortcutConflict('Enter', -1), /reserved/);
  assert.equal(sb.api.shortcutConflict('y', -1), '');
});

// --- Template bar ---

test('buildTemplateBar shows pinned templates first, by title when set', () => {
  var sb = makeSandbox();
  sb.api.saveTemplates(['Nice!']);
  sb.api.addTemplate('Please add tests', { title: 'Ask for tests', pinned: true });
  var bar = sb.api.buildTemplateBar({ onInsert: function () {}, onEdit: function () {} });
  assert.equal(bar.className, 'comment-template-bar');
  assert.deepEqual(chipLabels(bar), ['Accepted', 'Rejected', 'Ask for tests', 'Nice!']);
  assert.equal(bar.children[0].className, 'template-chip template-chip--pinned');
  assert.equal(bar.children[3].className, 'template-chip');
});

test('buildTemplateBar hides when every template is removed', () => {
  var sb = makeSandbox();
  sb.api.getTemplateItems();
  sb.api.removeTemplate(0);
  sb.api.removeTemplate(0);
  var bar = sb.api.buildTemplateBar({ onInsert: function () {} });
  assert.equal(bar.style.display, 'none');
});

test('open template bars redraw after a change, without a reload', () => {
  var sb = makeSandbox();
  var bar = sb.api.buildTemplateBar({ onInsert: function () {} });
  assert.deepEqual(chipLabels(bar), ['Accepted', 'Rejected']);
  sb.api.moveTemplate(1, 0);
  assert.deepEqual(chipLabels(bar), ['Rejected', 'Accepted']);
  sb.api.updateTemplate(0, { title: 'No' });
  assert.deepEqual(chipLabels(bar), ['No', 'Accepted']);
});

test('a chip carries an edit button only when onEdit is given', () => {
  var sb = makeSandbox();
  var withEdit = sb.api.buildTemplateBar({ onInsert: function () {}, onEdit: function () {} });
  var without = sb.api.buildTemplateBar({ onInsert: function () {} });
  var hasEdit = function (chip) { return chip.children.some(function (c) { return c.className === 'template-chip-edit'; }); };
  assert.equal(hasEdit(withEdit.children[0]), true);
  assert.equal(hasEdit(without.children[0]), false);
});

test('CommonJS module.exports matches window.crit.commentTemplates', () => {
  var sb = makeSandbox();
  assert.strictEqual(sb.mod.exports, sb.win.crit.commentTemplates);
});
