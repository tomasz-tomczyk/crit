// crit-glob-match.js — gitignore-ish glob matcher for auto-viewed-patterns
// (issue #658). Pure, dependency-free. Used by app.js (code-review mode) to
// decide which files an `auto_viewed_patterns` config entry should auto-mark
// as viewed. The same matching logic is inlined in crit-web's
// document-renderer.js for parity (crit-web has no shared crit-* modules).
//
// Supported pattern types (same set crit's Go `ignore_patterns` documents):
//   *.ext        basename glob          → matches any file ending in `.ext`
//   dir/         directory prefix       → matches any file under `dir/`
//   exact.file   exact path or basename → matches `a/b/exact.file` or `exact.file`
//   path/*.ext   glob with directory    → matches `path/foo.ext` (one segment)
//
// Exports onto window.crit.globMatch. No dependencies on other crit modules.

(function () {
  'use strict';

  // Escape a literal string for use inside a RegExp.
  function escapeRegExp(s) {
    return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  }

  // Translate a single glob segment ("*.ext", "foo*", "exact.file") into a
  // RegExp source fragment. `*` matches any run of non-slash characters.
  function segmentToRegExp(segment) {
    var out = '';
    for (var i = 0; i < segment.length; i++) {
      var ch = segment[i];
      if (ch === '*') {
        out += '[^/]*';
      } else {
        out += escapeRegExp(ch);
      }
    }
    return out;
  }

  function basename(path) {
    var idx = path.lastIndexOf('/');
    return idx === -1 ? path : path.slice(idx + 1);
  }

  // Does `filePath` match a single `pattern`?
  function matchOne(filePath, pattern) {
    if (!pattern || !filePath) return false;

    // Directory prefix: "dir/" matches any file under that directory at any
    // depth. Normalize a trailing slash and check the path prefix on a
    // segment boundary.
    if (pattern.charAt(pattern.length - 1) === '/') {
      var prefix = pattern; // includes trailing slash, e.g. "generated/"
      // Match "generated/foo", "a/generated/foo" not required by the issue's
      // examples, but anchor to start OR a "/" boundary to be gitignore-ish.
      if (filePath.indexOf(prefix) === 0) return true;
      return filePath.indexOf('/' + prefix) !== -1;
    }

    // Pattern contains a slash: match against the full path (glob per segment).
    if (pattern.indexOf('/') !== -1) {
      var parts = pattern.split('/').map(segmentToRegExp);
      var re = new RegExp('^' + parts.join('/') + '$');
      return re.test(filePath);
    }

    // No slash. If it contains a glob char, match against the basename.
    if (pattern.indexOf('*') !== -1) {
      var bre = new RegExp('^' + segmentToRegExp(pattern) + '$');
      return bre.test(basename(filePath));
    }

    // Plain string: exact full-path match OR exact basename match.
    return filePath === pattern || basename(filePath) === pattern;
  }

  // Does `filePath` match ANY pattern in `patterns` (array of strings)?
  function matchAny(filePath, patterns) {
    if (!patterns || !patterns.length) return false;
    for (var i = 0; i < patterns.length; i++) {
      if (matchOne(filePath, patterns[i])) return true;
    }
    return false;
  }

  var api = {
    matchOne: matchOne,
    matchAny: matchAny,
  };

  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.globMatch = api;
  }
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
