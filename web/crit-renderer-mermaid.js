// crit-renderer-mermaid.js — built-in Mermaid renderer (issue #989).
// Two registrations with window.crit.renderers: a fence renderer for
// ```mermaid (comments attach to the whole fence) and a file renderer for
// .mermaid / .mmd (the extensions recommended by mermaid.js.org), where a
// click on a node comments on the line that defines it.
// Dependencies: window.crit.renderers. mermaid.min.js (window.mermaid) is
// not part of the page: the first render injects it, so reviews without a
// diagram never download it.
(function () {
  'use strict';

  // Follows the Crit palette when one is active, else Mermaid's own themes.
  // theme is ctx.theme ('light' | 'dark').
  function options(theme) {
    var base = { startOnLoad: false, securityLevel: 'strict', suppressErrorRendering: true };
    if (!document.documentElement.dataset.critPalette) return Object.assign(base, { theme: theme === 'light' ? 'default' : 'dark' });
    var css = getComputedStyle(document.documentElement);
    var role = function (name) { return css.getPropertyValue('--crit-palette-' + name).trim(); };
    return Object.assign(base, { theme: 'base', themeVariables: {
      darkMode: theme === 'dark', background: role('bg'),
      primaryColor: role('surface'), primaryTextColor: role('fg'), primaryBorderColor: role('border'),
      secondaryColor: role('elevated'), tertiaryColor: role('bg'), lineColor: role('muted'),
      textColor: role('fg'), mainBkg: role('surface'), nodeBorder: role('border'),
      edgeLabelBackground: role('bg'), fontFamily: css.getPropertyValue('--crit-font-body').trim(),
    } });
  }

  // One shared load of mermaid.min.js, started by the first render. A failed
  // load rejects (the target shows its source and the error) and is retried
  // by the next render.
  var loading = null;

  function loadMermaid() {
    var ready = window.mermaid && typeof window.mermaid.render === 'function';
    if (ready) return Promise.resolve(window.mermaid);
    if (!loading) {
      loading = new Promise(function (resolve, reject) {
        var s = document.createElement('script');
        s.src = 'mermaid.min.js';
        s.onload = function () {
          if (window.mermaid && typeof window.mermaid.render === 'function') resolve(window.mermaid);
          else { loading = null; reject(new Error('Mermaid is not loaded')); }
        };
        s.onerror = function () {
          loading = null;
          s.remove();
          reject(new Error('Could not load mermaid.min.js'));
        };
        document.head.appendChild(s);
      });
    }
    return loading;
  }

  // mermaid.render shares global state (config, a scratch element), so
  // renders run one at a time.
  var queue = Promise.resolve();
  var seq = 0;

  function render(source, ctx) {
    var id = 'crit-mermaid-' + (++seq);
    var run = queue.then(loadMermaid).then(function (m) {
      m.initialize(options(ctx.theme));
      return m.render(id, source);
    });
    queue = run.catch(function () { /* next render starts clean */ });
    return run.then(function (res) {
      ctx.container.innerHTML = res.svg;
      if (typeof res.bindFunctions === 'function') res.bindFunctions(ctx.container);
    }, function (err) {
      // A failed render can leave its scratch node behind.
      var stray = document.getElementById('d' + id);
      if (stray) stray.remove();
      throw err;
    });
  }

  function escapeRegExp(s) {
    return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  }

  // Ids Mermaid gives SVG elements: flowchart-A-0, classId-Foo-1, state-Idle-2
  // (optionally prefixed with the diagram id).
  var ELEMENT_ID = /(?:^|-)(?:flowchart|classId|state)-(.+)-\d+$/;

  // Candidates from the clicked element up to the diagram root: diagram ids
  // first (exact), label text second.
  function candidates(el, stop) {
    var ids = [];
    var texts = [];
    for (var n = el; n && n !== stop && n.nodeType === 1; n = n.parentNode) {
      var dataId = n.getAttribute('data-id');
      if (dataId) ids.push(dataId);
      var m = ELEMENT_ID.exec(n.getAttribute('id') || '');
      if (m) ids.push(m[1]);
      var text = (n.textContent || '').trim();
      if (text && text.length <= 200 && texts.indexOf(text) === -1) texts.push(text);
      if (n.tagName && n.tagName.toLowerCase() === 'svg') break;
    }
    return { ids: ids, texts: texts };
  }

  function lineMatching(lines, re) {
    for (var j = 0; j < lines.length; j++) {
      if (re.test(lines[j])) return { startLine: j + 1, endLine: j + 1 };
    }
    return null;
  }

  // Map a clicked diagram element to the file line that defines it: the line
  // where its id opens a shape (B[...], B(...), B{...}, B>...], B@{...}),
  // else the first line naming the id as a whole word, else the first line
  // holding its label. The source is the whole file, so its lines are file
  // lines.
  function anchorFor(el, ctx) {
    var lines = String(ctx.source || '').split('\n');
    var c = candidates(el, ctx.container);
    for (var i = 0; i < c.ids.length; i++) {
      var id = '(^|[^\\w-])' + escapeRegExp(c.ids[i]);
      var found = lineMatching(lines, new RegExp(id + '\\s*(\\[|\\(|\\{|>|@\\{)')) ||
        lineMatching(lines, new RegExp(id + '($|[^\\w-])'));
      if (found) return found;
    }
    for (var k = 0; k < c.texts.length; k++) {
      for (var l = 0; l < lines.length; l++) {
        if (lines[l].indexOf(c.texts[k]) !== -1) return { startLine: l + 1, endLine: l + 1 };
      }
    }
    return null;
  }

  var fence = {
    kind: 'fence',
    name: 'mermaid',
    langs: ['mermaid'],
    render: render,
    zoomable: true,
  };

  var file = {
    kind: 'file',
    name: 'mermaid',
    label: 'Diagram',
    paths: ['*.mermaid', '*.mmd'],
    render: render,
    anchorFor: anchorFor,
    zoomable: true,
  };

  if (typeof window !== 'undefined' && window.crit && window.crit.renderers) {
    window.crit.renderers.register(fence);
    window.crit.renderers.register(file);
  }
  if (typeof module === 'object' && module.exports) {
    module.exports = { fence: fence, file: file, anchorFor: anchorFor };
  }
})();
