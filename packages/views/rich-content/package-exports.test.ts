// @vitest-environment node
/**
 * Package export-target guard.
 *
 * A dangling export can still resolve from the source tree during typecheck,
 * then fail only when a consuming application is bundled. This regressed in
 * MUL-4922, so validate the filesystem boundary rather than mirroring an
 * individual package.json entry.
 */
import { describe, expect, it } from "vitest";
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, relative } from "node:path";

const REPO_ROOT = join(__dirname, "..", "..", "..");

function workspacePackageJsons(): string[] {
  const out: string[] = [];
  for (const group of ["packages", "apps"]) {
    const groupDir = join(REPO_ROOT, group);
    if (!existsSync(groupDir)) continue;
    for (const entry of readdirSync(groupDir)) {
      const pkg = join(groupDir, entry, "package.json");
      if (existsSync(pkg) && statSync(pkg).isFile()) out.push(pkg);
    }
  }
  return out;
}

function exportTargets(value: unknown): string[] {
  if (typeof value === "string") return [value];
  if (value && typeof value === "object") {
    return Object.values(value as Record<string, unknown>).flatMap(exportTargets);
  }
  return [];
}

function exportTargetExists(pkgDir: string, target: string): boolean {
  const path = target.includes("*") ? target.slice(0, target.indexOf("*")) : target;
  return existsSync(join(pkgDir, path));
}

interface Dangling {
  pkg: string;
  subpath: string;
  target: string;
}

function findDanglingExports(): Dangling[] {
  const dangling: Dangling[] = [];

  for (const pkgPath of workspacePackageJsons()) {
    const pkgDir = dirname(pkgPath);
    let parsed: { exports?: unknown };
    try {
      parsed = JSON.parse(readFileSync(pkgPath, "utf8"));
    } catch {
      continue;
    }
    if (!parsed.exports || typeof parsed.exports !== "object") continue;

    for (const [subpath, value] of Object.entries(
      parsed.exports as Record<string, unknown>,
    )) {
      for (const target of exportTargets(value)) {
        if (!exportTargetExists(pkgDir, target)) {
          dangling.push({
            pkg: relative(REPO_ROOT, pkgPath),
            subpath,
            target,
          });
        }
      }
    }
  }

  return dangling;
}

describe("workspace package exports", () => {
  it("every export target points at a file that exists", () => {
    const dangling = findDanglingExports();
    expect(dangling.map((d) => `${d.pkg} :: "${d.subpath}" -> ${d.target}`)).toEqual([]);
  });

  it("does not pass vacuously when traversal or existence checks break", () => {
    const viewsDir = join(REPO_ROOT, "packages/views");
    expect(workspacePackageJsons().length).toBeGreaterThan(3);
    expect(exportTargetExists(viewsDir, "./editor/index.ts")).toBe(true);
    expect(exportTargetExists(viewsDir, "./definitely-missing.ts")).toBe(false);
  });
});
