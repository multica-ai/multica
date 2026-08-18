#!/usr/bin/env node
/**
 * Reports files in packages/ui that no other file imports.
 *
 * This exists because knip structurally cannot see them. knip derives entry
 * points from a workspace's package.json `exports` (graph/build.js calls
 * `getEntrySpecifiersFromManifest` unconditionally — there is no config flag to
 * opt out), and packages/ui exports four wildcards:
 *
 *     "./components/ui/*"     -> "./components/ui/*.tsx"
 *     "./components/common/*" -> "./components/common/*.tsx"
 *     "./markdown/*"          -> "./markdown/*.tsx"
 *     "./hooks/*"             -> "./hooks/*.ts"
 *
 * Each expands to a glob covering every file in the directory, so all of them
 * register as entry points and none can ever be reported unused. Setting
 * `entry: []`, negating the paths in `entry`, and `--production` were all
 * tried; manifest entries are a separate code path from configured ones and
 * none of them subtract from it. packages/views is unaffected — 44 of its 45
 * exports name a specific file — which is why knip does flag dead files there.
 *
 * That blind spot is exactly where the 16 dead components removed in MUL-6353
 * lived, so without this check the cleanup has no regression guard at all.
 *
 * Run: node scripts/check-ui-wildcard-exports.mjs
 */

import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, resolve, sep } from "node:path";

const repoRoot = resolve(import.meta.dirname, "..");
const uiRoot = join(repoRoot, "packages", "ui");
const pkgName = "@multica/ui";

const SOURCE_EXTENSIONS = new Set([".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"]);
const SKIP_DIRS = new Set(["node_modules", ".git", ".next", ".turbo", "out", "dist", "build"]);

function walk(dir, out = []) {
  for (const dirent of readdirSync(dir, { withFileTypes: true })) {
    if (dirent.name.startsWith(".") && dirent.name !== ".github") continue;
    const full = join(dir, dirent.name);
    if (dirent.isDirectory()) {
      if (SKIP_DIRS.has(dirent.name)) continue;
      walk(full, out);
    } else if (SOURCE_EXTENSIONS.has(dirent.name.slice(dirent.name.lastIndexOf(".")))) {
      out.push(full);
    }
  }
  return out;
}

// Derived from the manifest rather than hard-coded: if someone narrows a
// wildcard export, this check narrows with it instead of going quietly stale.
const manifest = JSON.parse(readFileSync(join(uiRoot, "package.json"), "utf8"));
const wildcards = Object.entries(manifest.exports ?? {})
  .filter(([subpath, target]) => subpath.includes("*") && String(target).includes("*"))
  .map(([subpath, target]) => {
    const targetPath = String(target).replace(/^\.\//, "");
    const dir = targetPath.slice(0, targetPath.lastIndexOf("/"));
    const extension = targetPath.slice(targetPath.lastIndexOf("*") + 1);
    return { subpath, dir, extension };
  });

if (wildcards.length === 0) {
  console.log("No wildcard exports in packages/ui — knip covers this workspace on its own.");
  process.exit(0);
}

const sourceFiles = walk(repoRoot);
// One read of every source file, reused across candidates. Reading per
// candidate would re-read the tree ~90 times.
const contents = new Map(sourceFiles.map((file) => [file, readFileSync(file, "utf8")]));

const dead = [];

for (const { dir, extension } of wildcards) {
  const absoluteDir = join(uiRoot, dir);
  let entries;
  try {
    entries = readdirSync(absoluteDir);
  } catch {
    continue; // Exported directory does not exist yet.
  }

  for (const entry of entries) {
    if (!entry.endsWith(extension)) continue;
    const file = join(absoluteDir, entry);
    if (!statSync(file).isFile()) continue;

    const name = entry.slice(0, -extension.length);
    // The public specifier, plus the relative forms a sibling inside
    // packages/ui would use. Anchored on a path boundary so `button` does not
    // match `button-group`.
    const specifier = `${pkgName}/${dir}/${name}`;
    const pattern = new RegExp(
      `["'](?:${escape(specifier)}|(?:\\.{1,2}/)+(?:[\\w./-]*/)?${escape(name)})["']`,
    );

    const referenced = sourceFiles.some(
      (candidate) => candidate !== file && pattern.test(contents.get(candidate)),
    );
    if (!referenced) dead.push(relative(repoRoot, file).split(sep).join("/"));
  }
}

if (dead.length > 0) {
  console.error(`Unused files in packages/ui wildcard exports (${dead.length})`);
  for (const file of dead.sort()) console.error(`  ${file}`);
  console.error(
    "\nNothing imports these. Delete them — shadcn components come back with" +
      "\n`pnpm ui:add <name>`. If a file is genuinely entry-like, give it a" +
      "\nnon-wildcard entry in packages/ui/package.json `exports`.",
  );
  process.exit(1);
}

console.log(
  `packages/ui wildcard exports clean (${wildcards.map((w) => w.dir).join(", ")}).`,
);

function escape(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
