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
import { copyFileSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from "fs";
import { join } from "path";
import { bundledLanguagesInfo, bundledThemesInfo, bundledThemes, normalizeTheme } from "shiki";
import { themePalette } from './crit-theme-palette.mjs';

export const PIERRE_DIR = "web/pierre";

// Shiki grammars shipped in the binary. Shiki bundles 260 grammars (7.8MB raw)
// and Go embeds files uncompressed, so shipping all of them would push the
// binary ~9MB past its budget. This covers mainstream languages plus
// everything Crit's special cases rely on; other files render as plain text.
// Grammars that embed others (vue → css/ts, markdown fences) still resolve
// those as static chunk imports. See docs/frontend-js.md for the table vs the
// previous highlight.js coverage.
export const SHIKI_LANGS = [
  "angular-html", "angular-ts", "apache", "applescript", "asm", "astro", "awk", "bat", "bicep", "blade",
  "c", "cairo", "clarity", "clojure", "cmake", "cobol", "coffee", "cpp", "crystal", "csharp", "css", "csv",
  "d", "dart", "diff", "docker", "dotenv", "elixir", "elm", "erb", "erlang", "fish", "fortran-free-form",
  "fsharp", "gherkin", "git-commit", "git-rebase", "gleam", "glsl", "go", "graphql", "groovy", "haml",
  "handlebars", "haskell", "hcl", "hlsl", "html", "html-derivative", "http", "ini", "java", "javascript",
  "jinja", "json", "json5", "jsonc", "jsonl", "jsx", "julia", "just", "kotlin", "latex", "less", "liquid",
  "log", "lua", "make", "markdown", "matlab", "mdx", "move", "nginx", "nim", "nix", "nushell",
  "objective-c", "objective-cpp", "ocaml", "odin", "perl", "php", "postcss", "powershell", "prisma",
  "proto", "pug", "purescript", "python", "r", "razor", "regexp", "ruby", "rust", "sass", "scala", "scss",
  "shellscript", "shellsession", "solidity", "sql", "ssh-config", "stylus", "svelte", "swift",
  "terraform", "tex", "toml", "tsv", "tsx", "twig", "typescript", "v", "vb", "viml", "vue", "vyper",
  "wasm", "wgsl", "xml", "yaml", "zig",
  // Languages highlight.js covered that Shiki has (fenced code and files):
  "actionscript-3", "ada", "asciidoc", "bsl", "common-lisp", "coq", "haxe", "hy", "llvm", "nsis",
  "openscad", "pascal", "prolog", "puppet", "qml", "sas", "scheme", "smalltalk", "stata", "tcl",
  "vala", "verilog", "vhdl", "wolfram",
];

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
    // Pierre only imports shiki/wasm for preferredHighlighter 'shiki-wasm';
    // Crit uses the JS regex engine, so keep the ~230 KB WASM chunk out.
    b.onResolve({ filter: /^shiki\/wasm$/ }, () => ({ path: "shiki-wasm-stub", namespace: "crit-stub" }));
    b.onLoad({ filter: /.*/, namespace: "crit-stub" }, () => ({ contents: "export default null;", loader: "js" }));
  },
};

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
  const result = await build({
    stdin: { contents: ENTRY, resolveDir: process.cwd(), sourcefile: "pierre-entry.js", loader: "js" },
    bundle: true,
    format: "esm",
    splitting: true,
    minify: true,
    platform: "browser",
    target: "es2022",
    outdir: PIERRE_DIR,
    entryNames: "pierre-diffs",
    chunkNames: "chunk-[hash]",
    legalComments: "none",
    plugins: [fineGrainedShiki],
    metafile: true,
    logLevel: "warning",
  });
  // The portable worker has no static imports; its only dynamic import is the
  // Oniguruma WASM chunk used by the 'shiki-wasm' engine, which Crit does not
  // select (it uses the JS regex engine), so the WASM chunk is not shipped.
  copyFileSync("node_modules/@pierre/diffs/dist/worker/worker-portable.js", join(PIERRE_DIR, "pierre-worker.js"));
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
