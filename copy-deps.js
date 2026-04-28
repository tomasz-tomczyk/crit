import { cpSync, readdirSync, readFileSync, writeFileSync, unlinkSync } from "fs";
import { execSync } from "child_process";

const dest = "frontend";

// markdown-it
cpSync("node_modules/markdown-it/dist/markdown-it.min.js", `${dest}/markdown-it.min.js`);

// Prism.js — bundle core + every available language in dependency order.
// Topologically sorted from components.json so each language's `require` deps
// load first.
const prismDir = "node_modules/prismjs";
const prismCompDir = `${prismDir}/components`;
const prismCore = readFileSync(`${prismDir}/prism.js`, "utf8");
const prismManifest = JSON.parse(readFileSync(`${prismDir}/components.json`, "utf8")).languages;
delete prismManifest.meta;
const prismLangs = [];
const prismVisited = new Set();
function visitPrismLang(name) {
  if (prismVisited.has(name)) return;
  prismVisited.add(name);
  const def = prismManifest[name];
  if (!def) return;
  let deps = def.require || [];
  if (typeof deps === "string") deps = [deps];
  for (const d of deps) visitPrismLang(d);
  prismLangs.push(name);
}
for (const n of Object.keys(prismManifest)) visitPrismLang(n);
const prismParts = [prismCore];
for (const l of prismLangs) {
  prismParts.push(readFileSync(`${prismCompDir}/prism-${l}.min.js`, "utf8"));
}
writeFileSync(`${dest}/prism.min.js`, prismParts.join("\n"));

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

console.log(`Frontend deps copied to frontend/ (${prismLangs.length} Prism.js languages bundled)`);
