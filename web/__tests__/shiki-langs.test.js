'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { pathToFileURL } = require('node:url');
const path = require('node:path');

const buildPierre = pathToFileURL(path.join(__dirname, '..', '..', 'scripts', 'build-pierre.mjs')).href;

// Pierre detects these files' languages, but they render as plain text unless
// the grammar is in SHIKI_LANGS.
test('Ember template-tag files resolve to a bundled grammar', async () => {
  const { getFiletypeFromFileName } = await import('@pierre/diffs');
  const { SHIKI_LANGS } = await import(buildPierre);
  for (const [file, lang] of [
    ['app/components/greeting.gts', 'glimmer-ts'],
    ['app/components/greeting.gjs', 'glimmer-js'],
  ]) {
    assert.equal(getFiletypeFromFileName(file), lang, file);
    assert.ok(SHIKI_LANGS.includes(lang), `${lang} missing from SHIKI_LANGS`);
  }
});
