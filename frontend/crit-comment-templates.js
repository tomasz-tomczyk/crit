// crit-comment-templates.js — template bar and saved-snippet CRUD.
// Vanilla JS, no module loader. Exports onto window.crit.commentTemplates.
//
// Depends on: window.crit.shared.getCookie, window.crit.shared.setCookie
// Cookie: `crit-templates` (separate from crit-settings — user-defined, can be longer).

(function () {
  'use strict';

  var ns = (window.crit = window.crit || {});
  var shared = ns.shared;

  // --- CRUD ---

  function getTemplates() {
    try {
      var raw = shared.getCookie('crit-templates');
      if (raw) {
        var parsed = JSON.parse(raw);
        if (Array.isArray(parsed)) return parsed;
      }
    } catch (_) {}
    return [];
  }

  function saveTemplates(templates) {
    shared.setCookie('crit-templates', JSON.stringify(templates));
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
      var templates = getTemplates();
      if (templates.length === 0) {
        bar.style.display = 'none';
        return;
      }
      bar.style.display = '';
      templates.forEach(function (tmpl, i) {
        var chip = document.createElement('button');
        chip.className = 'template-chip';
        chip.title = tmpl;

        var label = document.createElement('span');
        label.className = 'template-chip-label';
        label.textContent = tmpl;
        chip.appendChild(label);

        var del = document.createElement('span');
        del.className = 'template-chip-delete';
        del.textContent = '×';
        del.title = 'Remove template';
        del.addEventListener('click', function (e) {
          e.preventDefault();
          e.stopPropagation();
          var t = getTemplates();
          t.splice(i, 1);
          saveTemplates(t);
          populate();
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

    // Expose a refresh handle so callers can re-populate after saving.
    bar._populate = populate;

    // Wire up the onSaveNew callback so external "save template" buttons can
    // call bar._saveNew(body) and the bar refreshes automatically.
    bar._saveNew = function (body) {
      if (!body) return;
      var t = getTemplates();
      t.push(body);
      saveTemplates(t);
      populate();
      if (onSaveNew) onSaveNew(body);
    };

    return bar;
  }

  // --- Public API ---

  var api = {
    getTemplates: getTemplates,
    saveTemplates: saveTemplates,
    buildTemplateBar: buildTemplateBar,
  };

  ns.commentTemplates = api;

  // CommonJS dual-export for unit tests.
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
