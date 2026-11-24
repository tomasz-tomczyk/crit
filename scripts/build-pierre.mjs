// Build the @pierre/diffs browser bundle embedded in web/pierre/.
//
// Pierre ships bare-import ESM (shiki, diff, hast-util-to-html, …), so it needs
// one bundling pass. The output is split ESM: a small entry that exposes
// window.PierreDiffs, plus one chunk per Shiki grammar/theme that the browser
// only fetches when a file in that language is rendered. Everything is served
// from the embedded Go server (same origin), so it works offline.
//
// Go embeds files uncompressed, so the output is stored gzipped (*.js.gz) and
// the server sends it with Content-Encoding: gzip (internal/server
// precompressed handler). The build is deterministic — fixed chunk names,
// gzip header with no mtime and a fixed OS byte — so `make verify-assets`
// can rebuild and require a clean git diff on every platform.
import { build } from "esbuild";
import { gzipSync } from "zlib";
import { mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from "fs";
import { join } from "path";
import { bundledLanguagesInfo, bundledThemesInfo, bundledThemes, normalizeTheme } from "shiki";
import { EXTENSION_TO_FILE_FORMAT } from "@pierre/diffs";
import { themePalette } from './crit-theme-palette.mjs';

export const PIERRE_DIR = "web/pierre";

// Shiki grammars shipped in the binary: every language @pierre/diffs can
// detect from a file name, plus the few that only appear as markdown fence
// info strings. Deriving the list from Pierre's own extension table means a
// file whose language Pierre detects always has a grammar to load; a
// hand-kept list kept dropping some (Pierre picked the language, the file
// rendered as plain text). Shiki grammars Pierre never detects stay out to
// keep the binary small, unless FENCE_LANGS names them. A Pierre bump that
// maps new extensions grows web/pierre; `make verify-assets` and the asset
// budget show it. Grammars that embed others (vue → css/ts, markdown fences)
// still resolve those as static chunk imports.
const FENCE_LANGS = [
  "html-derivative", "just", "latex", "objective-c", "shellsession", "coq", "nsis", "openscad",
];

const canonicalLang = new Map(bundledLanguagesInfo.flatMap(l => [l.id, ...(l.aliases || [])].map(id => [id, l.id])));

export const SHIKI_LANGS = [...new Set([...Object.values(EXTENSION_TO_FILE_FORMAT), ...FENCE_LANGS].map(id => {
  const canonical = canonicalLang.get(id);
  if (!canonical) throw new Error(`Pierre or FENCE_LANGS names ${id}, which Shiki does not bundle`);
  return canonical;
}))].sort();

// Every name a file or fence can use for a shipped grammar (ids and aliases),
// written to web/pierre/languages.json so the Go side can reject a
// `languages` config value that would render as plain text.
export const SHIKI_LANG_NAMES = bundledLanguagesInfo
  .filter(l => SHIKI_LANGS.includes(l.id))
  .flatMap(l => [l.id, ...(l.aliases || [])])
  .sort();

// Use Shiki's public fine-grained bundle API instead of rewriting its generated
// index. Keep all themes, selected grammars, and the JavaScript regex engine.
const fineGrainedShiki = {
  name: "crit-shiki-bundle",
  setup(b) {
    b.onResolve({ filter: /^shiki$/ }, () => ({ path: "shiki", namespace: "crit-shiki" }));
    b.onLoad({ filter: /.*/, namespace: "crit-shiki" }, () => {
      const languages = bundledLanguagesInfo.filter(l => SHIKI_LANGS.includes(l.id));
      if (languages.length !== SHIKI_LANGS.length) throw new Error('Unknown Shiki language in SHIKI_LANGS');
      const loaders = languages.flatMap(l => [l.id, ...(l.aliases || [])].map(id =>
        `${JSON.stringify(id)}: () => import('@shikijs/langs/${l.id}')`));
      const themes = bundledThemesInfo.map(t => `${JSON.stringify(t.id)}: () => import('@shikijs/themes/${t.id}')`);
      return { resolveDir: process.cwd(), loader: "js", contents: `
        export * from 'shiki/core';
        export { createJavaScriptRegexEngine } from 'shiki/engine/javascript';
        export { createOnigurumaEngine } from 'shiki/engine/oniguruma';
        import { createBundledHighlighter, createSingletonShorthands } from 'shiki/core';
        import { createJavaScriptRegexEngine } from 'shiki/engine/javascript';
        export const bundledLanguages = {${loaders.join(',')}};
        export const bundledThemes = {${themes.join(',')}};
        export const bundledThemesInfo = ${JSON.stringify(bundledThemesInfo)};
        export const createHighlighter = createBundledHighlighter({
          langs: bundledLanguages, themes: bundledThemes, engine: () => createJavaScriptRegexEngine()
        });
        export const { codeToHtml } = createSingletonShorthands(createHighlighter);
      ` };
    });
    // Pierre resolves grammars through shiki/langs' bundledLanguages; point it
    // at the same trimmed set so it does not pull every Shiki grammar.
    b.onResolve({ filter: /^shiki\/langs$/ }, () => ({ path: "shiki", namespace: "crit-shiki" }));
    // Pierre only imports shiki/wasm for preferredHighlighter 'shiki-wasm' and
    // @pierre/highlights for 'highlights'; Crit pins 'shiki-js' (JS regex
    // engine), so keep both WASM payloads (~230 KB + ~400 KB) out.
    b.onResolve({ filter: /^shiki\/wasm$/ }, () => ({ path: "shiki-wasm-stub", namespace: "crit-stub" }));
    b.onResolve({ filter: /^@pierre\/highlights(\/.*)?$/ }, () => ({ path: "highlights-stub", namespace: "crit-stub" }));
    b.onLoad({ filter: /^shiki-wasm-stub$/, namespace: "crit-stub" }, () => ({ contents: "export default null;", loader: "js" }));
    b.onLoad({ filter: /^highlights-stub$/, namespace: "crit-stub" }, () => ({ contents: HIGHLIGHTS_STUB, loader: "js" }));
  },
};

// Stand-in for @pierre/highlights (Pierre's WASM highlighter). Pierre only
// reaches it when preferredHighlighter is 'highlights'; Crit never sets that.
const HIGHLIGHTS_STUB = `
const off = () => { throw new Error("@pierre/highlights is not bundled in Crit; use preferredHighlighter 'shiki-js'"); };
export class LiveTokenizer { constructor() { off(); } }
export const codeToTokens = off;
export const isSupportedLanguage = () => false;
export const themes = {};
`;

const ENTRY = `\
import {
  CodeView,
  File,
  FileDiff,
  processFile,
  parseDiffFromFile,
  getFiletypeFromFileName,
  setLanguageOverride,
  preloadHighlighter,
  getSharedHighlighter,
  registerCustomTheme,
  resolveTheme,
} from '@pierre/diffs';
import {
  getOrCreateWorkerPoolSingleton,
  terminateWorkerPoolSingleton,
} from '@pierre/diffs/worker';

window.PierreDiffs = {
  CodeView,
  File,
  FileDiff,
  processFile,
  parseDiffFromFile,
  getFiletypeFromFileName,
  setLanguageOverride,
  preloadHighlighter,
  getSharedHighlighter,
  registerCustomTheme,
  resolveTheme,
  getOrCreateWorkerPoolSingleton,
  terminateWorkerPoolSingleton,
};
`;

const critEntry = {
  name: "crit-pierre-entry",
  setup(b) {
    b.onResolve({ filter: /^crit-pierre-entry$/ }, () => ({ path: "crit-pierre-entry", namespace: "crit-entry" }));
    b.onLoad({ filter: /.*/, namespace: "crit-entry" }, () => ({ contents: ENTRY, resolveDir: process.cwd(), loader: "js" }));
  },
};

export async function buildPierre() {
  const palettes = await Promise.all(bundledThemesInfo.map(async info => {
    const theme = normalizeTheme((await bundledThemes[info.id]()).default);
    return themePalette({ ...theme, name: info.id, displayName: info.displayName });
  }));
  rmSync(PIERRE_DIR, { recursive: true, force: true });
  mkdirSync(PIERRE_DIR, { recursive: true });
  // Crit UI palettes as their own classic script: every page (review, live,
  // preview, /themes) loads it in <head> to theme the UI before first paint,
  // without the Pierre bundle.
  writeFileSync(join(PIERRE_DIR, "palettes.js"),
    `window.crit=window.crit||{};window.crit.palettes=${JSON.stringify(palettes)};\n`);
  writeFileSync(join(PIERRE_DIR, "languages.json"), JSON.stringify(SHIKI_LANG_NAMES, null, 1) + "\n");
  // One build, two entries: the page bundle and the highlight worker share
  // chunks, and each Shiki grammar/theme stays a lazy chunk. Pierre's
  // worker-portable.js is not used: since 1.6 it inlines every theme plus the
  // Oniguruma and Highlights WASM (~500 KB gzip).
  const result = await build({
    entryPoints: [
      { in: "crit-pierre-entry", out: "pierre-diffs" },
      { in: "@pierre/diffs/worker/worker.js", out: "pierre-worker" },
    ],
    bundle: true,
    format: "esm",
    splitting: true,
    minify: true,
    platform: "browser",
    target: "es2022",
    outdir: PIERRE_DIR,
    chunkNames: "chunk-[hash]",
    legalComments: "none",
    plugins: [critEntry, fineGrainedShiki],
    metafile: true,
    logLevel: "warning",
  });
  for (const name of readdirSync(PIERRE_DIR).filter(n => n.endsWith(".js"))) {
    const path = join(PIERRE_DIR, name);
    writeFileSync(`${path}.gz`, deterministicGzip(readFileSync(path)));
    rmSync(path);
  }
  return result.metafile;
}

// zlib writes mtime 0 already; byte 9 (OS) varies by platform, so pin it to
// 255 ("unknown") for byte-identical output on macOS, Linux and Windows.
function deterministicGzip(buf) {
  const gz = gzipSync(buf, { level: 9 });
  gz[9] = 0xff;
  return gz;
}

export function pierreFiles() {
  return readdirSync(PIERRE_DIR).filter(n => n.endsWith(".js.gz")).sort().map(n => `${PIERRE_DIR}/${n}`);
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const meta = await buildPierre();
  if (process.argv.includes("--metafile")) writeFileSync("pierre-meta.json", JSON.stringify(meta));
  console.log(`pierre bundle: ${pierreFiles().length} files in ${PIERRE_DIR}`);
}
