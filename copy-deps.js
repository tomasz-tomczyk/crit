import { cpSync } from "fs";

const dest = "frontend";

// markdown-it
cpSync("node_modules/markdown-it/dist/markdown-it.min.js", `${dest}/markdown-it.min.js`);

// mermaid
cpSync("node_modules/mermaid/dist/mermaid.min.js", `${dest}/mermaid.min.js`);

console.log("Frontend deps copied to frontend/");
