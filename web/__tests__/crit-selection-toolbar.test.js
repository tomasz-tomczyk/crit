const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');

const toolbar = require(path.join(__dirname, '..', 'crit-selection-toolbar.js'));
const { computeToolbarPosition, anchorRect, shortcutParts, keepsSelection, selectionSnapshot, sameSnapshot } = toolbar;

const viewport = { width: 1000, height: 800 };
const size = { width: 120, height: 34 };

function rect(top, left, width, height) {
  return { top: top, left: left, width: width, height: height, bottom: top + height, right: left + width };
}

test('places the toolbar centred above the selection', () => {
  const pos = computeToolbarPosition(rect(300, 400, 100, 20), size, viewport, 50);
  assert.equal(pos.placement, 'above');
  assert.equal(pos.top, 300 - 8 - 34);
  assert.equal(pos.left, 400 + 50 - 60);
});

test('flips below when the space above is under the top boundary', () => {
  const pos = computeToolbarPosition(rect(90, 400, 100, 20), size, viewport, 80);
  assert.equal(pos.placement, 'below');
  assert.equal(pos.top, 110 + 8);
});

test('keeps the toolbar inside the viewport horizontally', () => {
  assert.equal(computeToolbarPosition(rect(300, 0, 10, 20), size, viewport, 0).left, 8);
  assert.equal(computeToolbarPosition(rect(300, 990, 10, 20), size, viewport, 0).left, 1000 - 120 - 8);
});

test('keeps the toolbar inside the viewport vertically when flipped near the bottom', () => {
  const pos = computeToolbarPosition(rect(10, 400, 100, 790), size, viewport, 0);
  assert.equal(pos.placement, 'below');
  assert.equal(pos.top, 800 - 34 - 8);
});

test('anchors on the first and last non-empty lines, and below the whole selection', () => {
  const first = rect(100, 50, 0, 18);
  const second = rect(100, 60, 200, 18);
  const range = {
    getClientRects: () => [first, second, rect(120, 0, 300, 18), rect(140, 0, 0, 18)],
    getBoundingClientRect: () => rect(100, 0, 300, 38),
  };
  assert.deepEqual(anchorRect(range), {
    top: 100, left: 60, width: 200, bottom: 138,
    lastTop: 120, lastLeft: 0, lastRight: 300, lastBottom: 138,
  });
});

test('falls back to the bounding rect when no line rect has a size', () => {
  const range = { getClientRects: () => [], getBoundingClientRect: () => rect(100, 0, 300, 38) };
  assert.deepEqual(anchorRect(range), {
    top: 100, left: 0, width: 300, bottom: 138,
    lastTop: 100, lastLeft: 0, lastRight: 300, lastBottom: 138,
  });
});

test('top-center is the default placement', () => {
  const anchor = rect(300, 400, 100, 20);
  assert.deepEqual(computeToolbarPosition(anchor, size, viewport, 50),
    computeToolbarPosition(anchor, size, viewport, 50, 'top-center'));
});

test('top-left aligns the toolbar on the start of the first line', () => {
  const pos = computeToolbarPosition(rect(300, 400, 100, 20), size, viewport, 50, 'top-left');
  assert.equal(pos.placement, 'above');
  assert.equal(pos.top, 300 - 8 - 34);
  assert.equal(pos.left, 400);
});

test('bottom-right starts the toolbar at the end of the last line, below it', () => {
  const anchor = { top: 100, left: 60, width: 200, bottom: 138, lastTop: 120, lastRight: 300, lastBottom: 138 };
  const pos = computeToolbarPosition(anchor, size, viewport, 50, 'bottom-right');
  assert.equal(pos.placement, 'below');
  assert.equal(pos.top, 138 + 8);
  assert.equal(pos.left, 300);
});

test('bottom-right flips above the last line near the bottom of the viewport', () => {
  const anchor = { top: 700, left: 60, width: 200, bottom: 780, lastTop: 762, lastRight: 300, lastBottom: 780 };
  const pos = computeToolbarPosition(anchor, size, viewport, 50, 'bottom-right');
  assert.equal(pos.placement, 'above');
  assert.equal(pos.top, 762 - 8 - 34);
});

test('bottom-right stays inside the viewport at the right edge', () => {
  const anchor = { top: 100, left: 900, width: 95, bottom: 118, lastTop: 100, lastRight: 995, lastBottom: 118 };
  assert.equal(computeToolbarPosition(anchor, size, viewport, 0, 'bottom-right').left, 1000 - 120 - 8);
});

test('top-right starts the toolbar at the end of the first line, above it', () => {
  const pos = computeToolbarPosition(rect(300, 400, 100, 20), size, viewport, 50, 'top-right');
  assert.equal(pos.placement, 'above');
  assert.equal(pos.left, 500);
});

test('bottom-left starts the toolbar at the start of the last line, below it', () => {
  const anchor = { top: 100, left: 60, width: 200, bottom: 138, lastTop: 120, lastLeft: 20, lastRight: 300, lastBottom: 138 };
  const pos = computeToolbarPosition(anchor, size, viewport, 50, 'bottom-left');
  assert.equal(pos.placement, 'below');
  assert.equal(pos.top, 138 + 8);
  assert.equal(pos.left, 20);
});

test('the *-cursor placements follow the selection direction', () => {
  const { resolvePlacement } = toolbar;
  assert.equal(resolvePlacement('top-cursor', false), 'top-right');
  assert.equal(resolvePlacement('top-cursor', true), 'top-left');
  assert.equal(resolvePlacement('bottom-cursor', false), 'bottom-right');
  assert.equal(resolvePlacement('bottom-cursor', true), 'bottom-left');
  assert.equal(resolvePlacement('bottom-left', false), 'bottom-left');
  assert.equal(resolvePlacement('unknown', false), 'top-center');
});

test('reads the selection direction, natively or from its anchor and focus', () => {
  const { isBackward } = toolbar;
  assert.equal(isBackward({ direction: 'backward' }), true);
  assert.equal(isBackward({ direction: 'forward' }), false);
  const node = {};
  assert.equal(isBackward({ anchorNode: node, focusNode: node, anchorOffset: 5, focusOffset: 2 }), true);
  assert.equal(isBackward({ anchorNode: node, focusNode: node, anchorOffset: 2, focusOffset: 5 }), false);
  const before = { compareDocumentPosition: () => 2 };
  assert.equal(isBackward({ anchorNode: before, focusNode: {}, anchorOffset: 0, focusOffset: 0 }), true);
  assert.equal(isBackward(null), false);
});

test('exposes the placements a reader can choose', () => {
  assert.deepEqual(toolbar.PLACEMENTS, ['top-left', 'top-center', 'top-right', 'bottom-left', 'bottom-right', 'top-cursor', 'bottom-cursor']);
});

test('flips below the last line of a multi-line selection', () => {
  const anchor = { top: 90, left: 400, width: 100, bottom: 150 };
  const pos = computeToolbarPosition(anchor, size, viewport, 80);
  assert.equal(pos.placement, 'below');
  assert.equal(pos.top, 150 + 8);
});

test('splits a shortcut binding into key chord parts', () => {
  assert.deepEqual(shortcutParts('c'), ['c']);
  assert.deepEqual(shortcutParts('Shift+C'), ['Shift', 'C']);
  assert.deepEqual(shortcutParts(''), []);
});

test('only lone modifiers and copy keep the toolbar', () => {
  assert.equal(keepsSelection({ key: 'Shift' }), true);
  assert.equal(keepsSelection({ key: 'Meta' }), true);
  assert.equal(keepsSelection({ key: 'c', ctrlKey: true }), true);
  assert.equal(keepsSelection({ key: 'c', metaKey: true }), true);
  assert.equal(keepsSelection({ key: 'Insert', ctrlKey: true }), true);
  assert.equal(keepsSelection({ key: 'c' }), false);
  assert.equal(keepsSelection({ key: 'a', ctrlKey: true }), false);
  assert.equal(keepsSelection({ key: 'ArrowRight', ctrlKey: true, shiftKey: true }), false);
  assert.equal(keepsSelection({ key: 'ArrowRight', shiftKey: true }), false);
});

test('snapshots a selection through its composed range', () => {
  const start = {}, end = {};
  const selection = {
    rangeCount: 1,
    getComposedRanges: () => [{ startContainer: start, startOffset: 2, endContainer: end, endOffset: 5, collapsed: false }],
    getRangeAt: () => { throw new Error('composed range expected'); },
  };
  assert.deepEqual(selectionSnapshot(selection, []), { sc: start, so: 2, ec: end, eo: 5 });
});

test('a collapsed or empty selection has no snapshot', () => {
  assert.equal(selectionSnapshot({ rangeCount: 0 }, []), null);
  const collapsed = { rangeCount: 1, getRangeAt: () => ({ collapsed: true }) };
  assert.equal(selectionSnapshot(collapsed, []), null);
});

test('snapshots compare by their boundaries', () => {
  const node = {};
  const a = { sc: node, so: 1, ec: node, eo: 4 };
  assert.equal(sameSnapshot(a, { sc: node, so: 1, ec: node, eo: 4 }), true);
  assert.equal(sameSnapshot(a, { sc: node, so: 1, ec: node, eo: 5 }), false);
  assert.equal(sameSnapshot(null, null), true);
  assert.equal(sameSnapshot(a, null), false);
});

test('module.exports matches window.crit.selectionToolbar', () => {
  const src = fs.readFileSync(path.join(__dirname, '..', 'crit-selection-toolbar.js'), 'utf8');
  const sandbox = {};
  new Function('window', 'module', src)(sandbox, undefined);
  assert.deepEqual(Object.keys(sandbox.crit.selectionToolbar).sort(), Object.keys(toolbar).sort());
});

test('index.html loads crit-selection-toolbar.js before app.js', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const toolbarAt = html.indexOf("'crit-selection-toolbar.js'");
  const appAt = html.indexOf("a.src = 'app.js'");
  assert.ok(toolbarAt !== -1, 'crit-selection-toolbar.js should be loaded');
  assert.ok(toolbarAt < appAt, 'crit-selection-toolbar.js must load before app.js');
});
