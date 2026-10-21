'use strict';
// The sidebar file tree must list files in the same order as the review list
// (fileSortComparator), including folders whose single-child chains were
// collapsed into one 'a/b/c' row.

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const appJs = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');
const sharedSrc = fs.readFileSync(path.join(__dirname, '..', 'crit-shared.js'), 'utf8');

const sandbox = { window: {}, document: { cookie: '' } };
new Function('window', 'document', sharedSrc)(sandbox.window, sandbox.document);
const pathCompare = sandbox.window.crit.shared.pathCompare;

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

function fakeElement() {
  return {
    dataset: {},
    style: {},
    children: [],
    addEventListener() {},
    appendChild(child) { this.children.push(child); },
  };
}

function treeFileOrder(container) {
  const paths = [];
  (function walk(el) {
    if (el.dataset.treePath) paths.push(el.dataset.treePath);
    el.children.forEach(walk);
  })(container);
  return paths;
}

function renderTree(paths) {
  const fileList = paths.map(function(p) { return { path: p, status: 'modified', comments: [] }; });
  const container = fakeElement();
  const listOrder = new Function('pathCompare', 'document', 'fileList', 'container', `
    const session = { mode: 'git' };
    const treeFolderState = {};
    const activeTreePath = null;
    function escapeHtml(s) { return s; }
    function fileStatusIcon() { return ''; }
    ${extractFunction('fileThreadComments')}
    ${extractFunction('fileSortComparator')}
    ${extractFunction('buildFileTree')}
    ${extractFunction('collapseCommonPrefixes')}
    ${extractFunction('renderTreeNode')}
    const files = fileList.slice().sort(fileSortComparator);
    renderTreeNode(container, collapseCommonPrefixes(buildFileTree(files)), 0, '');
    return files.map(function(f) { return f.path; });
  `)(pathCompare, { createElement: fakeElement }, fileList, container);
  return { listOrder, treeOrder: treeFileOrder(container) };
}

test('git mode: tree orders a collapsed folder before a dotted sibling, like the list', () => {
  const { listOrder, treeOrder } = renderTree([
    'web.test/app/utils.test.ts',
    'web/src/app/utils.ts',
    'web.test/app/views/online/table.test.ts',
    'web/src/app/data/feature.ts',
    'web.test/app/format.test.ts',
    'web.test/app/views/calendar/month.test.ts',
    'web/src/app/format.ts',
    'web.test/app/views/online/chart.test.ts',
  ]);
  assert.deepEqual(listOrder, [
    'web/src/app/data/feature.ts',
    'web/src/app/format.ts',
    'web/src/app/utils.ts',
    'web.test/app/views/calendar/month.test.ts',
    'web.test/app/views/online/chart.test.ts',
    'web.test/app/views/online/table.test.ts',
    'web.test/app/format.test.ts',
    'web.test/app/utils.test.ts',
  ]);
  assert.deepEqual(treeOrder, listOrder);
});
