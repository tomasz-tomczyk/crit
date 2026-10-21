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

// Versions come from package-lock.json, so `npm run record-assets` can write
// them into ASSETS-PROVENANCE.txt after a dependency bump.
export function sourcesFor(asset, lock) {
  return asset.sources.map(name => `${name}@${lock.packages?.[`node_modules/${name}`]?.version ?? "missing"}`);
}

export const vendoredAssets = [
  {
    path: "web/dompurify.min.js",
    sources: ["dompurify"],
    packagePath: "dist/purify.min.js",
    kind: "copy",
  },
  {
    path: "web/markdown-it.min.js",
    sources: ["markdown-it"],
    packagePath: "dist/browser/markdown-it.umd.min.js",
    kind: "copy",
  },
  {
    path: "web/mermaid.min.js",
    sources: ["mermaid"],
    packagePath: "dist/mermaid.min.js",
    kind: "copy",
  },
  {
    path: "web/diff-match-patch.min.js",
    sources: ["@sanity/diff-match-patch", "esbuild"],
    packagePath: "-",
    kind: "generated",
  },
  {
    path: "web/pierre",
    sources: ["@pierre/diffs", "shiki", "@shikijs/themes", "esbuild"],
    packagePath: "-",
    kind: "generated",
  },
];
