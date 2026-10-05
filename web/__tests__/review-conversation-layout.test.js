'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

test('review conversation is centered for plan and files reviews', () => {
  const app = fs.readFileSync(path.join(__dirname, '../app.js'), 'utf8');
  const css = fs.readFileSync(path.join(__dirname, '../style.css'), 'utf8');
  assert.match(app, /if \(session\.mode === 'files' \|\| session\.mode === 'plan'\)\s*\{\s*section\.dataset\.docLayout = 'centered';/);
  assert.match(css, /\.review-conversation\[data-doc-layout="centered"\]\s*\{\s*width:\s*100%;\s*max-width:\s*min\(var\(--content-width\), calc\(100% - 32px\)\)\s*;\s*margin:\s*20px auto 24px;/);
});

test('review conversation stays left anchored for git reviews', () => {
  const app = fs.readFileSync(path.join(__dirname, '../app.js'), 'utf8');
  assert.match(app, /if \(session\.mode === 'files' \|\| session\.mode === 'plan'\)[\s\S]*?\} else \{\s*delete section\.dataset\.docLayout;/);
});
