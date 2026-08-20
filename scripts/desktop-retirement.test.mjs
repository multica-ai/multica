import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { extname, join, relative } from "node:path";
import test from "node:test";

const root = new URL("..", import.meta.url).pathname;
const sourceRoots = ["apps/web", "apps/tag-host", "apps/mobile", "packages", "server"];
const sourceExtensions = new Set([".cjs", ".go", ".js", ".mjs", ".ts", ".tsx"]);
const deferredSharedAuthCallers = new Set([
  "apps/web/app/(auth)/login/page.tsx",
  "apps/web/app/auth/callback/page.tsx",
]);
const forbiddenCallers = [
  /from\s+["']electron(?:["'/]|$)/u,
  /require\(["']electron["']\)/u,
  /electron-(?:builder|updater|vite)/u,
  /multica:\/\//u,
  /platform:desktop/u,
  /(?:identity\?\.)?platform\s*[!=]==?\s*["']desktop["']/u,
  /\bPlatformDesktop\b/u,
  /["']probe-runtimes["']/u,
  /\bdesktopAPI\b/u,
  /window\.electron\b/u,
];

function sourceFiles(directory) {
  if (!existsSync(directory)) return [];
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) return sourceFiles(path);
    if (!sourceExtensions.has(extname(path))) return [];
    if (/\.(?:test|spec)\.[^.]+$/u.test(path) || /_test\.go$/u.test(path)) return [];
    return [path];
  });
}

test("Desktop application and packaging surfaces are physically retired", () => {
  assert.equal(existsSync(join(root, "apps/desktop")), false);
  assert.equal(existsSync(join(root, ".github/workflows/desktop-smoke.yml")), false);

  const rootManifest = readFileSync(join(root, "package.json"), "utf8");
  assert.doesNotMatch(rootManifest, /dev:desktop|@multica\/desktop|["']electron["']/u);

  for (const config of [
    ".github/workflows/ci.yml",
    ".github/workflows/release.yml",
    "turbo.json",
  ]) {
    const contents = readFileSync(join(root, config), "utf8");
    assert.doesNotMatch(contents, /apps\/desktop|@multica\/desktop|electron-builder|DESKTOP_/u);
  }
});

test("production source outside #295 shared auth has no Electron or Desktop caller", () => {
  const violations = [];
  for (const sourceRoot of sourceRoots) {
    for (const file of sourceFiles(join(root, sourceRoot))) {
      if (deferredSharedAuthCallers.has(relative(root, file))) continue;
      const contents = readFileSync(file, "utf8");
      for (const pattern of forbiddenCallers) {
        if (pattern.test(contents)) {
          violations.push(`${relative(root, file)}: ${pattern.source}`);
        }
      }
    }
  }
  assert.deepEqual(violations, []);
});

test("shared Web, Tag, Mobile, Core, CLI, Daemon and Runtime roots remain", () => {
  for (const retained of [
    "apps/web",
    "apps/tag-host",
    "apps/mobile",
    "packages/core",
    "packages/views",
    "server/cmd/multica",
    "server/internal/daemon",
    "server/internal/runtimeapps",
  ]) {
    assert.equal(existsSync(join(root, retained)), true, retained);
  }
});
