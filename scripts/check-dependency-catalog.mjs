#!/usr/bin/env node
/**
 * Keeps shared external dependencies on the pnpm catalog (AGENTS.md).
 *
 * An external dependency declared by two or more workspaces must use
 * `catalog:` everywhere, so one edit to pnpm-workspace.yaml moves every
 * consumer together. A dependency the catalog already defines must use it even
 * in a single workspace, or the two versions drift silently.
 *
 * Peer ranges state compatibility rather than the installed version, so they
 * are not checked. apps/mobile is exempt: it pins Expo/React Native-aligned
 * versions in its own manifest (apps/mobile/AGENTS.md).
 *
 * Run: node scripts/check-dependency-catalog.mjs
 */

import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(import.meta.dirname, "..");
const checkedFields = ["dependencies", "devDependencies", "optionalDependencies"];
const exemptWorkspaces = new Set(["apps/mobile"]);

// Known drift that needs its own upgrade. Each entry must still be a live
// violation; the check fails once the drift is gone so the entry gets removed.
export const allowlist = new Map([
  [
    "@vitejs/plugin-react",
    "apps/desktop bundles with plugin-react 5; moving it to the catalog's 6 changes how pnpm resolves vite for electron-vite and needs its own verification.",
  ],
  [
    "fumadocs-core",
    "apps/web is on fumadocs 15 and apps/docs on 16; aligning them needs an apps/web fumadocs upgrade.",
  ],
  [
    "fumadocs-mdx",
    "apps/web is on fumadocs-mdx 12 and apps/docs on 15; aligning them needs an apps/web fumadocs upgrade.",
  ],
]);

const unquote = (value) => value.trim().replace(/^(['"])(.*)\1$/, "$2");

/** Reads `packages` and the default `catalog` from pnpm-workspace.yaml. */
export function parseWorkspaceManifest(source) {
  const packages = [];
  const catalog = new Map();
  let section = null;
  for (const raw of source.split("\n")) {
    const line = raw.replace(/\s+#.*$/, "");
    if (!line.trim() || line.trimStart().startsWith("#")) continue;
    if (!/^\s/.test(line)) {
      section = line.replace(/:.*$/, "").trim();
      continue;
    }
    if (section === "packages") {
      const item = line.match(/^\s+-\s+(.+)$/);
      if (item) packages.push(unquote(item[1]));
    } else if (section === "catalog") {
      const entry = line.match(/^\s+('[^']+'|"[^"]+"|[^\s:]+):\s*(.+)$/);
      if (entry) catalog.set(unquote(entry[1]), unquote(entry[2]));
    }
  }
  return { packages, catalog };
}

const isCatalogSpec = (spec) => spec.startsWith("catalog:");
const isWorkspaceSpec = (spec) => spec.startsWith("workspace:");

/**
 * @param {{ catalog: Map<string, string>, workspaces: { dir: string, manifest: object }[], allowlist?: Map<string, string> }} input
 * @returns {string[]} human-readable violations
 */
export function catalogViolations({ catalog, workspaces, allowlist: allowed = new Map() }) {
  const declarations = new Map();
  for (const { dir, manifest } of workspaces) {
    if (exemptWorkspaces.has(dir)) continue;
    for (const field of checkedFields) {
      for (const [name, spec] of Object.entries(manifest[field] ?? {})) {
        if (isWorkspaceSpec(spec)) continue;
        const list = declarations.get(name) ?? [];
        list.push({ dir, field, spec });
        declarations.set(name, list);
      }
    }
  }

  const violations = [];
  const liveAllowlist = new Set();
  for (const [name, list] of [...declarations].sort(([a], [b]) => a.localeCompare(b))) {
    const plain = list.filter(({ spec }) => !isCatalogSpec(spec));
    if (plain.length === 0) continue;
    if (!catalog.has(name) && list.length < 2) continue;
    if (allowed.has(name)) {
      liveAllowlist.add(name);
      continue;
    }

    const where = plain.map(({ dir, field, spec }) => `${dir} ${field} ${spec}`).join(", ");
    violations.push(
      catalog.has(name)
        ? `${name}: catalog defines ${catalog.get(name)}; use "catalog:" in ${where}`
        : `${name}: declared by ${list.length} workspaces; add it to the catalog and use "catalog:" in ${where}`,
    );
  }

  for (const name of allowed.keys()) {
    if (!liveAllowlist.has(name)) {
      violations.push(`${name}: allowlisted but no longer a non-catalog duplicate; remove it from the allowlist`);
    }
  }
  return violations;
}

function workspaceDirs(patterns) {
  const dirs = [""];
  for (const pattern of patterns) {
    if (!pattern.endsWith("/*")) {
      dirs.push(pattern);
      continue;
    }
    const parent = pattern.slice(0, -2);
    for (const entry of readdirSync(join(repoRoot, parent), { withFileTypes: true })) {
      if (entry.isDirectory()) dirs.push(`${parent}/${entry.name}`);
    }
  }
  return dirs.filter((dir) => existsSync(join(repoRoot, dir, "package.json")));
}

function main() {
  const { packages, catalog } = parseWorkspaceManifest(
    readFileSync(join(repoRoot, "pnpm-workspace.yaml"), "utf8"),
  );
  const workspaces = workspaceDirs(packages).map((dir) => ({
    dir: dir || ".",
    manifest: JSON.parse(readFileSync(join(repoRoot, dir, "package.json"), "utf8")),
  }));
  const violations = catalogViolations({ catalog, workspaces, allowlist });

  if (violations.length > 0) {
    console.error(`Dependencies off the pnpm catalog (${violations.length})`);
    for (const violation of violations) console.error(`  ${violation}`);
    console.error(
      "\nShared external dependencies use \"catalog:\" with the version in pnpm-workspace.yaml. apps/mobile pins its own versions and is exempt.",
    );
    process.exit(1);
  }

  console.log(`Dependency catalog clean (${workspaces.length} workspaces checked).`);
}

if (resolve(process.argv[1] ?? "") === fileURLToPath(import.meta.url)) main();
