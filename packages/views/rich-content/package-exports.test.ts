/**
 * Package export-target guard.
 *
 * Every path in a workspace's `exports` map must point at a file that actually
 * exists. Deleting or moving a module without updating `exports` leaves a
 * dangling subpath that still *resolves* for TypeScript (which reads the source
 * tree) but throws at bundle time in whichever app imports it — so it survives
 * typecheck and unit tests and only shows up in a consuming app's build.
 *
 * This regressed once already in MUL-4922: `./common/markdown` was deleted when
 * Chat moved onto RichContent, but its export entry stayed behind.
 *
 * Scoped to the whole monorepo rather than just `views`: the failure mode is a
 * property of package.json, not of this package.
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

/** Every string target in an exports value (string, or a conditions object). */
function exportTargets(value: unknown): string[] {
  if (typeof value === "string") return [value];
  if (value && typeof value === "object") {
    return Object.values(value as Record<string, unknown>).flatMap(exportTargets);
  }
  return [];
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
    const exports = parsed.exports;
    if (!exports || typeof exports !== "object") continue;

    for (const [subpath, value] of Object.entries(exports as Record<string, unknown>)) {
      for (const target of exportTargets(value)) {
        if (target.includes("*")) {
          // Wildcard subpath (e.g. "./locales/*"): the concrete file depends on
          // the importer, so assert the directory prefix exists instead.
          const prefix = target.slice(0, target.indexOf("*"));
          if (!existsSync(join(pkgDir, prefix))) {
            dangling.push({ pkg: relative(REPO_ROOT, pkgPath), subpath, target });
          }
          continue;
        }
        if (!existsSync(join(pkgDir, target))) {
          dangling.push({ pkg: relative(REPO_ROOT, pkgPath), subpath, target });
        }
      }
    }
  }

  return dangling;
}

describe("workspace package exports", () => {
  it("every export target points at a file that exists", () => {
    const dangling = findDanglingExports();
    // Surfaced as readable strings so a failure names the offending subpath
    // instead of printing an object diff.
    expect(dangling.map((d) => `${d.pkg} :: "${d.subpath}" -> ${d.target}`)).toEqual([]);
  });

  it("the deleted chat markdown bridge is not exported", () => {
    const views = JSON.parse(
      readFileSync(join(REPO_ROOT, "packages/views/package.json"), "utf8"),
    ) as { exports?: Record<string, unknown> };

    expect(Object.keys(views.exports ?? {})).not.toContain("./common/markdown");
  });

  it("detects a dangling target when one is introduced", () => {
    // Guards the guard: if the traversal silently stopped finding anything
    // (wrong root, changed layout), the first test would pass vacuously.
    const pkgDir = join(REPO_ROOT, "packages/views");
    expect(existsSync(join(pkgDir, "./editor/index.ts"))).toBe(true);
    expect(existsSync(join(pkgDir, "./common/markdown.tsx"))).toBe(false);
    expect(workspacePackageJsons().length).toBeGreaterThan(3);
  });
});
