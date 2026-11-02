// crit-renderer-excalidraw.js — built-in Excalidraw renderer.
// Draws Excalidraw scenes (the JSON Excalidraw saves) as SVG. Two
// registrations with window.crit.renderers: a file renderer for *.excalidraw
// and *.excalidraw.json, where a click on a shape, arrow or text comments on
// the JSON lines of that element, and a fence renderer for ```excalidraw
// blocks holding a scene (comments attach to the whole fence).
// Shapes go through rough.js, the library Excalidraw draws with, using each
// element's seed and Excalidraw's stroke options, and freedraw through
// perfect-freehand and Excalidraw's laser-pointer, so they come out as in the
// editor. Text uses Excalidraw's fonts (web/fonts/excalidraw) and layout.
// Dependencies: window.crit.renderers (when present). rough.min.js (window.rough),
// freehand.min.js (window.CritFreehand) and the fonts are not part of the
// page: the first render that needs them loads them, so reviews without a
// scene never download them.
(function () {
  'use strict';

  var SVG_NS = 'http://www.w3.org/2000/svg';
  var PADDING = 10;
  // In the dark theme Excalidraw turns each color it paints through this
  // filter (darkColor), and embedded SVG images through the filter itself.
  var DARK_THEME_FILTER = 'invert(93%) hue-rotate(180deg)';

  // Frame outline and name (Excalidraw's FRAME_STYLE).
  var FRAME_STROKE = '#bbb';
  var FRAME_NAME_COLOR = { light: '#999999', dark: '#7a7a7a' };

  // Sticky notes (Excalidraw's STICKY_NOTE_* constants and stickyNote.ts).
  var STICKY_NOTE = {
    background: '#ffdf6b',
    minSize: 75,
    defaultSize: 250,
    padding: 16,
    shadowOffset: 3,
    shadowOpacity: 0.16,
    edgeShadowWidth: 0.5,
    edgeShadowOpacity: 0.08,
    roughness: [0, 1.5, 8],
    cornerRadiusRatio: 0.04,
    maxCornerRadius: 16,
    footer: { fontSize: 12, fontFamily: 'Helvetica, Arial, sans-serif', baselineFromBottom: 14, opacity: 1, minBodyWidthForYear: 80 },
  };
  var MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

  // Excalidraw's font ids: the font-family stack it writes into SVG exports
  // (getFontFamilyString), the font it registers, and the metrics it lays
  // text out with (FONT_METADATA). Helvetica is a system font in Excalidraw
  // too; Crit does not ship Liberation Sans, which has Arial's metrics.
  var FONTS = {
    1: { family: 'Virgil, sans-serif, Segoe UI Emoji', name: 'Virgil', em: 1000, ascender: 886, descender: -374, lineHeight: 1.25 },
    2: { family: 'Helvetica, sans-serif, Segoe UI Emoji', name: 'Helvetica', em: 2048, ascender: 1577, descender: -471, lineHeight: 1.15 },
    3: { family: 'Cascadia, monospace, Segoe UI Emoji', name: 'Cascadia', em: 2048, ascender: 1900, descender: -480, lineHeight: 1.2 },
    5: { family: 'Excalifont, Xiaolai, sans-serif, Segoe UI Emoji', name: 'Excalifont', em: 1000, ascender: 886, descender: -374, lineHeight: 1.25 },
    6: { family: 'Nunito, sans-serif, Segoe UI Emoji', name: 'Nunito', em: 1000, ascender: 1011, descender: -353, lineHeight: 1.25 },
    7: { family: 'Lilita One, sans-serif, Segoe UI Emoji', name: 'Lilita One', em: 1000, ascender: 923, descender: -220, lineHeight: 1.15 },
    8: { family: 'Comic Shanns, monospace, Segoe UI Emoji', name: 'Comic Shanns', em: 1000, ascender: 750, descender: -250, lineHeight: 1.25 },
    9: { family: 'Liberation Sans, Arial, Helvetica, sans-serif, Segoe UI Emoji', name: 'Liberation Sans', em: 2048, ascender: 1854, descender: -434, lineHeight: 1.15 },
  };
  var FRAME_FONT = 'Helvetica, Arial, sans-serif';

  // The font files in web/fonts/excalidraw, copied from Excalidraw's
  // packages/excalidraw/fonts with the unicode ranges of their subsets
  // (<Family>/index.ts there). The browser fetches a subset only when text
  // uses a character in its range.
  var FONT_DIR = 'fonts/excalidraw/';
  // Crit's assets sit next to this script, so the libraries and fonts load
  // from where it was loaded, whatever the page's own path.
  var ASSET_BASE = (function () {
    var script = typeof document !== 'undefined' && document.currentScript;
    return script && script.src ? script.src.replace(/[^/]*$/, '') : '';
  })();
  var GOOGLE_LATIN = 'U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+2074, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD';
  var GOOGLE_LATIN_EXT = 'U+0100-02AF, U+0304, U+0308, U+0329, U+1E00-1E9F, U+1EF2-1EFF, U+2020, U+20A0-20AB, U+20AD-20C0, U+2113, U+2C60-2C7F, U+A720-A7FF';
  var FONT_FILES = {
    Excalifont: [
      { file: 'Excalifont/Excalifont-Regular-a88b72a24fb54c9f94e3b5fdaa7481c9.woff2', range: 'U+20-7e,U+a0-a3,U+a5-a6,U+a8-ab,U+ad-b1,U+b4,U+b6-b8,U+ba-ff,U+131,U+152-153,U+2bc,U+2c6,U+2da,U+2dc,U+304,U+308,U+2013-2014,U+2018-201a,U+201c-201e,U+2020,U+2022,U+2024-2026,U+2030,U+2039-203a,U+20ac,U+2122,U+2212' },
      { file: 'Excalifont/Excalifont-Regular-be310b9bcd4f1a43f571c46df7809174.woff2', range: 'U+100-130,U+132-137,U+139-149,U+14c-151,U+154-17e,U+192,U+1fc-1ff,U+218-21b,U+237,U+1e80-1e85,U+1ef2-1ef3,U+2113' },
      { file: 'Excalifont/Excalifont-Regular-b9dcf9d2e50a1eaf42fc664b50a3fd0d.woff2', range: 'U+400-45f,U+490-491,U+2116' },
      { file: 'Excalifont/Excalifont-Regular-41b173a47b57366892116a575a43e2b6.woff2', range: 'U+37e,U+384-38a,U+38c,U+38e-393,U+395-3a1,U+3a3-3a8,U+3aa-3cf,U+3d7' },
      { file: 'Excalifont/Excalifont-Regular-3f2c5db56cc93c5a6873b1361d730c16.woff2', range: 'U+2c7,U+2d8-2d9,U+2db,U+2dd,U+302,U+306-307,U+30a-30c,U+326-328,U+212e,U+2211,U+fb01-fb02' },
      { file: 'Excalifont/Excalifont-Regular-349fac6ca4700ffec595a7150a0d1e1d.woff2', range: 'U+462-463,U+472-475,U+4d8-4d9,U+4e2-4e3,U+4e6-4e9,U+4ee-4ef' },
      { file: 'Excalifont/Excalifont-Regular-623ccf21b21ef6b3a0d87738f77eb071.woff2', range: 'U+300-301,U+303' },
    ],
    Virgil: [
      { file: 'Virgil/Virgil-Regular.woff2' },
    ],
    Nunito: [
      { file: 'Nunito/Nunito-Regular-XRXI3I6Li01BKofiOc5wtlZ2di8HDIkhdTk3j6zbXWjgevT5.woff2', weight: '500', range: 'U+0460-052F, U+1C80-1C88, U+20B4, U+2DE0-2DFF, U+A640-A69F, U+FE2E-FE2F' },
      { file: 'Nunito/Nunito-Regular-XRXI3I6Li01BKofiOc5wtlZ2di8HDIkhdTA3j6zbXWjgevT5.woff2', weight: '500', range: 'U+0301, U+0400-045F, U+0490-0491, U+04B0-04B1, U+2116' },
      { file: 'Nunito/Nunito-Regular-XRXI3I6Li01BKofiOc5wtlZ2di8HDIkhdTs3j6zbXWjgevT5.woff2', weight: '500', range: 'U+0102-0103, U+0110-0111, U+0128-0129, U+0168-0169, U+01A0-01A1, U+01AF-01B0, U+0300-0301, U+0303-0304, U+0308-0309, U+0323, U+0329, U+1EA0-1EF9, U+20AB' },
      { file: 'Nunito/Nunito-Regular-XRXI3I6Li01BKofiOc5wtlZ2di8HDIkhdTo3j6zbXWjgevT5.woff2', weight: '500', range: GOOGLE_LATIN_EXT },
      { file: 'Nunito/Nunito-Regular-XRXI3I6Li01BKofiOc5wtlZ2di8HDIkhdTQ3j6zbXWjgeg.woff2', weight: '500', range: GOOGLE_LATIN },
    ],
    'Lilita One': [
      { file: 'Lilita/Lilita-Regular-i7dPIFZ9Zz-WBtRtedDbYE98RXi4EwSsbg.woff2', range: GOOGLE_LATIN_EXT },
      { file: 'Lilita/Lilita-Regular-i7dPIFZ9Zz-WBtRtedDbYEF8RXi4EwQ.woff2', range: GOOGLE_LATIN },
    ],
    'Comic Shanns': [
      { file: 'ComicShanns/ComicShanns-Regular-279a7b317d12eb88de06167bd672b4b4.woff2', range: 'U+20-7e,U+a1-a6,U+a8,U+ab-ac,U+af-b1,U+b4,U+b8,U+bb-bc,U+bf-cf,U+d1-d7,U+d9-de,U+e0-ef,U+f1-f7,U+f9-ff,U+131,U+152-153,U+2c6,U+2da,U+2dc,U+2013-2014,U+2018-201a,U+201c-201d,U+2020-2022,U+2026,U+2039-203a,U+2044,U+20ac,U+2191,U+2193,U+2212' },
      { file: 'ComicShanns/ComicShanns-Regular-fcb0fc02dcbee4c9846b3e2508668039.woff2', range: 'U+100-10f,U+112-125,U+128-130,U+134-137,U+139-13c,U+141-148,U+14c-151,U+154-161,U+164-165,U+168-17f,U+1bf,U+1f7,U+218-21b,U+237,U+1e80-1e85,U+1ef2-1ef3,U+a75b' },
      { file: 'ComicShanns/ComicShanns-Regular-dc6a8806fa96795d7b3be5026f989a17.woff2', range: 'U+2c7,U+2d8-2d9,U+2db,U+2dd,U+315,U+2190,U+2192,U+2200,U+2203-2204,U+2264-2265,U+f6c3' },
      { file: 'ComicShanns/ComicShanns-Regular-6e066e8de2ac57ea9283adb9c24d7f0c.woff2', range: 'U+3bb' },
    ],
    Cascadia: [
      { file: 'Cascadia/CascadiaCode-Regular.woff2' },
    ],
  };
  // How long a render waits for fonts before drawing with the fallback.
  var FONT_WAIT_MS = 3000;

  // Text that starts with a right-to-left character (Excalidraw's isRTL).
  var RTL = /^[^A-Za-zÀ-ÖØ-öø-ʸ̀-֐ࠀ-῿Ⰰ-﬜﷾-﹯﻽-￿]*[֑-߿יִ-﷽ﹰ-ﻼ]/;

  var FILL_STYLES = { hachure: 1, 'cross-hatch': 1, solid: 1, zigzag: 1, dots: 1, dashed: 1, 'zigzag-line': 1 };
  var ROUNDABLE = { rectangle: 1, iframe: 1, embeddable: 1, line: 1, diamond: 1, image: 1 };

  // --- scene ------------------------------------------------------------------

  function num(v, fallback) {
    return typeof v === 'number' && isFinite(v) ? v : fallback;
  }

  function str(v, fallback) {
    return typeof v === 'string' && v ? v : fallback;
  }

  // A stable seed for elements saved without one, so the strokes (and the
  // render cache) depend only on the source.
  // Arrowhead names from older scenes (Excalidraw's normalizeArrowhead).
  var LEGACY_ARROWHEADS = {
    dot: 'circle',
    crowfoot_one: 'cardinality_one',
    crowfoot_many: 'cardinality_many',
    crowfoot_one_or_many: 'cardinality_one_or_many',
  };

  function normalizeArrowhead(head) {
    var name = str(head, null);
    return name && LEGACY_ARROWHEADS[name] || name;
  }

  function seedFor(id) {
    var h = 0;
    for (var i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) | 0;
    return Math.abs(h) || 1;
  }

  function isPoint(p) {
    return Array.isArray(p) && typeof p[0] === 'number' && typeof p[1] === 'number' && isFinite(p[0]) && isFinite(p[1]);
  }

  function pointsOf(raw, width, height) {
    var pts = Array.isArray(raw.points) ? raw.points.filter(isPoint).map(function (p) { return [p[0], p[1]]; }) : [];
    return pts.length ? pts : [[0, 0], [width, height]];
  }

  // Freedraw points with the pressure recorded for each, dropping invalid
  // points (Excalidraw's restoreFreedrawPoints).
  function freedrawPoints(raw, el) {
    var pressures = Array.isArray(raw.pressures) ? raw.pressures : [];
    el.points = [];
    el.pressures = [];
    (Array.isArray(raw.points) ? raw.points : []).forEach(function (p, i) {
      if (!isPoint(p)) return;
      el.points.push([p[0], p[1]]);
      if (i in pressures) el.pressures.push(num(pressures[i], 0.5));
    });
  }

  // An element with Excalidraw's defaults for the fields a scene may leave out
  // (hand-written or generated JSON). `index` is its place in the file's
  // "elements" array, which anchorFor maps back to lines.
  function normalize(raw, index) {
    var el = {
      index: index,
      id: str(raw.id, String(index)),
      type: raw.type,
      x: num(raw.x, 0),
      y: num(raw.y, 0),
      width: num(raw.width, 0),
      height: num(raw.height, 0),
      angle: num(raw.angle, 0),
      strokeColor: str(raw.strokeColor, '#1e1e1e'),
      backgroundColor: str(raw.backgroundColor, 'transparent'),
      fillStyle: FILL_STYLES[raw.fillStyle] ? raw.fillStyle : 'solid',
      strokeWidth: num(raw.strokeWidth, 2),
      strokeStyle: raw.strokeStyle === 'dashed' || raw.strokeStyle === 'dotted' ? raw.strokeStyle : 'solid',
      roughness: num(raw.roughness, 1),
      opacity: num(raw.opacity, 100),
      roundness: raw.roundness && typeof raw.roundness === 'object' ? raw.roundness : null,
    };
    el.seed = num(raw.seed, 0) || seedFor(el.id);
    if (el.type === 'line' || el.type === 'arrow') {
      el.points = pointsOf(raw, el.width, el.height);
    }
    if (el.type === 'freedraw') {
      freedrawPoints(raw, el);
      // Passed through as saved: perfect-freehand defaults a missing value.
      el.simulatePressure = raw.simulatePressure;
      var stroke = raw.strokeOptions && typeof raw.strokeOptions === 'object' ? raw.strokeOptions : {};
      el.variability = stroke.variability === 'constant' ? 'constant' : 'variable';
      el.streamline = num(stroke.streamline, 0.5);
    }
    if (el.type === 'arrow') {
      el.elbowed = raw.elbowed === true;
      el.startId = raw.startBinding && str(raw.startBinding.elementId, null);
      el.endId = raw.endBinding && str(raw.endBinding.elementId, null);
      el.startArrowhead = normalizeArrowhead(raw.startArrowhead);
      // A missing end arrowhead is Excalidraw's default arrow; null is none.
      el.endArrowhead = raw.endArrowhead === undefined ? 'arrow' : normalizeArrowhead(raw.endArrowhead);
    }
    if (el.type === 'stickynote') {
      // A note is always filled and inked (normalizeStickyNote), and at least
      // its minimum size.
      if (isTransparent(el.backgroundColor)) el.backgroundColor = STICKY_NOTE.background;
      if (isTransparent(el.strokeColor)) el.strokeColor = '#1e1e1e';
      el.width = Math.max(el.width, STICKY_NOTE.minSize);
      var baseHeight = Math.max(num(raw.baseHeight, 0) || el.height || STICKY_NOTE.defaultSize, STICKY_NOTE.minSize);
      el.height = Math.max(el.height, baseHeight);
      el.created = num(raw.created, null);
    }
    if (el.type === 'text') {
      el.text = typeof raw.text === 'string' ? raw.text.replace(/\r\n?/g, '\n') : '';
      el.fontSize = num(raw.fontSize, 20);
      el.fontFamily = FONTS[raw.fontFamily] ? raw.fontFamily : 5;
      el.lineHeight = num(raw.lineHeight, FONTS[el.fontFamily].lineHeight);
      el.textAlign = raw.textAlign === 'center' || raw.textAlign === 'right' ? raw.textAlign : 'left';
      el.containerId = str(raw.containerId, null);
      var lines = el.text.split('\n');
      // Hand-written scenes may omit the size Excalidraw measures.
      if (!el.width) el.width = Math.max.apply(null, lines.map(function (l) { return l.length; })) * el.fontSize * 0.55;
      if (!el.height) el.height = lines.length * el.fontSize * el.lineHeight;
    }
    if (el.type === 'image') {
      el.fileId = str(raw.fileId, null);
      el.scale = Array.isArray(raw.scale) ? [num(raw.scale[0], 1), num(raw.scale[1], 1)] : [1, 1];
      el.crop = raw.crop && typeof raw.crop === 'object' ? raw.crop : null;
    }
    if (el.type === 'frame' || el.type === 'magicframe') {
      el.name = str(raw.name, el.type === 'magicframe' ? 'AI Frame' : 'Frame');
    }
    return el;
  }

  // The scene in `source`: its drawable elements in z-order (the order of the
  // "elements" array), the canvas background and the embedded image files.
  // Throws when the source is not a scene.
  function parseScene(source) {
    var data;
    try {
      data = JSON.parse(source);
    } catch (e) {
      throw new Error('not valid JSON: ' + e.message);
    }
    if (!data || !Array.isArray(data.elements)) throw new Error('not an Excalidraw scene: no "elements" array');
    var elements = [];
    data.elements.forEach(function (raw, index) {
      if (raw && typeof raw === 'object' && !raw.isDeleted && DRAW[raw.type]) elements.push(normalize(raw, index));
    });
    if (!elements.length) throw new Error('the scene has no elements');
    var appState = data.appState && typeof data.appState === 'object' ? data.appState : {};
    return {
      elements: elements,
      background: str(appState.viewBackgroundColor, '#ffffff'),
      files: data.files && typeof data.files === 'object' ? data.files : {},
    };
  }

  // --- geometry ---------------------------------------------------------------

  function rotate(p, center, angle) {
    if (!angle) return [p[0], p[1]];
    var cos = Math.cos(angle);
    var sin = Math.sin(angle);
    var dx = p[0] - center[0];
    var dy = p[1] - center[1];
    return [dx * cos - dy * sin + center[0], dx * sin + dy * cos + center[1]];
  }

  // The element's box in its own coordinates (origin at el.x, el.y): shapes
  // span width × height, lines and freedraw span their points.
  function localBox(el) {
    if (!el.points) {
      return { x1: Math.min(0, el.width), y1: Math.min(0, el.height), x2: Math.max(0, el.width), y2: Math.max(0, el.height) };
    }
    if (!el.points.length) return { x1: 0, y1: 0, x2: 0, y2: 0 };
    var b = { x1: Infinity, y1: Infinity, x2: -Infinity, y2: -Infinity };
    el.points.forEach(function (p) {
      b.x1 = Math.min(b.x1, p[0]);
      b.y1 = Math.min(b.y1, p[1]);
      b.x2 = Math.max(b.x2, p[0]);
      b.y2 = Math.max(b.y2, p[1]);
    });
    return b;
  }

  // How far a freedraw stroke's ink reaches past its points
  // (Excalidraw's getFreedrawMaxStrokeRadius).
  function inkRadius(el) {
    if (el.type !== 'freedraw') return 0;
    return el.variability === 'constant' ? el.strokeWidth * 1.4 : el.strokeWidth * 4.25 + 3;
  }

  // Scene-coordinate bounds of all elements, each rotated about its center.
  // A frame's name sits above the frame; freedraw ink reaches past its points.
  function sceneBounds(elements) {
    var out = { minX: Infinity, minY: Infinity, maxX: -Infinity, maxY: -Infinity };
    elements.forEach(function (el) {
      var b = localBox(el);
      var center = [(b.x1 + b.x2) / 2, (b.y1 + b.y2) / 2];
      var ink = inkRadius(el);
      var top = (el.name ? b.y1 - 24 : b.y1) - ink;
      var left = b.x1 - ink;
      var right = b.x2 + ink;
      var bottom = b.y2 + ink;
      [[left, top], [right, top], [right, bottom], [left, bottom]].forEach(function (corner) {
        var p = rotate(corner, center, el.angle);
        out.minX = Math.min(out.minX, el.x + p[0]);
        out.minY = Math.min(out.minY, el.y + p[1]);
        out.maxX = Math.max(out.maxX, el.x + p[0]);
        out.maxY = Math.max(out.maxY, el.y + p[1]);
      });
    });
    return out;
  }

  function isTransparent(color) {
    return color === 'transparent' || (color.length === 5 && color[4] === '0') || (color.length === 9 && color.slice(7) === '00');
  }

  // Excalidraw's isPathALoop: a line whose ends meet is a closed shape.
  function isLoop(points) {
    if (!points || points.length < 3) return false;
    var a = points[0];
    var b = points[points.length - 1];
    return Math.hypot(a[0] - b[0], a[1] - b[1]) <= 8;
  }

  // Small elements are drawn less rough (Excalidraw's adjustRoughness).
  function adjustRoughness(el) {
    var b = localBox(el);
    var maxSize = Math.max(b.x2 - b.x1, b.y2 - b.y1);
    var minSize = Math.min(b.x2 - b.x1, b.y2 - b.y1);
    if ((minSize >= 20 && maxSize >= 50) ||
      (minSize >= 15 && el.roundness && ROUNDABLE[el.type]) ||
      ((el.type === 'line' || el.type === 'arrow') && maxSize >= 50)) {
      return el.roughness;
    }
    return Math.min(el.roughness / (maxSize < 10 ? 3 : 2), 2.5);
  }

  // --- colors -----------------------------------------------------------------

  // A CSS color as {r, g, b, a}: hex and rgb() directly, anything else (named
  // colors, hsl()) through the browser's own parser. Unparseable colors are
  // black, as in tinycolor, which Excalidraw uses.
  var colorContext = null;

  function parseColor(color) {
    var s = String(color).trim();
    var m = /^#([0-9a-f]{3,4}|[0-9a-f]{6}|[0-9a-f]{8})$/i.exec(s);
    if (m) {
      var hex = m[1].length <= 4 ? m[1].replace(/./g, '$&$&') : m[1];
      return {
        r: parseInt(hex.slice(0, 2), 16),
        g: parseInt(hex.slice(2, 4), 16),
        b: parseInt(hex.slice(4, 6), 16),
        a: hex.length === 8 ? parseInt(hex.slice(6, 8), 16) / 255 : 1,
      };
    }
    m = /^rgba?\(\s*([\d.]+)[\s,]+([\d.]+)[\s,]+([\d.]+)\s*(?:[,/]\s*([\d.]+)\s*)?\)$/i.exec(s);
    if (m) {
      return { r: Math.round(+m[1]), g: Math.round(+m[2]), b: Math.round(+m[3]), a: m[4] === undefined ? 1 : Math.min(1, +m[4]) };
    }
    if (s.toLowerCase() === 'transparent') return { r: 0, g: 0, b: 0, a: 0 };
    if (typeof document !== 'undefined') {
      if (!colorContext) colorContext = document.createElement('canvas').getContext('2d');
      if (colorContext) {
        colorContext.fillStyle = '#000000';
        colorContext.fillStyle = s;
        var normalized = colorContext.fillStyle;
        if (normalized !== s) return parseColor(normalized);
      }
    }
    return { r: 0, g: 0, b: 0, a: 1 };
  }

  var darkColors = {};

  // A color as Excalidraw paints it in the dark theme (applyDarkModeFilter):
  // "invert(93%) hue-rotate(180deg)" applied to the color, alpha kept.
  function darkColor(color) {
    if (darkColors[color]) return darkColors[color];
    var c = parseColor(color);
    var inv = function (v) { return Math.round(Math.min(Math.max(v * (1 - 0.93) + (255 - v) * 0.93, 0), 255)); };
    var r = inv(c.r) / 255;
    var g = inv(c.g) / 255;
    var b = inv(c.b) / 255;
    var rad = (180 * Math.PI) / 180;
    var cos = Math.cos(rad);
    var sin = Math.sin(rad);
    var m = [
      0.213 + cos * 0.787 - sin * 0.213, 0.715 - cos * 0.715 - sin * 0.715, 0.072 - cos * 0.072 + sin * 0.928,
      0.213 - cos * 0.213 + sin * 0.143, 0.715 + cos * 0.285 + sin * 0.14, 0.072 - cos * 0.072 - sin * 0.283,
      0.213 - cos * 0.213 - sin * 0.787, 0.715 - cos * 0.715 + sin * 0.715, 0.072 + cos * 0.928 + sin * 0.072,
    ];
    var to255 = function (v) { return Math.round(Math.max(0, Math.min(1, v)) * 255); };
    var R = to255(r * m[0] + g * m[1] + b * m[2]);
    var G = to255(r * m[3] + g * m[4] + b * m[5]);
    var B = to255(r * m[6] + g * m[7] + b * m[8]);
    var out = '#' + ((1 << 24) + (R << 16) + (G << 8) + B).toString(16).slice(1);
    if (c.a < 1) {
      var alpha = Math.round(c.a * 255).toString(16);
      out += alpha.length < 2 ? '0' + alpha : alpha;
    }
    darkColors[color] = out;
    return out;
  }

  // A scene color as painted in the theme.
  function paint(color, dark) {
    return dark ? darkColor(color) : color;
  }

  // rough.js options for an element (Excalidraw's generateRoughOptions).
  function roughOptions(el, continuousPath, dark) {
    var o = {
      seed: el.seed,
      disableMultiStroke: el.strokeStyle !== 'solid',
      strokeWidth: el.strokeStyle !== 'solid' ? el.strokeWidth + 0.5 : el.strokeWidth,
      fillWeight: el.strokeWidth / 2,
      hachureGap: el.strokeWidth * 4,
      roughness: adjustRoughness(el),
      stroke: paint(el.strokeColor, dark),
      preserveVertices: !!continuousPath || el.roughness < 2,
    };
    if (el.strokeStyle === 'dashed') o.strokeLineDash = [8, 8 + el.strokeWidth];
    if (el.strokeStyle === 'dotted') o.strokeLineDash = [1.5, 6 + el.strokeWidth];
    if (el.type === 'rectangle' || el.type === 'iframe' || el.type === 'embeddable' ||
      el.type === 'diamond' || el.type === 'ellipse') {
      o.fillStyle = el.fillStyle;
      if (!isTransparent(el.backgroundColor)) o.fill = paint(el.backgroundColor, dark);
    } else if ((el.type === 'line' || el.type === 'freedraw') && isLoop(el.points)) {
      o.fillStyle = el.fillStyle;
      if (el.backgroundColor !== 'transparent') o.fill = paint(el.backgroundColor, dark);
    }
    if (el.type === 'ellipse') o.curveFitting = 1;
    return o;
  }

  // Corner radius for a rounded shape whose side is x: proportional
  // (roundness types 1 and 2) or adaptive (type 3, capped at its value).
  function cornerRadius(x, el) {
    var type = el.roundness && el.roundness.type;
    if (type === 1 || type === 2) return x * 0.25;
    if (type === 3) {
      var fixed = num(el.roundness.value, 32);
      return x <= fixed / 0.25 ? x * 0.25 : fixed;
    }
    return 0;
  }

  // An elbow arrow: straight runs joined by rounded corners
  // (Excalidraw's generateElbowArrowShape). A run from b to a is horizontal
  // when it heads right or left (vectorToHeading).
  function elbowPath(pts, radius) {
    var horizontal = function (a, b) {
      var x = a[0] - b[0];
      var absY = Math.abs(a[1] - b[1]);
      return x > absY || x <= -absY;
    };
    var sub = [];
    for (var i = 1; i < pts.length - 1; i++) {
      var prev = pts[i - 1];
      var p = pts[i];
      var next = pts[i + 1];
      var r = Math.min(radius, Math.hypot(next[0] - p[0], next[1] - p[1]) / 2, Math.hypot(prev[0] - p[0], prev[1] - p[1]) / 2);
      if (horizontal(p, prev)) sub.push([prev[0] < p[0] ? p[0] - r : p[0] + r, p[1]]);
      else sub.push([p[0], prev[1] < p[1] ? p[1] - r : p[1] + r]);
      sub.push(p);
      if (horizontal(next, p)) sub.push([next[0] < p[0] ? p[0] - r : p[0] + r, p[1]]);
      else sub.push([p[0], next[1] < p[1] ? p[1] - r : p[1] + r]);
    }
    var d = ['M ' + pts[0][0] + ' ' + pts[0][1]];
    for (var j = 0; j < sub.length; j += 3) {
      d.push('L ' + sub[j][0] + ' ' + sub[j][1]);
      d.push('Q ' + sub[j + 1][0] + ' ' + sub[j + 1][1] + ', ' + sub[j + 2][0] + ' ' + sub[j + 2][1]);
    }
    var last = pts[pts.length - 1];
    d.push('L ' + last[0] + ' ' + last[1]);
    return d.join(' ');
  }

  // --- arrowheads -------------------------------------------------------------

  // A port of Excalidraw's arrowheads (bounds.ts getArrowheadPoints and
  // shape.ts getArrowheadShapes): heads fit short last segments, and filled
  // heads are a single rough stroke filled along its own outline.

  var ELBOW_ARROW_CORNER_RADIUS = 16;
  var CARDINALITY_MARKER_SIZE = 20;
  var CROWFOOT_ARROWHEAD_SIZE = 15;
  // layout of the parts of composite cardinality markers, in multiples of
  // the part's size (negative offsets move a part toward the tip)
  var CARDINALITY_ONE_OR_MANY_BAR_OFFSET = -0.25;
  var CARDINALITY_INNER_BAR_OFFSET = -0.5;
  var CARDINALITY_ZERO_CIRCLE_OFFSET = 1.5;
  var CARDINALITY_ZERO_CIRCLE_SCALE = 0.8;
  var ARROW_ARROWHEAD_ANGLE = 20;
  // scaled down `arrow` heads open up to this angle so that they don't
  // collapse into the line, reaching it at ARROW_ARROWHEAD_WIDEST_SIZE
  var ARROW_ARROWHEAD_WIDEST_ANGLE = 30;
  var ARROW_ARROWHEAD_WIDEST_SIZE = 10;

  function clamp(value, min, max) {
    return Math.min(Math.max(value, min), max);
  }

  function arrowheadSize(head) {
    switch (head) {
      case 'arrow': return 25;
      case 'diamond':
      case 'diamond_outline': return 12;
      case 'cardinality_many':
      case 'cardinality_one_or_many':
      case 'cardinality_zero_or_many': return CROWFOOT_ARROWHEAD_SIZE;
      case 'cardinality_one':
      case 'cardinality_exactly_one':
      case 'cardinality_zero_or_one': return CARDINALITY_MARKER_SIZE;
      default: return 15;
    }
  }

  // In degrees; `size` is the size the head is drawn at, if scaled down.
  function arrowheadAngle(head, size) {
    if (size === undefined) size = arrowheadSize(head);
    switch (head) {
      case 'bar': return 90;
      case 'arrow': {
        var fullSize = arrowheadSize(head);
        var shrinkage = clamp((fullSize - size) / (fullSize - ARROW_ARROWHEAD_WIDEST_SIZE), 0, 1);
        return ARROW_ARROWHEAD_ANGLE + (ARROW_ARROWHEAD_WIDEST_ANGLE - ARROW_ARROWHEAD_ANGLE) * shrinkage;
      }
      default: return 25;
    }
  }

  // The last segment at the arrowhead's end and how much the arrow turns
  // into it from the previous one; null without a previous segment.
  function arrowheadCorner(el, position) {
    var pts = el.points;
    var n = pts.length;
    var tip = position === 'end' ? pts[n - 1] : pts[0];
    var corner = position === 'end' ? pts[n - 2] : pts[1];
    var prev = position === 'end' ? pts[n - 3] : pts[2];
    if (!tip || !corner || !prev) return null;
    var length = Math.hypot(corner[0] - tip[0], corner[1] - tip[1]);
    var prevLength = Math.hypot(prev[0] - corner[0], prev[1] - corner[1]);
    if (length === 0 || prevLength === 0) return null;
    // elbow arrows round their corners off, the arrow effectively turns
    // halfway into the rounding
    if (el.elbowed) {
      var rounding = Math.min(ELBOW_ARROW_CORNER_RADIUS, length / 2, prevLength / 2) / 2;
      length -= rounding;
      prevLength -= rounding;
    }
    var dot = (tip[0] - corner[0]) * (corner[0] - prev[0]) + (tip[1] - corner[1]) * (corner[1] - prev[1]);
    var turn = Math.acos(clamp(dot / (length * prevLength), -1, 1));
    return { length: length, prevLength: prevLength, turn: turn };
  }

  // Size up to which a head extending past the last segment's start keeps
  // its sides (at sideAngle to the last segment) clearance off the previous one.
  function sidesClearSize(corner, sideAngle, clearance) {
    var angle = (sideAngle * Math.PI) / 180;
    if (corner.turn <= angle) return Infinity;
    return (corner.length * Math.sin(corner.turn) - clearance) / Math.sin(corner.turn - angle);
  }

  function isCardinality(head) {
    return head.indexOf('cardinality_') === 0;
  }

  // Scale (at most 1) at which all parts of a cardinality marker fit on the
  // last segment.
  function cardinalityMarkerScale(marker, corner, strokeWidth) {
    var length = corner.length;
    var turn = corner.turn;
    var bar = function (offset) {
      var angle = (arrowheadAngle('cardinality_one') * Math.PI) / 180;
      return length / (arrowheadSize('cardinality_one') * (offset + Math.cos(angle) + Math.sin(angle) * Math.tan(turn / 2)));
    };
    var crowsFoot = function () { return length / arrowheadSize('cardinality_many'); };
    var circle = function () {
      return (length - ((strokeWidth - 2) * CARDINALITY_ZERO_CIRCLE_SCALE) / 2 - strokeWidth) /
        (arrowheadSize('circle_outline') * (CARDINALITY_ZERO_CIRCLE_OFFSET + CARDINALITY_ZERO_CIRCLE_SCALE / 2));
    };
    switch (marker) {
      case 'cardinality_one': return Math.min(1, bar(0));
      case 'cardinality_many': return Math.min(1, crowsFoot());
      case 'cardinality_one_or_many': return Math.min(1, crowsFoot(), bar(CARDINALITY_ONE_OR_MANY_BAR_OFFSET));
      case 'cardinality_exactly_one': return Math.min(1, bar(CARDINALITY_INNER_BAR_OFFSET), bar(0));
      case 'cardinality_zero_or_one': return Math.min(1, circle(), bar(CARDINALITY_INNER_BAR_OFFSET));
      case 'cardinality_zero_or_many': return Math.min(1, crowsFoot(), circle());
    }
    return 1;
  }

  // Size a head may grow to when the last segment is too short to fit it,
  // using up the whole segment or extending past its start; 0 to not grow.
  function arrowheadSizeBeyondLastSegment(el, position, head) {
    var corner = arrowheadCorner(el, position);
    if (!corner) return 0;
    var maxSize = (corner.length + corner.prevLength) / 2;
    var curved = !!el.roundness && !el.elbowed;
    var marker = position === 'start' ? el.startArrowhead : el.endArrowhead;
    if (marker && isCardinality(marker)) {
      return curved ? 0 : arrowheadSize(head) * cardinalityMarkerScale(marker, corner, el.strokeWidth);
    }
    if (head === 'circle' || head === 'circle_outline') return maxSize;
    if (curved) return 0;
    switch (head) {
      case 'bar':
        return Math.min(maxSize, corner.length / Math.tan(corner.turn / 2));
      case 'diamond':
      case 'diamond_outline':
        return corner.length / 2;
      case 'triangle':
      case 'triangle_outline':
        return Math.min(maxSize, sidesClearSize(corner, arrowheadAngle(head), 0));
      case 'arrow': {
        var legibleSize = arrowheadSize(head) / 2;
        return Math.min(
          legibleSize,
          maxSize,
          sidesClearSize(corner, arrowheadAngle(head, legibleSize), el.strokeWidth),
          sidesClearSize(corner, arrowheadAngle(head, 0), el.strokeWidth)
        );
      }
      default:
        return 0;
    }
  }

  function pathOps(drawable) {
    for (var i = 0; i < drawable.sets.length; i++) {
      if (drawable.sets[i].type === 'path') return drawable.sets[i].ops;
    }
    return drawable.sets[0].ops;
  }

  // Points of a head along the tangent of the drawn stroke's first or last
  // curve: [x, y, diameter] for circles; the tip and two wing points
  // otherwise (four points for a diamond). offsetMultiplier moves a part of
  // a composite marker back from the tip, in multiples of its size.
  function arrowheadPoints(el, shape, position, head, offsetMultiplier) {
    var ops = pathOps(shape);
    if (ops.length < 1) return null;
    var op = ops[position === 'start' ? 1 : ops.length - 1];
    if (!op || op.data.length !== 6) return null;
    var d = op.data;
    var p3 = [d[4], d[5]];
    var p2 = [d[2], d[3]];
    var p1 = [d[0], d[1]];
    var prevOp = ops[(position === 'start' ? 1 : ops.length - 1) - 1];
    var p0 = [0, 0];
    if (prevOp.op === 'move') p0 = [prevOp.data[0], prevOp.data[1]];
    else if (prevOp.op === 'bcurveTo') p0 = [prevOp.data[4], prevOp.data[5]];
    var equation = function (t, idx) {
      return Math.pow(1 - t, 3) * p3[idx] + 3 * t * Math.pow(1 - t, 2) * p2[idx] +
        3 * Math.pow(t, 2) * (1 - t) * p1[idx] + p0[idx] * Math.pow(t, 3);
    };
    var x2 = position === 'start' ? p0[0] : p3[0];
    var y2 = position === 'start' ? p0[1] : p3[1];
    var x1 = equation(0.3, 0);
    var y1 = equation(0.3, 1);
    var distance = Math.hypot(x2 - x1, y2 - y1);
    var nx = (x2 - x1) / distance;
    var ny = (y2 - y1) / distance;
    var size = arrowheadSize(head);

    var pts = el.points;
    var cur = position === 'end' ? pts[pts.length - 1] : pts[0];
    var before = pts.length > 1 ? (position === 'end' ? pts[pts.length - 2] : pts[1]) : [0, 0];
    var length = Math.hypot(cur[0] - before[0], cur[1] - before[1]);
    var lengthMultiplier = head === 'diamond' || head === 'diamond_outline' ? 0.25 : 0.5;
    var minSize = Math.min(size, length * lengthMultiplier);
    if (minSize < size) {
      minSize = Math.min(size, Math.max(minSize, arrowheadSizeBeyondLastSegment(el, position, head)));
    }
    var tx = x2 - nx * minSize * (offsetMultiplier || 0);
    var ty = y2 - ny * minSize * (offsetMultiplier || 0);
    var xs = tx - nx * minSize;
    var ys = ty - ny * minSize;

    if (head === 'circle' || head === 'circle_outline') {
      return [tx, ty, Math.hypot(ys - ty, xs - tx) + el.strokeWidth - 2];
    }
    var angle = arrowheadAngle(head, minSize);
    if (head === 'cardinality_many' || head === 'cardinality_one_or_many') {
      var c1 = rotate([tx, ty], [xs, ys], (-angle * Math.PI) / 180);
      var c2 = rotate([tx, ty], [xs, ys], (angle * Math.PI) / 180);
      return [xs, ys, c1[0], c1[1], c2[0], c2[1]];
    }
    var w1 = rotate([xs, ys], [tx, ty], (-angle * Math.PI) / 180);
    var w2 = rotate([xs, ys], [tx, ty], (angle * Math.PI) / 180);
    if (head === 'diamond' || head === 'diamond_outline') {
      var opposite;
      if (position === 'start') {
        var ps = pts.length > 1 ? pts[1] : [0, 0];
        opposite = rotate([tx + minSize * 2, ty], [tx, ty], Math.atan2(ps[1] - ty, ps[0] - tx));
      } else {
        var pe = pts.length > 1 ? pts[pts.length - 2] : [0, 0];
        opposite = rotate([tx - minSize * 2, ty], [tx, ty], Math.atan2(ty - pe[1], tx - pe[0]));
      }
      return [tx, ty, w1[0], w1[1], opposite[0], opposite[1], w2[0], w2[1]];
    }
    return [tx, ty, w1[0], w1[1], w2[0], w2[1]];
  }

  // Fill a head's outline (a polygon or circle drawn as one rough stroke)
  // along that stroke: rough.js jitters its own solid fill apart from the
  // outline, which shows on shapes this small.
  function fillOutline(outline, fill) {
    var ops = [];
    outline.sets.forEach(function (set) {
      set.ops.forEach(function (op) {
        ops.push(ops.length > 0 && op.op === 'move' ? { op: 'lineTo', data: op.data } : op);
      });
    });
    return {
      shape: outline.shape,
      options: Object.assign({}, outline.options, { fill: fill }),
      sets: [{ type: 'fillPath', ops: ops }].concat(outline.sets),
    };
  }

  // Line options for open heads: solid on dashed arrows, tighter dots on
  // dotted ones, never rougher than 1.
  function headLineOptions(el, options) {
    var o = Object.assign({}, options);
    if (el.strokeStyle === 'dotted') o.strokeLineDash = [1.5, 6 + (el.strokeWidth - 1) - 1];
    else delete o.strokeLineDash;
    o.roughness = Math.min(1, o.roughness || 0);
    return o;
  }

  // The drawables of one head. `options` are the arrow's rough options.
  function arrowheadShapes(el, shape, position, head, gen, options, stroke, background) {
    var pointsOf = function (part, offset) { return arrowheadPoints(el, shape, position, part, offset); };
    var circle = function (p, fill, scale) {
      if (!p) return [];
      var o = Object.assign({}, options, { stroke: stroke, roughness: Math.min(0.5, options.roughness || 0), disableMultiStroke: true });
      delete o.strokeLineDash;
      return [fillOutline(gen.circle(p[0], p[1], p[2] * (scale || 1), o), fill)];
    };
    var polygon = function (p, corners, fill) {
      if (!p) return [];
      var o = Object.assign({}, options, { roughness: Math.min(1, options.roughness || 0), disableMultiStroke: true, preserveVertices: true });
      delete o.strokeLineDash;
      var vertices = [];
      for (var i = 0; i < corners; i++) vertices.push([p[i * 2], p[i * 2 + 1]]);
      return [fillOutline(gen.polygon(vertices, o), fill)];
    };
    var bar = function (p, o) { return p ? [gen.line(p[2], p[3], p[4], p[5], o)] : []; };
    var linesToTip = function (p, o) { return p ? [gen.line(p[2], p[3], p[0], p[1], o), gen.line(p[4], p[5], p[0], p[1], o)] : []; };
    var lines = headLineOptions(el, options);
    switch (head) {
      case 'circle':
      case 'circle_outline':
        return circle(pointsOf(head), head === 'circle_outline' ? background : stroke);
      case 'triangle':
      case 'triangle_outline':
        return polygon(pointsOf(head), 3, head === 'triangle_outline' ? background : stroke);
      case 'diamond':
      case 'diamond_outline':
        return polygon(pointsOf(head), 4, head === 'diamond_outline' ? background : stroke);
      case 'cardinality_one':
        return bar(pointsOf(head), lines);
      case 'cardinality_many':
        return linesToTip(pointsOf(head), lines);
      case 'cardinality_one_or_many':
        return linesToTip(pointsOf('cardinality_many'), lines)
          .concat(bar(pointsOf('cardinality_one', CARDINALITY_ONE_OR_MANY_BAR_OFFSET), lines));
      case 'cardinality_exactly_one':
        return bar(pointsOf('cardinality_one', CARDINALITY_INNER_BAR_OFFSET), lines)
          .concat(bar(pointsOf('cardinality_one'), lines));
      case 'cardinality_zero_or_one':
        return circle(pointsOf('circle_outline', CARDINALITY_ZERO_CIRCLE_OFFSET), background, CARDINALITY_ZERO_CIRCLE_SCALE)
          .concat(bar(pointsOf('cardinality_one', CARDINALITY_INNER_BAR_OFFSET), lines));
      case 'cardinality_zero_or_many':
        return linesToTip(pointsOf('cardinality_many'), lines)
          .concat(circle(pointsOf('circle_outline', CARDINALITY_ZERO_CIRCLE_OFFSET), background, CARDINALITY_ZERO_CIRCLE_SCALE));
      default:
        return linesToTip(pointsOf(head), lines);
    }
  }

  // The drawables of a line or arrow: its stroke, then its heads
  // (Excalidraw's generateElementShape for linear elements).
  function linearShapes(el, gen, dark, background) {
    var options = roughOptions(el, false, dark);
    var pts = el.points.length ? el.points.map(function (p) { return [p[0], p[1]]; }) : [[0, 0]];
    var shape;
    if (el.elbowed) shape = gen.path(elbowPath(pts, ELBOW_ARROW_CORNER_RADIUS), roughOptions(el, true, dark));
    else if (!el.roundness) shape = options.fill ? gen.polygon(pts, options) : gen.linearPath(pts, options);
    else shape = gen.curve(pts, options);
    var shapes = [shape];
    if (el.type === 'arrow') {
      var stroke = paint(el.strokeColor, dark);
      var fill = paint(background, dark);
      if (el.startArrowhead) shapes = shapes.concat(arrowheadShapes(el, shape, 'start', el.startArrowhead, gen, options, stroke, fill));
      if (el.endArrowhead) shapes = shapes.concat(arrowheadShapes(el, shape, 'end', el.endArrowhead, gen, options, stroke, fill));
    }
    return shapes;
  }

  // --- drawing ----------------------------------------------------------------

  function svgEl(tag, attrs) {
    var node = document.createElementNS(SVG_NS, tag);
    for (var k in attrs) {
      if (attrs[k] !== null && attrs[k] !== undefined) node.setAttribute(k, attrs[k]);
    }
    return node;
  }

  // An invisible copy of an element's outline, so a click anywhere on the
  // element (not only on its drawn strokes) picks it for a comment. It is
  // the element's last child: on top, so it is what the pointer finds.
  // alongStroke takes only the outline (frames, whose inside belongs to the
  // elements in them).
  function hitArea(tag, attrs, alongStroke) {
    var node = svgEl(tag, attrs);
    node.setAttribute('data-excalidraw-hit', '');
    node.setAttribute('fill', 'none');
    if (alongStroke) {
      node.setAttribute('stroke', 'transparent');
      node.setAttribute('stroke-width', '12');
      node.setAttribute('pointer-events', 'stroke');
    } else {
      node.setAttribute('pointer-events', 'all');
    }
    return node;
  }

  // Path data for a band `half` wide either side of a polyline: one quad per
  // segment, all wound the same way so they union, and long enough to cover
  // the joins, so a click near a thin line or arrow still lands on it.
  function bandPath(points, half) {
    var r = function (v) { return Math.round(v * 10) / 10; };
    var d = [];
    for (var i = 1; i < points.length; i++) {
      var a = points[i - 1];
      var b = points[i];
      var len = Math.hypot(b[0] - a[0], b[1] - a[1]);
      if (!len) continue;
      var ux = (b[0] - a[0]) / len * half;
      var uy = (b[1] - a[1]) / len * half;
      d.push('M', r(a[0] - ux - uy), r(a[1] - uy + ux), 'L', r(b[0] + ux - uy), r(b[1] + uy + ux),
        'L', r(b[0] + ux + uy), r(b[1] + uy - ux), 'L', r(a[0] - ux + uy), r(a[1] - uy - ux), 'Z');
    }
    if (!d.length && points.length) {
      var p = points[0];
      d.push('M', r(p[0] - half), r(p[1] - half), 'h', half * 2, 'v', half * 2, 'h', -half * 2, 'Z');
    }
    return d.join(' ');
  }

  // The points a line is drawn through: its own for straight and elbow
  // lines, samples of the smooth curve for curved ones.
  function linePoints(el, gen) {
    if (!el.roundness || el.elbowed || el.points.length < 3) return el.points;
    var ops = gen.curve(el.points.map(function (p) { return [p[0], p[1]]; }),
      { roughness: 0, disableMultiStroke: true, preserveVertices: true, seed: 1 }).sets[0].ops;
    var out = [];
    ops.forEach(function (op) {
      if (op.op === 'move') { out.push([op.data[0], op.data[1]]); return; }
      if (op.op !== 'bcurveTo') return;
      var p0 = out[out.length - 1];
      var d = op.data;
      for (var t = 1; t <= 8; t++) {
        var s = t / 8;
        var k = 1 - s;
        out.push([
          k * k * k * p0[0] + 3 * k * k * s * d[0] + 3 * k * s * s * d[2] + s * s * s * d[4],
          k * k * k * p0[1] + 3 * k * k * s * d[1] + 3 * k * s * s * d[3] + s * s * s * d[5],
        ]);
      }
    });
    return out.length ? out : el.points;
  }

  function lineHitArea(el, points) {
    return hitArea('path', { d: bandPath(points, Math.max(6, el.strokeWidth * 2)) });
  }

  function polylinePoints(pts) {
    return pts.map(function (p) { return p[0] + ',' + p[1]; }).join(' ');
  }

  function drawRectangle(g, el, c) {
    var w = el.width;
    var h = el.height;
    var shape;
    if (el.roundness) {
      var r = cornerRadius(Math.min(w, h), el);
      shape = c.rs.generator.path(['M', r, 0, 'L', w - r, 0, 'Q', w, 0, w, r, 'L', w, h - r, 'Q', w, h, w - r, h,
        'L', r, h, 'Q', 0, h, 0, h - r, 'L', 0, r, 'Q', 0, 0, r, 0].join(' '), roughOptions(el, true, c.dark));
    } else {
      shape = c.rs.generator.rectangle(0, 0, w, h, roughOptions(el, false, c.dark));
    }
    g.appendChild(c.rs.draw(shape));
    g.appendChild(hitArea('rect', { width: w, height: h }));
  }

  function drawDiamond(g, el, c) {
    var w = el.width;
    var h = el.height;
    var tx = Math.floor(w / 2) + 1;
    var ry = Math.floor(h / 2) + 1;
    var top = [tx, 0];
    var right = [w, ry];
    var bottom = [tx, h];
    var left = [0, ry];
    var shape;
    if (el.roundness) {
      var vr = cornerRadius(Math.abs(top[0] - left[0]), el);
      var hr = cornerRadius(Math.abs(right[1] - top[1]), el);
      shape = c.rs.generator.path([
        'M', top[0] + vr, top[1] + hr, 'L', right[0] - vr, right[1] - hr,
        'C', right[0], right[1], right[0], right[1], right[0] - vr, right[1] + hr,
        'L', bottom[0] + vr, bottom[1] - hr,
        'C', bottom[0], bottom[1], bottom[0], bottom[1], bottom[0] - vr, bottom[1] - hr,
        'L', left[0] + vr, left[1] + hr,
        'C', left[0], left[1], left[0], left[1], left[0] + vr, left[1] - hr,
        'L', top[0] - vr, top[1] + hr,
        'C', top[0], top[1], top[0], top[1], top[0] + vr, top[1] + hr,
      ].join(' '), roughOptions(el, true, c.dark));
    } else {
      shape = c.rs.generator.polygon([top, right, bottom, left], roughOptions(el, false, c.dark));
    }
    g.appendChild(c.rs.draw(shape));
    g.appendChild(hitArea('polygon', { points: polylinePoints([top, right, bottom, left]) }));
  }

  function drawEllipse(g, el, c) {
    var w = el.width;
    var h = el.height;
    g.appendChild(c.rs.draw(c.rs.generator.ellipse(w / 2, h / 2, w, h, roughOptions(el, false, c.dark))));
    g.appendChild(hitArea('ellipse', { cx: w / 2, cy: h / 2, rx: Math.abs(w / 2), ry: Math.abs(h / 2) }));
  }

  function drawLinear(g, el, c) {
    var shapes = linearShapes(el, c.rs.generator, c.dark, c.background);
    shapes.forEach(function (shape) { g.appendChild(c.rs.draw(shape)); });
    g.appendChild(shapes[0].options.fill
      ? hitArea('polygon', { points: polylinePoints(el.points) })
      : lineHitArea(el, linePoints(el, c.rs.generator)));
  }

  // Excalidraw's seededRandom (a mulberry32 generator).
  function seededRandom(seed) {
    var value = seed >>> 0;
    return function () {
      value += 0x6d2b79f5;
      var next = value;
      next = Math.imul(next ^ (next >>> 15), next | 1);
      next ^= next + Math.imul(next ^ (next >>> 7), next | 61);
      return ((next ^ (next >>> 14)) >>> 0) / 4294967296;
    };
  }

  function pointAtDistance(from, to, distance) {
    var dx = to.x - from.x;
    var dy = to.y - from.y;
    var length = Math.hypot(dx, dy);
    if (!length) return from;
    var ratio = Math.min(distance / length, 1);
    return { x: from.x + dx * ratio, y: from.y + dy * ratio };
  }

  // The four corners of a sticky note's paper, jittered by its roughness
  // (Excalidraw's getStickyNoteRenderPoints).
  function stickyNoteCorners(el, offset, seedOffset) {
    var roughness = Math.max(0, Math.min(2, Math.round(el.roughness)));
    var amount = Math.min(STICKY_NOTE.roughness[roughness], Math.min(el.width, el.height) * 0.012);
    var o = offset || 0;
    if (!amount) {
      return [{ x: o, y: o }, { x: o + el.width, y: o }, { x: o + el.width, y: o + el.height }, { x: o, y: o + el.height }];
    }
    var random = seededRandom(el.seed + (seedOffset || 0));
    var jitter = function () { return (random() * 2 - 1) * amount; };
    // evaluation order matters: each corner takes x, then y
    var corners = [];
    [[0, 0], [el.width, 0], [el.width, el.height], [0, el.height]].forEach(function (c) {
      var x = o + c[0] + jitter();
      var y = o + c[1] + jitter();
      corners.push({ x: x, y: y });
    });
    return corners;
  }

  // Path commands of a sticky note's paper, or of its shadow: rounded
  // corners, and on the roughest notes one corner lifted off the page
  // (Excalidraw's getStickyNotePathCommands).
  function stickyNoteCommands(el, shadow) {
    var points = shadow ? stickyNoteCorners(el, STICKY_NOTE.shadowOffset, 1) : stickyNoteCorners(el);
    var radius = el.roundness
      ? Math.min(Math.min(el.width, el.height) * STICKY_NOTE.cornerRadiusRatio, STICKY_NOTE.maxCornerRadius)
      : 0;
    var lifted = el.roughness === 2 ? Math.floor(seededRandom(el.seed)() * points.length) : -1;
    if (!radius && lifted === -1) {
      return [{ type: 'move', point: points[0] }].concat(points.slice(1).map(function (p) { return { type: 'line', point: p }; }));
    }
    var corners = points.map(function (point, index) {
      var prev = points[(index + points.length - 1) % points.length];
      var next = points[(index + 1) % points.length];
      var r = Math.min(radius, Math.hypot(point.x - prev.x, point.y - prev.y) / 2, Math.hypot(point.x - next.x, point.y - next.y) / 2);
      if (index === lifted) {
        var size = Math.min(el.width, el.height);
        var reach = Math.min(size * 0.18, 40);
        var lift = Math.min(size * 0.02, 5) * (shadow ? 0.5 : 1);
        var tip = {
          x: point.x + (index === 1 || index === 2 ? -lift : lift),
          y: point.y + (index >= 2 ? -lift : lift),
        };
        var start = pointAtDistance(point, prev, reach);
        var end = pointAtDistance(point, next, reach);
        return [
          { type: 'line', point: start },
          { type: 'quadratic', control: pointAtDistance(point, prev, reach / 2), point: pointAtDistance(tip, start, r) },
          { type: 'quadratic', control: tip, point: pointAtDistance(tip, end, r) },
          { type: 'quadratic', control: pointAtDistance(point, next, reach / 2), point: end },
        ];
      }
      return [
        { type: 'line', point: pointAtDistance(point, prev, r) },
        { type: 'quadratic', control: point, point: pointAtDistance(point, next, r) },
      ];
    });
    var commands = [{ type: 'move', point: corners[0][corners[0].length - 1].point }];
    for (var i = 1; i <= corners.length; i++) commands = commands.concat(corners[i % corners.length]);
    return commands;
  }

  function commandsPath(commands) {
    return commands.map(function (c) {
      if (c.type === 'move') return 'M ' + c.point.x + ' ' + c.point.y;
      if (c.type === 'line') return 'L ' + c.point.x + ' ' + c.point.y;
      return 'Q ' + c.control.x + ' ' + c.control.y + ' ' + c.point.x + ' ' + c.point.y;
    }).join(' ').concat(' Z');
  }

  // The creation date in a note's footer: "7 Sep", with the year when it is
  // not this year and the note is wide enough (getStickyNoteFooter).
  function stickyNoteFooter(el, now) {
    if (el.width < STICKY_NOTE.minSize || el.height < STICKY_NOTE.minSize) return null;
    if (el.created === null || !isFinite(el.created)) return null;
    var date = new Date(el.created);
    if (isNaN(date.getTime())) return null;
    var label = date.getDate() + ' ' + MONTHS[date.getMonth()];
    var short = el.width - STICKY_NOTE.padding * 2 < STICKY_NOTE.footer.minBodyWidthForYear;
    var year = date.getFullYear();
    var text = short || year === new Date(now === undefined ? Date.now() : now).getFullYear() ? label : label + ' ' + year;
    return { text: text, x: el.width - STICKY_NOTE.padding, y: el.height - STICKY_NOTE.footer.baselineFromBottom };
  }

  // A sticky note: shadow, paper, a faint inner edge, and the footer date.
  // Its label is a text element bound to it, drawn as text.
  function drawStickyNote(g, el, c) {
    var paper = stickyNoteCommands(el);
    var shadow = svgEl('path', {
      d: commandsPath(stickyNoteCommands(el, true)), fill: '#000', 'fill-opacity': STICKY_NOTE.shadowOpacity, stroke: 'none',
    });
    var clipId = c.idPrefix + 'note-' + el.index;
    var clip = svgEl('clipPath', { id: clipId, clipPathUnits: 'userSpaceOnUse' });
    clip.appendChild(svgEl('path', { d: commandsPath(paper), fill: '#000', stroke: 'none' }));
    g.appendChild(clip);
    g.appendChild(shadow);
    g.appendChild(svgEl('path', { d: commandsPath(paper), fill: paint(el.backgroundColor, c.dark), stroke: 'none' }));
    g.appendChild(svgEl('path', {
      d: commandsPath(paper), fill: 'none', stroke: '#000', 'stroke-opacity': STICKY_NOTE.edgeShadowOpacity,
      'stroke-width': STICKY_NOTE.edgeShadowWidth * 2, 'clip-path': 'url(#' + clipId + ')',
    }));
    var footer = stickyNoteFooter(el);
    if (footer) {
      var date = svgEl('text', {
        x: footer.x, y: footer.y, 'font-family': STICKY_NOTE.footer.fontFamily, 'font-size': STICKY_NOTE.footer.fontSize + 'px',
        'text-anchor': 'end', direction: 'ltr', fill: paint(el.strokeColor, c.dark), 'fill-opacity': STICKY_NOTE.footer.opacity,
      });
      date.textContent = footer.text;
      g.appendChild(date);
    }
    g.appendChild(hitArea('rect', { width: el.width, height: el.height }));
  }

  // The outline of a freedraw stroke (Excalidraw's getFreedrawOutlinePoints):
  // pressure-sensitive through perfect-freehand, or of constant width through
  // Excalidraw's laser-pointer geometry.
  function freedrawOutline(el, freehand) {
    if (el.variability === 'constant') {
      var laser = new freehand.LaserPointer({
        size: el.strokeWidth * 1.4,
        streamline: el.streamline,
        simplify: 0,
        sizeMapping: function (details) { return Math.max(0.1, details.pressure); },
      });
      el.points.forEach(function (p) { laser.addPoint([p[0], p[1], 1]); });
      return laser.getStrokeOutline().map(function (p) { return [p[0], p[1]]; });
    }
    // An empty stroke is a dot, unless its pressure is simulated.
    var input = el.simulatePressure ? el.points
      : el.points.length ? el.points.map(function (p, i) { return [p[0], p[1], el.pressures[i]]; })
        : [[0, 0, 0.5]];
    return freehand.getStroke(input, {
      simulatePressure: el.simulatePressure,
      size: el.strokeWidth * 4.25,
      thinning: 0.6,
      smoothing: 0.5,
      streamline: el.streamline,
      easing: function (t) { return Math.sin((t * Math.PI) / 2); },
      last: true,
    });
  }

  // Path data for an outline: quadratic curves through the midpoints, with
  // numbers cut to two decimals (Excalidraw's getSvgPathFromStroke).
  var TO_FIXED_PRECISION = /(\s?[A-Z]?,?-?[0-9]*\.[0-9]{0,2})(([0-9]|e|-)*)/g;

  function outlinePath(points) {
    if (!points.length) return '';
    var max = points.length - 1;
    var mid = function (a, b) { return [(a[0] + b[0]) / 2, (a[1] + b[1]) / 2]; };
    return points.reduce(function (acc, point, i, arr) {
      if (i === max) acc.push(point, mid(point, arr[0]), 'L', arr[0], 'Z');
      else acc.push(point, mid(point, arr[i + 1]));
      return acc;
    }, ['M', points[0], 'Q']).join(' ').replace(TO_FIXED_PRECISION, '$1');
  }

  // A freedraw stroke is its outline filled with the stroke color, over the
  // rough fill of a closed loop.
  function drawFreedraw(g, el, c) {
    var o = roughOptions(el, false, c.dark);
    if (o.fill) {
      o.stroke = 'none';
      g.appendChild(c.rs.draw(c.rs.generator.curve(c.freehand.simplify(el.points, 0.75), o)));
    }
    g.appendChild(svgEl('path', { d: outlinePath(freedrawOutline(el, c.freehand)), fill: paint(el.strokeColor, c.dark) }));
    // Simplified first: a band per raw point would be a long outline.
    g.appendChild(lineHitArea(el, el.points.length > 2 ? c.freehand.simplify(el.points, 2) : el.points));
  }

  // Text lines at the baselines Excalidraw computes from the font's metrics
  // (getVerticalOffset), so labels sit where the editor put them.
  function drawText(g, el, c) {
    var font = FONTS[el.fontFamily];
    var lineHeightPx = el.fontSize * el.lineHeight;
    var em = el.fontSize / font.em;
    var baseline = em * font.ascender + (lineHeightPx - em * font.ascender + em * font.descender) / 2;
    var rtl = RTL.test(el.text);
    var x = el.textAlign === 'center' ? el.width / 2 : el.textAlign === 'right' ? el.width : 0;
    var anchor = el.textAlign === 'center' ? 'middle' : el.textAlign === 'right' || rtl ? 'end' : 'start';
    // A label on an arrow hides the arrow behind it, as in the editor.
    if (el.containerId && c.arrowIds[el.containerId]) {
      g.appendChild(svgEl('rect', { x: -4, y: -4, width: el.width + 8, height: el.height + 8, fill: paint(c.background, c.dark) }));
    }
    el.text.split('\n').forEach(function (line, i) {
      var t = svgEl('text', {
        x: x, y: i * lineHeightPx + baseline, 'font-family': font.family, 'font-size': el.fontSize + 'px',
        fill: paint(el.strokeColor, c.dark), 'text-anchor': anchor, direction: rtl ? 'rtl' : 'ltr', 'dominant-baseline': 'alphabetic',
        style: 'white-space: pre',
      });
      t.textContent = line;
      g.appendChild(t);
    });
    g.appendChild(hitArea('rect', { width: el.width, height: el.height }));
    // anchorFor quotes the first line, so a comment says which text it is on.
    var first = el.text.split('\n').map(function (l) { return l.trim(); }).filter(Boolean)[0];
    if (first) g.setAttribute('data-excalidraw-quote', first);
  }

  function drawImage(g, el, c) {
    var file = el.fileId ? c.files[el.fileId] : null;
    var url = file && typeof file.dataURL === 'string' && /^data:image\/[\w.+-]+[;,]/i.test(file.dataURL) ? file.dataURL : null;
    var w = el.width;
    var h = el.height;
    if (!url) {
      g.appendChild(svgEl('rect', { width: w, height: h, fill: 'none', stroke: paint(el.strokeColor, c.dark), 'stroke-dasharray': '6 6' }));
    } else {
      var img = svgEl('image', { href: url, preserveAspectRatio: 'none' });
      if (c.dark && file.mimeType === 'image/svg+xml') img.style.filter = DARK_THEME_FILTER;
      var node = img;
      var crop = el.crop;
      if (crop && num(crop.width, 0) > 0 && num(crop.height, 0) > 0) {
        img.setAttribute('width', num(crop.naturalWidth, w));
        img.setAttribute('height', num(crop.naturalHeight, h));
        node = svgEl('svg', { width: w, height: h, viewBox: [num(crop.x, 0), num(crop.y, 0), crop.width, crop.height].join(' '), preserveAspectRatio: 'none' });
        node.appendChild(img);
      } else {
        img.setAttribute('width', w);
        img.setAttribute('height', h);
      }
      if (el.scale[0] !== 1 || el.scale[1] !== 1) {
        var flip = svgEl('g', {
          transform: 'translate(' + (el.scale[0] < 0 ? w : 0) + ' ' + (el.scale[1] < 0 ? h : 0) + ') scale(' + el.scale[0] + ' ' + el.scale[1] + ')',
        });
        flip.appendChild(node);
        node = flip;
      }
      g.appendChild(node);
    }
    g.appendChild(hitArea('rect', { width: w, height: h }));
  }

  // A frame is an outline with its name above; its inside stays clickable
  // for the elements in it.
  function drawFrame(g, el, c) {
    g.appendChild(svgEl('rect', { width: el.width, height: el.height, rx: 8, ry: 8, fill: 'none', stroke: paint(FRAME_STROKE, c.dark), 'stroke-width': 2 }));
    var name = svgEl('text', {
      x: 0, y: -6, 'font-family': FRAME_FONT, 'font-size': '14px', style: 'white-space: pre',
      fill: c.dark ? FRAME_NAME_COLOR.dark : FRAME_NAME_COLOR.light,
    });
    name.textContent = el.name;
    g.appendChild(name);
    g.appendChild(hitArea('rect', { width: el.width, height: el.height, rx: 8, ry: 8 }, true));
  }

  var DRAW = {
    rectangle: drawRectangle,
    embeddable: drawRectangle,
    iframe: drawRectangle,
    diamond: drawDiamond,
    ellipse: drawEllipse,
    line: drawLinear,
    arrow: drawLinear,
    freedraw: drawFreedraw,
    text: drawText,
    image: drawImage,
    frame: drawFrame,
    magicframe: drawFrame,
    stickynote: drawStickyNote,
  };

  var renderSeq = 0;

  // The scene as an <svg> with its background, PADDING around the elements.
  // Each element is a <g data-excalidraw-index> holding what draws it.
  // freehand (window.CritFreehand) is needed only for freedraw elements.
  function drawScene(scene, rough, theme, freehand) {
    var b = sceneBounds(scene.elements);
    var width = Math.ceil(b.maxX - b.minX + PADDING * 2);
    var height = Math.ceil(b.maxY - b.minY + PADDING * 2);
    var svg = svgEl('svg', {
      viewBox: '0 0 ' + width + ' ' + height, width: width, height: height,
      role: 'img', 'aria-label': 'Excalidraw diagram',
    });
    var dark = theme === 'dark';
    svg.appendChild(svgEl('rect', { width: width, height: height, fill: scene.background === 'transparent' ? 'none' : paint(scene.background, dark) }));
    var c = {
      rs: rough.svg(svg),
      freehand: freehand,
      background: scene.background,
      files: scene.files,
      dark: dark,
      // unique ids for the clip paths of this render
      idPrefix: 'crit-excalidraw-' + (++renderSeq) + '-',
      arrowIds: {},
    };
    scene.elements.forEach(function (el) {
      if (el.type === 'arrow') c.arrowIds[el.id] = true;
    });
    scene.elements.forEach(function (el) {
      var box = localBox(el);
      var transform = 'translate(' + (el.x - b.minX + PADDING) + ' ' + (el.y - b.minY + PADDING) + ')';
      if (el.angle) {
        transform += ' rotate(' + (el.angle * 180 / Math.PI) + ' ' + (box.x1 + box.x2) / 2 + ' ' + (box.y1 + box.y2) / 2 + ')';
      }
      var g = svgEl('g', { 'data-excalidraw-index': el.index, transform: transform });
      if (el.opacity < 100) g.setAttribute('opacity', Math.max(0, el.opacity) / 100);
      DRAW[el.type](g, el, c);
      svg.appendChild(g);
    });
    return svg;
  }

  // One shared load per script, started by the first render that needs it.
  // ready() returns the library once the script has run. A failed load
  // rejects (the target shows its source and the error) and is retried by
  // the next render.
  var loading = {};

  function loadScript(src, ready) {
    if (ready()) return Promise.resolve(ready());
    if (!loading[src]) {
      loading[src] = new Promise(function (resolve, reject) {
        var s = document.createElement('script');
        s.src = src;
        s.onload = function () {
          if (ready()) resolve(ready());
          else { delete loading[src]; reject(new Error(src + ' did not load')); }
        };
        s.onerror = function () {
          delete loading[src];
          s.remove();
          reject(new Error('Could not load ' + src));
        };
        document.head.appendChild(s);
      });
    }
    return loading[src];
  }

  function loadRough() {
    return loadScript(ASSET_BASE + 'rough.min.js', function () {
      return window.rough && typeof window.rough.svg === 'function' ? window.rough : null;
    });
  }

  function loadFreehand() {
    return loadScript(ASSET_BASE + 'freehand.min.js', function () {
      var f = window.CritFreehand;
      return f && typeof f.getStroke === 'function' ? f : null;
    });
  }

  var fontsAdded = false;

  // Register Excalidraw's fonts with the page once. A FontFace downloads
  // nothing until text needs a character in its range.
  function addFonts() {
    if (fontsAdded) return true;
    if (typeof FontFace === 'undefined' || !document.fonts) return false;
    fontsAdded = true;
    Object.keys(FONT_FILES).forEach(function (name) {
      FONT_FILES[name].forEach(function (face) {
        var descriptors = {};
        if (face.range) descriptors.unicodeRange = face.range;
        if (face.weight) descriptors.weight = face.weight;
        document.fonts.add(new FontFace(name, 'url(' + ASSET_BASE + FONT_DIR + face.file + ')', descriptors));
      });
    });
    return true;
  }

  // Load the font subsets the scene's text uses, so labels are not drawn in
  // a fallback font first. A font that fails, or takes longer than
  // FONT_WAIT_MS, is left to the fallback; the browser swaps it in when it
  // arrives.
  function loadFonts(scene) {
    if (!addFonts()) return Promise.resolve();
    var text = {};
    scene.elements.forEach(function (el) {
      var name = el.type === 'text' && FONTS[el.fontFamily].name;
      if (name && FONT_FILES[name]) text[name] = (text[name] || '') + el.text;
    });
    var loads = Object.keys(text).map(function (name) {
      return document.fonts.load('16px "' + name + '"', text[name]).catch(function () { /* fallback font */ });
    });
    if (!loads.length) return Promise.resolve();
    return Promise.race([
      Promise.all(loads),
      new Promise(function (resolve) { setTimeout(resolve, FONT_WAIT_MS); }),
    ]);
  }

  // Draw a scene as an <svg> once rough.js, freehand.min.js (for freedraw)
  // and the fonts its text uses are loaded. Parses first, so a source that
  // is not a scene throws without a download.
  function render(source, ctx) {
    var scene = parseScene(source);
    var freedraw = scene.elements.some(function (el) { return el.type === 'freedraw'; });
    return Promise.all([
      loadRough(),
      freedraw ? loadFreehand() : null,
      loadFonts(scene),
    ]).then(function (libs) {
      return drawScene(scene, libs[0], ctx.theme, libs[1]);
    });
  }

  // --- anchors ----------------------------------------------------------------

  // Line spans of the objects in the scene's top-level "elements" array, in
  // array order: [{startLine, endLine}], 1-based. A scan rather than a parse,
  // since JSON.parse keeps no positions.
  function elementSpans(source) {
    var spans = [];
    var stack = [];
    var line = 1;
    var inString = false;
    var stringStart = 0;
    var lastString = null;
    var key = null;
    var arrayDepth = -1;
    var open = 0;
    for (var i = 0; i < source.length; i++) {
      var ch = source[i];
      if (ch === '\n') { line++; continue; }
      if (inString) {
        if (ch === '\\') i++;
        else if (ch === '"') { inString = false; lastString = source.slice(stringStart, i); }
        continue;
      }
      if (ch === '"') { inString = true; stringStart = i + 1; }
      else if (ch === ':' && stack.length === 1) key = lastString;
      else if (ch === ',' && stack.length === 1) key = null;
      else if (ch === '{' || ch === '[') {
        if (ch === '[' && stack.length === 1 && stack[0] === '{' && key === 'elements') arrayDepth = 2;
        if (ch === '{' && stack.length === arrayDepth) open = line;
        stack.push(ch);
      } else if (ch === '}' || ch === ']') {
        stack.pop();
        if (ch === '}' && stack.length === arrayDepth) spans.push({ startLine: open, endLine: line });
        if (ch === ']' && stack.length === 1 && arrayDepth === 2) arrayDepth = -1;
      }
    }
    return spans;
  }

  var spanCache = { source: null, spans: null };

  // Map a click to the lines of the element drawn there: shapes and arrows
  // by their own JSON object, text with its first line as the quote.
  function anchorFor(el, ctx) {
    var node = el && el.closest ? el.closest('[data-excalidraw-index]') : null;
    if (!node) return null;
    if (spanCache.source !== ctx.source) spanCache = { source: ctx.source, spans: elementSpans(String(ctx.source || '')) };
    var span = spanCache.spans[Number(node.getAttribute('data-excalidraw-index'))];
    if (!span) return null;
    var range = { startLine: span.startLine, endLine: span.endLine };
    var quote = node.getAttribute('data-excalidraw-quote');
    if (quote) range.quote = quote;
    return range;
  }

  var fence = {
    kind: 'fence',
    name: 'excalidraw',
    langs: ['excalidraw'],
    render: render,
    zoomable: true,
  };

  var file = {
    kind: 'file',
    name: 'excalidraw',
    label: 'Diagram',
    paths: ['*.excalidraw', '*.excalidraw.json'],
    render: render,
    anchorFor: anchorFor,
    zoomable: true,
  };

  if (typeof window !== 'undefined' && window.crit && window.crit.renderers) {
    window.crit.renderers.register(fence);
    window.crit.renderers.register(file);
  }
  if (typeof module === 'object' && module.exports) {
    module.exports = {
      fence: fence,
      file: file,
      anchorFor: anchorFor,
      parseScene: parseScene,
      sceneBounds: sceneBounds,
      elementSpans: elementSpans,
      drawScene: drawScene,
      bandPath: bandPath,
      freedrawOutline: freedrawOutline,
      linearShapes: linearShapes,
      darkColor: darkColor,
      stickyNoteCommands: stickyNoteCommands,
      stickyNoteFooter: stickyNoteFooter,
      outlinePath: outlinePath,
      fonts: FONTS,
      fontFiles: FONT_FILES,
    };
  }
})();
