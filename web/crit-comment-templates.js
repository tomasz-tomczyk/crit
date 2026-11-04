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
    // Why a template cannot be shown in the selection toolbar right now (the
    // toolbar or its templates are off in Settings), or '' when it can.
    toolbarBlocked: function () {
      var shared = ns.shared;
      if (!shared || !shared.getSetting) return '';
      if (shared.getSetting('selectionToolbarEnabled', true) === false) {
        return 'Turn on "Enable selection toolbar" in Settings > Selection toolbar first.';
      }
      if (shared.getSetting('selectionToolbarTemplates', true) === false) {
        return 'Turn off "Hide comment templates" in Settings > Selection toolbar > Advanced first.';
      }
      return '';
    },
  };

  // options: storage { get(key), set(key, value) } (string values),
  // shortcuts { eventToBinding(event), refusal(binding) }, toast(message),
  // toolbarBlocked() (why templates cannot show in the selection toolbar, or '').
  function configure(options) {
    if (options.storage) env.storage = options.storage;
    if (options.shortcuts) env.shortcuts = options.shortcuts;
    if (options.toast) env.toast = options.toast;
    if (options.toolbarBlocked) env.toolbarBlocked = options.toolbarBlocked;
  }

  var META_KEY = 'crit-template-meta';
  // Lucide "pencil" (ISC), trusted markup for the chip's edit button.
  var EDIT_ICON = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" ' +
    'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
    '<path d="M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z"/>' +
    '<path d="m15 5 4 4"/></svg>';
  // Pinned once on first use; a default the user removes, unpins or rebinds
  // stays so. Alt+P for "pro", Alt+C for "con".
  var DEFAULT_TEMPLATES = [
    { text: 'Accepted', emoji: '👍', pinned: true, display: 'emoji', shortcut: 'Alt+p' },
    { text: 'Rejected', emoji: '👎', pinned: true, display: 'emoji', shortcut: 'Alt+c' },
  ];
  // What a template's toolbar button shows; `emoji` falls back to the text
  // when the template has no emoji.
  var DISPLAYS = [
    { id: 'emoji', name: 'Icon' },
    { id: 'text', name: 'Text' },
    { id: 'both', name: 'Both' },
  ];

  // Template bars of comment forms, held weakly: a closed form's bar goes
  // with it, while a form Pierre keeps aside (scrolled away) still updates.
  var bars = new Set();
  // Hosts' change listeners (onChange): Settings counts, the Shortcuts list.
  var listeners = [];

  // Redraws every template bar after a change, so open forms follow without
  // a reload (the selection toolbar reads the templates each time it shows).
  function notifyChange() {
    bars.forEach(function (ref) {
      var bar = ref.deref();
      if (bar) bar._populate();
      else bars.delete(ref);
    });
    listeners.forEach(function (fn) { fn(); });
  }

  function onChange(fn) {
    if (listeners.indexOf(fn) === -1) listeners.push(fn);
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
        meta.items[d.text] = { emoji: d.emoji, pinned: d.pinned, display: d.display, shortcut: d.shortcut };
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
   * @param {function(Object): void} opts.onEdit — called with a template item to edit it
   * @returns {HTMLElement} the bar element (caller inserts into DOM)
   */
  function buildTemplateBar(opts) {
    var onInsert = opts.onInsert;
    var onSaveNew = opts.onSaveNew;
    var onEdit = opts.onEdit;

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

        if (onEdit) {
          var edit = document.createElement('span');
          edit.className = 'template-chip-edit';
          edit.innerHTML = EDIT_ICON;
          edit.title = 'Edit template';
          edit.addEventListener('click', function (e) {
            e.preventDefault();
            e.stopPropagation();
            onEdit(item);
          });
          chip.appendChild(edit);
        }

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

  // Emoji / Text / Emoji + Text pill for a template's toolbar button. Without
  // an emoji only Text can be chosen; the wanted choice comes back once an
  // emoji is set (setHasEmoji). Options: value, hasEmoji, onChange(id).
  function buildDisplayPicker(opts) {
    var wanted = cleanDisplay(opts.value);
    var hasEmoji = !!opts.hasEmoji;
    var value = hasEmoji ? wanted : 'text';
    var pill = document.createElement('div');
    pill.className = 'settings-pill template-display';
    pill.setAttribute('role', 'group');
    pill.setAttribute('aria-label', 'Toolbar button shows');
    var indicator = document.createElement('div');
    indicator.className = 'settings-pill-indicator';
    pill.appendChild(indicator);
    function render() {
      var at = 0;
      DISPLAYS.forEach(function (d, i) { if (d.id === value) at = i; });
      indicator.style.left = (at * 100 / DISPLAYS.length) + '%';
      pill.querySelectorAll('.settings-pill-btn').forEach(function (btn) {
        var on = btn.dataset.display === value;
        btn.classList.toggle('active', on);
        btn.setAttribute('aria-pressed', on ? 'true' : 'false');
        btn.disabled = !hasEmoji && btn.dataset.display !== 'text';
        btn.title = btn.disabled ? 'Set an emoji first' : '';
      });
    }
    DISPLAYS.forEach(function (d) {
      var btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'settings-pill-btn';
      btn.dataset.display = d.id;
      btn.textContent = d.name;
      btn.addEventListener('click', function () {
        value = d.id;
        wanted = d.id;
        render();
        if (opts.onChange) opts.onChange(value);
      });
      pill.appendChild(btn);
    });
    render();
    pill.getValue = function () { return value; };
    pill.setHasEmoji = function (next) {
      hasEmoji = !!next;
      value = hasEmoji ? wanted : 'text';
      render();
    };
    return pill;
  }

  // A button that records the next key chord as a template shortcut, like the
  // bindings of Settings > Shortcuts: Esc cancels, Backspace or Delete clears,
  // a refused chord says why in a toast. Options: value, index (the template's,
  // -1 for a new one), onChange(binding).
  function buildShortcutInput(opts) {
    var value = opts.value || '';
    var index = opts.index === undefined ? -1 : opts.index;
    var button = document.createElement('button');
    button.type = 'button';
    button.className = 'shortcut-binding template-shortcut';

    function render(capturing) {
      button.classList.toggle('is-capturing', capturing);
      button.setAttribute('aria-pressed', capturing ? 'true' : 'false');
      button.textContent = '';
      if (capturing) {
        var prompt = document.createElement('kbd');
        prompt.textContent = 'Press keys…';
        button.appendChild(prompt);
      } else if (value) {
        value.split('+').forEach(function (part, i) {
          if (i > 0) button.appendChild(document.createTextNode('+'));
          var kbd = document.createElement('kbd');
          kbd.textContent = part;
          button.appendChild(kbd);
        });
      } else {
        var none = document.createElement('span');
        none.className = 'shortcut-unassigned';
        none.textContent = 'No shortcut';
        button.appendChild(none);
      }
      button.title = capturing ? 'Press a key, Esc to cancel, Backspace to clear' : 'Set the shortcut that comments a selection with this template';
      button.setAttribute('aria-label', capturing ? 'Press new keys for the template shortcut' :
        (value ? 'Template shortcut ' + value + ', change' : 'Set a template shortcut'));
    }

    function commit(binding) {
      value = binding;
      render(false);
      if (opts.onChange) opts.onChange(binding);
    }

    button.addEventListener('click', function () { render(!button.classList.contains('is-capturing')); });
    button.addEventListener('blur', function () { render(false); });
    button.addEventListener('keydown', function (e) {
      if (!button.classList.contains('is-capturing') || e.key === 'Tab') return;
      e.preventDefault();
      e.stopPropagation();
      if (e.key === 'Escape') { render(false); return; }
      if (e.key === 'Backspace' || e.key === 'Delete') { commit(''); return; }
      var binding = env.shortcuts.eventToBinding(e);
      if (!binding) return; // a lone modifier: wait for the key
      var reason = shortcutConflict(binding, index);
      if (reason) {
        render(false);
        env.toast(reason);
        return;
      }
      commit(binding);
    });
    render(false);
    button.getValue = function () { return value; };
    return button;
  }

  // "Delete", which turns into "Delete this template? Keep / Delete" on
  // click: only the second, distinct button deletes, and Keep takes the focus
  // so a reflex Enter deletes nothing.
  function buildDeleteControl(onDelete) {
    var zone = document.createElement('div');
    zone.className = 'settings-template-delete-zone';
    function ask() {
      zone.textContent = '';
      var question = document.createElement('span');
      question.className = 'settings-template-delete-question';
      question.textContent = 'Delete this template?';
      var keep = document.createElement('button');
      keep.type = 'button';
      keep.className = 'btn btn-sm';
      keep.textContent = 'Keep';
      keep.addEventListener('click', function () { idle(); zone.querySelector('button').focus(); });
      var confirm = document.createElement('button');
      confirm.type = 'button';
      confirm.className = 'btn btn-sm btn-danger';
      confirm.textContent = 'Delete';
      confirm.addEventListener('click', onDelete);
      zone.appendChild(question);
      zone.appendChild(keep);
      zone.appendChild(confirm);
      keep.focus();
    }
    function idle() {
      zone.textContent = '';
      var start = document.createElement('button');
      start.type = 'button';
      start.className = 'settings-template-delete';
      start.textContent = 'Delete';
      start.addEventListener('click', ask);
      zone.appendChild(start);
    }
    idle();
    return zone;
  }

  function field(className, labelText, control) {
    var wrap = document.createElement('label');
    wrap.className = className;
    var name = document.createElement('span');
    name.className = 'settings-template-field-name';
    name.textContent = labelText;
    wrap.appendChild(name);
    wrap.appendChild(control);
    return wrap;
  }

  // A Settings switch (the comments panel's) around a checkbox.
  function buildSwitch(checked, ariaLabel) {
    var wrap = document.createElement('label');
    wrap.className = 'comments-panel-switch';
    var input = document.createElement('input');
    input.type = 'checkbox';
    input.checked = !!checked;
    input.setAttribute('aria-label', ariaLabel);
    var track = document.createElement('span');
    track.className = 'comments-panel-switch-track';
    var thumb = document.createElement('span');
    thumb.className = 'comments-panel-switch-thumb';
    track.appendChild(thumb);
    wrap.appendChild(input);
    wrap.appendChild(track);
    wrap.input = input;
    return wrap;
  }

  // A labelled line of the editor: name on the left, control on the right.
  function editorLine(labelText, control, className) {
    var line = document.createElement('div');
    line.className = 'settings-template-line' + (className ? ' ' + className : '');
    var name = document.createElement('span');
    name.className = 'settings-template-line-name';
    name.textContent = labelText;
    line.appendChild(name);
    line.appendChild(control);
    return line;
  }

  function appendKeys(parent, binding) {
    binding.split('+').forEach(function (part, i) {
      if (i > 0) parent.appendChild(document.createTextNode('+'));
      var kbd = document.createElement('kbd');
      kbd.textContent = part;
      parent.appendChild(kbd);
    });
  }

  // The editor a row unfolds into, or a new template's (item null, starting
  // from `text`): emoji, name, the text it posts, its toolbar button and its
  // key, then Delete (existing templates), Cancel and Save. Ctrl/Cmd+Enter
  // saves; Esc cancels without closing Settings. done(savedIndex) runs after a
  // save, a delete or a cancel (savedIndex is -1 unless saved).
  function buildEditor(item, text, done) {
    var card = document.createElement('div');
    card.className = 'settings-template-editor';
    card.setAttribute('role', 'group');
    card.setAttribute('aria-label', item ? 'Edit "' + templateLabel(item) + '"' : 'New template');

    var head = document.createElement('div');
    head.className = 'settings-template-editor-head';
    head.textContent = item ? (item.emoji ? item.emoji + ' ' : '') + templateLabel(item) : 'New template';
    card.appendChild(head);

    var identity = document.createElement('div');
    identity.className = 'settings-template-identity';
    var emojiInput = document.createElement('input');
    emojiInput.type = 'text';
    emojiInput.className = 'settings-template-emoji-input';
    emojiInput.maxLength = 16;
    emojiInput.spellcheck = false;
    emojiInput.setAttribute('autocomplete', 'off');
    emojiInput.value = item ? item.emoji : '';
    identity.appendChild(field('settings-template-field settings-template-field--emoji', 'Emoji', emojiInput));
    var nameInput = document.createElement('input');
    nameInput.type = 'text';
    nameInput.className = 'settings-template-name-input';
    nameInput.maxLength = 60;
    nameInput.setAttribute('autocomplete', 'off');
    nameInput.value = item ? item.title : '';
    identity.appendChild(field('settings-template-field settings-template-field--name', 'Name', nameInput));
    card.appendChild(identity);
    var note = document.createElement('p');
    note.className = 'settings-template-note';
    note.textContent = 'Optional name. Without one, the comment text shows on the toolbar button and the form chip.';
    card.appendChild(note);

    var textInput = document.createElement('textarea');
    textInput.className = 'settings-template-text-input';
    textInput.rows = 3;
    textInput.value = item ? item.text : (text || '');
    card.appendChild(field('settings-template-field', 'Posts as comment', textInput));

    // A Hide switch, off by default, like "Hide resolved comments": checked
    // means the template stays out of the toolbar (not pinned).
    var hide = buildSwitch(item ? !item.pinned : false, 'Hide from the selection toolbar');
    var pinLine = editorLine('Hide from selection toolbar', hide);
    // Off while the toolbar or its templates are off: the switch keeps its
    // value, and its tooltip says what to turn on.
    var blocked = env.toolbarBlocked();
    if (blocked) {
      hide.input.disabled = true;
      pinLine.classList.add('is-disabled');
      pinLine.title = blocked;
      hide.input.setAttribute('aria-description', blocked);
    }
    card.appendChild(pinLine);
    // What the toolbar button shows only matters when it shows: kept while hidden.
    var displayPicker = buildDisplayPicker({ value: item ? item.display : 'emoji', hasEmoji: !!emojiInput.value.trim() });
    emojiInput.addEventListener('input', function () { displayPicker.setHasEmoji(!!emojiInput.value.trim()); });
    var displayLine = editorLine('Button shows', displayPicker);
    displayLine.hidden = hide.input.checked;
    hide.input.addEventListener('change', function () { displayLine.hidden = hide.input.checked; });
    card.appendChild(displayLine);
    var shortcutInput = buildShortcutInput({ value: item ? item.shortcut : '', index: item ? item.index : -1 });
    card.appendChild(editorLine('Shortcut', shortcutInput));

    var actions = document.createElement('div');
    actions.className = 'settings-template-actions';
    if (item) {
      actions.appendChild(buildDeleteControl(function () {
        removeTemplate(item.index);
        done(-1);
      }));
    }
    var right = document.createElement('div');
    right.className = 'settings-template-actions-end';
    var cancelBtn = document.createElement('button');
    cancelBtn.type = 'button';
    cancelBtn.className = 'btn btn-sm';
    cancelBtn.textContent = 'Cancel';
    var saveBtn = document.createElement('button');
    saveBtn.type = 'button';
    saveBtn.className = 'btn btn-sm btn-primary';
    saveBtn.textContent = 'Save';
    right.appendChild(cancelBtn);
    right.appendChild(saveBtn);
    actions.appendChild(right);
    card.appendChild(actions);

    function save() {
      var value = textInput.value.trim();
      if (!value) { textInput.focus(); return; }
      var changes = {
        title: nameInput.value,
        emoji: emojiInput.value,
        shortcut: shortcutInput.getValue(),
        display: displayPicker.getValue(),
        pinned: !hide.input.checked,
      };
      var saved = item ? updateTemplate(item.index, Object.assign({ text: value }, changes)) : addTemplate(value, changes);
      if (!saved) return;
      done(item ? item.index : getTemplates().length - 1);
    }
    cancelBtn.addEventListener('click', function () { done(-1); });
    saveBtn.addEventListener('click', save);
    card.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        e.stopPropagation();
        save();
      } else if (e.key === '?' && (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA')) {
        // Typed text: Settings' "?" jumps to the Shortcuts tab otherwise.
        e.stopPropagation();
      } else if (e.key === 'Escape' && !e.defaultPrevented) {
        e.preventDefault();
        e.stopPropagation();
        done(-1);
      }
    });
    card.focusFirst = function () {
      if (item) nameInput.focus();
      else { textInput.focus(); textInput.select(); }
    };
    return card;
  }

  // Lucide "pencil" (ISC) at the Settings list size.
  var LIST_EDIT_ICON = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" ' +
    'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
    '<path d="M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z"/>' +
    '<path d="m15 5 4 4"/></svg>';

  // Lucide "grip-vertical" (ISC), the drag handle of a template row.
  var GRIP_ICON = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" ' +
    'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="9" cy="12" r="1"/><circle cx="9" cy="5" r="1"/>' +
    '<circle cx="9" cy="19" r="1"/><circle cx="15" cy="12" r="1"/><circle cx="15" cy="5" r="1"/><circle cx="15" cy="19" r="1"/></svg>';

  // The Comment templates tab: one row per template (handle, emoji, name and
  // text, key, edit), then "+ Add template". The
  // pencil unfolds a row into its editor, one at a time; "+ Add template"
  // unfolds an empty one at the end. The handle reorders: drag it onto
  // another row, or press Up / Down on it.
  // Options: expand (an index, or 'new'), text (what a new template starts
  // with), focusHandle / focusEdit (index of the control to focus).
  function renderSettingsList(container, options) {
    if (!container) return;
    var o = options || {};
    var expand = o.expand === undefined ? null : o.expand;
    function redraw(next) { renderSettingsList(container, next); }
    var dragFrom = -1;
    function clearDropMarks() {
      container.querySelectorAll('.is-drop-before, .is-drop-after').forEach(function (el) {
        el.classList.remove('is-drop-before', 'is-drop-after');
      });
    }
    function dropsAfter(row, e) {
      var rect = row.getBoundingClientRect();
      return e.clientY > rect.top + rect.height / 2;
    }
    function closeEditor(savedIndex) {
      redraw({ focusEdit: savedIndex >= 0 ? savedIndex : (typeof expand === 'number' ? expand : undefined), focusAdd: expand === 'new' && savedIndex < 0 });
    }
    container.textContent = '';
    var items = getTemplateItems();
    var editor = null;
    items.forEach(function (item) {
      if (expand === item.index) {
        editor = buildEditor(item, '', closeEditor);
        container.appendChild(editor);
        return;
      }
      var label = templateLabel(item);
      var row = document.createElement('div');
      row.className = 'settings-template-row';

      // A span, not a <button>: Firefox does not start a drag from a button.
      var handle = document.createElement('span');
      handle.className = 'settings-template-handle';
      handle.setAttribute('role', 'button');
      handle.tabIndex = 0;
      handle.innerHTML = GRIP_ICON;
      handle.title = 'Drag to reorder (or Up / Down)';
      handle.setAttribute('aria-label', 'Move "' + label + '", ' + (item.index + 1) + ' of ' + items.length);
      // Only a drag from the handle moves a row: a click elsewhere stays a click.
      handle.addEventListener('pointerdown', function () { row.draggable = true; });
      handle.addEventListener('pointerup', function () { row.draggable = false; });
      handle.addEventListener('keydown', function (e) {
        var to = e.key === 'ArrowUp' ? item.index - 1 : e.key === 'ArrowDown' ? item.index + 1 : -1;
        if (to < 0 || to >= items.length) return;
        e.preventDefault();
        e.stopPropagation();
        moveTemplate(item.index, to);
        redraw({ focusHandle: to });
      });
      row.appendChild(handle);
      row.addEventListener('dragstart', function (e) {
        if (!row.draggable) { e.preventDefault(); return; }
        dragFrom = item.index;
        e.dataTransfer.effectAllowed = 'move';
        e.dataTransfer.setData('text/plain', String(item.index));
        row.classList.add('is-dragging');
      });
      row.addEventListener('dragend', function () {
        row.draggable = false;
        row.classList.remove('is-dragging');
        clearDropMarks();
        dragFrom = -1;
      });
      row.addEventListener('dragover', function (e) {
        if (dragFrom < 0) return;
        e.preventDefault();
        e.dataTransfer.dropEffect = 'move';
        clearDropMarks();
        row.classList.add(dropsAfter(row, e) ? 'is-drop-after' : 'is-drop-before');
      });
      row.addEventListener('drop', function (e) {
        if (dragFrom < 0) return;
        e.preventDefault();
        var to = item.index + (dropsAfter(row, e) ? 1 : 0);
        if (dragFrom < to) to -= 1;
        moveTemplate(dragFrom, to);
        redraw({ expand: expand === 'new' ? 'new' : undefined });
      });

      var emoji = document.createElement('span');
      emoji.className = 'settings-template-emoji';
      emoji.setAttribute('aria-hidden', 'true');
      emoji.textContent = item.emoji;
      row.appendChild(emoji);

      var text = document.createElement('span');
      text.className = 'settings-template-label';
      text.title = item.text;
      var name = document.createElement('span');
      name.className = 'settings-template-name';
      name.textContent = label;
      text.appendChild(name);
      if (item.title) {
        var body = document.createElement('span');
        body.className = 'settings-template-body';
        body.textContent = item.text;
        text.appendChild(body);
      }
      row.appendChild(text);

      if (item.shortcut) {
        var keys = document.createElement('span');
        keys.className = 'template-shortcut';
        appendKeys(keys, item.shortcut);
        row.appendChild(keys);
      }

      var edit = document.createElement('button');
      edit.type = 'button';
      edit.className = 'settings-template-action';
      edit.innerHTML = LIST_EDIT_ICON;
      edit.title = 'Edit template';
      edit.setAttribute('aria-label', 'Edit "' + label + '"');
      edit.addEventListener('click', function () { redraw({ expand: item.index }); });
      row.appendChild(edit);
      container.appendChild(row);
    });

    if (expand === 'new') {
      editor = buildEditor(null, o.text, closeEditor);
      container.appendChild(editor);
    }

    var add = document.createElement('button');
    add.type = 'button';
    add.className = 'settings-template-new';
    add.textContent = '+ Add template';
    add.hidden = expand === 'new';
    add.addEventListener('click', function () { redraw({ expand: 'new' }); });
    container.appendChild(add);

    if (editor) {
      editor.scrollIntoView({ block: 'nearest' });
      editor.focusFirst();
    } else if (o.focusHandle !== undefined) {
      var moved = container.querySelectorAll('.settings-template-handle')[o.focusHandle];
      if (moved) moved.focus();
    } else if (o.focusEdit !== undefined) {
      var rows = container.querySelectorAll('.settings-template-row');
      var target = rows[Math.min(o.focusEdit, rows.length - 1)];
      var btn = target && target.querySelector('.settings-template-action');
      if (btn) btn.focus();
    } else if (o.focusAdd) {
      add.focus();
    }
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
    buildShortcutInput: buildShortcutInput,
    buildDisplayPicker: buildDisplayPicker,
    onChange: onChange,
    templateLabel: templateLabel,
    renderSettingsList: renderSettingsList,
  };

  ns.commentTemplates = api;

  // CommonJS dual-export for unit tests.
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
