(function() {
  'use strict';

  // Colour maths shared by the palette build (scripts/crit-theme-palette.mjs)
  // and the optional "boost low-contrast syntax colours" setting.
  //
  // Boost: themes are shown as designed by default. With the setting on, the
  // selected theme is registered with Pierre a second time under a new name,
  // with every token colour that falls under TOKEN_CONTRAST on the
  // backgrounds code sits on (plain, added/deleted line, word highlight) moved
  // in lightness only, keeping hue and chroma. Readable colours are untouched.
  // Pierre resolves themes on the main thread and hands the data to its
  // workers, so the copy applies to diffs, story and fenced code alike.
  // Reads window.PierreDiffs (registerCustomTheme, resolveTheme) at call time.

  var TOKEN_CONTRAST = 4.6; // WCAG AA 4.5 plus rounding headroom
  var SUFFIX = '--crit-boost';

  // CSS hex in short, long and alpha forms; alpha is flattened onto `background`.
  function parse(value, background) {
    if (typeof value !== 'string' || !/^#(?:[\da-f]{3}|[\da-f]{4}|[\da-f]{6}|[\da-f]{8})$/i.test(value)) return null;
    var hex = value.slice(1);
    if (hex.length <= 4) hex = hex.split('').map(function(d) { return d + d; }).join('');
    var rgb = '#' + hex.slice(0, 6).toLowerCase();
    return hex.length === 8 ? mix(rgb, background, parseInt(hex.slice(6), 16) / 255) : rgb;
  }
  function channels(hex) { return [1, 3, 5].map(function(i) { return parseInt(hex.slice(i, i + 2), 16); }); }
  function mix(a, b, share) {
    var right = channels(b);
    return '#' + channels(a).map(function(v, i) {
      return Math.round(v * share + right[i] * (1 - share)).toString(16).padStart(2, '0');
    }).join('');
  }
  function linear(v) { return v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4); }
  function luminance(hex) {
    var rgb = channels(hex).map(function(v) { return linear(v / 255); });
    return rgb[0] * 0.2126 + rgb[1] * 0.7152 + rgb[2] * 0.0722;
  }
  function contrast(a, b) {
    var x = luminance(a), y = luminance(b);
    return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
  }

  // OKLab / OKLCH (perceptual lightness, chroma, hue) <-> sRGB hex.
  function oklab(hex) {
    var rgb = channels(hex).map(function(v) { return linear(v / 255); });
    var l = Math.cbrt(0.4122214708 * rgb[0] + 0.5363325363 * rgb[1] + 0.0514459929 * rgb[2]);
    var m = Math.cbrt(0.2119034982 * rgb[0] + 0.6806995451 * rgb[1] + 0.1073969566 * rgb[2]);
    var s = Math.cbrt(0.0883024619 * rgb[0] + 0.2817188376 * rgb[1] + 0.6299787005 * rgb[2]);
    return [0.2104542553 * l + 0.7936177850 * m - 0.0040720468 * s,
      1.9779984951 * l - 2.4285922050 * m + 0.4505937099 * s,
      0.0259040371 * l + 0.7827717662 * m - 0.8086757660 * s];
  }
  function oklch(hex) {
    var lab = oklab(hex);
    return [lab[0], Math.hypot(lab[1], lab[2]), (Math.atan2(lab[2], lab[1]) * 180 / Math.PI + 360) % 360];
  }
  function distance(x, y) {
    var a = oklab(x), b = oklab(y);
    return Math.hypot(a[0] - b[0], a[1] - b[1], a[2] - b[2]);
  }
  function encode(v) {
    var x = Math.min(1, Math.max(0, v));
    var e = x <= 0.0031308 ? x * 12.92 : 1.055 * Math.pow(x, 1 / 2.4) - 0.055;
    return Math.round(e * 255).toString(16).padStart(2, '0');
  }
  // Out-of-gamut colours lose chroma until they fit, keeping lightness and hue.
  function fromOklch(l, c, h) {
    for (var chroma = c; chroma >= 0; chroma -= 0.005) {
      var a = chroma * Math.cos(h * Math.PI / 180), b = chroma * Math.sin(h * Math.PI / 180);
      var lc = Math.pow(l + 0.3963377774 * a + 0.2158037573 * b, 3);
      var mc = Math.pow(l - 0.1055613458 * a - 0.0638541728 * b, 3);
      var sc = Math.pow(l - 0.0894841775 * a - 1.2914855480 * b, 3);
      var rgb = [4.0767416621 * lc - 3.3077115913 * mc + 0.2309699292 * sc,
        -1.2684380046 * lc + 2.6097574011 * mc - 0.3413193965 * sc,
        -0.0041960863 * lc - 0.7034186147 * mc + 1.7076147010 * sc];
      if (rgb.every(function(v) { return v >= -0.0001 && v <= 1.0001; })) return '#' + rgb.map(encode).join('');
    }
    return '#' + [l, l, l].map(function(v) { return encode(Math.pow(Math.min(1, Math.max(0, v)), 3)); }).join('');
  }

  // Move only lightness until `value` reaches `target` on every background.
  function lighten(value, backgrounds, target, up) {
    var lch = oklch(value);
    for (var step = 0; step <= 100; step++) {
      var l = up ? lch[0] + step / 100 : lch[0] - step / 100;
      if (l < 0 || l > 1) break;
      var candidate = step === 0 ? value : fromOklch(l, lch[1], lch[2]);
      if (backgrounds.every(function(bg) { return contrast(candidate, bg) >= target; })) return candidate;
    }
    return up ? '#ffffff' : '#000000';
  }

  // Backgrounds code sits on, as Pierre paints them (getHighlighterThemeStyles
  // + its CSS): line tint 20% (dark) / 12% (light) of the theme's git colour,
  // word highlight another 20% / 15% on top.
  function codeBackgrounds(theme) {
    var dark = theme.type !== 'light';
    var bg = parse(theme.bg, dark ? '#000000' : '#ffffff') || (dark ? '#000000' : '#ffffff');
    var colors = theme.colors || {};
    var git = function(keys, fallback) {
      for (var i = 0; i < keys.length; i++) {
        var v = parse(colors[keys[i]], bg);
        if (v) return v;
      }
      return fallback;
    };
    var add = git(['gitDecoration.addedResourceForeground', 'terminal.ansiGreen'], dark ? '#5ecc71' : '#0dbe4e');
    var del = git(['gitDecoration.deletedResourceForeground', 'terminal.ansiRed'], dark ? '#ff6762' : '#ff2e3f');
    var line = dark ? 0.2 : 0.12, word = dark ? 0.2 : 0.15;
    var addLine = mix(add, bg, line), delLine = mix(del, bg, line);
    return [bg, addLine, delLine, mix(add, addLine, word), mix(del, delLine, word)];
  }

  // A copy of a resolved Shiki theme with readable token colours.
  function boostTheme(theme, name) {
    var copy = JSON.parse(JSON.stringify(theme));
    copy.name = name;
    var backgrounds = codeBackgrounds(theme);
    var up = theme.type !== 'light';
    var fix = function(value) {
      var hex = parse(value, backgrounds[0]);
      return hex ? lighten(hex, backgrounds, TOKEN_CONTRAST, up) : value;
    };
    (copy.settings || copy.tokenColors || []).forEach(function(rule) {
      if (rule.settings && rule.settings.foreground) rule.settings.foreground = fix(rule.settings.foreground);
    });
    if (copy.fg) copy.fg = fix(copy.fg);
    if (copy.colors && copy.colors['editor.foreground']) copy.colors['editor.foreground'] = fix(copy.colors['editor.foreground']);
    return copy;
  }

  var registered = {};
  // Name of the boosted copy of theme `id`, registered with Pierre on first use.
  function boostedName(P, id) {
    var name = id + SUFFIX;
    if (!registered[name]) {
      registered[name] = true;
      P.registerCustomTheme(name, function() {
        return P.resolveTheme(id).then(function(theme) { return boostTheme(theme, name); });
      });
    }
    return name;
  }

  // Pierre theme pair for the current setting.
  function themes(P, pair, on) {
    if (!on || !P || !P.registerCustomTheme) return pair;
    return { light: boostedName(P, pair.light), dark: boostedName(P, pair.dark) };
  }

  var api = {
    TOKEN_CONTRAST: TOKEN_CONTRAST,
    parse: parse, mix: mix, luminance: luminance, contrast: contrast,
    oklch: oklch, fromOklch: fromOklch, distance: distance,
    codeBackgrounds: codeBackgrounds, boostTheme: boostTheme, themes: themes,
  };
  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.themeBoost = api;
  }
  if (typeof module === 'object' && module.exports) module.exports = api;
})();
