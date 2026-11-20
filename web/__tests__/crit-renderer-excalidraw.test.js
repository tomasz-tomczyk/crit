'use strict';
// Built-in Excalidraw renderer: registrations, scene parsing and defaults,
// bounds, the element → JSON line spans that anchorFor comments on, freedraw
// outlines, the vendored fonts, and the Source view's JSON highlighting.
// Drawing needs a DOM and rough.js; the E2E suite covers it
// (renderers.multifile.spec.ts).

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const excalidraw = require('../crit-renderer-excalidraw.js');

// freehand.min.js as the page loads it: an IIFE that sets window.CritFreehand.
function loadFreehand() {
  const sandbox = { window: {} };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '..', 'freehand.min.js'), 'utf8'), sandbox);
  return sandbox.window.CritFreehand;
}

// rough.min.js as the page loads it: a script that defines `rough`.
function loadRough() {
  const sandbox = {};
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '..', 'rough.min.js'), 'utf8') + '\nthis.rough = rough;', sandbox);
  return sandbox.rough;
}

function arrowEl(points, startArrowhead, endArrowhead, extra) {
  return excalidraw.parseScene(scene([Object.assign({ id: 'a', type: 'arrow', x: 0, y: 0, seed: 3, points, startArrowhead, endArrowhead }, extra)])).elements[0];
}

function freshRegistry() {
  delete require.cache[require.resolve('../crit-renderers.js')];
  return require('../crit-renderers.js');
}

function scene(elements, extra) {
  return JSON.stringify(Object.assign({ type: 'excalidraw', version: 2, source: 'test', elements: elements }, extra), null, 2);
}

const box = { id: 'box', type: 'rectangle', x: 10, y: 20, width: 100, height: 50, seed: 7 };
const label = { id: 'label', type: 'text', x: 20, y: 32, width: 80, height: 25, text: 'Auth {service}\nv2', containerId: 'box' };
const arrow = { id: 'arrow', type: 'arrow', x: 110, y: 45, points: [[0, 0], [90, -5]], endArrowhead: 'arrow' };

test('excalidraw claims ```excalidraw fences and .excalidraw files at any depth', () => {
  const r = freshRegistry();
  r.register(excalidraw.fence);
  r.register(excalidraw.file);
  assert.equal(r.forFence('excalidraw').name, 'excalidraw');
  assert.equal(r.forFence('Excalidraw').name, 'excalidraw');
  for (const p of ['arch.excalidraw', 'docs/plans/arch.excalidraw', 'flow.excalidraw.json', 'a/b/flow.excalidraw.json']) {
    assert.equal(r.forFile(p) && r.forFile(p).name, 'excalidraw', p);
  }
  // Exports and other formats keep the plain code view.
  for (const p of ['arch.json', 'arch.excalidraw.svg', 'arch.excalidraw.png', 'arch.excalidraw.md', 'excalidraw']) {
    assert.equal(r.forFile(p), null, p);
  }
  assert.equal(r.byName('file', 'excalidraw').label, 'Diagram');
  assert.equal(r.byName('file', 'excalidraw').zoomable, true);
  assert.equal(r.byName('fence', 'excalidraw').zoomable, true);
  assert.equal(typeof r.byName('file', 'excalidraw').anchorFor, 'function');
  assert.equal(r.byName('fence', 'excalidraw').anchorFor, null, 'fence comments attach to the whole block');
});

test('parseScene rejects text that is not a scene', () => {
  assert.throws(() => excalidraw.parseScene('{"elements": ['), /not valid JSON/);
  assert.throws(() => excalidraw.parseScene('{"type": "excalidraw"}'), /no "elements" array/);
  assert.throws(() => excalidraw.parseScene('[]'), /no "elements" array/);
  assert.throws(() => excalidraw.parseScene(scene([])), /no elements/);
  assert.throws(() => excalidraw.parseScene(scene([Object.assign({}, box, { isDeleted: true })])), /no elements/);
});

test('render fails on a bad source before loading rough.js', () => {
  assert.throws(() => excalidraw.file.render('not json', { theme: 'light' }), /not valid JSON/);
});

test('parseScene keeps each element\'s array index past deleted and unknown elements', () => {
  const s = excalidraw.parseScene(scene([
    Object.assign({}, box, { isDeleted: true }),
    { id: 'odd', type: 'sticker', x: 0, y: 0 },
    label,
    arrow,
  ]));
  assert.deepEqual(s.elements.map(e => [e.id, e.index]), [['label', 2], ['arrow', 3]]);
  assert.equal(s.background, '#ffffff');
  assert.deepEqual(s.files, {});
});

test('parseScene fills Excalidraw defaults for fields a scene leaves out', () => {
  const s = excalidraw.parseScene(scene([
    { id: 'r', type: 'rectangle', x: 0, y: 0, width: 10, height: 10 },
    { id: 'a', type: 'arrow', x: 0, y: 0, width: 30, height: 40 },
    { id: 'n', type: 'arrow', x: 0, y: 0, points: [[0, 0], [5, 5]], endArrowhead: null, startArrowhead: 'dot' },
    { id: 't', type: 'text', x: 0, y: 0, text: 'abcd\nab', fontSize: 20 },
  ], { appState: { viewBackgroundColor: '#fffce8' } }));
  const [r, a, n, t] = s.elements;
  assert.equal(r.strokeColor, '#1e1e1e');
  assert.equal(r.strokeWidth, 2);
  assert.equal(r.roughness, 1);
  assert.ok(r.seed > 0, 'a missing seed is derived from the id');
  assert.equal(r.seed, excalidraw.parseScene(scene([{ id: 'r', type: 'rectangle', x: 5, y: 5 }])).elements[0].seed);
  assert.deepEqual(a.points, [[0, 0], [30, 40]], 'an arrow without points spans its box');
  assert.equal(a.endArrowhead, 'arrow', 'a missing end arrowhead is the default arrow');
  assert.equal(a.startArrowhead, null);
  assert.equal(n.endArrowhead, null, 'null means no arrowhead');
  assert.equal(n.startArrowhead, 'circle', 'legacy names are renamed as Excalidraw restores them');
  const legacy = excalidraw.parseScene(scene([
    { id: 'l', type: 'arrow', x: 0, y: 0, points: [[0, 0], [5, 5]], startArrowhead: 'crowfoot_one_or_many', endArrowhead: 'crowfoot_many' },
  ])).elements[0];
  assert.equal(legacy.startArrowhead, 'cardinality_one_or_many');
  assert.equal(legacy.endArrowhead, 'cardinality_many');
  assert.equal(t.fontFamily, 5);
  assert.equal(t.lineHeight, 1.25);
  assert.equal(t.textAlign, 'left');
  assert.ok(t.width > 0 && t.height === 50, 'unmeasured text gets an estimated box');
  assert.equal(s.background, '#fffce8');
});

test('sceneBounds spans arrow points, rotated boxes and frame names', () => {
  const b = excalidraw.sceneBounds(excalidraw.parseScene(scene([box, arrow])).elements);
  assert.deepEqual(b, { minX: 10, minY: 20, maxX: 200, maxY: 70 });

  const square = { id: 's', type: 'rectangle', x: 0, y: 0, width: 100, height: 100, angle: Math.PI / 4 };
  const r = excalidraw.sceneBounds(excalidraw.parseScene(scene([square])).elements);
  const half = 50 * Math.SQRT2;
  assert.ok(Math.abs(r.minX - (50 - half)) < 1e-9 && Math.abs(r.maxY - (50 + half)) < 1e-9);

  const frame = { id: 'f', type: 'frame', x: 0, y: 100, width: 50, height: 50, name: 'Area' };
  assert.equal(excalidraw.sceneBounds(excalidraw.parseScene(scene([frame])).elements).minY, 76);
});

test('an arrow\'s label is centered where Excalidraw draws it, not at its saved x and y', () => {
  const gen = loadRough().generator();
  // The label's center after placement, for an arrow with these fields.
  const centerOn = (fields, labelFields) => {
    const elements = excalidraw.parseScene(scene([
      Object.assign({ id: 'a', type: 'arrow', x: 10, y: 20 }, fields),
      Object.assign({ id: 'l', type: 'text', x: -500, y: -500, width: 40, height: 20, text: 'yes', containerId: 'a' }, labelFields),
    ])).elements;
    excalidraw.placeArrowLabels(elements, gen);
    const label = elements[1];
    return [label.x + label.width / 2, label.y + label.height / 2];
  };
  const near = (actual, expected) => assert.ok(
    Math.abs(actual[0] - expected[0]) < 0.5 && Math.abs(actual[1] - expected[1]) < 0.5,
    JSON.stringify(actual) + ' is not near ' + JSON.stringify(expected));

  // An even count: the middle of the middle segment.
  near(centerOn({ points: [[0, 0], [100, 40]] }), [60, 40]);
  // An odd count: the middle point.
  near(centerOn({ points: [[0, 0], [100, 0], [100, 100]] }), [110, 20]);
  // Dragged along the arrow: a fraction of its whole length, here 50 of 200.
  near(centerOn({ points: [[0, 0], [100, 0], [100, 100]] }, { labelPosition: 0.25 }), [60, 20]);
  near(centerOn({ points: [[0, 0], [100, 0], [100, 100]] }, { labelPosition: 0.75 }), [110, 70]);
  // A curved arrow: half the length of rough.js's middle curve, which bulges
  // right; (105.59, 49.27) by dense integration. The chord's middle would be
  // (100, 50), the curve's t = 0.5 point (106.25, 43.75).
  near(centerOn({ points: [[0, 0], [100, 0], [100, 100], [100, 200]], roundness: { type: 2 } }), [115.59, 69.27]);
  // An elbow arrow: the middle of its unrounded middle run.
  near(centerOn({ points: [[0, 0], [0, 50], [100, 50], [100, 100]], elbowed: true, roundness: null }), [60, 70]);
  // Turned with the arrow, about the middle of its points.
  near(centerOn({ points: [[0, 0], [100, 0], [100, 100]], angle: Math.PI }), [10, 120]);

  // A label in a shape, or on an arrow with no length, keeps its place.
  const elements = excalidraw.parseScene(scene([
    box, label,
    { id: 'dot', type: 'arrow', x: 0, y: 0, points: [[0, 0]] },
    { id: 'dot-label', type: 'text', x: 5, y: 6, width: 40, height: 20, text: 'x', containerId: 'dot' },
  ])).elements;
  excalidraw.placeArrowLabels(elements, gen);
  assert.deepEqual(elements.filter(el => el.type === 'text').map(el => [el.x, el.y]), [[20, 32], [5, 6]]);
});

test('elementSpans finds each element\'s lines in a pretty-printed scene', () => {
  const src = scene([box, label, arrow], { appState: { viewBackgroundColor: '#fff' }, files: {} });
  const lines = src.split('\n');
  const spans = excalidraw.elementSpans(src);
  assert.equal(spans.length, 3);
  ['box', 'label', 'arrow'].forEach((id, i) => {
    const body = lines.slice(spans[i].startLine - 1, spans[i].endLine);
    assert.equal(body[0].trim(), '{', id);
    assert.match(body[body.length - 1].trim(), /^},?$/, id);
    assert.ok(body.some(l => l.includes(`"id": "${id}"`)), id);
  });
});

test('elementSpans ignores brackets in strings, nested values and other arrays', () => {
  const src = [
    '{',
    '  "note": "elements: [ {",',
    '  "files": [{ "id": "not an element" }],',
    '  "elements": [',
    '    { "id": "a", "text": "a \\" } ] {", "points": [[0, 0], [1, 1]],',
    '      "roundness": { "type": 3 } },',
    '    { "id": "b" }',
    '  ],',
    '  "appState": { "elements": [{ "id": "nested" }] }',
    '}',
  ].join('\n');
  JSON.parse(src);
  assert.deepEqual(excalidraw.elementSpans(src), [{ startLine: 5, endLine: 6 }, { startLine: 7, endLine: 7 }]);
  // A minified scene still maps, to its single line.
  assert.deepEqual(excalidraw.elementSpans(JSON.stringify({ elements: [box, label] })), [
    { startLine: 1, endLine: 1 }, { startLine: 1, endLine: 1 },
  ]);
});

// Minimal stand-in for a clicked SVG node: closest() finds the element group.
function node(group) {
  return {
    closest(sel) { return sel === '[data-excalidraw-index]' ? group : null; },
  };
}

function group(attrs) {
  return { getAttribute(name) { return name in attrs ? attrs[name] : null; } };
}

test('anchorFor comments on the clicked element\'s JSON object', () => {
  const src = scene([box, label, arrow]);
  const spans = excalidraw.elementSpans(src);
  const ctx = { source: src, container: null };
  assert.deepEqual(excalidraw.anchorFor(node(group({ 'data-excalidraw-index': '0' })), ctx), spans[0]);
  assert.deepEqual(excalidraw.anchorFor(node(group({ 'data-excalidraw-index': '2' })), ctx), spans[2]);
  // Text carries its first line as the quote.
  assert.deepEqual(
    excalidraw.anchorFor(node(group({ 'data-excalidraw-index': '1', 'data-excalidraw-quote': 'Auth {service}' })), ctx),
    Object.assign({ quote: 'Auth {service}' }, spans[1]),
  );
  assert.equal(excalidraw.anchorFor(node(null), ctx), null, 'the background is not an element');
  assert.equal(excalidraw.anchorFor(node(group({ 'data-excalidraw-index': '9' })), ctx), null);
});

test('parseScene restores freedraw points, pressures and stroke options', () => {
  const s = excalidraw.parseScene(scene([
    { id: 'f', type: 'freedraw', x: 0, y: 0, points: [[0, 0], ['x', 1], [10, 5], [20, 0]], pressures: [0.3, 0.9, 'p'], simulatePressure: false },
    { id: 'c', type: 'freedraw', x: 0, y: 0, points: [[0, 0], [5, 5]], strokeOptions: { variability: 'constant', streamline: 0.2 } },
    { id: 'e', type: 'freedraw', x: 0, y: 0 },
  ]));
  const [f, c, e] = s.elements;
  // The invalid point is dropped with its pressure; a bad pressure becomes 0.5;
  // the last point has no recorded pressure.
  assert.deepEqual(f.points, [[0, 0], [10, 5], [20, 0]]);
  assert.deepEqual(f.pressures, [0.3, 0.5]);
  assert.equal(f.simulatePressure, false);
  assert.equal(f.variability, 'variable');
  assert.equal(f.streamline, 0.5);
  assert.equal(c.variability, 'constant');
  assert.equal(c.streamline, 0.2);
  assert.deepEqual(e.points, []);
});

test('sceneBounds leaves room for freedraw ink past its points', () => {
  const stroke = { id: 'f', type: 'freedraw', x: 100, y: 100, strokeWidth: 2, points: [[0, 0], [50, 0]] };
  const b = excalidraw.sceneBounds(excalidraw.parseScene(scene([stroke])).elements);
  const ink = 2 * 4.25 + 3;
  assert.deepEqual(b, { minX: 100 - ink, minY: 100 - ink, maxX: 150 + ink, maxY: 100 + ink });
  const laser = Object.assign({}, stroke, { strokeOptions: { variability: 'constant' } });
  assert.equal(excalidraw.sceneBounds(excalidraw.parseScene(scene([laser])).elements).minX, 100 - 2 * 1.4);
});

test('freedraw outlines come from perfect-freehand or the laser-pointer geometry', () => {
  const freehand = loadFreehand();
  assert.equal(typeof freehand.getStroke, 'function');
  assert.equal(typeof freehand.LaserPointer, 'function');
  assert.equal(typeof freehand.simplify, 'function');
  const points = [[0, 0], [10, 4], [22, 3], [30, 12], [41, 15]];
  const [variable, constant, dot] = excalidraw.parseScene(scene([
    { id: 'v', type: 'freedraw', x: 0, y: 0, points, simulatePressure: true },
    { id: 'c', type: 'freedraw', x: 0, y: 0, points, strokeOptions: { variability: 'constant' } },
    { id: 'd', type: 'freedraw', x: 0, y: 0, simulatePressure: false },
  ])).elements;

  const outline = excalidraw.freedrawOutline(variable, freehand);
  assert.ok(outline.length > points.length, 'a filled outline around the points');
  assert.ok(outline.every(p => p.length === 2 && Number.isFinite(p[0]) && Number.isFinite(p[1])));
  const laser = excalidraw.freedrawOutline(constant, freehand);
  assert.ok(laser.length > 0 && laser.every(p => p.length === 2), 'laser outlines drop the size channel');
  assert.notDeepEqual(laser, outline);
  assert.ok(excalidraw.freedrawOutline(dot, freehand).length > 0, 'an empty stroke without simulated pressure is a dot');

  const d = excalidraw.outlinePath(outline);
  assert.match(d, /^M -?\d+(\.\d{1,2})?,-?\d+(\.\d{1,2})? Q /);
  assert.match(d, / L -?[\d.]+,-?[\d.]+ Z$/);
  assert.doesNotMatch(d, /\.\d{3}/, 'numbers are cut to two decimals');
  assert.equal(excalidraw.outlinePath([]), '');
});

test('text uses Excalidraw\'s font stacks, Excalifont for unknown ids', () => {
  const [a, b, c] = excalidraw.parseScene(scene([
    { id: 'a', type: 'text', x: 0, y: 0, text: 'a', fontFamily: 5 },
    { id: 'b', type: 'text', x: 0, y: 0, text: 'b', fontFamily: 8 },
    { id: 'c', type: 'text', x: 0, y: 0, text: 'c', fontFamily: 42 },
  ])).elements;
  assert.equal(excalidraw.fonts[a.fontFamily].family, 'Excalifont, Xiaolai, sans-serif, Segoe UI Emoji');
  assert.equal(excalidraw.fonts[b.fontFamily].family, 'Comic Shanns, monospace, Segoe UI Emoji');
  assert.equal(c.fontFamily, 5);
});

test('every vendored font file is registered, present and matches SHA256SUMS', () => {
  const dir = path.join(__dirname, '..', 'fonts', 'excalidraw');
  const sums = new Map(fs.readFileSync(path.join(dir, 'SHA256SUMS'), 'utf8').trim().split('\n').map(line => {
    const [hash, file] = line.split(/\s+/);
    return [file, hash];
  }));
  const registered = Object.values(excalidraw.fontFiles).flat().map(face => face.file);
  assert.equal(new Set(registered).size, registered.length, 'no file registered twice');
  for (const file of registered) {
    const bytes = fs.readFileSync(path.join(dir, file));
    assert.equal(crypto.createHash('sha256').update(bytes).digest('hex'), sums.get(file), file);
  }
  const onDisk = fs.readdirSync(dir, { recursive: true }).filter(f => f.endsWith('.woff2')).map(f => f.split(path.sep).join('/'));
  assert.deepEqual(onDisk.sort(), registered.slice().sort(), 'every shipped font is registered');
  assert.deepEqual([...sums.keys()].sort(), registered.slice().sort(), 'SHA256SUMS lists exactly the shipped fonts');
  // Every registered family has a text font id, and the licenses name each one.
  const licenses = fs.readFileSync(path.join(dir, 'LICENSES.txt'), 'utf8');
  for (const name of Object.keys(excalidraw.fontFiles)) {
    assert.ok(Object.values(excalidraw.fonts).some(f => f.name === name), name);
    assert.ok(licenses.includes(name), name);
  }
});

test('the dark theme paints each color as Excalidraw does', () => {
  // invert(93%) then hue-rotate(180deg), alpha kept
  assert.equal(excalidraw.darkColor('#1e1e1e'), '#d3d3d3');
  assert.equal(excalidraw.darkColor('#ffffff'), '#121212');
  assert.equal(excalidraw.darkColor('#fff'), '#121212');
  assert.equal(excalidraw.darkColor('#00000080'), '#ededed80');
  assert.equal(excalidraw.darkColor('rgb(0, 0, 0)'), '#ededed');
  assert.equal(excalidraw.darkColor('transparent'), '#ededed00');
  assert.match(excalidraw.darkColor('#a5d8ff'), /^#[0-9a-f]{6}$/);
});

test('arrowheads fit a short last segment instead of shrinking to half of it', () => {
  const gen = loadRough().generator();
  // A 12px last segment after a turn: the old rule capped the head at 6px.
  const shapes = excalidraw.linearShapes(arrowEl([[0, 0], [150, 0], [150, 12]], null, 'triangle'), gen, false, '#ffffff');
  const head = shapes[1];
  const tip = head.sets[1].ops[0].data;
  const corners = head.sets[1].ops.filter(op => op.op === 'move').map(op => op.data);
  const reach = Math.max(...corners.map(p => Math.hypot(p[0] - tip[0], p[1] - tip[1])));
  assert.ok(reach > 6, `triangle reaches ${reach}px back from its tip`);
  // On a long single segment the head keeps its full size.
  const long = excalidraw.linearShapes(arrowEl([[0, 0], [200, 0]], null, 'triangle'), gen, false, '#ffffff')[1];
  assert.ok(long.sets[1].ops.length > 0);
});

test('filled arrowheads are one rough stroke, filled along that stroke', () => {
  const gen = loadRough().generator();
  for (const head of ['triangle', 'triangle_outline', 'diamond', 'circle', 'circle_outline']) {
    const [, drawn] = excalidraw.linearShapes(arrowEl([[0, 0], [200, 0]], null, head, { strokeColor: '#e03131' }), gen, false, '#fafafa');
    assert.deepEqual(drawn.sets.map(s => s.type), ['fillPath', 'path'], head);
    const fillOps = drawn.sets[0].ops;
    assert.equal(fillOps.filter(op => op.op === 'move').length, 1, `${head}: one continuous fill`);
    assert.equal(drawn.options.fill, head.endsWith('_outline') ? '#fafafa' : '#e03131', head);
  }
});

test('cardinality markers draw all their parts, and legacy crow\'s feet still draw', () => {
  const gen = loadRough().generator();
  const parts = head => excalidraw.linearShapes(arrowEl([[0, 0], [200, 0]], null, head), gen, false, '#ffffff').length - 1;
  assert.equal(parts('cardinality_one'), 1);
  assert.equal(parts('cardinality_many'), 2);
  assert.equal(parts('cardinality_one_or_many'), 3);
  assert.equal(parts('cardinality_exactly_one'), 2);
  assert.equal(parts('cardinality_zero_or_one'), 2);
  assert.equal(parts('cardinality_zero_or_many'), 3);
  assert.equal(parts('crowfoot_one_or_many'), 3);
});

test('sticky notes are filled, inked and at least their minimum size', () => {
  const [plain, small] = excalidraw.parseScene(scene([
    { id: 'n', type: 'stickynote', x: 0, y: 0, width: 200, height: 150, backgroundColor: 'transparent', strokeColor: 'transparent' },
    { id: 's', type: 'stickynote', x: 0, y: 0, width: 20, height: 10, baseHeight: 120 },
  ])).elements;
  assert.equal(plain.backgroundColor, '#ffdf6b');
  assert.equal(plain.strokeColor, '#1e1e1e');
  assert.equal(small.width, 75);
  assert.equal(small.height, 120, 'the note is at least its base height');
});

test('sticky note paper is jittered by its seed, with a lifted corner when roughest', () => {
  const note = (roughness, extra) => excalidraw.parseScene(scene([Object.assign(
    { id: 'n', type: 'stickynote', x: 0, y: 0, width: 240, height: 200, seed: 77, roughness }, extra)])).elements[0];
  const straight = excalidraw.stickyNoteCommands(note(0));
  assert.deepEqual(straight.map(c => c.type), ['move', 'line', 'line', 'line']);
  assert.deepEqual(straight[1].point, { x: 240, y: 0 });
  const rough = excalidraw.stickyNoteCommands(note(1));
  assert.notDeepEqual(rough[1].point, { x: 240, y: 0 });
  assert.deepEqual(excalidraw.stickyNoteCommands(note(1)), rough, 'same seed, same paper');
  assert.ok(excalidraw.stickyNoteCommands(note(2)).some(c => c.type === 'quadratic'), 'a lifted corner bends');
  const shadow = excalidraw.stickyNoteCommands(note(0), true);
  assert.deepEqual(shadow[0].point, { x: 3, y: 3 }, 'the shadow sits down-right');
});

test('the sticky note footer shows its creation date, with the year when not this year', () => {
  const now = new Date(2026, 9, 30).getTime();
  const note = (created, width) => excalidraw.parseScene(scene([
    { id: 'n', type: 'stickynote', x: 0, y: 0, width: width || 240, height: 200, created },
  ])).elements[0];
  assert.deepEqual(excalidraw.stickyNoteFooter(note(new Date(2026, 8, 7).getTime()), now), { text: '7 Sep', x: 224, y: 186 });
  assert.equal(excalidraw.stickyNoteFooter(note(new Date(2024, 1, 3).getTime()), now).text, '3 Feb 2024');
  assert.equal(excalidraw.stickyNoteFooter(note(new Date(2024, 1, 3).getTime(), 100), now).text, '3 Feb', 'narrow notes drop the year');
  assert.equal(excalidraw.stickyNoteFooter(note(null), now), null);
});

test('preview pins name an element by its kind and the words on it', () => {
  const els = excalidraw.parseScene(scene([
    { id: 'box', type: 'rectangle', x: 0, y: 0, width: 100, height: 50 },
    { id: 'box-label', type: 'text', x: 0, y: 0, text: 'Auth\nservice', containerId: 'box' },
    { id: 'db', type: 'ellipse', x: 200, y: 0, width: 100, height: 50 },
    { id: 'db-label', type: 'text', x: 0, y: 0, text: 'Orders DB', containerId: 'db' },
    { id: 'call', type: 'arrow', x: 100, y: 25, points: [[0, 0], [100, 0]], startBinding: { elementId: 'box' }, endBinding: { elementId: 'db' } },
    { id: 'named', type: 'arrow', x: 100, y: 25, points: [[0, 0], [100, 0]] },
    { id: 'named-label', type: 'text', x: 0, y: 0, text: 'yes', containerId: 'named' },
    { id: 'loose', type: 'arrow', x: 0, y: 0, points: [[0, 0], [10, 0]] },
    { id: 'note', type: 'stickynote', x: 0, y: 0, width: 200, height: 200 },
    { id: 'f', type: 'frame', x: 0, y: 0, width: 10, height: 10, name: 'Checkout' },
  ])).elements;
  const labels = { box: 'Auth service', db: 'Orders DB', named: 'yes' };
  const by = id => els.find(e => e.id === id);
  assert.equal(excalidraw.describe(by('box'), labels), 'rectangle "Auth service"');
  assert.equal(excalidraw.describe(by('box-label'), labels), 'text "Auth service"');
  assert.equal(excalidraw.describe(by('call'), labels), 'arrow from "Auth service" to "Orders DB"');
  assert.equal(excalidraw.describe(by('named'), labels), 'arrow "yes"');
  assert.equal(excalidraw.describe(by('loose'), labels), 'arrow');
  assert.equal(excalidraw.describe(by('note'), labels), 'sticky note');
  assert.equal(excalidraw.describe(by('f'), labels), 'frame "Checkout"');
});

test('a line\'s hit area is a band with a box even when the line is flat', () => {
  const d = excalidraw.bandPath([[0, 0], [100, 0]], 6);
  const nums = d.match(/-?\d+(\.\d+)?/g).map(Number);
  const ys = nums.filter((_, i) => i % 2 === 1);
  assert.equal(Math.min(...ys), -6);
  assert.equal(Math.max(...ys), 6);
  // one quad per segment, a square for a lone point
  assert.equal((excalidraw.bandPath([[0, 0], [50, 0], [50, 40]], 6).match(/Z/g) || []).length, 2);
  assert.match(excalidraw.bandPath([[5, 5]], 6), /^M -1 -1 h 12 v 12 h -12 Z$/);
});

// Just enough DOM for drawScene: elements keep their attributes in the order
// they were set, like the real thing, and serialize like outerHTML.
function fakeDocument() {
  const doc = {};
  // createElementNS(ns, tag) or createElement(tag): the tag comes last.
  doc.createElementNS = doc.createElement = (...args) => {
    const tag = args[args.length - 1];
    const attrs = new Map();
    const node = {
      tagName: tag, ownerDocument: doc, children: [], style: {}, textContent: '',
      setAttribute(k, v) { attrs.set(k, String(v)); },
      getAttribute(k) { return attrs.has(k) ? attrs.get(k) : null; },
      hasAttribute(k) { return attrs.has(k); },
      removeAttribute(k) { attrs.delete(k); },
      appendChild(child) { node.children.push(child); return child; },
      get lastChild() { return node.children[node.children.length - 1] || null; },
      get outerHTML() { return '<' + tag + [...attrs].map(([k, v]) => ` ${k}="${v}"`).join('') + '>'; },
      all() { return [node].concat(...node.children.map(c => c.all())); },
    };
    return node;
  };
  return doc;
}

test('in preview mode each element\'s hit area is an anchor named after it', () => {
  const pts = Array.from({ length: 200 }, (_, i) => [i * 2, Math.round(20 * Math.sin(i / 7))]);
  const s = excalidraw.parseScene(scene([
    { id: 'box', type: 'rectangle', x: 0, y: 0, width: 120, height: 60 },
    { id: 'box-label', type: 'text', x: 10, y: 20, width: 100, height: 25, text: 'Auth service', containerId: 'box' },
    { id: 'note', type: 'text', x: 0, y: 100, width: 80, height: 25, text: 'loose note' },
    { id: 'scribble', type: 'freedraw', x: 0, y: 150, points: pts, simulatePressure: true },
  ]));
  const saved = global.document;
  global.document = fakeDocument();
  try {
    const svg = excalidraw.drawScene(s, loadRough(), 'light', loadFreehand(), true);
    const byId = id => svg.all().find(n => n.getAttribute('id') === id);
    assert.equal(svg.getAttribute('pointer-events'), 'none', 'only hit areas take the pointer');
    assert.equal(svg.children[0].hasAttribute('id'), false, 'the empty canvas is no anchor');
    assert.equal(byId('excalidraw-box').getAttribute('aria-label'), 'rectangle "Auth service"');
    assert.equal(byId('excalidraw-box').getAttribute('data-excalidraw-id'), 'box');
    assert.equal(byId('excalidraw-note').getAttribute('aria-label'), 'text "loose note"');
    assert.equal(byId('excalidraw-box-label'), undefined, 'a shape\'s label is part of the shape');
    const label = svg.all().find(n => n.hasAttribute('data-excalidraw-hit') && n.getAttribute('pointer-events') === 'none');
    assert.ok(label, 'the label lets the pointer through to its shape');
    // A long stroke's anchor keeps its identity inside the 2 KB the agent stores.
    const html = byId('excalidraw-scribble').outerHTML;
    assert.ok(html.slice(0, 2048).includes('data-excalidraw-id="scribble"'), html.slice(0, 200));
    assert.ok(html.indexOf(' d="') > html.indexOf(' aria-label='), 'geometry comes after identity');
  } finally {
    global.document = saved;
  }
});

test('Source view highlights .excalidraw files as JSON', () => {
  const adapter = require('../crit-pierre-adapter.js');
  assert.equal(adapter.languageOverride('docs/arch.excalidraw'), 'json');
});
