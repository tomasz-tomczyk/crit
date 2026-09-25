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
];

// Drop what Crit never loads: all bundled Shiki themes (Crit uses Pierre's
// own pierre-dark/pierre-light), the Oniguruma WASM engine (Crit uses Shiki's
// JS regex engine), and grammars outside SHIKI_LANGS.
const trimShiki = {
  name: "crit-trim-shiki",
  setup(b) {
    const allowed = new Set(SHIKI_LANGS);
    b.onLoad({ filter: /shiki[\\/]dist[\\/]langs-bundle-full-[^\\/]+\.mjs$/ }, args => {
      const src = readFileSync(args.path, "utf8");
      let kept = 0;
      const out = src.replace(/\t\{\n[\s\S]*?\n\t\}(,?)\n/g, (block, comma) => {
        const id = /"id": "([^"]+)"/.exec(block);
        if (id && allowed.has(id[1])) { kept++; return block.replace(/\}(,?)\n$/, "},\n"); }
        return "";
      }).replace(/,\n\];/, "\n];");
      if (kept !== allowed.size) throw new Error(`shiki lang allowlist: kept ${kept}, expected ${allowed.size}`);
      return { contents: out, loader: "js" };
    });
    b.onLoad({ filter: /shiki[\\/]dist[\\/]themes\.mjs$/ }, () => ({
      contents: "export const bundledThemesInfo = []; export const bundledThemes = {};",
      loader: "js",
    }));
    b.onResolve({ filter: /^shiki\/wasm$/ }, () => ({ path: "shiki-wasm-stub", namespace: "crit-stub" }));
    b.onLoad({ filter: /.*/, namespace: "crit-stub" }, () => ({
      contents: "export default null;",
      loader: "js",
    }));
  },
};

const ENTRY = `\
import {
  CodeView,
  FileDiff,
  processFile,
  parseDiffFromFile,
  getFiletypeFromFileName,
  setLanguageOverride,
  preloadHighlighter,
} from '@pierre/diffs';
import {
  getOrCreateWorkerPoolSingleton,
  terminateWorkerPoolSingleton,
} from '@pierre/diffs/worker';

window.PierreDiffs = {
  CodeView,
  FileDiff,
  processFile,
  parseDiffFromFile,
  getFiletypeFromFileName,
  setLanguageOverride,
  preloadHighlighter,
  getOrCreateWorkerPoolSingleton,
  terminateWorkerPoolSingleton,
};
`;

export async function buildPierre() {
  rmSync(PIERRE_DIR, { recursive: true, force: true });
  mkdirSync(PIERRE_DIR, { recursive: true });
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
    plugins: [trimShiki],
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
