'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { pathToFileURL } = require('node:url');
const path = require('node:path');

const buildPierre = pathToFileURL(path.join(__dirname, '..', '..', 'scripts', 'build-pierre.mjs')).href;

// A language Pierre detects renders as plain text unless its grammar ships.
// SHIKI_LANGS is derived from Pierre's extension map, so every detected
// language (by id or alias) must be in SHIKI_LANG_NAMES.
test('every language Pierre detects from a file name has a bundled grammar', async () => {
  const { EXTENSION_TO_FILE_FORMAT } = await import('@pierre/diffs');
  const { SHIKI_LANG_NAMES } = await import(buildPierre);
  const missing = [...new Set(Object.values(EXTENSION_TO_FILE_FORMAT))].filter(l => !SHIKI_LANG_NAMES.includes(l));
  assert.deepEqual(missing, []);
});

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
