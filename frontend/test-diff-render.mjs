// Test for highlightDiffLine cache integrity in branch-scope diffs.
// Run: node frontend/test-diff-render.mjs
//
// Bug repro: in branch-scope diff view, leading add lines of a hunk render
// the wrong content under the right line numbers because highlightDiffLine
// looks up highlightCache[NewNum] without verifying that the cached source
// equals the diff line's actual Content. The cache is built from
// file.content (working tree), but in some scopes / commit-pinned diffs
// NewNum addresses a different revision, so the cache returns content
// from the wrong place.

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import vm from 'node:vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const appJsPath = resolve(__dirname, 'app.js');
const appJs = readFileSync(appJsPath, 'utf8');

// Extract highlightDiffLine source from app.js (minimum-invasive: no module
// boundaries needed). Match the function definition through its closing brace
// at the matching indent level.
function extractFn(src, name) {
  const startRe = new RegExp(`^( *)function ${name}\\(`, 'm');
  const m = src.match(startRe);
  if (!m) throw new Error(`could not find function ${name}`);
  const startIdx = m.index;
  const indent = m[1];
  const closeRe = new RegExp(`^${indent}\\}`, 'm');
  // Search after the opening line
  const after = src.slice(startIdx + m[0].length);
  const closeMatch = after.match(closeRe);
  if (!closeMatch) throw new Error(`could not find end of ${name}`);
  return src.slice(startIdx, startIdx + m[0].length + closeMatch.index + closeMatch[0].length);
}

const highlightDiffLineSrc = extractFn(appJs, 'highlightDiffLine');
// Sanity-check that the extractor grabbed the real function (not a same-indent
// `}` from a nested closure). The fix this test guards must be present.
if (!highlightDiffLineSrc.includes('entry.raw === content')) {
  throw new Error('extractFn returned wrong slice — `entry.raw === content` guard missing');
}

// Stub `escapeHtml` and `hljs` — we only care about cache-hit behavior.
const sandbox = {
  console,
  escapeHtml: (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'),
  hljs: { getLanguage: () => null },
};
vm.createContext(sandbox);
vm.runInContext(highlightDiffLineSrc, sandbox, { filename: 'highlightDiffLine.js' });

const highlightDiffLine = sandbox.highlightDiffLine;

let pass = 0;
let fail = 0;

function assertEq(label, got, want) {
  const ok = got === want;
  if (ok) { pass++; console.log(`PASS: ${label}`); }
  else { fail++; console.log(`FAIL: ${label}\n  got:  ${JSON.stringify(got)}\n  want: ${JSON.stringify(want)}`); }
}

// ---- The bug scenario ----
// Imagine a branch-scope diff: hunk header @@ -162,4 +209,120 @@.
// The new file (branch tip) at lines 209+ contains brand-new code.
// But file.content (working tree the cache was built from) at lines 209+
// contains older / unrelated content (e.g. user is reviewing a feature
// branch, and the working tree's content for those line numbers belongs
// to other code).
//
// The cache is keyed by line number, so cache[212] returns the working-tree
// line 212's HTML — not the branch-tip line 212's content that the diff
// emitted as an add line.

// Build a cache that simulates working-tree content where line 212 is
// "      user ->" (older code). The real diff add line at NewNum 212 is "".
// The cache stores both the raw source line AND the highlighted HTML so the
// renderer can verify a hit before returning stale content.
const cache = [null];
for (let i = 1; i <= 211; i++) cache.push({ raw: `wt-line-${i}`, html: `<wt-line-${i}>` });
cache.push({ raw: '      user ->', html: '      user -&gt;' });                  // cache[212]
cache.push({ raw: '        case Repo.delete(user) do', html: '        case Repo.delete(user) do' }); // cache[213]

// The diff says: add line at NewNum=212 with Content=""  (a blank add)
const diffLine = { Type: 'add', NewNum: 212, Content: '' };

const got = highlightDiffLine(diffLine.Content, diffLine.NewNum, '', cache, 'elixir');

// Correct behavior: rendered HTML must reflect the diff's Content (""), not
// some unrelated cached working-tree line.
const gotStr = typeof got === 'string' ? got : JSON.stringify(got);
const looksLikeWrongContent = gotStr.includes('user') || gotStr.includes('Repo.delete');
assertEq(
  'add line content matches diff Content, not stale cache entry',
  looksLikeWrongContent,
  false,
);

// Sanity: when cache is correct (raw matches the diff content), cache hit is fine.
const goodCache = [null];
goodCache[100] = { raw: 'def foo do', html: '<span>good highlight for line 100</span>' };
const got2 = highlightDiffLine('def foo do', 100, '', goodCache, 'elixir');
assertEq(
  'cache hit returns cached HTML when raw matches',
  got2,
  '<span>good highlight for line 100</span>',
);

// Old-side lines should never use the cache (cache is new-side only).
const got3 = highlightDiffLine('removed line', 50, 'old', cache, 'elixir');
assertEq(
  'old-side never uses new-side cache',
  got3.includes('wt-line-50'),
  false,
);

// Context line where cache entry happens to match (the common branch-scope
// path: working tree == branch tip == base for unchanged lines) — cache hit
// is still used and saves the per-line highlight.
const ctxCache = [null];
ctxCache[209] = { raw: '    end', html: '<span class="hljs-keyword">end</span>' };
const gotCtx = highlightDiffLine('    end', 209, '', ctxCache, 'elixir');
assertEq(
  'context line cache hit (raw matches Content) returns cached HTML',
  gotCtx,
  '<span class="hljs-keyword">end</span>',
);

console.log(`\n${pass} passed, ${fail} failed`);
process.exit(fail === 0 ? 0 : 1);
