import { cpSync, readdirSync, readFileSync, writeFileSync } from "fs";

const dest = "frontend";

// markdown-it
cpSync("node_modules/markdown-it/dist/markdown-it.min.js", `${dest}/markdown-it.min.js`);

// highlight.js — bundle core + all languages into a single file
const core = readFileSync("node_modules/@highlightjs/cdn-assets/highlight.min.js", "utf8");
const langDir = "node_modules/@highlightjs/cdn-assets/languages";
const langFiles = readdirSync(langDir).filter(f => f.endsWith(".min.js")).sort();
const langs = langFiles.map(f => readFileSync(`${langDir}/${f}`, "utf8")).join("\n");
writeFileSync(`${dest}/highlight.min.js`, core + "\n" + langs);

// mermaid
cpSync("node_modules/mermaid/dist/mermaid.min.js", `${dest}/mermaid.min.js`);

console.log(`Frontend deps copied to frontend/ (${langFiles.length} highlight.js languages bundled)`);
