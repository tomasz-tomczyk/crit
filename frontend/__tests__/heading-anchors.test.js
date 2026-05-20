const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');

// Load crit-line-blocks.js in a fake-browser shim.
const src = fs.readFileSync(path.join(__dirname, '..', 'crit-line-blocks.js'), 'utf8');
const fn = new Function('window', 'document', src + '\nreturn window;');
const sandbox = {
  window: {
    crit: {
      commentCardHelpers: {
        escapeHtml: function(s) {
          return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
        }
      }
    },
    hljs: {
      getLanguage: function() { return null; },
      highlight: function() { return { value: '' }; }
    }
  },
  document: {}
};
fn(sandbox.window, sandbox.document);
const { slugifyHeading } = sandbox.window.crit.lineBlocks;

// --- slugifyHeading ---

test('slugifyHeading lowercases and replaces spaces with hyphens', () => {
  assert.equal(slugifyHeading('Hello World'), 'hello-world');
});

test('slugifyHeading strips non-alphanumeric non-hyphen non-space chars', () => {
  assert.equal(slugifyHeading('Making HTTP requests by framework'), 'making-http-requests-by-framework');
});

test('slugifyHeading handles special characters', () => {
  assert.equal(slugifyHeading('What\'s new in v2.0?'), 'whats-new-in-v20');
});

test('slugifyHeading collapses multiple spaces/hyphens', () => {
  assert.equal(slugifyHeading('foo  --  bar'), 'foo-bar');
});

test('slugifyHeading trims leading and trailing hyphens', () => {
  assert.equal(slugifyHeading(' Hello '), 'hello');
  assert.equal(slugifyHeading('---hello---'), 'hello');
});

test('slugifyHeading handles inline code and markup in heading text', () => {
  assert.equal(slugifyHeading('Using `fetch()` for APIs'), 'using-fetch-for-apis');
});

test('slugifyHeading preserves unicode letters', () => {
  assert.equal(slugifyHeading('Über cool héading'), 'über-cool-héading');
});

test('slugifyHeading handles empty string', () => {
  assert.equal(slugifyHeading(''), '');
});

test('slugifyHeading matches GitHub-style slug for typical headings', () => {
  assert.equal(slugifyHeading('Getting Started'), 'getting-started');
  assert.equal(slugifyHeading('API Reference (v3)'), 'api-reference-v3');
  assert.equal(slugifyHeading('1. First Step'), '1-first-step');
});

// --- Duplicate heading dedup (mirrors the Map-based counter in app.js heading_open) ---

// Helper: simulates the dedup logic used in app.js heading_open renderer
function dedupSlug(baseSlug, counter) {
  const count = counter.get(baseSlug) || 0;
  const slug = count === 0 ? baseSlug : baseSlug + '-' + count;
  counter.set(baseSlug, count + 1);
  return slug;
}

test('duplicate headings get unique IDs (GitHub-style -1, -2 suffix)', () => {
  const counter = new Map();
  const slug1 = dedupSlug(slugifyHeading('Examples'), counter);
  const slug2 = dedupSlug(slugifyHeading('Examples'), counter);
  const slug3 = dedupSlug(slugifyHeading('Examples'), counter);
  assert.equal(slug1, 'examples');
  assert.equal(slug2, 'examples-1');
  assert.equal(slug3, 'examples-2');
});

test('different headings do not interfere with each other', () => {
  const counter = new Map();
  const slug1 = dedupSlug(slugifyHeading('Setup'), counter);
  const slug2 = dedupSlug(slugifyHeading('Usage'), counter);
  const slug3 = dedupSlug(slugifyHeading('Setup'), counter);
  assert.equal(slug1, 'setup');
  assert.equal(slug2, 'usage');
  assert.equal(slug3, 'setup-1');
});

test('counter resets between render passes (fresh Map)', () => {
  const counter1 = new Map();
  const first = dedupSlug(slugifyHeading('Examples'), counter1);
  dedupSlug(slugifyHeading('Examples'), counter1);

  // Simulate new render pass — fresh counter
  const counter2 = new Map();
  const fresh = dedupSlug(slugifyHeading('Examples'), counter2);

  assert.equal(first, 'examples');
  assert.equal(fresh, 'examples', 'fresh render should produce same ID as first render');
});
