const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');

// --- Sandbox setup ---
// We need window.crit.shared.getCookie/setCookie available before loading
// crit-comment-templates.js. Build a minimal shim.

function makeSandbox() {
  var cookieJar = '';
  var doc = {
    get cookie() { return cookieJar; },
    set cookie(v) { cookieJar = v; },
    createElement: function (tag) {
      var el = {
        tagName: tag.toUpperCase(),
        className: '',
        textContent: '',
        title: '',
        style: {},
        innerHTML: '',
        children: [],
        _listeners: {},
        appendChild: function (child) { el.children.push(child); return child; },
        addEventListener: function (evt, fn) {
          if (!el._listeners[evt]) el._listeners[evt] = [];
          el._listeners[evt].push(fn);
        },
      };
      return el;
    },
  };
  var win = { crit: { shared: {} } };

  // Load crit-shared.js to get real getCookie/setCookie.
  var sharedSrc = fs.readFileSync(path.join(__dirname, '..', 'crit-shared.js'), 'utf8');
  var sharedFn = new Function('window', 'document', sharedSrc);
  sharedFn(win, doc);

  // Now load the module under test.
  var src = fs.readFileSync(path.join(__dirname, '..', 'crit-comment-templates.js'), 'utf8');
  var fn = new Function('window', 'document', 'module', src + '\nreturn window;');
  var mod = { exports: {} };
  fn(win, doc, mod);

  return { win: win, doc: doc, mod: mod, setCookieRaw: function (v) { cookieJar = v; } };
}

// --- Tests ---

test('getTemplates returns empty array when no cookie', () => {
  var sb = makeSandbox();
  var api = sb.win.crit.commentTemplates;
  assert.deepEqual(api.getTemplates(), []);
});

test('saveTemplates + getTemplates roundtrip', () => {
  var sb = makeSandbox();
  var api = sb.win.crit.commentTemplates;
  api.saveTemplates(['Fix typo', 'LGTM']);
  // Simulate browser echoing cookie back (only name=value, no attributes).
  var raw = sb.doc.cookie.split(';')[0]; // "crit-templates=..."
  sb.setCookieRaw(raw);
  assert.deepEqual(api.getTemplates(), ['Fix typo', 'LGTM']);
});

test('buildTemplateBar returns a DOM element with correct class', () => {
  var sb = makeSandbox();
  var api = sb.win.crit.commentTemplates;

  // Seed one template so the bar renders chips.
  api.saveTemplates(['Nice!']);
  var raw = sb.doc.cookie.split(';')[0];
  sb.setCookieRaw(raw);

  var inserted = [];
  var bar = api.buildTemplateBar({
    onInsert: function (text) { inserted.push(text); },
    onSaveNew: function () {},
  });
  assert.equal(bar.className, 'comment-template-bar');
  // Should have one chip child.
  assert.equal(bar.children.length, 1);
  assert.equal(bar.children[0].className, 'template-chip');
});

test('buildTemplateBar hides when no templates', () => {
  var sb = makeSandbox();
  var api = sb.win.crit.commentTemplates;
  var bar = api.buildTemplateBar({ onInsert: function () {}, onSaveNew: function () {} });
  assert.equal(bar.style.display, 'none');
});

test('CommonJS module.exports matches window.crit.commentTemplates', () => {
  var sb = makeSandbox();
  assert.strictEqual(sb.mod.exports, sb.win.crit.commentTemplates);
});
