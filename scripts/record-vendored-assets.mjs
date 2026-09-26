#!/usr/bin/env node

import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { hashAsset } from "./vendored-assets.mjs";

const root = new URL("../", import.meta.url);
const manifestPath = new URL("ASSETS-PROVENANCE.txt", root);
const lines = readFileSync(manifestPath, "utf8").split("\n");

const updated = lines.map(line => {
  if (!line || line.startsWith("#")) return line;
  const fields = line.split(/\s+/);
  if (fields.length !== 5) throw new Error(`invalid provenance row: ${line}`);
  fields[0] = hashAsset(fileURLToPath(new URL(fields[1], root)));
  return fields.join(" ");
});

writeFileSync(manifestPath, updated.join("\n"));
console.log("Recorded asset hashes. Now run: npm run verify-assets:npm");
