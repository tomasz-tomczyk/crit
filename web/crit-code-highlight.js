(function () {
  'use strict';

  // Syntax highlighting for code outside Pierre's diff/file surfaces: fenced
  // blocks in rendered markdown documents and in comments. Tokenization runs
  // in Pierre's worker pool (the same Shiki grammars and themes as the
  // diffs), so Crit ships one highlighter and a
  // long fence never blocks the main thread.
  //
  //   prime(fences) → Promise   tokenize off-thread, fill the cache
  //   lines(code, lang)          highlighted HTML per line from the cache,
  //                              or null (not primed / unsupported language)
  //   html(code, lang)           whole-block form (markdown-it `highlight`)
  //   upgrade(root)              highlight mounted <code class="language-x">
  //                              blocks in place (comments render plain)
  //
  // Documents prime their fences while the file loads (already async), so
  // line blocks render highlighted on first paint. Token colours are the
  // --diffs-token-light / --diffs-token-dark properties Pierre emits;
  // theme.css picks one per theme for `.crit-code`.
  //
  // configure({ pool }) hands over a function returning Pierre's worker pool,
  // or a promise of it (app.js: once the Pierre bundle has loaded).

  var getPool = null;
  var cache = new Map();   // lang + '\0' + code → string[] | null (LRU)
  var CACHE_LIMIT = 500;

  function remember(key, value) {
    cache.delete(key);
    cache.set(key, value);
    if (cache.size > CACHE_LIMIT) cache.delete(cache.keys().next().value);
  }
  var pending = new Map(); // same key → Promise
  var generation = 0;

  function configure(options) {
    generation++;
    getPool = options && options.pool;
    cache.clear();
    pending.clear();
    // Comment cards and the review conversation may survive a palette change.
    // Re-tokenize their mounted fences as well as clearing future cache reads.
    if (typeof document !== 'undefined' && document.body) {
      document.querySelectorAll('pre > code[data-crit-code]').forEach(function(code) {
        code.removeAttribute('data-crit-code');
      });
      upgrade(document.body);
    }
  }

  function escapeHtml(s) {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  // Fence names people wrote for highlight.js that Shiki knows under another
  // id (Shiki's own aliases, like js/sh/yml, resolve without this).
  var FENCE_ALIASES = {
    '1c': 'bsl', actionscript: 'actionscript-3', arduino: 'cpp', delphi: 'pascal', dos: 'bat',
    fortran: 'fortran-free-form', gradle: 'groovy', heex: 'html', leex: 'html', lisp: 'common-lisp',
    mathematica: 'wolfram', vbscript: 'vb',
  };

  function normalize(lang) {
    var id = String(lang || '').trim().toLowerCase().split(/[\s{]/)[0];
    return FENCE_ALIASES[id] || id;
  }

  function cacheKey(code, lang) {
    return normalize(lang) + '\0' + code;
  }

  // Per-operation IDs prevent overlapping old/new theme generations from
  // sharing a Pierre cache entry (and avoid collisions between fence texts).
  var nextFileId = 0;

  // hast (Pierre's per-line element) → HTML. Lines hold token spans whose
  // only property is the style carrying the token colours.
  function nodeHtml(node) {
    if (node.type === 'text') return escapeHtml(node.value);
    if (node.type !== 'element') return '';
    var inner = (node.children || []).map(nodeHtml).join('');
    if (node.tagName !== 'span') return inner;
    var style = node.properties && node.properties.style;
    return style ? '<span style="' + escapeHtml(String(style)) + '">' + inner + '</span>' : inner;
  }

  function linesFromResult(result, code) {
    var out = (result.code || []).map(nodeHtml);
    // Pierre renders the empty line after a trailing newline; a fence's
    // content always ends with one.
    if (code.endsWith('\n') && out.length > 0) out.pop();
    return out;
  }

  function tokenize(code, lang) {
    var started = generation;
    var key = cacheKey(code, lang);
    if (cache.has(key)) return Promise.resolve(cache.get(key));
    if (pending.has(key)) return pending.get(key);
    var id = normalize(lang);
    if (!id || !getPool) {
      remember(key, null);
      return Promise.resolve(null);
    }
    var file = { name: 'fence', contents: code, cacheKey: 'crit-fence:' + (++nextFileId), lang: id };
    var pool = null;
    var p = Promise.resolve(getPool()).then(function(ready) {
      pool = ready;
      if (!pool) throw new Error('no worker pool');
      return pool.primeFileHighlightCache(file);
    }).then(function() {
      var res = pool.getFileResultCache(file);
      var out = res && res.result ? linesFromResult(res.result, code) : null;
      pool.evictFileFromCache(file.cacheKey);
      if (started === generation) remember(key, out);
      return out;
    }, function() {
      if (started === generation) remember(key, null); // unknown grammar: plain text
      return null;
    });
    pending.set(key, p);
    return p.finally(function() { if (pending.get(key) === p) pending.delete(key); });
  }

  // fences: [{ code, lang }]
  function prime(fences) {
    return Promise.all((fences || []).map(function(f) { return tokenize(f.code, f.lang); }));
  }

  function lines(code, lang) {
    return cache.get(cacheKey(code, lang)) || null;
  }

  function html(code, lang) {
    var out = lines(code, lang);
    return out ? out.join('\n') + '\n' : '';
  }

  // Highlight <pre><code class="language-x"> blocks rendered plain (comment
  // HTML is sanitised, so token styles are added after mounting). Each block
  // is done once; unknown languages stay plain.
  function upgrade(root) {
    if (!root || !root.querySelectorAll) return;
    var blocks = root.querySelectorAll('pre > code[class*="language-"]:not([data-crit-code])');
    Array.prototype.forEach.call(blocks, function(code) {
      var m = /(?:^|\s)language-([^\s]+)/.exec(code.className);
      if (!m) return;
      var text = code.textContent;
      var started = generation;
      code.setAttribute('data-crit-code', 'pending');
      tokenize(text, m[1]).then(function(out) {
        if (started !== generation) return; // configure already queued the current theme
        if (!out || code.textContent !== text) {
          code.setAttribute('data-crit-code', 'plain');
          return;
        }
        code.innerHTML = out.join('\n');
        code.classList.add('crit-code');
        code.setAttribute('data-crit-code', 'highlighted');
      });
    });
  }

  var api = {
    configure: configure,
    prime: prime,
    lines: lines,
    html: html,
    upgrade: upgrade,
    linesFromResult: linesFromResult,
  };

  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.codeHighlight = api;
  }
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
