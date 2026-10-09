// crit-renderers.js — pluggable content renderers (issue #989).
// Dependencies: window.crit.shared.escapeHTML, window.crit.globMatch (file
// patterns), window.crit.diagramOverlay (optional, fullscreen view of SVG output).
//
// A renderer turns source text into a visual view. Two kinds, registered
// separately:
//   - fence renderers, picked by a markdown code-fence info string
//     (```mermaid), render inside markdown documents, story text and
//     comments shown in Document view (comments on diff lines may show code);
//   - file renderers, picked by a path glob (*.mmd), give a whole file a
//     "<label> / Source" toggle, like Document / Diff for markdown.
//
//   window.crit.renderers.register({
//     kind: 'fence',                   // 'fence' | 'file'
//     name: 'mermaid',                 // unique per kind; re-registering replaces
//     langs: ['mermaid'],              // fence: info strings
//     render(source, ctx) {},          // fill ctx.container, or return an
//                                      // HTML string / Node (or a Promise)
//     zoomable: true,                  // optional: fullscreen pan/zoom of <svg> output
//   });
//
//   window.crit.renderers.register({
//     kind: 'file',
//     name: 'mermaid',
//     label: 'Diagram',                // toggle label (default "Rendered")
//     paths: ['*.mmd', '**/*.mermaid'],// file: path globs
//     render(source, ctx) {},
//     anchorFor(element, ctx) {},      // optional: rendered element → file
//                                      // lines {startLine, endLine, quote?}
//                                      // (1-based from the start of the
//                                      // file), or null
//     zoomable: true,
//   });
//
// ctx: { container, source, kind: 'fence'|'file', path, startLine, endLine,
// theme: 'light'|'dark', renderer, renderAs(name, source) }. startLine/endLine
// are the source's lines in the file (0 when the source is not a file, e.g. a
// fence inside a comment). renderAs hands derived source to the fence
// renderer `name` (same container), so a file renderer can compose another.
// Fence comments attach to the whole fence through the gutter.
// A renderer that throws or rejects leaves the source visible with the error;
// the rest of the review is unaffected. The contract is experimental.
(function () {
  'use strict';

  var escapeHtml = (typeof window !== 'undefined' && window.crit && window.crit.shared)
    ? window.crit.shared.escapeHTML
    : function (s) { return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;'); };

  var registry = [];
  var clickInstalled = false;

  function register(def) {
    if (!def || typeof def.name !== 'string' || !def.name) throw new Error('renderer needs a name');
    if (def.kind !== 'fence' && def.kind !== 'file') throw new Error('renderer ' + def.name + ' needs kind \'fence\' or \'file\'');
    if (typeof def.render !== 'function') throw new Error('renderer ' + def.name + ' needs render()');
    var entry = {
      kind: def.kind,
      name: def.name,
      label: typeof def.label === 'string' && def.label ? def.label : 'Rendered',
      langs: def.kind === 'fence' ? (def.langs || []).map(function (f) { return String(f).toLowerCase(); }) : [],
      paths: def.kind === 'file' ? (def.paths || []).map(String) : [],
      render: def.render,
      anchorFor: def.kind === 'file' && typeof def.anchorFor === 'function' ? def.anchorFor : null,
      zoomable: !!def.zoomable,
    };
    unregister(entry.kind, entry.name);
    registry.push(entry);
    return entry;
  }

  function unregister(kind, name) {
    registry = registry.filter(function (r) { return r.kind !== kind || r.name !== name; });
  }

  function list() { return registry.slice(); }

  function byName(kind, name) {
    for (var i = 0; i < registry.length; i++) {
      if (registry[i].kind === kind && registry[i].name === name) return registry[i];
    }
    return null;
  }

  // Last registration wins, so a later renderer can override an earlier one.
  function forFence(lang) {
    var l = String(lang || '').toLowerCase();
    if (!l) return null;
    for (var i = registry.length - 1; i >= 0; i--) {
      if (registry[i].langs.indexOf(l) !== -1) return registry[i];
    }
    return null;
  }

  // crit-glob-match's rules plus a leading "**/" meaning "at any depth".
  function matchFile(path, pattern) {
    var gm = typeof window !== 'undefined' && window.crit && window.crit.globMatch;
    if (!gm) gm = typeof require === 'function' ? require('./crit-glob-match.js') : null;
    if (!gm || typeof path !== 'string' || !path || !pattern) return false;
    if (pattern.indexOf('**/') === 0) {
      var rest = pattern.slice(3);
      var segs = path.split('/');
      for (var i = 0; i < segs.length; i++) {
        if (gm.matchOne(segs.slice(i).join('/'), rest)) return true;
      }
      return false;
    }
    return gm.matchOne(path, pattern);
  }

  function forFile(path) {
    for (var i = registry.length - 1; i >= 0; i--) {
      var r = registry[i];
      for (var j = 0; j < r.paths.length; j++) if (matchFile(path, r.paths[j])) return r;
    }
    return null;
  }

  function lineCount(source) {
    if (!source) return 0;
    return source.replace(/\n$/, '').split('\n').length;
  }

  // Markup for a render target: the source stays in the page as a <pre> so
  // there is something to show before rendering and when rendering fails.
  // startLine/endLine place the source in its file (0 = not line-mapped); the
  // file path comes from the enclosing [data-file-path] element.
  function targetHTML(rendererName, kind, source, opts) {
    var o = opts || {};
    var attrs = ' data-crit-renderer="' + escapeHtml(rendererName) + '" data-crit-kind="' + kind + '"';
    if (o.startLine) attrs += ' data-source-start="' + o.startLine + '" data-source-end="' + (o.endLine || o.startLine) + '"';
    var lang = o.lang ? ' class="language-' + escapeHtml(o.lang) + '"' : '';
    return '<div class="crit-render"' + attrs + '><pre class="crit-render-source"><code' + lang + '>' +
      escapeHtml(source) + '</code></pre></div>';
  }

  function currentTheme() {
    if (typeof document === 'undefined') return 'dark';
    var t = document.documentElement.getAttribute('data-theme');
    if (t === 'light' || t === 'dark') return t;
    return window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
  }

  // The theme and palette a target renders in. Targets record it, so a
  // target Pierre cached off-screen during a theme change re-renders when it
  // mounts again.
  function themeKey() {
    if (typeof document === 'undefined') return 'dark';
    var palette = document.documentElement.getAttribute('data-crit-palette');
    return currentTheme() + (palette ? '/' + palette : '');
  }

  function targetContext(target, renderer, container) {
    var start = parseInt(target.getAttribute('data-source-start'), 10) || 0;
    var end = parseInt(target.getAttribute('data-source-end'), 10) || 0;
    var ctx = {
      container: container,
      source: sourceOf(target),
      kind: renderer.kind,
      path: filePathOf(target),
      startLine: start,
      endLine: end,
      theme: currentTheme(),
      renderer: renderer.name,
    };
    ctx.renderAs = function (name, source) { return renderAs(name, source, ctx); };
    return ctx;
  }

  // Render `source` with the fence renderer `name` into the caller's
  // container, e.g. a file renderer that converts its format to mermaid.
  // Resolves to that renderer's result (an HTML string, a Node, or nothing
  // when it filled the container). The derived source has no file lines.
  function renderAs(name, source, parent) {
    var r = byName('fence', name);
    if (!r) return Promise.reject(new Error('no fence renderer named ' + name));
    var p = parent || {};
    var ctx = {
      container: p.container,
      source: source,
      kind: 'fence',
      path: p.path || '',
      startLine: 0,
      endLine: 0,
      theme: p.theme || currentTheme(),
      renderer: r.name,
    };
    ctx.renderAs = function (n, s) { return renderAs(n, s, ctx); };
    return Promise.resolve().then(function () { return r.render(source, ctx); });
  }

  // The file a target belongs to: its enclosing line block's path.
  function filePathOf(target) {
    var holder = target.closest('[data-file-path]');
    return holder ? holder.getAttribute('data-file-path') : '';
  }

  function sourceOf(target) {
    if (typeof target._critSource !== 'string') {
      var pre = target.querySelector('.crit-render-source');
      target._critSource = pre ? pre.textContent : '';
    }
    return target._critSource;
  }

  function showError(target, err) {
    target.setAttribute('data-crit-state', 'error');
    var out = target.querySelector(':scope > .crit-render-output');
    if (out) out.remove();
    var pre = target.querySelector(':scope > .crit-render-source');
    if (pre) pre.hidden = false;
    var expand = target.querySelector(':scope > .crit-render-expand');
    if (expand) expand.remove();
    var box = target.querySelector(':scope > .crit-render-error');
    if (!box) {
      box = document.createElement('div');
      box.className = 'crit-render-error';
      box.setAttribute('role', 'note');
      target.insertBefore(box, target.firstChild);
    }
    var name = target.getAttribute('data-crit-renderer');
    var msg = err && err.message ? err.message : String(err || 'unknown error');
    box.textContent = 'Could not render ' + name + ': ' + msg.split('\n')[0];
  }

  function addExpandButton(target, renderer, output) {
    var overlay = window.crit && window.crit.diagramOverlay;
    if (!renderer.zoomable || !overlay || target.querySelector(':scope > .crit-render-expand')) return;
    if (!output.querySelector('svg')) return;
    var btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'crit-render-expand';
    btn.setAttribute('aria-label', 'Open diagram fullscreen');
    btn.setAttribute('title', 'Open fullscreen');
    btn.textContent = '⛶ Expand';
    btn.addEventListener('click', function (e) {
      e.stopPropagation();
      var svg = target.querySelector(':scope > .crit-render-output svg');
      if (svg) overlay.open(svg, btn);
    });
    target.appendChild(btn);
  }

  function rendererOf(target) {
    return byName(target.getAttribute('data-crit-kind') || 'fence', target.getAttribute('data-crit-renderer'));
  }

  // Render one target. Returns a Promise that settles when done; never rejects.
  function renderTarget(target) {
    var renderer = rendererOf(target);
    if (!renderer) return Promise.resolve();
    var gen = (target._critGen || 0) + 1;
    target._critGen = gen;
    target.setAttribute('data-crit-state', 'pending');
    target.setAttribute('data-crit-theme', themeKey());
    var output = document.createElement('div');
    output.className = 'crit-render-output';
    var ctx = targetContext(target, renderer, output);
    // First render: the output is in the page while rendering, so renderers
    // can measure it. Re-render: the previous output stays until replaced.
    var old = target.querySelector(':scope > .crit-render-output');
    if (!old) target.appendChild(output);
    return Promise.resolve().then(function () {
      return renderer.render(ctx.source, ctx);
    }).then(function (result) {
      if (target._critGen !== gen) return;
      if (typeof result === 'string') output.innerHTML = result;
      else if (result && result.nodeType) output.appendChild(result);
      if (old) old.replaceWith(output);
      var err = target.querySelector(':scope > .crit-render-error');
      if (err) err.remove();
      var pre = target.querySelector(':scope > .crit-render-source');
      if (pre) pre.hidden = true;
      target.setAttribute('data-crit-state', 'rendered');
      if (renderer.anchorFor && ctx.endLine) target.setAttribute('data-crit-anchorable', '');
      addExpandButton(target, renderer, output);
    }).catch(function (err) {
      if (target._critGen !== gen) return;
      console.warn('[crit] renderer ' + renderer.name + ' failed:', err);
      showError(target, err);
    });
  }

  // Wrap bare `pre > code.language-X` blocks (comment bodies, story diagrams)
  // that a fence renderer claims, so they render like document fences.
  function wrapBareFences(root) {
    var codes = root.querySelectorAll('pre > code[class*="language-"]');
    for (var i = 0; i < codes.length; i++) {
      var code = codes[i];
      var pre = code.parentElement;
      if (pre.closest('.crit-render') || pre.closest('.comment-form')) continue;
      var m = /(?:^|\s)language-([^\s]+)/.exec(code.className);
      var renderer = m && forFence(m[1]);
      if (!renderer) continue;
      var target = document.createElement('div');
      target.className = 'crit-render';
      target.setAttribute('data-crit-renderer', renderer.name);
      target.setAttribute('data-crit-kind', 'fence');
      pre.replaceWith(target);
      pre.classList.add('crit-render-source');
      target.appendChild(pre);
    }
  }

  // A target that was never rendered, or was rendered in another theme
  // (it was off-screen in Pierre's cache when the theme changed).
  function isStale(target) {
    return !target.hasAttribute('data-crit-state') || target.getAttribute('data-crit-theme') !== themeKey();
  }

  // Does anything under root need renderAll?
  function needsRender(root) {
    var targets = (root || document).querySelectorAll('.crit-render');
    for (var i = 0; i < targets.length; i++) if (isStale(targets[i])) return true;
    return false;
  }

  // Render every target under root that is not rendered in the current theme.
  function renderAll(root) {
    var r = root || document;
    installAnchorClicks();
    wrapBareFences(r);
    var targets = r.querySelectorAll('.crit-render');
    var pending = [];
    for (var i = 0; i < targets.length; i++) {
      if (isStale(targets[i])) pending.push(renderTarget(targets[i]));
    }
    return Promise.all(pending);
  }

  // Re-render every target under root (theme change).
  function rerenderAll(root) {
    var targets = (root || document).querySelectorAll('.crit-render[data-crit-state]');
    var pending = [];
    for (var i = 0; i < targets.length; i++) pending.push(renderTarget(targets[i]));
    return Promise.all(pending);
  }

  // Map a rendered element to file lines through a file renderer's anchorFor,
  // which counts lines from the start of the file. null when the target is
  // not a file, the renderer has no anchorFor, or it declines (comment on the
  // whole file via the gutter). An optional `quote` is kept only when it is
  // text of the chosen lines, like a selection quote.
  function anchorAt(target, element) {
    var renderer = rendererOf(target);
    if (!renderer || !renderer.anchorFor) return null;
    var ctx = targetContext(target, renderer, target.querySelector(':scope > .crit-render-output'));
    if (!ctx.endLine) return null;
    var range;
    try { range = renderer.anchorFor(element, ctx); } catch (_) { return null; }
    if (!range || !(range.startLine >= 1)) return null;
    var s = Math.min(range.startLine, ctx.endLine);
    var e = Math.min(Math.max(range.endLine || s, s), ctx.endLine);
    var out = { startLine: s, endLine: e };
    if (typeof range.quote === 'string' && range.quote.trim()) {
      var first = ctx.startLine || 1;
      var text = String(ctx.source || '').split('\n').slice(s - first, e - first + 1).join('\n');
      if (text.indexOf(range.quote) !== -1) out.quote = range.quote;
    }
    return out;
  }

  // A click on rendered output that the renderer maps to lines dispatches a
  // bubbling `crit:render-anchor` event ({path, startLine, endLine, quote}); the
  // review page opens a comment form for it.
  function installAnchorClicks() {
    if (clickInstalled || typeof document === 'undefined') return;
    clickInstalled = true;
    document.addEventListener('click', function (e) {
      var el = e.target;
      if (!el || !el.closest || e.defaultPrevented || e.button !== 0) return;
      var output = el.closest('.crit-render-output');
      var target = output && output.parentElement;
      if (!target || !target.classList.contains('crit-render')) return;
      var sel = window.getSelection && window.getSelection();
      if (sel && !sel.isCollapsed && sel.toString().trim()) return;
      var range = anchorAt(target, el);
      if (!range) return;
      e.preventDefault();
      target.dispatchEvent(new CustomEvent('crit:render-anchor', {
        bubbles: true,
        detail: { path: filePathOf(target), startLine: range.startLine, endLine: range.endLine, quote: range.quote || null },
      }));
    });
  }

  var api = {
    register: register,
    unregister: unregister,
    list: list,
    byName: byName,
    forFence: forFence,
    forFile: forFile,
    matchFile: matchFile,
    lineCount: lineCount,
    targetHTML: targetHTML,
    renderAll: renderAll,
    rerenderAll: rerenderAll,
    needsRender: needsRender,
    renderAs: renderAs,
    anchorAt: anchorAt,
  };

  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.renderers = api;
  }
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
