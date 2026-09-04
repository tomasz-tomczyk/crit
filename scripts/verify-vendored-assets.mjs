#!/usr/bin/env node

import { createHash } from "node:crypto";
import { mkdtempSync, readFileSync, readdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import process from "node:process";
import { vendoredAssets } from "./vendored-assets.mjs";

const root = new URL("../", import.meta.url);
const manifestPath = new URL("ASSETS-PROVENANCE.txt", root);
const lock = JSON.parse(readFileSync(new URL("package-lock.json", root), "utf8"));
const packageJson = JSON.parse(readFileSync(new URL("package.json", root), "utf8"));
const checkNpm = process.argv.includes("--npm");
const checkInstalled = checkNpm || process.argv.includes("--installed");
let failed = false;

function sha256(path) {
  return createHash("sha256").update(readFileSync(path)).digest("hex");
}

function parseSource(source) {
  const split = source.lastIndexOf("@");
  return { name: source.slice(0, split), version: source.slice(split + 1) };
}

function packageNameFromLockPath(path) {
  const remainder = path.slice(path.lastIndexOf("node_modules/") + "node_modules/".length);
  const parts = remainder.split("/");
  return parts[0].startsWith("@") ? `${parts[0]}/${parts[1]}` : parts[0];
}

function canonicalTarball(name, version) {
  const basename = name.slice(name.lastIndexOf("/") + 1);
  return `https://registry.npmjs.org/${name}/-/${basename}-${version}.tgz`;
}

function run(command, args, options = {}) {
  const result = spawnSync(command, args, { encoding: "utf8", ...options });
  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(" ")} failed:\n${result.stderr || result.stdout}`);
  }
  return result.stdout;
}

const rows = readFileSync(manifestPath, "utf8")
  .split("\n")
  .filter(line => line && !line.startsWith("#"))
  .map(line => {
    const fields = line.split(/\s+/);
    if (fields.length !== 5) throw new Error(`invalid provenance row: ${line}`);
    const [hash, path, sources, packagePath, kind] = fields;
    if (!new Set(["copy", "generated"]).has(kind)) throw new Error(`invalid asset kind in row: ${line}`);
    if ((kind === "copy") !== (packagePath !== "-")) throw new Error(`invalid package path for ${kind} row: ${line}`);
    return { hash, path, sources: sources.split(","), packagePath, kind };
  });

if (rows.length !== vendoredAssets.length) throw new Error("provenance row count does not match vendored asset definitions");
for (let index = 0; index < rows.length; index++) {
  const { hash: _hash, ...actual } = rows[index];
  if (JSON.stringify(actual) !== JSON.stringify(vendoredAssets[index])) {
    throw new Error(`provenance metadata mismatch for row ${index + 1}`);
  }
}

const recordedPaths = new Set(rows.map(row => row.path));
for (const name of readdirSync(new URL("web/", root)).filter(name => name.endsWith(".min.js"))) {
  const path = `web/${name}`;
  if (!recordedPaths.has(path)) {
    console.error(`UNRECORDED ${path}: add it to ASSETS-PROVENANCE.txt`);
    failed = true;
  }
}

for (const [name, version] of Object.entries({
  ...packageJson.dependencies,
  ...packageJson.devDependencies,
})) {
  const resolved = lock.packages?.[`node_modules/${name}`]?.version;
  if (version !== resolved) {
    console.error(`UNPINNED ${name}: package.json says ${version}, lockfile resolves ${resolved ?? "nothing"}`);
    failed = true;
  }
}

for (const [path, entry] of Object.entries(lock.packages ?? {})) {
  if (!path || entry.link) continue;
  const name = packageNameFromLockPath(path);
  const expected = canonicalTarball(name, entry.version);
  if (entry.resolved !== expected || !entry.integrity?.startsWith("sha512-")) {
    console.error(`UNTRUSTED LOCK ENTRY ${path}: expected ${expected} with sha512 integrity`);
    failed = true;
  }
}

for (const row of rows) {
  const asset = new URL(row.path, root);
  const actual = sha256(asset);
  if (actual !== row.hash) {
    console.error(`MODIFIED ${row.path}\n recorded: ${row.hash}\n actual:   ${actual}`);
    failed = true;
  } else {
    console.log(`ok ${row.path} (${row.kind})`);
  }

  for (const source of row.sources) {
    const { name, version } = parseSource(source);
    const entry = lock.packages?.[`node_modules/${name}`];
    if (entry?.version !== version) {
      console.error(`LOCK MISMATCH ${name}: provenance says ${version}, lockfile resolves ${entry?.version ?? "nothing"}`);
      failed = true;
    }
  }

  if (checkInstalled && row.kind === "copy") {
    const { name } = parseSource(row.sources[0]);
    const installed = new URL(`node_modules/${name}/${row.packagePath}`, root);
    if (sha256(installed) !== actual) {
      console.error(`INSTALLED MISMATCH ${row.path}: differs from ${row.sources[0]} ${row.packagePath}`);
      failed = true;
    } else {
      console.log(`installed ${row.path} is byte-identical to ${row.sources[0]} ${row.packagePath}`);
    }
  }

  if (checkNpm && row.kind === "copy") {
    const temp = mkdtempSync(join(tmpdir(), "crit-asset-"));
    try {
      const packed = JSON.parse(run("npm", ["pack", row.sources[0], "--json", "--pack-destination", temp], { cwd: new URL(".", root) }));
      const tarball = join(temp, packed[0].filename);
      run("tar", ["-xzf", tarball, "-C", temp]);
      const published = join(temp, "package", row.packagePath);
      if (sha256(published) !== actual) {
        console.error(`NPM MISMATCH ${row.path}: differs from ${row.sources[0]} ${row.packagePath}`);
        failed = true;
      } else {
        console.log(`npm ${row.path} is byte-identical to ${row.sources[0]} ${row.packagePath}`);
      }
    } finally {
      rmSync(temp, { recursive: true, force: true });
    }
  }
}

if (failed) {
  console.error("Vendored asset verification failed.");
  process.exit(1);
}

console.log("All vendored assets verified.");
