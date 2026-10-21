#!/usr/bin/env node

import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { hashAsset, sourcesFor, vendoredAssets } from "./vendored-assets.mjs";

const root = new URL("../", import.meta.url);
const manifestPath = new URL("ASSETS-PROVENANCE.txt", root);
const lock = JSON.parse(readFileSync(new URL("package-lock.json", root), "utf8"));
const lines = readFileSync(manifestPath, "utf8").split("\n");

const updated = lines.map(line => {
  if (!line || line.startsWith("#")) return line;
  const fields = line.split(/\s+/);
  if (fields.length !== 5) throw new Error(`invalid provenance row: ${line}`);
  const asset = vendoredAssets.find(asset => asset.path === fields[1]);
  if (!asset) throw new Error(`no vendored asset definition for ${fields[1]}`);
  fields[0] = hashAsset(fileURLToPath(new URL(fields[1], root)));
  fields[2] = sourcesFor(asset, lock).join(",");
  return fields.join(" ");
});

writeFileSync(manifestPath, updated.join("\n"));
console.log("Recorded asset hashes and versions. Now run: npm run verify-assets:npm");
