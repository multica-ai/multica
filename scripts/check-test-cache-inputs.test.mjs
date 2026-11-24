// An undeclared read is invisible by construction: the cached pass looks like
// any other pass. These fixtures prove the check still sees each shape of read
// the repository's tests use.
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import test from "node:test";
import { covers, globToRegExp } from "./check-test-cache-inputs.mjs";

const script = resolve(import.meta.dirname, "check-test-cache-inputs.mjs");

// `views` depends on `ui`, which depends on `core`; `ee` and the apps are
// outside its hash.
function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), "cache-inputs-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const write = (rel, text) => {
    mkdirSync(dirname(join(root, rel)), { recursive: true });
    writeFileSync(join(root, rel), text);
  };
  const pkg = (dir, name, deps = {}, test = "vitest run") =>
    write(`${dir}/package.json`, JSON.stringify({ name, scripts: test ? { test } : {}, dependencies: deps }));
  const inputs = (dir, list) =>
    write(`${dir}/turbo.json`, `{\n  // comment\n  "extends": ["//"],\n  "tasks": { "test": { "inputs": ${JSON.stringify(["$TURBO_DEFAULT$", ...list])} } }\n}\n`);
  const ciPaths = (groups) => write(".github/ci-paths.json", JSON.stringify(groups));

  write("pnpm-workspace.yaml", "packages:\n  - apps/*\n  - packages/*\n");
  ciPaths({ frontend: ["apps/web/**", "packages/**"], docs: ["apps/docs/**"] });
  pkg("packages/core", "@multica/core");
  pkg("packages/ui", "@multica/ui", { "@multica/core": "workspace:*" });
  pkg("packages/views", "@multica/views", { "@multica/ui": "workspace:*" });
  pkg("packages/ee", "@multica/ee", { "@multica/views": "workspace:*" }, null);
  pkg("apps/web", "@multica/web", { "@multica/views": "workspace:*" });
  pkg("apps/docs", "@multica/docs");
  write("packages/core/tokens.css", ":root {}\n");
  write("packages/ee/locales/en.json", "{}\n");
  write("apps/web/app/globals.css", "body {}\n");
  write("apps/web/app/page.tsx", "export {};\n");
  write("docs/theming.md", "# Theming\n");
  write("README.md", "# Repo\n");

  const check = () => {
    const result = spawnSync(process.execPath, [script, root], { encoding: "utf8" });
    return { status: result.status, output: result.stdout + result.stderr };
  };
  const passes = (message) => {
    const { status, output } = check();
    assert.equal(status, 0, `${message}\n${output}`);
  };
  const failsNaming = (names, message) => {
    const { status, output } = check();
    assert.equal(status, 1, `${message}\n${output}`);
    for (const name of names) assert.ok(output.includes(name), `should name ${name}\n${output}`);
  };
  return { write, inputs, ciPaths, passes, failsNaming };
}

const viewsTest = "packages/views/common/probe.test.ts";

test("a relative read of a non-dependency fails until it is declared", (t) => {
  const { write, inputs, passes, failsNaming } = fixture(t);
  passes("a clean tree should pass");
  write(viewsTest, 'const css = readFileSync(resolve(__dirname, "../../../apps/web/app/globals.css"), "utf8");\n');
  failsNaming(["@multica/views", `${viewsTest}:1`, "apps/web/app/globals.css", "packages/views/turbo.json"], "an app stylesheet is outside views' hash");
  inputs("packages/views", ["$TURBO_ROOT$/apps/web/app/*.css"]);
  passes("a matching input should declare it");
});

test("reads of the package itself and of transitive dependencies need nothing", (t) => {
  const { write, passes } = fixture(t);
  write(viewsTest, [
    'readFileSync(new URL("../../core/tokens.css", import.meta.url));',
    'readFileSync(join(__dirname, "..", "package.json"));',
    'import x from "../../ui/package.json";',
  ].join("\n"));
  passes("core is a dependency of ui, which views depends on");
});

test("a root-relative path in a test that computes the repo root is a read", (t) => {
  const { write, inputs, ciPaths, passes, failsNaming } = fixture(t);
  ciPaths({ frontend: ["apps/web/**", "packages/**", "docs/theming.md", "README.md"] });
  write(viewsTest, [
    'const REPO = join(__dirname, "../../..");',
    "const read = (path) => readFileSync(join(REPO, path), \"utf8\");",
    'const docs = read("docs/theming.md");',
    'const ROOTS = ["packages/views", "apps/web/app"];',
    "const readme = read(`README.md`);",
  ].join("\n"));
  failsNaming(["docs/theming.md", "apps/web/app/", "README.md"], "helpers joined onto the root still read the repo");
  inputs("packages/views", [
    "$TURBO_ROOT$/docs/theming.md",
    "$TURBO_ROOT$/apps/web/app/**/*.tsx",
    "$TURBO_ROOT$/README.md",
  ]);
  passes("a glob under a scanned directory declares the scan");
});

test("template prefixes and process.cwd() are followed", (t) => {
  const { write, failsNaming } = fixture(t);
  write("apps/web/app/probe.test.ts", [
    'const root = resolve(process.cwd(), "../..");',
    "const locale = (l) => readFileSync(join(root, `packages/ee/locales/${l}.json`));",
  ].join("\n"));
  failsNaming(["@multica/web", "packages/ee/locales/"], "web does not depend on ee");
});

test("fixture strings, parent-dir steps and paths outside the repo are not reads", (t) => {
  const { write, passes } = fixture(t);
  write(viewsTest, [
    'const REPO = join(__dirname, "../../..");',
    'expect(parse("../../etc/passwd")).toBe(null);',
    'tagsIn(join(REPO, "fixture.tsx"), "apps/web/app/missing.tsx");',
    'resolve(REPO, "node_modules", ".bin");',
  ].join("\n"));
  passes("nothing here is a file the test reads from the repo");
});

test("a declared input must also start the CI job that runs the tests", (t) => {
  const { write, inputs, ciPaths, passes, failsNaming } = fixture(t);
  write(viewsTest, 'readFileSync(join(__dirname, "../../../docs/theming.md"));\n');
  inputs("packages/views", ["$TURBO_ROOT$/docs/theming.md"]);
  failsNaming(['"frontend" group', "docs/theming.md"], "a theming-only PR would skip the views tests");
  ciPaths({ frontend: ["apps/web/**", "packages/**", "docs/theming.md"], docs: ["apps/docs/**"] });
  passes("the frontend group now starts the job");

  write("apps/docs/lib/probe.test.ts", 'readFileSync(join(__dirname, "../../../README.md"));\n');
  inputs("apps/docs", ["$TURBO_ROOT$/README.md"]);
  failsNaming(['"docs" group', "README.md"], "docs tests run under the docs group");
});

test("globs follow turbo and ci-paths semantics", () => {
  assert.ok(globToRegExp("apps/desktop/src/**/*.{ts,tsx,css}").test("apps/desktop/src/renderer/src/globals.css"));
  assert.ok(globToRegExp("apps/desktop/src/**/*.{ts,tsx,css}").test("apps/desktop/src/a.ts"));
  assert.ok(!globToRegExp("apps/desktop/src/**/*.{ts,tsx,css}").test("apps/desktop/src/a.md"));
  assert.ok(globToRegExp("apps/*/package.json").test("apps/web/package.json"));
  assert.ok(!globToRegExp("apps/*/package.json").test("apps/web/node_modules/x/package.json"));
  assert.ok(globToRegExp("README.md").test("README.md") && !globToRegExp("README.md").test("READMExmd"));
  assert.ok(covers(["packages/*/package.json"], { path: "packages", kind: "dir" }));
  assert.ok(covers(["apps/web/**/*.css"], { path: "apps/web/app", kind: "dir" }));
  assert.ok(!covers(["apps/web/**/*.css"], { path: "apps/desktop", kind: "dir" }));
  assert.ok(!covers(["apps/web/**/*.css"], { path: "apps/web/app/page.tsx", kind: "file" }));
});

test("a single-level wildcard does not declare the directories below it", () => {
  const manifests = ["apps/*/package.json", "packages/*/package.json"];
  for (const path of ["apps/mobile/src", "apps/desktop/src/renderer", "packages/ee/project-folders/locales"]) {
    assert.ok(!covers(manifests, { path, kind: "dir" }), path);
  }
  assert.ok(covers(manifests, { path: "apps", kind: "dir" }), "the manifests are read from under apps/");
  assert.ok(covers(manifests, { path: "apps/web", kind: "dir" }), "apps/web/package.json is under apps/web");
  assert.ok(!covers(["README*.md"], { path: "apps/web", kind: "dir" }), "a root-level wildcard stays at the root");
  assert.ok(covers(["**/*.css"], { path: "apps/web/app", kind: "dir" }));
  assert.ok(covers(["packages/ee/project-folders/locales/*.json"], { path: "packages/ee/project-folders/locales", kind: "dir" }));
  assert.ok(!covers(["packages/ee/project-folders/locales/*.json"], { path: "packages/ee/project-folders/locales/en", kind: "dir" }));
});

test("dropping a scanned directory from views' inputs fails even with workspace manifests declared", (t) => {
  const { write, inputs, ciPaths, passes, failsNaming } = fixture(t);
  ciPaths({ frontend: ["apps/**", "packages/**"] });
  write(viewsTest, [
    'const REPO = join(__dirname, "../../..");',
    'const ROOTS = ["apps/web/app"];',
  ].join("\n"));
  inputs("packages/views", ["$TURBO_ROOT$/apps/*/package.json", "$TURBO_ROOT$/packages/*/package.json"]);
  failsNaming(["apps/web/app/"], "apps/*/package.json does not declare the app sources");
  inputs("packages/views", ["$TURBO_ROOT$/apps/*/package.json", "$TURBO_ROOT$/apps/web/app/**/*.tsx"]);
  passes("the scan is declared once its own glob is");
});
