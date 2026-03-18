import { cpSync, readFileSync, writeFileSync } from "fs";

const dest = "frontend";

// markdown-it
cpSync("node_modules/markdown-it/dist/markdown-it.min.js", `${dest}/markdown-it.min.js`);

// highlight.js core
cpSync("node_modules/@highlightjs/cdn-assets/highlight.min.js", `${dest}/highlight.min.js`);

// highlight.js languages
const langs = [
  "bash", "css", "elixir", "go", "javascript", "json",
  "python", "ruby", "rust", "sql", "typescript", "xml", "yaml",
];
for (const lang of langs) {
  cpSync(
    `node_modules/@highlightjs/cdn-assets/languages/${lang}.min.js`,
    `${dest}/hljs-${lang}.min.js`,
  );
}

// mermaid
cpSync("node_modules/mermaid/dist/mermaid.min.js", `${dest}/mermaid.min.js`);

// diff-match-patch (wrap module.exports for browser use)
let dmpSrc = readFileSync("node_modules/diff-match-patch/index.js", "utf8");
dmpSrc = dmpSrc.replace(
  /^module\.exports.*$/gm,
  "// $& (stripped for browser)",
);
writeFileSync(`${dest}/diff-match-patch.js`, dmpSrc);

console.log("Frontend deps copied to frontend/");
