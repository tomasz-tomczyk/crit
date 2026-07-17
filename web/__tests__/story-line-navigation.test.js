'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const appJs = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');

function extractFunction(name) {
  const start = appJs.indexOf(`function ${name}(`);
  assert.notEqual(start, -1, `${name} must exist`);
  const bodyStart = appJs.indexOf('{', start);
  let depth = 0;
  for (let i = bodyStart; i < appJs.length; i++) {
    if (appJs[i] === '{') depth++;
    if (appJs[i] === '}') {
      depth--;
      if (depth === 0) return appJs.slice(start, i + 1);
    }
  }
  throw new Error(`could not extract ${name}`);
}

const source = [
  extractFunction('storyRawHunks'),
  extractFunction('storyRawHunksWithTrailingContext'),
  extractFunction('buildContextLines'),
  extractFunction('expandHunksForComments'),
  'function buildHunkHeader() { return "@@"; }',
  extractFunction('autoExpandSmallGaps'),
  extractFunction('storyRawHunkForLine'),
  extractFunction('storyDisplayAnchorForLine'),
  'return { expandHunksForComments, autoExpandSmallGaps, storyRawHunkForLine, storyDisplayAnchorForLine };',
].join('\n');
const {
  expandHunksForComments,
  autoExpandSmallGaps,
  storyRawHunkForLine,
  storyDisplayAnchorForLine,
} = new Function(source)();

const fileNavigationSource = [
  'let storyState = null;',
  'let storyView = "overview";',
  'let shown = null;',
  'function storyPageId(page) { return page.id; }',
  'function showStory(id) { shown = id; storyView = id; }',
  extractFunction('storyPageForFile'),
  extractFunction('storyActivatePageForFile'),
  'return {',
  '  setState: function (state) { storyState = state; },',
  '  setView: function (view) { storyView = view; },',
  '  shown: function () { return shown; },',
  '  storyPageForFile,',
  '  storyActivatePageForFile,',
  '};',
].join('\n');
const fileNavigation = new Function(fileNavigationSource)();

function line(Type, OldNum, NewNum) {
  return { Type, OldNum, NewNum };
}

test('new-side line wins over an old-side collision after a net deletion', () => {
  const first = {
    OldStart: 7,
    Lines: [
      line('context', 7, 7),
      line('del', 17, 0),
      line('del', 18, 0),
      line('context', 22, 12),
    ],
  };
  const second = {
    OldStart: 24,
    Lines: [
      line('context', 24, 14),
      line('add', 0, 17),
      line('context', 30, 20),
    ],
  };

  const match = storyRawHunkForLine([first, second], 17, 'new');
  assert.equal(match.rawHunk.OldStart, 24);
});

test('old-side line wins over a new-side collision', () => {
  const deleted = {
    OldStart: 7,
    Lines: [
      line('context', 7, 7),
      line('del', 17, 0),
    ],
  };
  const later = {
    OldStart: 24,
    Lines: [
      line('context', 24, 14),
      line('add', 0, 17),
    ],
  };

  const match = storyRawHunkForLine([deleted, later], 17, 'old');
  assert.equal(match.rawHunk.OldStart, 7);
});

test('merged visual hunk preserves raw ownership for both sides', () => {
  const first = {
    OldStart: 7,
    OldCount: 16,
    NewStart: 7,
    NewCount: 6,
    Header: '@@ -7,16 +7,6 @@',
    Lines: [line('context', 7, 7), line('del', 17, 0), line('context', 22, 12)],
  };
  const second = {
    OldStart: 24,
    OldCount: 7,
    NewStart: 14,
    NewCount: 7,
    Header: '@@ -24,7 +14,7 @@',
    Lines: [line('context', 24, 14), line('add', 0, 17), line('context', 30, 20)],
  };
  const file = {
    content: Array.from({ length: 30 }, (_, i) => `line ${i + 1}`).join('\n'),
    diffHunks: [first, second],
  };
  autoExpandSmallGaps(file);

  assert.equal(file.diffHunks.length, 1);
  const newMatch = storyRawHunkForLine(file.diffHunks, 17, 'new');
  const oldMatch = storyRawHunkForLine(file.diffHunks, 17, 'old');
  assert.equal(newMatch.rawHunk.OldStart, 24);
  assert.equal(oldMatch.rawHunk.OldStart, 7);
});

test('comment-gap expansion preserves the later raw hunk owner', () => {
  const first = {
    OldStart: 10,
    OldCount: 2,
    NewStart: 10,
    NewCount: 2,
    Header: '@@ -10,2 +10,2 @@',
    Lines: [line('context', 10, 10), line('context', 11, 11)],
  };
  const second = {
    OldStart: 20,
    OldCount: 2,
    NewStart: 20,
    NewCount: 2,
    Header: '@@ -20,2 +20,2 @@',
    Lines: [line('context', 20, 20), line('add', 0, 21)],
  };
  const file = {
    content: Array.from({ length: 30 }, (_, i) => `line ${i + 1}`).join('\n'),
    comments: [{ scope: 'line', side: 'new', end_line: 15 }],
    diffHunks: [first, second],
  };

  expandHunksForComments(file);

  assert.equal(file.diffHunks.length, 1);
  const gapMatch = storyRawHunkForLine(file.diffHunks, 15, 'new');
  assert.equal(gapMatch.rawHunk.OldStart, 10, 'synthesized gap context belongs to the preceding raw hunk');
  const match = storyRawHunkForLine(file.diffHunks, 21, 'new');
  assert.equal(match.rawHunk.OldStart, 20);
});

test('automatic small-gap expansion preserves synthesized context ownership', () => {
  const first = {
    OldStart: 10,
    OldCount: 2,
    NewStart: 10,
    NewCount: 2,
    Header: '@@ -10,2 +10,2 @@',
    Lines: [line('context', 10, 10), line('context', 11, 11)],
  };
  const second = {
    OldStart: 15,
    OldCount: 2,
    NewStart: 15,
    NewCount: 2,
    Header: '@@ -15,2 +15,2 @@',
    Lines: [line('context', 15, 15), line('add', 0, 16)],
  };
  const file = {
    content: Array.from({ length: 20 }, (_, i) => `line ${i + 1}`).join('\n'),
    diffHunks: [first, second],
  };

  autoExpandSmallGaps(file);

  const gapMatch = storyRawHunkForLine(file.diffHunks, 13, 'new');
  assert.equal(gapMatch.rawHunk.OldStart, 10);
  const laterMatch = storyRawHunkForLine(file.diffHunks, 16, 'new');
  assert.equal(laterMatch.rawHunk.OldStart, 15);
});

test('existing insertion mapping still selects its new-side hunk', () => {
  const first = {
    OldStart: 10,
    Lines: [line('context', 10, 10), line('add', 0, 11)],
  };
  const second = {
    OldStart: 20,
    Lines: [line('context', 20, 21), line('add', 0, 22)],
  };

  const match = storyRawHunkForLine([first, second], 22, 'new');
  assert.equal(match.rawHunk.OldStart, 20);
});

test('deleted lines fall back to their old-side raw hunk', () => {
  const deleted = {
    OldStart: 30,
    Lines: [line('del', 31, 0)],
  };

  const match = storyRawHunkForLine([deleted], 31, 'old');
  assert.equal(match.rawHunk.OldStart, 30);
});

test('side-less legacy lookup keeps new-then-old fallback', () => {
  const deleted = {
    OldStart: 7,
    Lines: [line('del', 17, 0)],
  };
  const added = {
    OldStart: 24,
    Lines: [line('add', 0, 17)],
  };

  const match = storyRawHunkForLine([deleted, added], 17);
  assert.equal(match.rawHunk.OldStart, 24);
});

test('side-less legacy display mapping follows the matched coordinate side', () => {
  const deleted = {
    OldStart: 7,
    Lines: [line('del', 17, 0)],
  };

  assert.deepEqual(
    storyDisplayAnchorForLine([deleted], 17, undefined, 'unified'),
    { line: 17, side: 'old' }
  );
});

test('file-level navigation activates the first chapter containing the file', () => {
  fileNavigation.setState({
    pages: [{ id: 'chapter-1' }, { id: 'chapter-2' }],
    fileChapters: new Map([['src/a.go', new Set([1])]]),
  });
  fileNavigation.setView('overview');

  assert.equal(fileNavigation.storyPageForFile('src/a.go'), 'chapter-2');
  assert.equal(fileNavigation.storyActivatePageForFile('src/a.go'), true);
  assert.equal(fileNavigation.shown(), 'chapter-2');
  assert.equal(fileNavigation.storyActivatePageForFile('missing.go'), false);
});

test('display mapping preserves route side while matching split and unified DOM semantics', () => {
  const hunk = {
    OldStart: 20,
    Lines: [
      line('context', 24, 14),
      line('del', 25, 0),
      line('add', 0, 15),
      line('context', 26, 16),
    ],
  };

  assert.deepEqual(
    storyDisplayAnchorForLine([hunk], 24, 'old', 'unified'),
    { line: 14, side: '' },
    'unified old-side context maps to its single new-side context row'
  );
  assert.deepEqual(
    storyDisplayAnchorForLine([hunk], 25, 'old', 'unified'),
    { line: 25, side: 'old' },
    'unified old-side deletion keeps its old-side deleted row'
  );
  assert.deepEqual(
    storyDisplayAnchorForLine([hunk], 15, 'new', 'unified'),
    { line: 15, side: '' },
    'unified new-side addition keeps its new-side row'
  );
  assert.deepEqual(
    storyDisplayAnchorForLine([hunk], 16, 'new', 'unified'),
    { line: 16, side: '' },
    'unified new-side context keeps its new-side row'
  );
  assert.deepEqual(
    storyDisplayAnchorForLine([hunk], 24, 'old', 'split'),
    { line: 24, side: 'old' },
    'split old-side context keeps its distinct left-side row'
  );
});

test('production Story callers thread side and display mapping into scroll routing', () => {
  assert.match(
    appJs,
    /storyActivatePageForLine\(filePath,\s*line,\s*side\)/,
    'comment-card navigation must preserve the comment side'
  );
  assert.match(
    appJs,
    /fileScope\s*\?\s*storyActivatePageForFile\(filePath\)/,
    'file-level comments must navigate by file ownership instead of a fake line'
  );
  assert.match(
    appJs,
    /storyActivatePageForLine\(anchor\.filePath,\s*anchor\.endLine,\s*side\)/,
    'renderer anchor navigation must preserve the anchor side'
  );
  assert.match(
    appJs,
    /storyDisplayAnchorForLine\(file\.diffHunks,\s*anchor\.endLine,\s*side,\s*diffMode\)/,
    'renderer scrolling must translate route coordinates into renderer coordinates'
  );
  assert.match(
    appJs,
    /data-diff-side="['"]\s*\+\s*displayAnchor\.side/,
    'Story scroll target must use the translated display side'
  );
  assert.match(
    appJs,
    /anchor\.scope\s*===\s*['"]file['"]/,
    'renderer file anchors must use file-level Story navigation'
  );
  const reloadStart = appJs.indexOf('async function reloadForScope()');
  const reloadEnd = appJs.indexOf('function storyPageById', reloadStart);
  const reloadSource = appJs.slice(reloadStart, reloadEnd);
  assert.match(
    reloadSource,
    /storyExpandedFileCache\.clear\(\)[\s\S]*loadAllFileData/,
    'scope reloads must invalidate Story clones before loading replacement hunks'
  );
});
