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
  // Lucide "pencil" (ISC), trusted markup for the chip's edit button.
  var EDIT_ICON = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" ' +
    'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
    '<path d="M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z"/>' +
    '<path d="m15 5 4 4"/></svg>';
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

  // "Delete template", which turns into "Delete this template? Keep / Delete"
  // on click: only the second, distinct button deletes, and Keep takes the
  // focus so a reflex Enter deletes nothing.
  function buildDeleteControl(onDelete) {
    var zone = document.createElement('div');
    zone.className = 'save-template-delete-zone';
    function ask() {
      zone.textContent = '';
      var question = document.createElement('span');
      question.className = 'save-template-delete-question';
      question.textContent = 'Delete this template?';
      var keep = document.createElement('button');
      keep.type = 'button';
      keep.className = 'btn btn-sm';
      keep.textContent = 'Keep';
      keep.addEventListener('click', idle);
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
      start.className = 'btn btn-sm save-template-delete';
      start.textContent = 'Delete template';
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
    name.className = 'save-template-field-name';
    name.textContent = labelText;
    wrap.appendChild(name);
    wrap.appendChild(control);
    return wrap;
  }

  // The one dialog that creates a template ("Save as template", from
  // opts.text) or edits opts.item ("Edit template"): title, text, emoji,
  // shortcut, what its toolbar button shows, and its pin; editing also offers
  // "Delete template", confirmed in the dialog before anything is removed.
  // Ctrl/Cmd+Enter saves, Esc cancels without closing what lies under the
  // dialog (Settings). Options: item, text, onSaved() (after a save or a
  // delete), onClose() (after save, delete or cancel).
  function openTemplateDialog(opts) {
    var item = opts.item || null;
    var overlay = document.createElement('div');
    overlay.className = 'save-template-overlay active';
    var dialog = document.createElement('div');
    dialog.className = 'save-template-dialog';
    dialog.setAttribute('role', 'dialog');
    dialog.setAttribute('aria-modal', 'true');
    dialog.setAttribute('aria-label', item ? 'Edit template' : 'Save as template');

    var heading = document.createElement('h3');
    heading.textContent = item ? 'Edit template' : 'Save as template';
    dialog.appendChild(heading);

    var titleInput = document.createElement('input');
    titleInput.type = 'text';
    titleInput.className = 'save-template-title';
    titleInput.maxLength = 60;
    titleInput.placeholder = 'Short name for the toolbar';
    titleInput.setAttribute('autocomplete', 'off');
    titleInput.value = item ? item.title : '';
    dialog.appendChild(field('save-template-field', 'Title', titleInput));
    var note = document.createElement('p');
    note.className = 'save-template-note';
    note.textContent = 'Optional. Shown in the selection toolbar and on the template chip; without a title, the template text is used.';
    dialog.appendChild(note);

    var input = document.createElement('textarea');
    input.className = 'save-template-input';
    input.rows = 3;
    input.value = item ? item.text : (opts.text || '');
    dialog.appendChild(field('save-template-field', 'Template text', input));

    var options = document.createElement('div');
    options.className = 'save-template-options';
    var emojiInput = document.createElement('input');
    emojiInput.type = 'text';
    emojiInput.className = 'save-template-emoji';
    emojiInput.maxLength = 16;
    emojiInput.spellcheck = false;
    emojiInput.setAttribute('autocomplete', 'off');
    emojiInput.value = item ? item.emoji : '';
    options.appendChild(field('save-template-option', 'Emoji', emojiInput));
    var shortcutOption = document.createElement('span');
    shortcutOption.className = 'save-template-option';
    shortcutOption.appendChild(document.createTextNode('Shortcut'));
    var shortcutInput = buildShortcutInput({ value: item ? item.shortcut : '', index: item ? item.index : -1 });
    shortcutOption.appendChild(shortcutInput);
    options.appendChild(shortcutOption);
    var pinLabel = document.createElement('label');
    pinLabel.className = 'save-template-option save-template-option--row';
    var pinInput = document.createElement('input');
    pinInput.type = 'checkbox';
    pinInput.checked = !!(item && item.pinned);
    pinLabel.appendChild(pinInput);
    pinLabel.appendChild(document.createTextNode('Pin to the selection toolbar'));
    options.appendChild(pinLabel);
    // What the toolbar button shows only matters for a pinned template: shown
    // under the pin, while it is checked. The choice is kept while hidden.
    var displayOption = document.createElement('span');
    displayOption.className = 'save-template-option save-template-option--row save-template-option--sub';
    displayOption.appendChild(document.createTextNode('Toolbar button'));
    var displayPicker = buildDisplayPicker({ value: item ? item.display : 'emoji', hasEmoji: !!emojiInput.value.trim() });
    emojiInput.addEventListener('input', function () { displayPicker.setHasEmoji(!!emojiInput.value.trim()); });
    displayOption.appendChild(displayPicker);
    displayOption.hidden = !pinInput.checked;
    pinInput.addEventListener('change', function () { displayOption.hidden = !pinInput.checked; });
    options.appendChild(displayOption);
    dialog.appendChild(options);

    var actions = document.createElement('div');
    actions.className = 'save-template-actions';
    var cancelBtn = document.createElement('button');
    cancelBtn.type = 'button';
    cancelBtn.className = 'btn btn-sm';
    cancelBtn.textContent = 'Cancel';
    var saveBtn = document.createElement('button');
    saveBtn.type = 'button';
    saveBtn.className = 'btn btn-sm btn-primary';
    saveBtn.textContent = 'Save';
    if (item) actions.appendChild(buildDeleteControl(function () {
      removeTemplate(item.index);
      overlay.remove();
      if (opts.onSaved) opts.onSaved();
      if (opts.onClose) opts.onClose();
    }));
    actions.appendChild(cancelBtn);
    actions.appendChild(saveBtn);
    dialog.appendChild(actions);

    function close() {
      overlay.remove();
      if (opts.onClose) opts.onClose();
    }
    function save() {
      var text = input.value.trim();
      if (!text) { input.focus(); return; }
      var changes = {
        title: titleInput.value,
        emoji: emojiInput.value,
        shortcut: shortcutInput.getValue(),
        display: displayPicker.getValue(),
        pinned: pinInput.checked,
      };
      var saved = item ? updateTemplate(item.index, Object.assign({ text: text }, changes)) : addTemplate(text, changes);
      if (!saved) return;
      overlay.remove();
      if (opts.onSaved) opts.onSaved();
      if (opts.onClose) opts.onClose();
    }
    cancelBtn.addEventListener('click', close);
    saveBtn.addEventListener('click', save);
    dialog.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        e.stopPropagation();
        save();
      } else if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        close();
      }
    });
    overlay.addEventListener('click', function (e) { if (e.target === overlay) close(); });
    overlay.appendChild(dialog);
    document.body.appendChild(overlay);
    requestAnimationFrame(function () {
      input.focus();
      if (!item) input.select();
    });
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

  // One summary row per comment template (handle, emoji, title or text,
  // shortcut, pin mark, edit), then "New template": the Comment templates
  // section of both settings panels. Editing, deleting and creating go
  // through the template dialog.
  // The handle reorders: drag it onto another row, or press Up / Down on it.
  function renderSettingsList(container, focusHandle) {
    if (!container) return;
    function redraw() { renderSettingsList(container); }
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
    container.textContent = '';
    var items = getTemplateItems();
    items.forEach(function (item) {
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
        renderSettingsList(container, to);
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
        redraw();
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
        item.shortcut.split('+').forEach(function (part, i) {
          if (i > 0) keys.appendChild(document.createTextNode('+'));
          var kbd = document.createElement('kbd');
          kbd.textContent = part;
          keys.appendChild(kbd);
        });
        row.appendChild(keys);
      }
      if (item.pinned) {
        var pinned = document.createElement('span');
        pinned.className = 'settings-template-pinned';
        pinned.textContent = 'Pinned';
        row.appendChild(pinned);
      }

      var edit = document.createElement('button');
      edit.type = 'button';
      edit.className = 'settings-template-action';
      edit.innerHTML = LIST_EDIT_ICON;
      edit.title = 'Edit template';
      edit.setAttribute('aria-label', 'Edit "' + label + '"');
      edit.addEventListener('click', function () {
        openTemplateDialog({ item: item, onSaved: redraw, onClose: function () { edit.focus(); } });
      });
      row.appendChild(edit);
      container.appendChild(row);
    });

    if (focusHandle !== undefined) {
      var moved = container.querySelectorAll('.settings-template-handle')[focusHandle];
      if (moved) moved.focus();
    }

    var add = document.createElement('button');
    add.type = 'button';
    add.className = 'btn btn-sm settings-template-new';
    add.textContent = '+ New template';
    add.addEventListener('click', function () {
      openTemplateDialog({ onSaved: redraw, onClose: function () { add.focus(); } });
    });
    container.appendChild(add);

    var hint = document.createElement('p');
    hint.className = 'settings-template-hint';
    hint.textContent = 'Pinned templates show in the selection toolbar: one click comments the selection with the template text.';
    container.appendChild(hint);
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
    openTemplateDialog: openTemplateDialog,
    templateLabel: templateLabel,
    renderSettingsList: renderSettingsList,
  };

  ns.commentTemplates = api;

  // CommonJS dual-export for unit tests.
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
