import { cpSync, readdirSync, readFileSync, writeFileSync, unlinkSync } from "fs";
import { execSync } from "child_process";

const dest = "frontend";

// markdown-it
cpSync("node_modules/markdown-it/dist/markdown-it.min.js", `${dest}/markdown-it.min.js`);

// highlight.js — bundle core + all languages + local patches into a single file.
// Order: core (defines hljs) → languages (registers 'markdown') → patches
// (re-registers 'markdown' with crit's fixes). See highlight-markdown-patch.js.
const core = readFileSync("node_modules/@highlightjs/cdn-assets/highlight.min.js", "utf8");
const langDir = "node_modules/@highlightjs/cdn-assets/languages";
const langFiles = readdirSync(langDir).filter(f => f.endsWith(".min.js")).sort();
const langs = langFiles.map(f => readFileSync(`${langDir}/${f}`, "utf8")).join("\n");
const patch = readFileSync(`${dest}/highlight-markdown-patch.js`, "utf8");
const heex = readFileSync("node_modules/highlightjs-heex/dist/heex.min.js", "utf8");
const heexReg = heex + "\nhljs.registerLanguage('heex', hljsDefineHeex);";
writeFileSync(`${dest}/highlight.min.js`, core + "\n" + langs + "\n" + patch + "\n" + heexReg);

// mermaid
cpSync("node_modules/mermaid/dist/mermaid.min.js", `${dest}/mermaid.min.js`);

// @sanity/diff-match-patch — ESM-only, bundle to IIFE with esbuild
// Expose makeDiff, cleanupSemantic, and constants as window.DiffMatchPatch
const dmpEntry = `${dest}/_dmp-entry.js`;
writeFileSync(dmpEntry, `\
import {makeDiff, cleanupSemantic, DIFF_DELETE, DIFF_EQUAL, DIFF_INSERT} from '@sanity/diff-match-patch';
window.DiffMatchPatch = {makeDiff, cleanupSemantic, DIFF_DELETE, DIFF_EQUAL, DIFF_INSERT};
`);
execSync(`npx esbuild ${dmpEntry} --bundle --format=iife --minify --outfile=${dest}/diff-match-patch.min.js`, { stdio: 'inherit' });
// Clean up temporary entry file
unlinkSync(dmpEntry);

console.log(`Frontend deps copied to frontend/ (${langFiles.length} highlight.js languages bundled)`);
