import { createHash } from "node:crypto";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

// sha256 of a vendored asset. A generated directory (web/pierre) hashes as the
// sha256 of its sorted "name sha256" listing, so one provenance row covers
// every chunk and any added, removed or changed file changes the hash.
export function hashAsset(path) {
  if (!statSync(path).isDirectory()) {
    return createHash("sha256").update(readFileSync(path)).digest("hex");
  }
  const listing = readdirSync(path)
    .sort()
    .map(name => `${name} ${createHash("sha256").update(readFileSync(join(path, name))).digest("hex")}`)
    .join("\n");
  return createHash("sha256").update(listing).digest("hex");
}

export const vendoredAssets = [
  {
    path: "web/dompurify.min.js",
    sources: ["dompurify@3.4.15"],
    packagePath: "dist/purify.min.js",
    kind: "copy",
  },
  {
    path: "web/markdown-it.min.js",
    sources: ["markdown-it@15.0.2"],
    packagePath: "dist/browser/markdown-it.umd.min.js",
    kind: "copy",
  },
  {
    path: "web/mermaid.min.js",
    sources: ["mermaid@11.17.2"],
    packagePath: "dist/mermaid.min.js",
    kind: "copy",
  },
  {
    path: "web/highlight.min.js",
    sources: [
      "@highlightjs/cdn-assets@11.12.0",
      "highlightjs-heex@1.0.1",
      "highlightjs-vue@1.0.0",
      "highlightjs-astro-js@1.0.0",
    ],
    packagePath: "-",
    kind: "generated",
  },
  {
    path: "web/diff-match-patch.min.js",
    sources: ["@sanity/diff-match-patch@3.2.0", "esbuild@0.28.2"],
    packagePath: "-",
    kind: "generated",
  },
  {
    path: "web/pierre",
    sources: ["@pierre/diffs@1.5.1", "shiki@4.4.3", "esbuild@0.28.2"],
    packagePath: "-",
    kind: "generated",
  },
];
