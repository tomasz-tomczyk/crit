// crit-comment-templates.js — template bar and saved-snippet CRUD.
// Vanilla JS, no module loader. Exports onto window.crit.commentTemplates.
//
// Shared with crit-web (vendored by scripts/sync-pierre.sh there), which
// passes its own storage and shortcut registry through configure().
//
// Storage keys (cookies in the CLI, localStorage in crit-web):
//   `crit-templates`     template texts (separate from crit-settings — user-defined, can be longer).
//   `crit-template-meta` title, emoji, shortcut, display and pin per template
//     text, plus the `seeded` flag. Kept apart so `crit-templates` keeps its
//     shape: localhost cookies are shared across ports, and builds without
//     these templates read it too.

(function () {
  'use strict';

  var ns = (window.crit = window.crit || {});

  // What the host supplies through configure(). The defaults are the CLI's:
  // cookies through window.crit.shared and key bindings from
  // window.crit.shortcuts, both looked up when used rather than at load.
  var env = {
    storage: {
      get: function (key) { return ns.shared ? ns.shared.getCookie(key) : null; },
      set: function (key, value) { if (ns.shared) ns.shared.setCookie(key, value); },
    },
    shortcuts: {
      eventToBinding: function (e) { return ns.shortcuts ? ns.shortcuts.eventToBinding(e) : ''; },
      // Why crit refuses `binding` as a template shortcut, or ''.
      refusal: function (binding) {
        var registry = ns.shortcuts;
        if (!registry) return '';
        if (registry.isReservedBinding(binding)) return binding + ' is reserved by Crit.';
        var taken = '';
        registry.groups.some(function (group) {
          return group.shortcuts.some(function (shortcut) {
            if (shortcut.modes.indexOf('code-review') === -1) return false;
            var current = shortcut.id ? registry.getBinding(shortcut.id) : shortcut.binding;
            if (current !== binding) return false;
            taken = shortcut.action;
            return true;
          });
        });
        return taken ? binding + ' already does: ' + taken + '.' : '';
      },
    },
    toast: function (message) {
      if (ns.shared && ns.shared.showToast) ns.shared.showToast(message, { kind: 'error' });
    },
  };

  // options: storage { get(key), set(key, value) } (string values),
  // shortcuts { eventToBinding(event), refusal(binding) }, toast(message).
  function configure(options) {
    if (options.storage) env.storage = options.storage;
    if (options.shortcuts) env.shortcuts = options.shortcuts;
    if (options.toast) env.toast = options.toast;
  }

  var META_KEY = 'crit-template-meta';
  // Pinned once on first use; a default the user removes or unpins stays so.
  var DEFAULT_TEMPLATES = [
    { text: 'Accepted', emoji: '👍', pinned: true, display: 'emoji' },
    { text: 'Rejected', emoji: '👎', pinned: true, display: 'emoji' },
  ];
  // What a template's toolbar button shows; `emoji` falls back to the text
  // when the template has no emoji.
  var DISPLAYS = [
    { id: 'emoji', name: 'Emoji' },
    { id: 'text', name: 'Text' },
    { id: 'both', name: 'Emoji + Text' },
  ];

  // Template bars of comment forms, held weakly: a closed form's bar goes
  // with it, while a form Pierre keeps aside (scrolled away) still updates.
  var bars = new Set();

  // Redraws every template bar after a change, so open forms follow without
  // a reload (the selection toolbar reads the templates each time it shows).
  function notifyChange() {
    bars.forEach(function (ref) {
      var bar = ref.deref();
      if (bar) bar._populate();
      else bars.delete(ref);
    });
  }

  // --- CRUD ---

  function getTemplates() {
    try {
      var raw = env.storage.get('crit-templates');
      if (raw) {
        var parsed = JSON.parse(raw);
        if (Array.isArray(parsed)) return parsed;
      }
    } catch (_) {}
    return [];
  }

  function saveTemplates(templates) {
    env.storage.set('crit-templates', JSON.stringify(templates));
  }

  function getMeta() {
    try {
      var raw = env.storage.get(META_KEY);
      if (raw) {
        var parsed = JSON.parse(raw);
        if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
          var items = parsed.items && typeof parsed.items === 'object' && !Array.isArray(parsed.items) ? parsed.items : {};
          return { seeded: !!parsed.seeded, items: items };
        }
      }
    } catch (_) {}
    return { seeded: false, items: {} };
  }

  // Writes the metadata of the texts still in `templates` only, so entries of
  // templates removed elsewhere (another build, the form's template bar) go.
  function saveMeta(meta, templates) {
    var items = {};
    templates.forEach(function (text) {
      if (Object.prototype.hasOwnProperty.call(meta.items, text)) items[text] = meta.items[text];
    });
    env.storage.set(META_KEY, JSON.stringify({ seeded: meta.seeded, items: items }));
  }

  function ensureSeeded() {
    var meta = getMeta();
    if (meta.seeded) return;
    var templates = getTemplates();
    DEFAULT_TEMPLATES.forEach(function (d) {
      if (templates.indexOf(d.text) === -1) templates.push(d.text);
      if (!Object.prototype.hasOwnProperty.call(meta.items, d.text)) {
        meta.items[d.text] = { emoji: d.emoji, pinned: d.pinned, display: d.display };
      }
    });
    meta.seeded = true;
    saveTemplates(templates);
    saveMeta(meta, templates);
  }

  function cleanEmoji(value) {
    return typeof value === 'string' ? value.trim() : '';
  }

  function cleanText(value) {
    return typeof value === 'string' ? value.trim() : '';
  }

  // The short name a template shows (toolbar button, chip, Settings list):
  // its title, or its text when it has none.
  function templateLabel(item) {
    return item.title || item.text;
  }

  function cleanDisplay(value) {
    return DISPLAYS.some(function (d) { return d.id === value; }) ? value : 'emoji';
  }

  // Templates with their metadata: [{ index, text, title, emoji, shortcut, display, pinned }],
  // in saved order; `index` is the position the CRUD functions take.
  function getTemplateItems() {
    ensureSeeded();
    var meta = getMeta();
    return getTemplates().map(function (text, index) {
      var entry = Object.prototype.hasOwnProperty.call(meta.items, text) ? meta.items[text] : {};
      return {
        index: index,
        text: text,
        title: cleanText(entry.title),
        emoji: cleanEmoji(entry.emoji),
        shortcut: typeof entry.shortcut === 'string' ? entry.shortcut : '',
        display: cleanDisplay(entry.display),
        pinned: !!entry.pinned,
      };
    });
  }

  function getPinnedTemplates() {
    return getTemplateItems().filter(function (item) { return item.pinned; });
  }

  // The template a key binding (shortcuts.eventToBinding) belongs to.
  function findByShortcut(binding) {
    if (!binding) return null;
    var items = getTemplateItems();
    for (var i = 0; i < items.length; i++) {
      if (items[i].shortcut === binding) return items[i];
    }
    return null;
  }

  // Why `binding` cannot be the shortcut of the template at `index` (-1 for a
  // new one), or '' when it can. A template shortcut must not hide a crit
  // shortcut of code review, a key crit reserves, a browser chord or another
  // template's shortcut.
  function shortcutConflict(binding, index) {
    if (!binding) return '';
    var parts = binding.split('+');
    if (parts.indexOf('Ctrl') !== -1 || parts.indexOf('Meta') !== -1) {
      return 'Ctrl and Cmd shortcuts belong to the browser.';
    }
    var refusal = env.shortcuts.refusal(binding);
    if (refusal) return refusal;
    var owner = findByShortcut(binding);
    if (owner && owner.index !== index) return binding + ' already comments "' + templateLabel(owner) + '".';
    return '';
  }

  function applyChanges(entry, changes) {
    if ('title' in changes) entry.title = cleanText(changes.title);
    if ('emoji' in changes) entry.emoji = cleanEmoji(changes.emoji);
    if ('shortcut' in changes) entry.shortcut = typeof changes.shortcut === 'string' ? changes.shortcut : '';
    if ('display' in changes) entry.display = cleanDisplay(changes.display);
    if ('pinned' in changes) entry.pinned = !!changes.pinned;
    return entry;
  }

  // Appends a template, with optional { title, emoji, shortcut, display, pinned }; returns
  // false for an empty text.
  function addTemplate(text, changes) {
    var value = typeof text === 'string' ? text.trim() : '';
    if (!value) return false;
    ensureSeeded();
    var templates = getTemplates();
    templates.push(value);
    var meta = getMeta();
    if (changes) meta.items[value] = applyChanges({}, changes);
    saveTemplates(templates);
    saveMeta(meta, templates);
    notifyChange();
    return true;
  }

  // Applies { text?, title?, emoji?, shortcut?, display?, pinned? } to the template at `index`;
  // a new text takes the template's metadata with it. Returns false when
  // nothing can be saved (bad index, empty text).
  function updateTemplate(index, changes) {
    var templates = getTemplates();
    if (index < 0 || index >= templates.length) return false;
    var meta = getMeta();
    var oldText = templates[index];
    var entry = Object.assign({}, Object.prototype.hasOwnProperty.call(meta.items, oldText) ? meta.items[oldText] : {});
    if (typeof changes.text === 'string') {
      var text = changes.text.trim();
      if (!text) return false;
      templates[index] = text;
    }
    meta.items[templates[index]] = applyChanges(entry, changes);
    saveTemplates(templates);
    saveMeta(meta, templates);
    notifyChange();
    return true;
  }

  // Moves the template at `from` to position `to`; metadata, keyed by text,
  // follows. The toolbar and the chips show templates in this order.
  function moveTemplate(from, to) {
    var templates = getTemplates();
    if (from === to || from < 0 || to < 0 || from >= templates.length || to >= templates.length) return false;
    templates.splice(to, 0, templates.splice(from, 1)[0]);
    saveTemplates(templates);
    notifyChange();
    return true;
  }

  function removeTemplate(index) {
    var templates = getTemplates();
    if (index < 0 || index >= templates.length) return;
    templates.splice(index, 1);
    saveTemplates(templates);
    saveMeta(getMeta(), templates);
    notifyChange();
  }

  // --- DOM ---

  /**
   * Build the template bar element.
   * @param {Object} opts
   * @param {function(string): void} opts.onInsert — called with template text when user picks one
   * @param {function(string): void} opts.onSaveNew — called with body text to save as a new template
   * @returns {HTMLElement} the bar element (caller inserts into DOM)
   */
  function buildTemplateBar(opts) {
    var onInsert = opts.onInsert;
    var onSaveNew = opts.onSaveNew;

    var bar = document.createElement('div');
    bar.className = 'comment-template-bar';

    function populate() {
      bar.innerHTML = '';
      var items = getTemplateItems();
      if (items.length === 0) {
        bar.style.display = 'none';
        return;
      }
      bar.style.display = '';
      // Pinned templates first, each group in saved order.
      items.filter(function (item) { return item.pinned; })
        .concat(items.filter(function (item) { return !item.pinned; }))
        .forEach(function (item) {
        var tmpl = item.text;
        var chip = document.createElement('button');
        chip.className = 'template-chip' + (item.pinned ? ' template-chip--pinned' : '');
        chip.title = item.pinned ? tmpl + '\n(pinned to the selection toolbar)' : tmpl;

        if (item.emoji) {
          var emoji = document.createElement('span');
          emoji.className = 'template-chip-emoji';
          emoji.setAttribute('aria-hidden', 'true');
          emoji.textContent = item.emoji;
          chip.appendChild(emoji);
        }

        var label = document.createElement('span');
        label.className = 'template-chip-label';
        label.textContent = templateLabel(item);
        chip.appendChild(label);

        var del = document.createElement('span');
        del.className = 'template-chip-delete';
        del.textContent = '×';
        del.title = 'Remove template';
        del.addEventListener('click', function (e) {
          e.preventDefault();
          e.stopPropagation();
          removeTemplate(item.index);
        });
        chip.appendChild(del);

        chip.addEventListener('click', function (e) {
          e.preventDefault();
          if (onInsert) onInsert(tmpl);
        });

        bar.appendChild(chip);
      });
    }

    populate();

    // Expose a refresh handle; notifyChange calls it after every change.
    bar._populate = populate;
    bars.add(new WeakRef(bar));

    // Wire up the onSaveNew callback so external "save template" buttons can
    // call bar._saveNew(body, { emoji, shortcut, pinned }) and the bar
    // refreshes automatically.
    bar._saveNew = function (body, changes) {
      if (!addTemplate(body, changes)) return;
      if (onSaveNew) onSaveNew(body);
    };

    return bar;
  }

  // --- Public API ---

  var api = {
    configure: configure,
    getTemplates: getTemplates,
    saveTemplates: saveTemplates,
    getTemplateItems: getTemplateItems,
    getPinnedTemplates: getPinnedTemplates,
    findByShortcut: findByShortcut,
    shortcutConflict: shortcutConflict,
    addTemplate: addTemplate,
    updateTemplate: updateTemplate,
    moveTemplate: moveTemplate,
    removeTemplate: removeTemplate,
    buildTemplateBar: buildTemplateBar,
    templateLabel: templateLabel,
  };

  ns.commentTemplates = api;

  // CommonJS dual-export for unit tests.
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
