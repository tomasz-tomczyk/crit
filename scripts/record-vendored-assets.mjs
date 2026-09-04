#!/usr/bin/env node

import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";

const root = new URL("../", import.meta.url);
const manifestPath = new URL("ASSETS-PROVENANCE.txt", root);
const lines = readFileSync(manifestPath, "utf8").split("\n");

const updated = lines.map(line => {
  if (!line || line.startsWith("#")) return line;
  const fields = line.split(/\s+/);
  if (fields.length !== 5) throw new Error(`invalid provenance row: ${line}`);
  const asset = new URL(fields[1], root);
  fields[0] = createHash("sha256").update(readFileSync(asset)).digest("hex");
  return fields.join(" ");
});

writeFileSync(manifestPath, updated.join("\n"));
console.log("Recorded asset hashes. Now run: npm run verify-assets:npm");
