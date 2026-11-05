// @vitest-environment node
/**
 * Package export-target guard.
 *
 * A dangling export can still resolve from the source tree during typecheck,
 * then fail only when a consuming application is bundled. This regressed in
 * MUL-4922, so validate the filesystem boundary rather than mirroring an
 * individual package.json entry.
 */
import { afterEach, describe, expect, it } from "vitest";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { tmpdir } from "node:os";
import ts from "typescript";

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
    return Object.values(value as Record<string, unknown>).flatMap(
      exportTargets,
    );
  }
  return [];
}

function exportTargetExists(pkgDir: string, target: string): boolean {
  const path = target.includes("*")
    ? target.slice(0, target.indexOf("*"))
    : target;
  return (
    existsSync(join(pkgDir, path)) || isDeclaredCompilerOutput(pkgDir, target)
  );
}

function isDeclaredCompilerOutput(pkgDir: string, target: string): boolean {
  // Compiled workspaces keep outputs out of git; validate their actual compiler
  // mapping rather than requiring a previous build or trusting a dist prefix.
  const manifestPath = join(pkgDir, "package.json");
  if (!existsSync(manifestPath)) return false;
  const manifest = JSON.parse(readFileSync(manifestPath, "utf8")) as {
    scripts?: Record<string, string>;
  };
  for (const [name, command] of Object.entries(manifest.scripts ?? {})) {
    if (!/^build(?::|$)/.test(name)) continue;
    const project = /\btsc\s+(?:-p|--project)\s+([\w./-]+\.json)\b/.exec(
      command,
    )?.[1];
    if (!project) continue;
    const configPath = resolve(pkgDir, project);
    const config = ts.readConfigFile(configPath, ts.sys.readFile);
    if (config.error) continue;
    const parsed = ts.parseJsonConfigFileContent(
      config.config,
      ts.sys,
      dirname(configPath),
      undefined,
      configPath,
    );
    if (parsed.errors.length || parsed.options.noEmit || !parsed.options.outDir)
      continue;
    const output = resolve(pkgDir, target);
    if (
      parsed.fileNames.some(
        (file) =>
          !/\.d\.[cm]?ts$/.test(file) &&
          ts
            .getOutputFileNames(parsed, file, false)
            .some((path) => resolve(path) === output),
      )
    )
      return true;
  }
  return false;
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

const fixtures: string[] = [];
afterEach(() => {
  for (const fixture of fixtures.splice(0))
    rmSync(fixture, { recursive: true, force: true });
});

function compiledWorkspace(noEmit = false): string {
  const root = mkdtempSync(join(tmpdir(), "multica-export-targets-"));
  fixtures.push(root);
  mkdirSync(join(root, "src"));
  writeFileSync(
    join(root, "package.json"),
    JSON.stringify({
      scripts: { "build:manual": "tsc -p tsconfig.build.json" },
    }),
  );
  writeFileSync(
    join(root, "tsconfig.build.json"),
    JSON.stringify({
      compilerOptions: {
        rootDir: "src",
        outDir: "dist",
        declaration: true,
        noEmit,
      },
      include: ["src/**/*.ts"],
    }),
  );
  writeFileSync(join(root, "src/index.ts"), "export const value = 1;\n");
  return root;
}

describe("workspace package exports", () => {
  it("every export target has a file or a declared compiler output", () => {
    const dangling = findDanglingExports();
    expect(
      dangling.map((d) => `${d.pkg} :: "${d.subpath}" -> ${d.target}`),
    ).toEqual([]);
  });

  it("accepts generated JavaScript and declarations before a compiled workspace is built", () => {
    const root = compiledWorkspace();
    expect(existsSync(join(root, "dist"))).toBe(false);
    expect(exportTargetExists(root, "./dist/index.js")).toBe(true);
    expect(exportTargetExists(root, "./dist/index.d.ts")).toBe(true);
    expect(exportTargetExists(root, "./dist/missing.js")).toBe(false);
    expect(exportTargetExists(root, "./outside/index.js")).toBe(false);
  });

  it("rejects generated targets without an emitting build and existing input", () => {
    const root = compiledWorkspace(true);
    expect(exportTargetExists(root, "./dist/index.js")).toBe(false);
    const missingInput = compiledWorkspace();
    rmSync(join(missingInput, "src/index.ts"));
    expect(exportTargetExists(missingInput, "./dist/index.d.ts")).toBe(false);
  });

  it("does not pass vacuously when traversal or existence checks break", () => {
    const viewsDir = join(REPO_ROOT, "packages/views");
    expect(workspacePackageJsons().length).toBeGreaterThan(3);
    expect(exportTargetExists(viewsDir, "./editor/index.ts")).toBe(true);
    expect(exportTargetExists(viewsDir, "./definitely-missing.ts")).toBe(false);
  });
});
